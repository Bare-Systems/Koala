package main

import (
	"context"
	"flag"
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
)

func main() {
	cfgPath := flag.String("config", "configs/koala.yaml", "path to config")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	registry := camera.NewRegistry(toCameras(cfg))
	prober := camera.Prober{Timeout: 2 * time.Second}
	for _, c := range registry.List() {
		if c.RTSPURL == "" {
			registry.SetStatus(c.ID, camera.StatusUnavailable)
			continue
		}
		result := prober.Probe(context.Background(), c)
		registry.SetStatus(c.ID, result.Status)
		if result.Error != "" {
			log.Printf("camera probe failed camera=%s err=%s", c.ID, result.Error)
		}
	}

	aggregator := state.NewAggregator(time.Duration(cfg.Runtime.FreshnessWindow) * time.Second)
	client := inference.NewHTTPClient(cfg.Worker.URL)
	svc := service.New(registry, aggregator, client, cfg.Runtime.QueueSize)
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
	defer cancel()
	svc.Start(ctx)
	if cfg.Runtime.EnableStreamWorkers {
		sampleEvery := time.Second / time.Duration(cfg.Runtime.StreamSampleFPS)
		captureTimeout := time.Duration(cfg.Runtime.StreamCaptureTimeoutS) * time.Second
		streamManager := ingest.NewManager(registry, svc, ingest.NewFFMpegSnapshotter(), sampleEvery, captureTimeout)
		streamManager.Start(ctx)
	}
	if poller != nil {
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
					log.Printf("worker health degraded")
				}
			}
		}
	}()

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mcp.NewServer(cfg.MCPToken, svc, updater, agent, poller, auditStore).Routes(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("koala orchestrator listening on %s", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, syscall.SIGINT, syscall.SIGTERM)
	<-sigch

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

func toCameras(cfg config.Config) []camera.Camera {
	cameras := make([]camera.Camera, 0, len(cfg.Cameras))
	for _, c := range cfg.Cameras {
		cameras = append(cameras, camera.Camera{
			ID:        c.ID,
			Name:      c.Name,
			RTSPURL:   c.RTSPURL,
			ZoneID:    c.ZoneID,
			FrontDoor: c.FrontDoor,
			Status:    camera.StatusUnknown,
		})
	}
	return cameras
}
