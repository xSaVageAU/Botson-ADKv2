package adkgateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/Savs-Agents/NATS-ADK-Proxy/protocol"
)

// wellKnownAgentCardPath mirrors a2asrv.WellKnownAgentCardPath
// ("/.well-known/agent-card.json"), duplicated here as a constant so this
// package doesn't need to import a2asrv just for one string.
const wellKnownAgentCardPath = "/.well-known/agent-card.json"

const (
	a2aV1InvokePath = "/a2a/v1/invoke"
	a2aV0InvokePath = "/a2a/invoke"
)

// gatewayOptions configures a gateway.
type gatewayOptions struct {
	// subjectPrefix is the NATS subject prefix; defaults to protocol.DefaultSubjectPrefix.
	subjectPrefix string
	// queueGroup, if non-empty, makes the gateway join a NATS queue group so
	// multiple gateway instances load-balance requests instead of each
	// receiving every message.
	queueGroup string
	// httpClient is used to call the backend; defaults to http.DefaultClient.
	httpClient *http.Client
	// requestTimeout bounds each forwarded request; defaults to 30s.
	requestTimeout time.Duration
	// maxConcurrency bounds how many requests are forwarded at once; defaults to 64.
	maxConcurrency int
	// logger is used for gateway-level diagnostics; defaults to slog.Default().
	logger *slog.Logger
}

func (o gatewayOptions) withDefaults() gatewayOptions {
	if o.subjectPrefix == "" {
		o.subjectPrefix = protocol.DefaultSubjectPrefix
	}
	if o.httpClient == nil {
		o.httpClient = http.DefaultClient
	}
	if o.requestTimeout <= 0 {
		o.requestTimeout = 30 * time.Second
	}
	if o.maxConcurrency <= 0 {
		o.maxConcurrency = 64
	}
	if o.logger == nil {
		o.logger = slog.Default()
	}
	return o
}

// gateway forwards NATS requests to a local ADK REST/A2A HTTP backend.
type gateway struct {
	nc      *nats.Conn
	baseURL string
	opts    gatewayOptions

	sem  chan struct{}
	subs []*nats.Subscription
	wg   sync.WaitGroup
}

// newGateway creates a gateway that forwards to backendBaseURL (e.g.
// "http://127.0.0.1:8080").
func newGateway(nc *nats.Conn, backendBaseURL string, opts gatewayOptions) *gateway {
	opts = opts.withDefaults()
	return &gateway{
		nc:      nc,
		baseURL: strings.TrimSuffix(backendBaseURL, "/"),
		opts:    opts,
		sem:     make(chan struct{}, opts.maxConcurrency),
	}
}

// Start subscribes to the REST and A2A subjects and begins forwarding
// requests in background goroutines. It returns once subscriptions are
// established.
func (g *gateway) Start() error {
	type route struct {
		subject string
		handler func(*nats.Msg)
	}
	routes := []route{
		{protocol.RESTSubjectWildcard(g.opts.subjectPrefix), g.handleREST},
		{protocol.A2AV1InvokeSubject(g.opts.subjectPrefix), g.fixedRouteHandler(http.MethodPost, a2aV1InvokePath)},
		{protocol.A2AV0InvokeSubject(g.opts.subjectPrefix), g.fixedRouteHandler(http.MethodPost, a2aV0InvokePath)},
		{protocol.A2AAgentCardSubject(g.opts.subjectPrefix), g.fixedRouteHandler(http.MethodGet, wellKnownAgentCardPath)},
	}

	for _, r := range routes {
		sub, err := g.subscribe(r.subject, r.handler)
		if err != nil {
			g.unsubscribeAll()
			return fmt.Errorf("adk gateway: subscribe %q: %w", r.subject, err)
		}
		g.subs = append(g.subs, sub)
	}
	return nil
}

func (g *gateway) subscribe(subject string, handler func(*nats.Msg)) (*nats.Subscription, error) {
	wrapped := func(msg *nats.Msg) {
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			g.sem <- struct{}{}
			defer func() { <-g.sem }()
			handler(msg)
		}()
	}
	if g.opts.queueGroup != "" {
		return g.nc.QueueSubscribe(subject, g.opts.queueGroup, wrapped)
	}
	return g.nc.Subscribe(subject, wrapped)
}

// Close unsubscribes from all subjects and waits for in-flight requests to finish.
func (g *gateway) Close() error {
	g.unsubscribeAll()
	g.wg.Wait()
	return nil
}

func (g *gateway) unsubscribeAll() {
	for _, sub := range g.subs {
		_ = sub.Unsubscribe()
	}
	g.subs = nil
}

// handleREST handles a message on the "<prefix>.rest.*" wildcard: the method
// comes from the subject, the path from the Adk-Path header.
func (g *gateway) handleREST(msg *nats.Msg) {
	if msg.Reply == "" {
		g.opts.logger.Warn("adk gateway: dropping REST request with no reply subject", "subject", msg.Subject)
		return
	}

	method := protocol.MethodFromRESTSubject(g.opts.subjectPrefix, msg.Subject)
	if !protocol.IsValidHTTPMethod(method) {
		g.replyError(msg, fmt.Errorf("unsupported or missing HTTP method in subject %q", msg.Subject))
		return
	}

	path := msg.Header.Get(protocol.HeaderPath)
	if !strings.HasPrefix(path, "/") {
		g.replyError(msg, fmt.Errorf("missing or invalid %s header", protocol.HeaderPath))
		return
	}

	g.forward(msg, method, path)
}

// fixedRouteHandler returns a handler for subjects that always map to a
// single fixed backend method+path (the A2A invoke and agent card subjects).
func (g *gateway) fixedRouteHandler(method, path string) func(*nats.Msg) {
	return func(msg *nats.Msg) {
		if msg.Reply == "" {
			g.opts.logger.Warn("adk gateway: dropping request with no reply subject", "subject", msg.Subject)
			return
		}
		g.forward(msg, method, path)
	}
}

// forward builds an HTTP request from msg targeting method+path on the
// backend, executes it, and publishes the response to msg.Reply.
func (g *gateway) forward(msg *nats.Msg, method, path string) {
	ctx, cancel := context.WithTimeout(context.Background(), g.opts.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, bytes.NewReader(msg.Data))
	if err != nil {
		g.replyError(msg, fmt.Errorf("build backend request: %w", err))
		return
	}
	req.Header = protocol.DecodeForwardedHeaders(msg.Header)

	resp, err := g.opts.httpClient.Do(req)
	if err != nil {
		g.replyError(msg, fmt.Errorf("call backend: %w", err))
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		g.replyError(msg, fmt.Errorf("read backend response: %w", err))
		return
	}

	reply := nats.NewMsg(msg.Reply)
	reply.Header = make(nats.Header)
	protocol.SetStatus(reply.Header, resp.StatusCode)
	protocol.EncodeForwardedHeaders(reply.Header, resp.Header)
	reply.Data = body

	if err := g.nc.PublishMsg(reply); err != nil {
		g.opts.logger.Error("adk gateway: failed to publish reply", "subject", msg.Reply, "error", err)
	}
}

// replyError publishes a gateway-level error response (Adk-Gateway-Error set,
// no Adk-Status) to msg.Reply.
func (g *gateway) replyError(msg *nats.Msg, err error) {
	reply := nats.NewMsg(msg.Reply)
	reply.Header = make(nats.Header)
	protocol.SetGatewayError(reply.Header, err.Error())
	if pubErr := g.nc.PublishMsg(reply); pubErr != nil {
		g.opts.logger.Error("adk gateway: failed to publish error reply", "subject", msg.Reply, "error", pubErr)
	}
}
