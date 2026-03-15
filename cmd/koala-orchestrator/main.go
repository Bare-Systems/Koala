package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/barelabs/koala/internal/audit"
	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/config"
	"github.com/barelabs/koala/internal/inference"
	"github.com/barelabs/koala/internal/ingest"
	"github.com/barelabs/koala/internal/mcp"
	"github.com/barelabs/koala/internal/service"
	"github.com/barelabs/koala/internal/state"
	"github.com/barelabs/koala/internal/update"
	"github.com/barelabs/koala/internal/zone"
)

func main() {
	cfgPath := flag.String("config", "configs/koala.yaml", "path to config")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	registry := camera.NewRegistry(toCameras(cfg))

	// Prime the registry with last-known probe results before this session's
	// live probes run. Any camera that gets a fresh probe result will overwrite
	// the cached value; cameras that fail to probe this session keep their
	// cached status so agents see a useful state rather than StatusUnknown.
	var capCache *camera.CapabilityCache
	if cfg.Runtime.CapabilityCachePath != "" {
		var cacheErr error
		capCache, cacheErr = camera.LoadCapabilityCache(cfg.Runtime.CapabilityCachePath)
		if cacheErr != nil {
			log.Printf("camera capability cache load failed (continuing without cache): %v", cacheErr)
		} else {
			capCache.Warm(registry)
			log.Printf("startup: capability cache loaded path=%s", cfg.Runtime.CapabilityCachePath)
		}
	}

	prober := camera.Prober{Timeout: 2 * time.Second}
	for _, c := range registry.List() {
		result := prober.Probe(context.Background(), c)
		registry.SetCapability(c.ID, result.Capability)
		registry.SetStatus(c.ID, result.Status)
		if result.DiscoveredRTSPURL != "" {
			registry.SetRTSPURL(c.ID, result.DiscoveredRTSPURL)
		}
	}
	logDiscoveryReport(registry)

	// Persist fresh probe results for next startup.
	if capCache != nil {
		if err := capCache.Snapshot(registry); err != nil {
			log.Printf("camera capability cache snapshot failed: %v", err)
		}
	}

	freshnessWindow := time.Duration(cfg.Runtime.FreshnessWindow) * time.Second
	if cfg.Privacy.MetadataRetentionSeconds > 0 {
		freshnessWindow = time.Duration(cfg.Privacy.MetadataRetentionSeconds) * time.Second
	}
	aggregator := state.NewAggregator(freshnessWindow, cfg.Runtime.MinDetections)

	var inferenceClient inference.Client
	switch cfg.Worker.Protocol {
	case "grpc":
		grpcClient, grpcErr := inference.NewGRPCClient(cfg.Worker.GRPCAddr, 5, 15*time.Second)
		if grpcErr != nil {
			log.Fatalf("init grpc inference client addr=%s: %v", cfg.Worker.GRPCAddr, grpcErr)
		}
		defer grpcClient.Close()
		inferenceClient = grpcClient
		log.Printf("startup: inference transport=grpc addr=%s", cfg.Worker.GRPCAddr)
	default: // "http"
		inferenceClient = inference.NewHTTPClient(cfg.Worker.URL)
		log.Printf("startup: inference transport=http url=%s", cfg.Worker.URL)
	}
	svc := service.New(registry, aggregator, inferenceClient, cfg.Runtime.QueueSize)
	zoneFilter := service.NewZoneFilter(toZoneFilters(cfg))
	// Wire per-camera confidence thresholds (priority over zone/global).
	cameraThresholds := make(map[string]float64)
	for _, c := range cfg.Cameras {
		if c.ConfidenceThreshold > 0 {
			cameraThresholds[c.ID] = c.ConfidenceThreshold
		}
	}
	if len(cameraThresholds) > 0 {
		zoneFilter = zoneFilter.WithCameraThresholds(cameraThresholds)
	}
	if cfg.Runtime.ConfidenceThreshold > 0 {
		zoneFilter = zoneFilter.WithGlobalThreshold(cfg.Runtime.ConfidenceThreshold)
	}
	svc.Filter = zoneFilter
	svc.FrameBufferEnabled = cfg.Privacy.FrameBufferEnabled
	if !cfg.Privacy.FrameBufferEnabled {
		log.Printf("startup: privacy=metadata-only frame_buffer_enabled=false")
	}
	auditStore, err := audit.NewSQLiteStore(cfg.Update.AuditDBPath)
	if err != nil {
		log.Fatalf("init audit sqlite: %v", err)
	}
	defer func() {
		if cerr := auditStore.Close(); cerr != nil {
			log.Printf("close audit store: %v", cerr)
		}
	}()
	var updater *update.Manager
	var agent update.Agent
	var poller *update.Poller
	var ingestManager *ingest.Manager
	if cfg.Update.Enabled {
		executor := update.NewHTTPExecutor(cfg.MCPToken, 3*time.Second)
		updater = update.NewManager(cfg.Service.Version, "0.1.0-dev", cfg.Service.DeviceID, cfg.Service.Address, cfg.Service.Version, executor)
		var verifier update.Verifier
		if cfg.Update.RotationOnlyMode || cfg.Update.PublicKeyBase64 == "" {
			rotatingVerifier, verr := update.NewRotatingVerifier(cfg.Update.ActiveKeyID, cfg.Update.PublicKeys, cfg.Update.PreviousKeys)
			if verr != nil {
				log.Fatalf("invalid rotating update key configuration: %v", verr)
			}
			rotatingVerifier.SetUnknownKeyAlertHook(func(kind string, keyID string) {
				log.Printf("SECURITY ALERT: unknown update %s key_id=%s", kind, keyID)
				_ = auditStore.Record(context.Background(), audit.Event{
					Category:  "security",
					EventType: "unknown_key_id",
					Severity:  "high",
					Message:   "unknown update signing key_id observed",
					KeyID:     keyID,
					Payload: map[string]any{
						"kind": kind,
					},
					CreatedAt: time.Now().UTC().Format(time.RFC3339),
				})
			})
			verifier = rotatingVerifier
		} else {
			log.Printf("warning: update.public_key_base64 is deprecated; migrate to rotation keyring and enable update.rotation_only_mode")
			edVerifier, verr := update.NewEd25519VerifierFromBase64(cfg.Update.PublicKeyBase64)
			if verr != nil {
				log.Fatalf("invalid update public key: %v", verr)
			}
			verifier = edVerifier
		}
		encryptionKey, kerr := update.ParseAES256KeyBase64(cfg.Update.EncryptionKeyBase64)
		if kerr != nil {
			log.Fatalf("invalid update encryption key: %v", kerr)
		}
		agent = update.NewFileAgent(cfg.Service.Version, cfg.Update.StagingDir, cfg.Update.ActiveDir, update.NewHTTPDownloader(10*time.Second), verifier, encryptionKey)
		if cfg.Update.PollEnabled {
			poller = update.NewPoller(
				updater,
				update.NewHTTPDownloader(10*time.Second),
				cfg.Service.DeviceID,
				cfg.Update.ManifestURL,
				time.Duration(cfg.Update.PollIntervalSeconds)*time.Second,
				time.Duration(cfg.Update.PollJitterSeconds)*time.Second,
				func(e update.PollEvent) {
					_ = auditStore.Record(context.Background(), audit.Event{
						Category:  e.Category,
						EventType: e.EventType,
						Severity:  e.Severity,
						Message:   e.Message,
						DeviceID:  e.DeviceID,
						Payload:   e.Payload,
						CreatedAt: time.Now().UTC().Format(time.RFC3339),
					})
				},
			)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	logStartupReport(cfg, registry)
	log.Printf("startup: starting inference service worker pool cameras=%d queue=%d",
		len(registry.List()), cfg.Runtime.QueueSize)
	svc.Start(ctx)

	if cfg.Runtime.EnableStreamWorkers {
		log.Printf("startup: starting stream ingest workers fps=%d", cfg.Runtime.StreamSampleFPS)
		sampleEvery := time.Second / time.Duration(cfg.Runtime.StreamSampleFPS)
		captureTimeout := time.Duration(cfg.Runtime.StreamCaptureTimeoutS) * time.Second
		snapshotter := ingest.NewPersistentFFMpegSnapshotter(cfg.Runtime.StreamSampleFPS)
		ingestManager = ingest.NewManager(registry, svc, snapshotter, sampleEvery, captureTimeout)
		ingestManager.Start(ctx)
	}
	if poller != nil {
		log.Printf("startup: starting update poller interval=%ds", cfg.Update.PollIntervalSeconds)
		poller.Start(ctx)
	}

	go func() {
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				healthy := svc.WorkerHealthy(ctx)
				if !healthy {
					log.Printf("service=worker status=degraded")
				}
			}
		}
	}()

	mcpSrv := mcp.NewServer(cfg.MCPToken, svc, updater, agent, poller, ingestManager, auditStore).
		WithConfigSnapshot(buildConfigSnapshot(cfg))
	if len(cfg.AllowedIPs) > 0 {
		allowlist, alErr := mcp.NewIPAllowlist(cfg.AllowedIPs)
		if alErr != nil {
			log.Fatalf("invalid allowed_ips config: %v", alErr)
		}
		mcpSrv = mcpSrv.WithAllowlist(allowlist)
		log.Printf("startup: IP allowlist enabled entries=%d", len(cfg.AllowedIPs))
	}
	mcpServer := mcpSrv
	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mcpServer.Routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("startup: koala orchestrator ready addr=%s", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigch
	log.Printf("shutdown: received signal=%s — draining in-flight work", sig)

	// Stop accepting new requests; allow up to 15s for in-flight HTTP to finish.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: HTTP server shutdown error: %v", err)
	}

	// Cancel background workers then drain the frame processing queue.
	cancel()
	svc.Drain()
	log.Printf("shutdown: complete")
}

