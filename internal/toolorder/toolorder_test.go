package toolorder

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

// fakeCtx is a minimal agent.Context stand-in: everything this package
// actually reads (SessionID, FunctionCallID, ToolConfirmation,
// RequestConfirmation, and the embedded context.Context for cancellation)
// is overridden; anything else would panic via StrictContextMock if this
// package ever started using it.
type fakeCtx struct {
	agent.StrictContextMock
	sessionID    string
	callID       string
	confirmation *toolconfirmation.ToolConfirmation

	requestedHint    string
	requestedPayload any
}

func newFakeCtx(ctx context.Context, sessionID, callID string) *fakeCtx {
	return &fakeCtx{StrictContextMock: agent.NewStrictContextMock(ctx), sessionID: sessionID, callID: callID}
}

func (f *fakeCtx) SessionID() string      { return f.sessionID }
func (f *fakeCtx) FunctionCallID() string { return f.callID }
func (f *fakeCtx) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return f.confirmation
}
func (f *fakeCtx) RequestConfirmation(hint string, payload any) error {
	f.requestedHint = hint
	f.requestedPayload = payload
	return nil
}

var _ agent.Context = (*fakeCtx)(nil)

// fakeTool is the little bit of tool.Tool the plugin reads (Name only).
type fakeTool struct {
	tool.Tool
	name string
}

func (f fakeTool) Name() string { return f.name }

// newPlugin builds an orderPlugin whose gated set is exactly gatedNames --
// mirroring how bootstrap wires New to internal/agent.RequiresConfirmation.
func newPlugin(gatedNames ...string) *orderPlugin {
	gated := map[string]bool{}
	for _, n := range gatedNames {
		gated[n] = true
	}
	return &orderPlugin{requiresConfirmation: func(name string) bool { return gated[name] }}
}

const shortWait = 100 * time.Millisecond

func mustNotProceed(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
		t.Fatalf("%s proceeded when it should have blocked", what)
	case <-time.After(shortWait):
	}
}

func mustProceed(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s never proceeded", what)
	}
}

// Two calls that never need confirmation (or already have it) must still
// execute their real work in position order.
func TestRealPhaseSerializesInOrder(t *testing.T) {
	op := newPlugin()
	sess := "sess-real"
	registerBatch(sess, []string{"a", "b"})
	ctxA := newFakeCtx(context.Background(), sess, "a")
	ctxB := newFakeCtx(context.Background(), sess, "b")

	if _, err := op.beforeTool(ctxA, nil, nil); err != nil {
		t.Fatalf("beforeTool(A): %v", err)
	}

	bDone := make(chan struct{})
	go func() {
		if _, err := op.beforeTool(ctxB, nil, nil); err != nil {
			t.Errorf("beforeTool(B): %v", err)
		}
		close(bDone)
	}()
	mustNotProceed(t, bDone, "B (position 1)")

	if _, err := afterTool(ctxA, nil, nil, nil, nil); err != nil {
		t.Fatalf("afterTool(A): %v", err)
	}
	mustProceed(t, bDone, "B (position 1)")

	if _, err := afterTool(ctxB, nil, nil, nil, nil); err != nil {
		t.Fatalf("afterTool(B): %v", err)
	}
}

// A gated call merely asking for confirmation (ErrConfirmationRequired) must
// not block a later gated sibling's own ask -- only its real completion
// would. The sibling is gated, so it is left to pause itself rather than
// being deferred.
func TestAskPhasePauseDoesNotBlockSiblingGatedAsk(t *testing.T) {
	op := newPlugin("writeFile", "editFile")
	sess := "sess-ask"
	registerBatch(sess, []string{"a", "b"})
	ctxA := newFakeCtx(context.Background(), sess, "a")
	ctxB := newFakeCtx(context.Background(), sess, "b")

	if _, err := op.beforeTool(ctxA, fakeTool{name: "writeFile"}, nil); err != nil {
		t.Fatalf("beforeTool(A ask): %v", err)
	}
	pauseErr := fmt.Errorf("tool %q %w", "a", tool.ErrConfirmationRequired)
	if _, err := afterTool(ctxA, nil, nil, nil, pauseErr); err != nil {
		t.Fatalf("afterTool(A pause): %v", err)
	}

	bDone := make(chan struct{})
	go func() {
		if _, err := op.beforeTool(ctxB, fakeTool{name: "editFile"}, nil); err != nil {
			t.Errorf("beforeTool(B ask): %v", err)
		}
		close(bDone)
	}()
	mustProceed(t, bDone, "B's ask")
	if ctxB.requestedHint != "" {
		t.Fatalf("gated B was given a synthetic confirmation (hint %q); its own Run must ask instead", ctxB.requestedHint)
	}
}

