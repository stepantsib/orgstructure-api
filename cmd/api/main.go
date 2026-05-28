package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"orgstructure/internal/app"
	"orgstructure/internal/config"
	"orgstructure/internal/repository"
	"orgstructure/internal/service"
	apphttp "orgstructure/internal/transport/http"
)

func main() {
	if code := run(); code != 0 {
		os.Exit(code)
	}
}

// run keeps main one-line and makes the exit code testable: any returned
// non-zero value propagates to os.Exit at the very top.
func run() int {
	cfg, err := config.Load()
	if err != nil {
		// We don't have a logger yet, but stderr is fine for boot failures.
		slog.New(slog.NewTextHandler(os.Stderr, nil)).
			Error("load config", "error", err)
		return 1
	}

	logger := app.NewLogger(cfg.Log, os.Stdout)
	slog.SetDefault(logger)

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- DB ---
	gormDB, err := app.OpenDB(rootCtx, cfg.DB, logger)
	if err != nil {
		logger.Error("open db", "error", err)
		return 1
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		logger.Error("unwrap sql db", "error", err)
		return 1
	}
	defer sqlDB.Close()

	if cfg.RunMigrationsOnStart {
		logger.Info("running migrations", "dir", cfg.MigrationsDir)
		if err := app.RunMigrations(sqlDB, cfg.MigrationsDir, logger); err != nil {
			logger.Error("migrations", "error", err)
			return 1
		}
		logger.Info("migrations applied")
	}

	// --- Wiring ---
	repos := repository.New(gormDB)
	svcs := service.New(repos.Departments, repos.Employees)
	handler := apphttp.NewRouter(svcs, logger)

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	// --- Serve & graceful shutdown ---
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTP.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case <-rootCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErr:
		logger.Error("server error", "error", err)
		return 1
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "error", err)
		// fall through — give the deferred close a chance
	}
	logger.Info("server stopped cleanly")
	return 0
}