// logDiscoveryReport emits a structured startup summary of camera probe results.
func logDiscoveryReport(reg *camera.Registry) {
	cameras := reg.List()
	log.Printf("startup: discovery report cameras=%d", len(cameras))
	for _, c := range cameras {
		source := c.Capability.SelectedSource
		if source == "" {
			source = "none"
		}
		if c.Capability.LastError != "" {
			log.Printf("startup: camera id=%s status=%s source=%s error=%q", c.ID, c.Status, source, c.Capability.LastError)
		} else {
			log.Printf("startup: camera id=%s status=%s source=%s", c.ID, c.Status, source)
		}
	}
}

// logStartupReport emits a single consolidated boot-time summary so operators
// can verify the effective configuration at a glance. It is printed after all
// wiring (zone filters, privacy, update, etc.) is complete but before any
// background goroutines start.
func logStartupReport(cfg config.Config, reg *camera.Registry) {
	cams := reg.List()
	frontDoor := 0
	for _, c := range cams {
		if c.FrontDoor {
			frontDoor++
		}
	}

	transport := cfg.Worker.Protocol
	if transport == "" {
		transport = "http"
	}

	privacyMode := "frame-buffer-enabled"
	if !cfg.Privacy.FrameBufferEnabled {
		privacyMode = "metadata-only"
	}

	updateMode := "disabled"
	if cfg.Update.Enabled {
		updateMode = "enabled"
		if cfg.Update.PollEnabled {
			updateMode = fmt.Sprintf("enabled poll=%ds", cfg.Update.PollIntervalSeconds)
		}
	}

	streamMode := "disabled"
	if cfg.Runtime.EnableStreamWorkers {
		streamMode = fmt.Sprintf("enabled fps=%d", cfg.Runtime.StreamSampleFPS)
	}

	allowlistMode := "disabled"
	if len(cfg.AllowedIPs) > 0 {
		allowlistMode = fmt.Sprintf("enabled entries=%d", len(cfg.AllowedIPs))
	}

	log.Printf("startup: ── Koala Orchestrator ──────────────────────────────────────")
	log.Printf("startup: version=%s device_id=%s", cfg.Service.Version, cfg.Service.DeviceID)
	log.Printf("startup: listen=%s service_addr=%s", cfg.ListenAddr, cfg.Service.Address)
	log.Printf("startup: inference transport=%s", transport)
	log.Printf("startup: privacy=%s", privacyMode)
	log.Printf("startup: runtime queue=%d freshness=%ds min_detections=%d confidence=%.2f",
		cfg.Runtime.QueueSize, cfg.Runtime.FreshnessWindow,
		cfg.Runtime.MinDetections, cfg.Runtime.ConfidenceThreshold)
	log.Printf("startup: cameras=%d front_door=%d zones=%d",
		len(cams), frontDoor, len(cfg.Zones))
	log.Printf("startup: stream_workers=%s update=%s ip_allowlist=%s",
		streamMode, updateMode, allowlistMode)
	log.Printf("startup: ─────────────────────────────────────────────────────────────")
}

