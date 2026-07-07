package web

import (
	"embed"
	"io/fs"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	"google.golang.org/adk/v2/cmd/launcher"
)

//go:embed webui/index.html webui/static/*
var content embed.FS

func setupAPIRoutes(r *mux.Router, configLauncher *launcher.Config) {
	// Register Dashboard Stats Route
	registerDashboardRoutes(r, configLauncher)

	// Register Agent Config Builder Routes
	registerBuilderRoutes(r)

	// Register Workflow Studio Routes
	registerWorkflowRoutes(r)
}

// StartServer starts a standalone server listening on the specified port.
// This is used for cmd/agent-builder standalone execution.
func StartServer(port string) error {
	muxRouter := mux.NewRouter()

	// Redirect root to /botson/
	muxRouter.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/botson/", http.StatusFound)
	})

	// Setup static files
	staticFS, err := fs.Sub(content, "webui/static")
	if err != nil {
		return err
	}
	muxRouter.PathPrefix("/botson/static/").Handler(http.StripPrefix("/botson/static/", http.FileServer(http.FS(staticFS))))

	// Serve SPA html
	muxRouter.HandleFunc("/botson/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, err := content.ReadFile("webui/index.html")
		if err != nil {
			http.Error(w, "Failed to read index.html", http.StatusInternalServerError)
			return
		}
		w.Write(data)
	})

	// Set up custom APIs under /botson/api
	rAPI := muxRouter.PathPrefix("/botson/api").Subrouter()
	setupAPIRoutes(rAPI, nil)

	log.Printf("Starting Standalone Console on http://localhost%s/botson/\n", port)
	return http.ListenAndServe(port, muxRouter)
}
