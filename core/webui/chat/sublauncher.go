package chat

import (
	"flag"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/gorilla/mux"

	"google.golang.org/adk/v2/cmd/launcher"
	weblauncher "google.golang.org/adk/v2/cmd/launcher/web"
)

type chatSublauncher struct {
	flags *flag.FlagSet
}

func (c *chatSublauncher) Keyword() string {
	return "chat"
}

func (c *chatSublauncher) Parse(args []string) ([]string, error) {
	err := c.flags.Parse(args)
	if err != nil || !c.flags.Parsed() {
		return nil, fmt.Errorf("failed to parse chat flags: %v", err)
	}
	return c.flags.Args(), nil
}

func (c *chatSublauncher) CommandLineSyntax() string {
	return ""
}

func (c *chatSublauncher) SimpleDescription() string {
	return "starts Botson Custom Chat Interface"
}

func (c *chatSublauncher) SetupSubrouters(router *mux.Router, config *launcher.Config) error {
	pathPrefix := "/chat/"

	rChat := router.Methods("GET").PathPrefix(pathPrefix).Subrouter()

	// Redirect /chat to /chat/
	router.Methods("GET").Path("/chat").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, pathPrefix, http.StatusFound)
	})



	// Serve the main chat HTML
	rChat.Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, err := content.ReadFile("index.html")
		if err != nil {
			http.Error(w, "Failed to read index.html", http.StatusInternalServerError)
			return
		}
		w.Write(data)
	})

	// Serve static files (under /chat/static/...)
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		return fmt.Errorf("cannot prepare chat static files: %v", err)
	}
	rChat.PathPrefix("/static/").Handler(http.StripPrefix(pathPrefix+"static/", http.FileServer(http.FS(staticFS))))

	return nil
}

func (c *chatSublauncher) UserMessage(webURL string, printer func(v ...any)) {
	printer(fmt.Sprintf("         chat:  you can access Custom Chat UI using %s/chat/", webURL))
}

// NewSublauncher creates a new Sublauncher for Custom Chat.
func NewSublauncher() weblauncher.Sublauncher {
	return &chatSublauncher{
		flags: flag.NewFlagSet("chat", flag.ContinueOnError),
	}
}
