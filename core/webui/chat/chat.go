package chat

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

//go:embed index.html static/*
var content embed.FS

// NewHandler returns a new http.Handler representing the Custom Chat web application.
func NewHandler() http.Handler {
	mux := http.NewServeMux()

	// Serves main chat HTML and static files
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			data, err := content.ReadFile("index.html")
			if err != nil {
				http.Error(w, "Failed to read index.html", http.StatusInternalServerError)
				return
			}
			w.Write(data)
			return
		}

		filePath := strings.TrimPrefix(r.URL.Path, "/")
		data, err := content.ReadFile(filePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if strings.HasSuffix(filePath, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		} else if strings.HasSuffix(filePath, ".js") {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		}
		w.Write(data)
	})

	return mux
}

// StartServerGracefully starts the Custom Chat server and shuts it down when the context is cancelled.
func StartServerGracefully(ctx context.Context, port string) error {
	handler := NewHandler()
	srv := &http.Server{
		Addr:    port,
		Handler: handler,
	}

	errChan := make(chan error, 1)

	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			errChan <- err
		}
		close(errChan)
	}()

	select {
	case <-ctx.Done():
		log.Println("Shutting down Custom Chat server gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("custom chat shutdown failed: %w", err)
		}
		return nil
	case err := <-errChan:
		return err
	}
}