// A non-gated call positioned after a paused gated call must not really run
// in the ask pass: beforeTool defers it with a synthetic confirmation
// (payload marked DeferredPayloadKey) and ErrConfirmationRequired, and the
// resume pass then serializes it after the gated call's real completion.
func TestNonGatedCallDeferredBehindPause(t *testing.T) {
	op := newPlugin("writeFile")
	sess := "sess-defer"
	registerBatch(sess, []string{"a", "b"})
	ctxA := newFakeCtx(context.Background(), sess, "a")
	ctxB := newFakeCtx(context.Background(), sess, "b")

	// Ask pass: gated A pauses; non-gated B must be deferred, not run.
	if _, err := op.beforeTool(ctxA, fakeTool{name: "writeFile"}, nil); err != nil {
		t.Fatalf("beforeTool(A ask): %v", err)
	}
	pauseErr := fmt.Errorf("tool %q %w", "a", tool.ErrConfirmationRequired)
	if _, err := afterTool(ctxA, nil, nil, nil, pauseErr); err != nil {
		t.Fatalf("afterTool(A pause): %v", err)
	}

	_, err := op.beforeTool(ctxB, fakeTool{name: "readFile"}, nil)
	if !errors.Is(err, tool.ErrConfirmationRequired) {
		t.Fatalf("beforeTool(B ask) = %v, want ErrConfirmationRequired", err)
	}
	payload, ok := ctxB.requestedPayload.(map[string]any)
	if !ok || payload[DeferredPayloadKey] != true {
		t.Fatalf("B's synthetic confirmation payload = %#v, want map with %q: true", ctxB.requestedPayload, DeferredPayloadKey)
	}
	if _, aerr := afterTool(ctxB, nil, nil, nil, err); aerr != nil {
		t.Fatalf("afterTool(B pause): %v", aerr)
	}

	// Resume pass (both confirmed, dispatched B-first like ADK's
	// map-iteration resume can): B must wait for A's real completion.
	ctxA.confirmation = &toolconfirmation.ToolConfirmation{Confirmed: true}
	ctxB.confirmation = &toolconfirmation.ToolConfirmation{Confirmed: true}

	bDone := make(chan struct{})
	go func() {
		if _, err := op.beforeTool(ctxB, fakeTool{name: "readFile"}, nil); err != nil {
			t.Errorf("beforeTool(B resume): %v", err)
		}
		close(bDone)
	}()
	mustNotProceed(t, bDone, "B's resume (position 1)")

	if _, err := op.beforeTool(ctxA, fakeTool{name: "writeFile"}, nil); err != nil {
		t.Fatalf("beforeTool(A resume): %v", err)
	}
	if _, err := afterTool(ctxA, nil, nil, nil, nil); err != nil {
		t.Fatalf("afterTool(A resume): %v", err)
	}
	mustProceed(t, bDone, "B's resume (position 1, after A finished)")

	if _, err := afterTool(ctxB, nil, nil, nil, nil); err != nil {
		t.Fatalf("afterTool(B resume): %v", err)
	}
}

// A non-gated call whose earlier siblings all really finished must run
// immediately in the ask pass -- no synthetic confirmation.
func TestNonGatedCallRunsWhenEarlierDone(t *testing.T) {
	op := newPlugin("writeFile")
	sess := "sess-done"
	registerBatch(sess, []string{"a", "b"})
	ctxA := newFakeCtx(context.Background(), sess, "a")
	ctxB := newFakeCtx(context.Background(), sess, "b")

	if _, err := op.beforeTool(ctxA, fakeTool{name: "listFiles"}, nil); err != nil {
		t.Fatalf("beforeTool(A): %v", err)
	}
	if _, err := afterTool(ctxA, nil, nil, nil, nil); err != nil {
		t.Fatalf("afterTool(A): %v", err)
	}

	if _, err := op.beforeTool(ctxB, fakeTool{name: "readFile"}, nil); err != nil {
		t.Fatalf("beforeTool(B) = %v, want nil (no deferral needed)", err)
	}
	if ctxB.requestedHint != "" {
		t.Fatalf("B was deferred (hint %q) even though A had really finished", ctxB.requestedHint)
	}
}

