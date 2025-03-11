package middlewares

import (
	"log/slog"
	"net/http"
)

// Auth logs request details
func Auth(logger *slog.Logger, secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Example: Check Authorization header (simplified)
			authHeader := r.Header.Get("Authorization")
			if authHeader != secret { // Replace with real auth logic
				logger.LogAttrs(r.Context(), slog.LevelWarn, "unauthorized access",
					slog.String("path", r.URL.Path),
				)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORSMiddleware adds basic CORS headers
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
