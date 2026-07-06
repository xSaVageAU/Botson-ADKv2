package webui

import (
	"flag"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/gorilla/mux"
	"google.golang.org/adk/v2/cmd/launcher"
	weblauncher "google.golang.org/adk/v2/cmd/launcher/web"
)

type botsonSublauncher struct {
	flags *flag.FlagSet
}

func (b *botsonSublauncher) Keyword() string {
	return "botson"
}

func (b *botsonSublauncher) Parse(args []string) ([]string, error) {
	err := b.flags.Parse(args)
	return b.flags.Args(), err
}

func (b *botsonSublauncher) CommandLineSyntax() string { return "" }
func (b *botsonSublauncher) SimpleDescription() string  { return "starts the unified Botson Workspace Console" }

func (b *botsonSublauncher) SetupSubrouters(router *mux.Router, config *launcher.Config) error {
	pathPrefix := "/botson/"

	rUI := router.Methods("GET").PathPrefix(pathPrefix).Subrouter()

	// Redirect /botson and root / to /botson/
	router.Methods("GET").Path("/botson").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, pathPrefix, http.StatusFound)
	})
	router.Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, pathPrefix, http.StatusFound)
	})

	// Serve SPA HTML
	rUI.Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, err := content.ReadFile("webui/index.html")
		if err != nil {
			http.Error(w, "Failed to read index.html", http.StatusInternalServerError)
			return
		}
		w.Write(data)
	})

	// Serve Static Files
	staticFS, err := fs.Sub(content, "webui/static")
	if err != nil {
		return fmt.Errorf("cannot prepare static files: %v", err)
	}
	rUI.PathPrefix("/static/").Handler(http.StripPrefix(pathPrefix+"static/", http.FileServer(http.FS(staticFS))))

	// Mount Custom APIs under /botson/api/
	rAPI := router.PathPrefix("/botson/api").Subrouter()
	setupAPIRoutes(rAPI, config)

	return nil
}

func (b *botsonSublauncher) UserMessage(webURL string, printer func(v ...any)) {
	printer(fmt.Sprintf("       botson:  you can access the Botson Web Console at %s/botson/", webURL))
}

func NewSublauncher() weblauncher.Sublauncher {
	return &botsonSublauncher{
		flags: flag.NewFlagSet("botson", flag.ContinueOnError),
	}
}
