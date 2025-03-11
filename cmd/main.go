package main

import (
	"context"
	"database/sql"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

var (
	// Prepared statement for inserting files
	insertFileStmt *sql.Stmt
)

func main() {
	s := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Database connection
	connStr := "postgres://postgres:postgres@localhost:5432/filedb?sslmode=disable"
	dbConn, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer func() {
		err = dbConn.Close()
		if err != nil {
			s.Log(context.Background(), slog.LevelInfo, "problem to close db connection")
		}
	}()

	// Ensure table exists
	err = ensureTables(dbConn)
	if err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}

	// Prepare the insert statement
	insertFileStmt, err = dbConn.Prepare(`
        INSERT INTO files (filename, mime_type, size, content)
        VALUES ($1, $2, $3, $4)
        RETURNING id`)
	if err != nil {
		log.Fatalf("Failed to prepare insert statement: %v", err)
	}
	defer func() {
		if err := insertFileStmt.Close(); err != nil {
			s.Log(context.Background(), slog.LevelInfo, "problem closing prepared statement")
		}
	}()

	mux := http.NewServeMux()

	mux.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse multipart form (max 10MB in memory)
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// Get file from form
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Failed to get file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Read file content
		content, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Failed to read file", http.StatusInternalServerError)
			return
		}

		// Save to database with prepared statement
		var fileID int
		err = insertFileStmt.QueryRowContext(r.Context(),
			header.Filename,
			header.Header.Get("Content-Type"),
			header.Size,
			content,
		).Scan(&fileID)
		if err != nil {
			s.LogAttrs(r.Context(), slog.LevelError, "Failed to save file to database", slog.String("error", err.Error()))
			http.Error(w, "Failed to save file to database", http.StatusInternalServerError)
			return
		}

		// Response
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("File uploaded successfully with ID: " + string(rune(fileID))))
	})

	// Inject middlewares
	handler := mux // Start with mux as http.Handler
	handler = LoggingMiddleware(s)(handler)
	handler = CORSMiddleware(handler)
	handler = RecoveryMiddleware(s)(handler)
	handler = Auth(s, "your-secret-here")(handler) // Add Auth middleware

	server := http.Server{
		Addr:              ":8081",
		Handler:           handler, // Use the wrapped handler
		ReadHeaderTimeout: time.Second * 5,
	}

	go func() {
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("run server: %v", err)
		}
	}()

	q := make(chan os.Signal, 1)
	signal.Notify(q, syscall.SIGTERM)
	<-q

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	err = server.Shutdown(ctx)
	if err != nil {
		log.Fatalf("graceful shutdown problem: %v", err)
	}
}

// ensureTables creates the files table if it doesn't exist
func ensureTables(db *sql.DB) error {
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS files (
            id SERIAL PRIMARY KEY,
            filename VARCHAR(255) NOT NULL,
            mime_type VARCHAR(100) NOT NULL,
            size BIGINT NOT NULL,
            content BYTEA,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );
        CREATE INDEX IF NOT EXISTS idx_files_filename ON files(filename);
    `)
	return err
}

// LoggingMiddleware logs request details
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "request completed",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Duration("duration", time.Since(start)),
			)
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

// RecoveryMiddleware recovers from panics and logs them
func RecoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
						slog.Any("error", err),
					)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
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
