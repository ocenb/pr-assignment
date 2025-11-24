package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/ocenb/pr-assignment/internal/api"
	"github.com/ocenb/pr-assignment/internal/config"
	"github.com/ocenb/pr-assignment/internal/logattr"
	"github.com/ocenb/pr-assignment/internal/middlewares"
	"github.com/ocenb/pr-assignment/openapi"
	httpSwagger "github.com/swaggo/http-swagger"
)

type HttpServer struct {
	log        *slog.Logger
	cfg        *config.Config
	httpServer *http.Server
	handler    http.Handler
}

func New(log *slog.Logger, cfg *config.Config, handler api.Handler) (*HttpServer, error) {
	server, err := api.NewServer(handler)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		if _, err := w.Write(openapi.Spec); err != nil {
			log.Error("failed to write openapi spec", logattr.Err(err))
		}
	})
	mux.HandleFunc("GET /docs/{path...}", func(w http.ResponseWriter, r *http.Request) {
		httpSwagger.Handler(
			httpSwagger.URL("/openapi.yaml"),
		).ServeHTTP(w, r)
	})
	mux.Handle("/", server)

	var h http.Handler = mux
	h = middlewares.LoggingMiddleware(log, h)
	h = middleware.RequestID(h)
	h = middleware.RealIP(h)
	h = middleware.Recoverer(h)

	return &HttpServer{log: log, cfg: cfg, handler: h}, nil
}

func (s *HttpServer) Start() error {
	s.log.Info("starting HTTP server", slog.String("port", s.cfg.Server.Port))

	s.httpServer = &http.Server{
		Addr:              ":" + s.cfg.Server.Port,
		Handler:           s.handler,
		ReadTimeout:       s.cfg.Server.ReadTimeout,
		WriteTimeout:      s.cfg.Server.WriteTimeout,
		IdleTimeout:       s.cfg.Server.IdleTimeout,
		ReadHeaderTimeout: s.cfg.Server.ReadHeaderTimeout,
	}

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

func (s *HttpServer) Stop(ctx context.Context) error {
	s.log.Info("stopping HTTP server")

	return s.httpServer.Shutdown(ctx)
}
