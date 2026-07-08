package web

import (
	"embed"

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
}
