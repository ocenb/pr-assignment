package middlewares

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *statusRecorder) WriteHeader(statusCode int) {
	rec.statusCode = statusCode
	rec.ResponseWriter.WriteHeader(statusCode)
}

func LoggingMiddleware(log *slog.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		reqLog := log.With(
			slog.String("req_id", middleware.GetReqID(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("query", r.URL.RawQuery),
			slog.String("user_agent", r.UserAgent()),
		)

		recorder := &statusRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		reqLog.Debug("Request")

		h.ServeHTTP(recorder, r)

		reqLog.Info("Response", slog.Int("status", recorder.statusCode), slog.Duration("duration", time.Since(start)))
	})
}
