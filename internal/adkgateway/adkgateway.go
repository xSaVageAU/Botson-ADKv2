package adkgateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"google.golang.org/adk/v2/cmd/launcher"
)

// Config configures a Gateway.
type Config struct {
	// NATSConn is the NATS connection to serve requests on. Required.
	NATSConn *nats.Conn

	// ADK is the ADK launcher configuration, passed through to
	// prod.NewLauncher(). ADK.AgentLoader is required; other fields fall
	// back to ADK's own defaults (e.g. an in-memory SessionService) when
	// left zero.
	ADK launcher.Config

	// SubjectPrefix is the NATS subject prefix; defaults to "adk".
	SubjectPrefix string

	// QueueGroup, if set, makes this Gateway join a NATS queue group so
	// multiple Gateway instances load-balance requests for horizontal scaling.
	QueueGroup string

	// Port is the loopback port the backend ADK server binds to; 0 (the
	// default) picks a free ephemeral port.
	Port int

	// RequestTimeout bounds each request forwarded to the backend; defaults to 30s.
	RequestTimeout time.Duration

	// Logger is used for diagnostics; defaults to slog.Default().
	Logger *slog.Logger
}

// Gateway fronts an ADK agent registry with a NATS gateway, backed by a real
// ADK REST server running on a loopback port.
type Gateway struct {
	cfg Config

	mu      sync.RWMutex
	backend *backend
}

// New validates cfg and returns a Gateway ready to Run.
func New(cfg Config) (*Gateway, error) {
	if cfg.NATSConn == nil {
		return nil, errors.New("adkgateway: Config.NATSConn is required")
	}
	if cfg.ADK.AgentLoader == nil {
		return nil, errors.New("adkgateway: Config.ADK.AgentLoader is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Gateway{cfg: cfg}, nil
}

// Run starts the ADK backend, waits for it to become ready, starts the NATS
// gateway, and blocks until ctx is cancelled or a fatal error occurs. On
// return, both the gateway and the backend have been shut down.
func (p *Gateway) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	b, err := startBackend(runCtx, p.cfg.ADK, p.cfg.Port)
	if err != nil {
		return fmt.Errorf("adkgateway: start backend: %w", err)
	}
	p.setBackend(b)
	defer p.setBackend(nil)

	gw := newGateway(p.cfg.NATSConn, b.BaseURL(), gatewayOptions{
		subjectPrefix:  p.cfg.SubjectPrefix,
		queueGroup:     p.cfg.QueueGroup,
		requestTimeout: p.cfg.RequestTimeout,
		logger:         p.cfg.Logger,
	})
	if err := gw.Start(); err != nil {
		return fmt.Errorf("adkgateway: start gateway: %w", err)
	}
	defer gw.Close()

	select {
	case <-ctx.Done():
		cancel()
		<-b.Done()
		return nil
	case <-b.Done():
		if err := b.Err(); err != nil {
			return fmt.Errorf("adkgateway: backend exited unexpectedly: %w", err)
		}
		return errors.New("adkgateway: backend exited unexpectedly")
	}
}

// Addr returns the backend's local HTTP base URL (e.g.
// "http://127.0.0.1:8080"), useful for health checks/debugging. It returns ""
// when the Gateway isn't currently running.
func (p *Gateway) Addr() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.backend == nil {
		return ""
	}
	return p.backend.BaseURL()
}

func (p *Gateway) setBackend(b *backend) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backend = b
}
