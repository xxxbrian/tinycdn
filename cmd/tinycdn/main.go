package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"tinycdn/internal/admin"
	"tinycdn/internal/app"
	"tinycdn/internal/config"
	"tinycdn/internal/proxy"
	"tinycdn/internal/runtime"
	"tinycdn/internal/telemetry"
)

func main() {
	var (
		adminAddr   = flag.String("admin-addr", ":8787", "admin API and UI listen address")
		proxyAddr   = flag.String("proxy-addr", ":8080", "proxy data-plane listen address")
		configPath  = flag.String("config", "./data/config.yaml", "path to persisted YAML config")
		uiDir       = flag.String("ui-dir", "./web/dist", "path to built frontend assets")
		cacheDir    = flag.String("cache-dir", "./data/cache/badger", "path to TinyCDN badger cache directory")
		telemetryDB = flag.String("telemetry-db", "./data/telemetry/telemetry.db", "path to TinyCDN telemetry SQLite database")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	store := config.NewStore(*configPath)
	cfg, err := store.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(*cacheDir), 0o755); err != nil {
		logger.Error("failed to prepare cache directory", "error", err)
		os.Exit(1)
	}

	snapshot, err := runtime.Compile(cfg)
	if err != nil {
		logger.Error("failed to compile runtime", "error", err)
		os.Exit(1)
	}

	runtimeManager := runtime.NewManager(snapshot)
	service := app.NewService(store, runtimeManager, cfg)
	telemetryService, err := telemetry.NewService(*telemetryDB, logger)
	if err != nil {
		logger.Error("failed to initialize telemetry service", "error", err)
		os.Exit(1)
	}
	proxyRouter, err := proxy.NewRouter(service.RuntimeSnapshot, *cacheDir, telemetryService)
	if err != nil {
		logger.Error("failed to initialize proxy router", "error", err)
		_ = telemetryService.Close()
		os.Exit(1)
	}
	service.SetCacheController(proxyRouter)
	adminServer := &http.Server{
		Addr:    *adminAddr,
		Handler: admin.NewRouter(service, telemetryService, *uiDir),
	}
	defer func() {
		if err := telemetryService.Close(); err != nil {
			logger.Error("telemetry service shutdown failed", "error", err)
		}
		if err := proxyRouter.Close(); err != nil {
			logger.Error("proxy router shutdown failed", "error", err)
		}
	}()

	proxyServer := &http.Server{
		Addr:    *proxyAddr,
		Handler: proxyRouter,
	}

	go func() {
		logger.Info("admin server listening", "addr", *adminAddr)
		if err := adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("admin server failed", "error", err)
			os.Exit(1)
		}
	}()

	go func() {
		logger.Info("proxy server listening", "addr", *proxyAddr)
		if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("proxy server failed", "error", err)
			os.Exit(1)
		}
	}()

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-signalContext.Done()
	logger.Info("shutdown signal received")

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adminServer.Shutdown(shutdownContext); err != nil {
		logger.Error("admin server shutdown failed", "error", err)
	}
	if err := proxyServer.Shutdown(shutdownContext); err != nil {
		logger.Error("proxy server shutdown failed", "error", err)
	}
}
