// Command zenflash-llm starts the API gateway and its management interface.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	adminui "zenflash-llm/internal/admin"
	"zenflash-llm/internal/buildinfo"
	"zenflash-llm/internal/cline/app"
	"zenflash-llm/internal/config"
	"zenflash-llm/internal/gateway"
	"zenflash-llm/internal/telemetry"
)

// version remains the linker injection point used by release builds.
var version = "dev"

func main() {
	buildinfo.Version = version
	configPath := flag.String("config", "config.json", "path to config.json")
	listen := flag.String("listen", "", "override the configured API listen address")
	webListen := flag.String("web-listen", "", "override the configured WebUI listen address")
	showVersion := flag.Bool("version", false, "print version and exit")
	clineHost := flag.String("cline-host", "127.0.0.1", "embedded Cline proxy listen host")
	clinePort := flag.Int("cline-port", 3457, "embedded Cline proxy listen port; 0 disables it")
	flag.Parse()
	if *showVersion {
		fmt.Println("zenflash-llm", buildinfo.Version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *webListen != "" {
		cfg.WebUI.Listen = *webListen
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	level := new(slog.LevelVar)
	telemetry.SetLogLevel(level, cfg.Logging.Level)
	hub := telemetry.NewLogHub(cfg.Logging.RingSize)
	redactor := config.NewSecretRedactor()
	redactor.Replace(cfg)
	logger := telemetry.NewStructuredLogger(level, hub, redactor)
	monitor := telemetry.NewMonitor()
	manager, err := gateway.NewRuntimeManager(ctx, *configPath, cfg, logger, monitor, hub, redactor, level)
	if err != nil {
		logger.Error("failed to initialize runtime", "component", "runtime", "event", "runtime_initialization_failed", "error", err)
		os.Exit(1)
	}
	defer manager.Shutdown()

	servers := []*http.Server{}
	var apiHandler http.Handler = manager.Handler()
	if cfg.WebUI.Enabled {
		admin := adminui.New(manager, monitor, hub, logger)
		root := http.NewServeMux()
		root.Handle("/v1/", manager.Handler())
		root.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
			manager.Handler().ServeHTTP(w, r)
		})
		root.Handle("/", admin.Handler())
		apiHandler = root
		if cfg.WebUI.Listen != "" && cfg.WebUI.Listen != cfg.Listen {
			webServer := &http.Server{
				Addr: cfg.WebUI.Listen, Handler: admin.Handler(), ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 120 * time.Second,
			}
			servers = append(servers, webServer)
			go serveHTTP(cancel, logger, webServer, "webui")
		}
	}
	apiServer := &http.Server{
		Addr: cfg.Listen, Handler: apiHandler, ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 120 * time.Second,
	}
	servers = append(servers, apiServer)
	go serveHTTP(cancel, logger, apiServer, "api")

	if *clinePort != 0 {
		go func() {
			logger.Info("embedded Cline proxy listening", "component", "cline", "event", "server_started", "host", *clineHost, "port", *clinePort)
			if err := app.StartProxy(*clineHost, *clinePort); err != nil {
				logger.Error("embedded Cline proxy stopped unexpectedly", "component", "cline", "event", "server_failed", "error", err)
			}
		}()
	}

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	for _, server := range servers {
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "component", "server", "event", "shutdown_failed", "address", server.Addr, "error", err)
		}
	}
}

func serveHTTP(cancel context.CancelFunc, logger *slog.Logger, server *http.Server, component string) {
	logger.Info("server listening", "component", component, "event", "server_started", "address", server.Addr, "version", buildinfo.Version)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped unexpectedly", "component", component, "event", "server_failed", "address", server.Addr, "error", err)
		cancel()
	}
}