// buildConfigSnapshot returns a sanitised view of cfg for the /admin/config
// endpoint. Sensitive fields (tokens, keys, passwords) are redacted.
func buildConfigSnapshot(cfg config.Config) map[string]any {
	cameras := make([]map[string]any, 0, len(cfg.Cameras))
	for _, c := range cfg.Cameras {
		cameras = append(cameras, map[string]any{
			"id":         c.ID,
			"name":       c.Name,
			"zone_id":    c.ZoneID,
			"front_door": c.FrontDoor,
			"rtsp_set":   c.RTSPURL != "",
			"onvif_set":  c.ONVIFURL != "",
			"max_fps":    c.MaxFPS,
		})
	}
	zones := make([]map[string]any, 0, len(cfg.Zones))
	for _, z := range cfg.Zones {
		zones = append(zones, map[string]any{
			"id":               z.ID,
			"polygon_vertices": len(z.Polygon),
			"min_bbox_overlap": z.MinBBoxOverlap,
		})
	}
	return map[string]any{
		"listen_addr": cfg.ListenAddr,
		"service": map[string]any{
			"version":   cfg.Service.Version,
			"device_id": cfg.Service.DeviceID,
			"address":   cfg.Service.Address,
		},
		"runtime": map[string]any{
			"freshness_window_s":      cfg.Runtime.FreshnessWindow,
			"queue_size":              cfg.Runtime.QueueSize,
			"min_detections":          cfg.Runtime.MinDetections,
			"confidence_threshold":    cfg.Runtime.ConfidenceThreshold,
			"enable_stream_workers":   cfg.Runtime.EnableStreamWorkers,
			"stream_sample_fps":       cfg.Runtime.StreamSampleFPS,
		},
		"worker": map[string]any{
			"protocol":     cfg.Worker.Protocol,
			"url_set":      cfg.Worker.URL != "",
			"grpc_addr_set": cfg.Worker.GRPCAddr != "",
		},
		"update": map[string]any{
			"enabled":         cfg.Update.Enabled,
			"poll_enabled":    cfg.Update.PollEnabled,
			"poll_interval_s": cfg.Update.PollIntervalSeconds,
			// sensitive fields (keys, tokens) are intentionally omitted
		},
		"cameras": cameras,
		"zones":   zones,
		"mcp_token_set": cfg.MCPToken != "",
	}
}

