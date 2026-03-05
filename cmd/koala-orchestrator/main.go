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

	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/config"
	"github.com/barelabs/koala/internal/inference"
	"github.com/barelabs/koala/internal/mcp"
	"github.com/barelabs/koala/internal/service"
	"github.com/barelabs/koala/internal/state"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

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
		Handler:      mcp.NewServer(cfg.MCPToken, svc).Routes(),
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
