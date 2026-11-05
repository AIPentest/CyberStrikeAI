package main

import (
	"context"
	"cyberstrike-ai/internal/app"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/logger"
	"cyberstrike-ai/internal/processguard"
	"cyberstrike-ai/internal/security"
	"cyberstrike-ai/internal/termout"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
	"golang.org/x/term"
)

func main() {
	var configPath = flag.String("config", "config.yaml", "Path to the configuration file")
	var httpsBootstrap = flag.Bool("https", false, "Enable HTTPS for the main site; uses an in-memory self-signed certificate when no cert/key is configured")
	var httpBootstrap = flag.Bool("http", false, "Force plain HTTP for the main site, overriding TLS settings in the configuration file")
	var resetAdminPassword = flag.Bool("reset-admin-password", false, "Interactively reset the built-in admin password and exit")
	checkIsolation := flag.Bool("check-process-isolation", false, "Probe task containment and cleanup, then exit without starting services")
	checkUpdate := flag.Bool("check-update", false, "Report whether this installation's own repository has anything newer, then exit")
	applyUpdate := flag.Bool("update", false, "Fast-forward this installation to its own remote, rebuild the binary, and exit")
	updateRollback := flag.Bool("update-rollback", false, "Undo the last --update: back to the commit and binary kept before it")
	flag.Parse()

	if handled, code := updateCommandIfNeeded(strings.TrimSpace(*configPath), *checkUpdate, *applyUpdate, *updateRollback); handled {
		os.Exit(code)
	}

	// environment variable compatibility (for systemd/docker scenarios that pass no arguments)
	if *httpsBootstrap && *httpBootstrap {
		fmt.Fprintln(os.Stderr, "--http and --https cannot be used together")
		os.Exit(2)
	}
	if !*httpsBootstrap && !*httpBootstrap {
		v := strings.TrimSpace(os.Getenv("CYBERSTRIKE_HTTPS"))
		if v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") {
			*httpsBootstrap = true
		}
	}

	// load config
	cp := strings.TrimSpace(*configPath)
	if cp == "" {
		cp = "config.yaml"
	}
	if strings.HasPrefix(cp, "-") {
		fmt.Fprintf(os.Stderr, "Invalid -config path %q.\nIf HTTPS is also needed, use: ./cyberstrike-ai --https -config config.yaml (-config must be followed by a yaml file path).\n", cp)
		os.Exit(2)
	}
	localConfig, err := config.EnsureLocalConfig(cp)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		return
	}

	cfg, err := config.Load(cp)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		return
	}
	if localConfig.Created {
		termout.PrintConfigCreated()
	}

	if *checkIsolation {
		if err := configureProcessIsolation(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		checkCtx, cancelCheck := context.WithTimeout(context.Background(), 15*time.Second)
		backend, checkErr := processguard.Check(checkCtx)
		cancelCheck()
		if checkErr != nil {
			fmt.Fprintln(os.Stderr, checkErr)
			os.Exit(1)
		}
		fmt.Printf("{\"checked\":true,\"backend\":%q}\n", backend)
		return
	}

	if *resetAdminPassword {
		if err := runResetAdminPassword(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to reset admin password: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *httpBootstrap {
		config.ApplyPlainHTTPBootstrap(cfg)
	} else if *httpsBootstrap {
		config.ApplyDevHTTPSBootstrap(cfg)
	}

	port := cfg.Server.Port
	if port <= 0 {
		port = 8080
	}
	scheme := "http"
	if config.MainWebUIUsesHTTPS(&cfg.Server) {
		scheme = "https"
	}
	termout.PrintStartupWebUI(termout.StartupWebUIOptions{
		Scheme:       scheme,
		Host:         cfg.Server.Host,
		Port:         port,
		SelfSigned:   scheme == "https" && cfg.Server.TLSAutoSelfSign,
		HTTPRedirect: scheme == "https" && config.ServerHTTPRedirectEnabled(&cfg.Server),
	})

	// When MCP is enabled and auth_header_value is empty, generate a random key and write it back to the config
	if err := config.EnsureMCPAuth(cp, cfg); err != nil {
		fmt.Printf("Failed to configure MCP authentication: %v\n", err)
		return
	}
	if cfg.MCP.Enabled {
		config.PrintMCPConfigJSON(cfg.MCP)
	}

	// initialize the logger
	log := logger.New(cfg.Log.Level, cfg.Log.Output, logger.DiagnosticOptions{
		Dir:           cfg.Log.DiagnosticDir,
		Disabled:      cfg.Log.DiagnosticDisabled,
		RetentionDays: cfg.Log.DiagnosticRetentionDays,
	})
	defer log.Sync()

	if err := configureProcessIsolation(cfg); err != nil {
		log.Fatal("process isolation init failed", zap.Error(err))
	}

	probeCtx, probeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	backend, probeErr := processguard.Check(probeCtx)
	probeCancel()
	if probeErr != nil {
		log.Fatal("process isolation startup check failed", zap.Error(probeErr))
	}
	log.Info("task process isolation ready", zap.String("backend", backend))

	// create a cancellable root context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// listen for system signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// create the application
	application, err := app.New(cfg, log, cp)
	if err != nil {
		log.Fatal("application init failed", zap.Error(err))
	}

	// listen for signals in the background
	go func() {
		sig := <-sigCh
		log.Info("received system signal, starting graceful shutdown: " + sig.String())
		application.Shutdown()
		cancel()
	}()

	// start the server (passing ctx to support graceful shutdown)
	if err := application.RunWithContext(ctx); err != nil {
		// a shutdown caused by context cancellation is not treated as an error
		if ctx.Err() != nil {
			log.Info("server shut down gracefully")
		} else {
			log.Fatal("server start failed", zap.Error(err))
		}
	}
}

func runResetAdminPassword(cfg *config.Config) error {
	dbPath := strings.TrimSpace(cfg.Database.Path)
	if dbPath == "" {
		dbPath = "data/conversations.db"
	}
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("database does not exist: %s; start the service once to initialize it first", dbPath)
		}
		return err
	}

	fmt.Println("Reset built-in admin password")
	fmt.Println()

	password, err := readHiddenPassword("New admin password: ")
	if err != nil {
		return err
	}
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		return fmt.Errorf("new password must be at least 8 characters")
	}
	confirm, err := readHiddenPassword("Confirm new password: ")
	if err != nil {
		return err
	}
	if password != strings.TrimSpace(confirm) {
		return fmt.Errorf("passwords do not match")
	}

	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}

	db, err := database.NewDB(dbPath, zap.NewNop())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	admin, err := db.GetRBACUserByUsername("admin")
	if err != nil {
		return fmt.Errorf("built-in admin account was not found; start the service once to initialize it first: %w", err)
	}
	if !admin.IsBuiltin {
		return fmt.Errorf("admin account is not built in; refusing to reset it")
	}
	if err := db.UpdateRBACAdminPassword(hash); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Admin password has been reset.")
	fmt.Println("If the service is running, existing login sessions remain valid until the service restarts or the sessions expire.")
	return nil
}

func readHiddenPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(password), nil
}

func configureProcessIsolation(cfg *config.Config) error {
	isolation := cfg.Security.ProcessIsolation
	return processguard.Configure(processguard.Options{Mode: isolation.Mode, CgroupRoot: isolation.CgroupRoot, MaxProcesses: isolation.MaxProcesses, MemoryMaxBytes: isolation.MemoryMaxBytes, CPUQuotaMicros: isolation.CPUQuotaMicros})
}
