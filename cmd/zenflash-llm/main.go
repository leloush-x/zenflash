// Command zenflash-llm starts the API gateway and its management interface.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	adminui "zenflash-llm/internal/admin"
	"zenflash-llm/internal/antigravity"
	"zenflash-llm/internal/buildinfo"
	"zenflash-llm/internal/cline/app"
	"zenflash-llm/internal/codex"
	"zenflash-llm/internal/config"
	"zenflash-llm/internal/gateway"
	"zenflash-llm/internal/store"
	"zenflash-llm/internal/telemetry"
)

// codexLogin runs the ChatGPT PKCE flow and saves credentials beside the config.
func codexLogin(configPath string, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
	defer cancel()
	return codex.AuthenticateDevice(ctx, nil, codex.DefaultConfig(), codex.AuthPath(configPath), out)
}

func antigravityLogin(configPath string, port int, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
	defer cancel()
	return antigravity.AuthenticateDevice(ctx, nil, antigravity.AuthPath(configPath), port, out)
}

// version remains the linker injection point used by release builds.
var version = "dev"

func main() {
	buildinfo.Version = version
	if len(os.Args) > 1 && os.Args[1] == "login" {
		fs := flag.NewFlagSet("login", flag.ExitOnError)
		configPath := fs.String("config", "config.json", "path to config.json")
		port := fs.Int("port", 51121, "loopback port for Antigravity OAuth callback")
		_ = fs.Parse(os.Args[2:])
		target := "codex"
		if fs.NArg() > 0 {
			target = fs.Arg(0)
		}
		switch target {
		case "codex":
			if err := codexLogin(*configPath, os.Stdout); err != nil {
				slog.Error("codex sign-in failed", "error", err)
				os.Exit(1)
			}
		case "antigravity":
			if err := antigravityLogin(*configPath, *port, os.Stdout); err != nil {
				slog.Error("antigravity sign-in failed", "error", err)
				os.Exit(1)
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown login target %q (want codex or antigravity)\n", target)
			os.Exit(2)
		}
		return
	}
	configPath := flag.String("config", "config.json", "path to config.json")
	listen := flag.String("listen", "", "override the configured API listen address")
	webListen := flag.String("web-listen", "", "override the configured WebUI listen address")
	showVersion := flag.Bool("version", false, "print version and exit")
	clineHost := flag.String("cline-host", config.DefaultClineHost, "embedded Cline proxy listen host")
	clinePort := flag.Int("cline-port", config.DefaultClinePort, "embedded Cline proxy listen port; 0 disables it")
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
	if v := config.LoadEnvOnce().WebUIUsername; v != "" {
		cfg.WebUI.Username = v
	}
	if v := config.LoadEnvOnce().WebUIPassword; v != "" {
		cfg.WebUI.Password = v
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	codexService, err := codex.New(*configPath + ".codex.enc")
	if err != nil {
		slog.Error("failed to initialize Codex account store", "error", err)
		os.Exit(1)
	}
	codexService.Start(ctx, 10*time.Minute)
	level := new(slog.LevelVar)
	telemetry.SetLogLevel(level, cfg.Logging.Level)
	hub := telemetry.NewLogHub(cfg.Logging.RingSize)
	redactor := config.NewSecretRedactor()
	redactor.Replace(cfg)
	logger := telemetry.NewStructuredLogger(level, hub, redactor)
	monitor := telemetry.NewMonitor()
	env := config.LoadEnvOnce()
	if err := env.Validate(); err != nil {
		logger.Error("invalid environment", "error", err)
		slog.Error("invalid environment", "error", err)
		os.Exit(1)
	}
	var durable *store.Store
	if env.DatabaseURL != "" {
		ds, err := store.Open(ctx, env.DatabaseURL, logger)
		if err != nil {
			logger.Error("postgres unavailable at startup; continuing with file state", "error", err)
		} else if ds != nil {
			durable = ds
			defer durable.Close()
			rawCfg, _ := os.ReadFile(*configPath)
			_ = durable.SeedKeys(ctx, cfg.ServerKeys)
			if _, ierr := durable.ImportOnce(ctx, *configPath, rawCfg); ierr != nil {
				logger.Warn("postgres import skipped", "error", ierr)
			}
			durable.SyncFiles(ctx, *configPath)
			durable.StartFileSync(ctx, *configPath)
		}
	}
	clineURL := ""
	if *clinePort != 0 {
		clineURL = fmt.Sprintf("http://%s:%d", *clineHost, *clinePort)
		go func() {
			logger.Info("embedded Cline proxy listening", "component", "cline", "event", "server_started", "host", *clineHost, "port", *clinePort)
			if err := app.StartProxy(*clineHost, *clinePort); err != nil {
				logger.Error("embedded Cline proxy stopped unexpectedly", "component", "cline", "event", "server_failed", "error", err)
			}
		}()
		// The first catalog refresh races the Cline proxy startup; give it a
		// brief head start so the go tier actually enumerates on cold boots.
		waitForHTTP(ctx, clineURL+"/health", config.ClineReadyTimeout, logger)
	}

	manager, err := gateway.NewRuntimeManager(ctx, *configPath, cfg, logger, monitor, hub, redactor, level)
	if err == nil && durable != nil {
		manager.SetStore(durable)
	}
	if err != nil {
		logger.Error("failed to initialize runtime", "component", "runtime", "event", "runtime_initialization_failed", "error", err)
		os.Exit(1)
	}
	defer manager.Shutdown()

	servers := []*http.Server{}
	var apiHandler http.Handler = manager.Handler()
	if cfg.WebUI.Enabled {
		admin := adminui.New(manager, monitor, hub, logger, *configPath+".sessions.json", clineURL, codexService)
		admin.SetStore(durable)
		root := http.NewServeMux()
		root.Handle("/v1/", manager.Handler())
		root.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
			manager.Handler().ServeHTTP(w, r)
		})
		root.Handle("/", admin.Handler())
		apiHandler = root
		if cfg.WebUI.Listen != "" && cfg.WebUI.Listen != cfg.Listen {
			webServer := &http.Server{
				Addr: cfg.WebUI.Listen, Handler: admin.Handler(), ReadHeaderTimeout: config.ReadHeaderTimeout, IdleTimeout: config.IdleTimeout,
			}
			servers = append(servers, webServer)
			go serveHTTP(cancel, logger, webServer, "webui")
		}
	}
	apiHandler = codexService.WrapAPI(apiHandler, func() []string { return manager.Config().ServerKeys })
	apiServer := &http.Server{
		Addr: cfg.Listen, Handler: apiHandler, ReadHeaderTimeout: config.ReadHeaderTimeout, IdleTimeout: config.IdleTimeout,
	}
	servers = append(servers, apiServer)
	go serveHTTP(cancel, logger, apiServer, "api")

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
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

// waitForHTTP polls url until it answers with any HTTP status or the timeout
// elapses. It is only used to order startup work, so a slow or absent target
// must not block boot: the gateway still comes up after timeout.
func waitForHTTP(ctx context.Context, url string, timeout time.Duration, logger *slog.Logger) {
	client := &http.Client{Timeout: config.HTTPClientTimeout}
	deadline := time.Now().Add(timeout)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			if resp, err := client.Do(req); err == nil {
				_ = resp.Body.Close()
				logger.Info("upstream ready", "component", "cline", "event", "health_ready", "url", url)
				return
			}
		}
		if time.Now().After(deadline) {
			logger.Warn("upstream not ready before timeout; the go tier will retry in the next refresh window", "component", "cline", "event", "health_wait_timeout", "url", url)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(config.ClineReadyInterval):
		}
	}
}
