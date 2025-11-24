package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ocenb/pr-assignment/internal/config"
	"github.com/ocenb/pr-assignment/internal/handler"
	"github.com/ocenb/pr-assignment/internal/logattr"
	"github.com/ocenb/pr-assignment/internal/logger"
	prrepo "github.com/ocenb/pr-assignment/internal/repos/pr"
	statsrepo "github.com/ocenb/pr-assignment/internal/repos/stats"
	teamrepo "github.com/ocenb/pr-assignment/internal/repos/team"
	userrepo "github.com/ocenb/pr-assignment/internal/repos/user"
	"github.com/ocenb/pr-assignment/internal/server"
	prsvc "github.com/ocenb/pr-assignment/internal/services/pr"
	statssvc "github.com/ocenb/pr-assignment/internal/services/stats"
	teamsvc "github.com/ocenb/pr-assignment/internal/services/team"
	usersvc "github.com/ocenb/pr-assignment/internal/services/user"
	"github.com/ocenb/pr-assignment/internal/storage/postgres"
	"github.com/ocenb/pr-assignment/internal/storage/transactor"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg := config.MustLoad()
	log := logger.New(cfg)

	log.Info("connecting to database",
		slog.String("host", cfg.Postgres.Host),
		slog.String("port", cfg.Postgres.Port),
		slog.String("database", cfg.Postgres.Name),
	)
	connectCtx, connectCancel := context.WithTimeout(context.Background(), cfg.DBConnectTimeout)
	defer connectCancel()
	pool, err := postgres.NewPool(connectCtx, cfg)
	if err != nil {
		log.Error("failed to connect to postgres", logattr.Err(err))
		return 1
	}
	defer pool.Close()

	tm := transactor.New(pool)

	prRepo := prrepo.New(tm)
	teamRepo := teamrepo.New(tm)
	userRepo := userrepo.New(tm)
	statsRepo := statsrepo.New(tm)

	prService := prsvc.New(log, prRepo, tm)
	teamService := teamsvc.New(log, teamRepo, prRepo, tm)
	userService := usersvc.New(log, userRepo, prRepo, tm)
	statsService := statssvc.New(log, statsRepo)

	h := handler.New(prService, teamService, userService, statsService)

	httpServer, err := server.New(log, cfg, h)
	if err != nil {
		log.Error("failed to create server", logattr.Err(err))
		return 1
	}

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- httpServer.Start()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-serverErrors:
		log.Error("server failed to start or crashed", logattr.Err(err))
		return 1
	case sig := <-stop:
		log.Info("received shutdown signal", slog.String("signal", sig.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Stop(ctx); err != nil {
		log.Error("HTTP server shutdown error", logattr.Err(err))
		return 1
	}

	log.Info("server gracefully stopped")
	return 0
}