// Once both calls are resumed (ToolConfirmation now set on the context),
// the later one must wait for the earlier one's real completion -- even if
// the resume dispatches the later position's BeforeToolCallback first,
// mirroring ADK's own map-iteration-order resume dispatch.
func TestResumePassSerializesRegardlessOfDispatchOrder(t *testing.T) {
	op := newPlugin()
	sess := "sess-resume"
	registerBatch(sess, []string{"a", "b"})
	ctxA := newFakeCtx(context.Background(), sess, "a")
	ctxB := newFakeCtx(context.Background(), sess, "b")

	// Ask phase: both pause (gated tools pause themselves; the plugin's
	// gated-set is irrelevant here since afterTool drives the state).
	for _, c := range []*fakeCtx{ctxA, ctxB} {
		if _, err := op.beforeTool(c, nil, nil); err != nil && !errors.Is(err, tool.ErrConfirmationRequired) {
			t.Fatalf("beforeTool(%s ask): %v", c.callID, err)
		}
		pauseErr := fmt.Errorf("tool %q %w", c.callID, tool.ErrConfirmationRequired)
		if _, err := afterTool(c, nil, nil, nil, pauseErr); err != nil {
			t.Fatalf("afterTool(%s pause): %v", c.callID, err)
		}
	}

	// Resume: both now carry a confirmation. Dispatch B (position 1) first.
	ctxA.confirmation = &toolconfirmation.ToolConfirmation{Confirmed: true}
	ctxB.confirmation = &toolconfirmation.ToolConfirmation{Confirmed: true}

	bDone := make(chan struct{})
	go func() {
		if _, err := op.beforeTool(ctxB, nil, nil); err != nil {
			t.Errorf("beforeTool(B resume): %v", err)
		}
		close(bDone)
	}()
	mustNotProceed(t, bDone, "B's resume (position 1)")

	if _, err := op.beforeTool(ctxA, nil, nil); err != nil {
		t.Fatalf("beforeTool(A resume): %v", err)
	}
	mustNotProceed(t, bDone, "B's resume (position 1, A still running)")

	if _, err := afterTool(ctxA, nil, nil, nil, nil); err != nil {
		t.Fatalf("afterTool(A resume): %v", err)
	}
	mustProceed(t, bDone, "B's resume (position 1, after A finished)")

	if _, err := afterTool(ctxB, nil, nil, nil, nil); err != nil {
		t.Fatalf("afterTool(B resume): %v", err)
	}
}

// ADK's tool-not-found dispatch path never reaches the after-tool callbacks,
// only the on-error ones -- onToolError must resolve that position so later
// siblings don't block until the turn's deadline.
func TestOnToolErrorResolvesPosition(t *testing.T) {
	op := newPlugin()
	sess := "sess-onerror"
	registerBatch(sess, []string{"a", "b"})
	ctxA := newFakeCtx(context.Background(), sess, "a")
	ctxB := newFakeCtx(context.Background(), sess, "b")

	if _, err := onToolError(ctxA, nil, nil, errors.New("tool \"nope\" not found")); err != nil {
		t.Fatalf("onToolError(A): %v", err)
	}

	if _, err := op.beforeTool(ctxB, fakeTool{name: "readFile"}, nil); err != nil {
		t.Fatalf("beforeTool(B) = %v, want nil (A's failure is final)", err)
	}
}

// A waiter blocked on an earlier call must be released by context
// cancellation instead of hanging forever.
func TestWaitTurnUnblocksOnContextCancellation(t *testing.T) {
	op := newPlugin()
	sess := "sess-cancel"
	registerBatch(sess, []string{"a", "b"})

	ctxA := newFakeCtx(context.Background(), sess, "a")
	if _, err := op.beforeTool(ctxA, nil, nil); err != nil {
		t.Fatalf("beforeTool(A ask): %v", err)
	}
	pauseErr := fmt.Errorf("tool %q %w", "a", tool.ErrConfirmationRequired)
	if _, err := afterTool(ctxA, nil, nil, nil, pauseErr); err != nil {
		t.Fatalf("afterTool(A pause): %v", err)
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	ctxB := newFakeCtx(cancelCtx, sess, "b")
	ctxB.confirmation = &toolconfirmation.ToolConfirmation{Confirmed: true} // resume, strict wait

	bErr := make(chan error, 1)
	go func() {
		_, err := op.beforeTool(ctxB, nil, nil)
		bErr <- err
	}()

	select {
	case err := <-bErr:
		t.Fatalf("B proceeded before cancellation (err=%v)", err)
	case <-time.After(shortWait):
	}

	cancel()

	select {
	case err := <-bErr:
		if err == nil {
			t.Fatal("expected an error from waitTurn after cancellation, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("B never unblocked after context cancellation")
	}
}