func toZoneFilters(cfg config.Config) map[string]service.ZonePolygonConfig {
	out := make(map[string]service.ZonePolygonConfig, len(cfg.Zones))
	for _, z := range cfg.Zones {
		if len(z.Polygon) < 3 {
			continue
		}
		poly := make(zone.Polygon, len(z.Polygon))
		for i, pt := range z.Polygon {
			poly[i] = zone.Point{X: pt[0], Y: pt[1]}
		}
		out[z.ID] = service.ZonePolygonConfig{
			Polygon:             poly,
			MinOverlap:          z.MinBBoxOverlap,
			ConfidenceThreshold: z.ConfidenceThreshold,
		}
	}
	return out
}

func toCameras(cfg config.Config) []camera.Camera {
	cameras := make([]camera.Camera, 0, len(cfg.Cameras))
	for _, c := range cfg.Cameras {
		cameras = append(cameras, camera.Camera{
			ID:                  c.ID,
			Name:                c.Name,
			RTSPURL:             c.RTSPURL,
			ONVIFURL:            c.ONVIFURL,
			ZoneID:              c.ZoneID,
			FrontDoor:           c.FrontDoor,
			Status:              camera.StatusUnknown,
			MaxFPS:              c.MaxFPS,
			ConfidenceThreshold: c.ConfidenceThreshold,
		})
	}
	return cameras
}
