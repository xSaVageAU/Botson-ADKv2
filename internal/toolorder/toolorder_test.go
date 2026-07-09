package toolorder

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

// fakeCtx is a minimal agent.Context stand-in: everything this package
// actually reads (SessionID, FunctionCallID, ToolConfirmation, and the
// embedded context.Context for cancellation) is overridden; anything else
// would panic via StrictContextMock if this package ever started using it.
type fakeCtx struct {
	agent.StrictContextMock
	sessionID    string
	callID       string
	confirmation *toolconfirmation.ToolConfirmation
}

func newFakeCtx(ctx context.Context, sessionID, callID string) *fakeCtx {
	return &fakeCtx{StrictContextMock: agent.NewStrictContextMock(ctx), sessionID: sessionID, callID: callID}
}

func (f *fakeCtx) SessionID() string      { return f.sessionID }
func (f *fakeCtx) FunctionCallID() string { return f.callID }
func (f *fakeCtx) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return f.confirmation
}

var _ agent.Context = (*fakeCtx)(nil)

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
	sess := "sess-real"
	registerBatch(sess, []string{"a", "b"})
	ctxA := newFakeCtx(context.Background(), sess, "a")
	ctxB := newFakeCtx(context.Background(), sess, "b")

	if _, err := beforeTool(ctxA, nil, nil); err != nil {
		t.Fatalf("beforeTool(A): %v", err)
	}

	bDone := make(chan struct{})
	go func() {
		if _, err := beforeTool(ctxB, nil, nil); err != nil {
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
// not block a later sibling's own ask -- only its real completion would.
func TestAskPhasePauseDoesNotBlockSiblingAsk(t *testing.T) {
	sess := "sess-ask"
	registerBatch(sess, []string{"a", "b"})
	ctxA := newFakeCtx(context.Background(), sess, "a")
	ctxB := newFakeCtx(context.Background(), sess, "b")

	if _, err := beforeTool(ctxA, nil, nil); err != nil {
		t.Fatalf("beforeTool(A ask): %v", err)
	}
	pauseErr := fmt.Errorf("tool %q %w", "a", tool.ErrConfirmationRequired)
	if _, err := afterTool(ctxA, nil, nil, nil, pauseErr); err != nil {
		t.Fatalf("afterTool(A pause): %v", err)
	}

	bDone := make(chan struct{})
	go func() {
		if _, err := beforeTool(ctxB, nil, nil); err != nil {
			t.Errorf("beforeTool(B ask): %v", err)
		}
		close(bDone)
	}()
	mustProceed(t, bDone, "B's ask")
}

// Once both calls are resumed (ToolConfirmation now set on the context),
// the later one must wait for the earlier one's real completion -- even if
// the resume dispatches the later position's BeforeToolCallback first,
// mirroring ADK's own map-iteration-order resume dispatch.
func TestResumePassSerializesRegardlessOfDispatchOrder(t *testing.T) {
	sess := "sess-resume"
	registerBatch(sess, []string{"a", "b"})
	ctxA := newFakeCtx(context.Background(), sess, "a")
	ctxB := newFakeCtx(context.Background(), sess, "b")

	// Ask phase: both pause.
	for _, c := range []*fakeCtx{ctxA, ctxB} {
		if _, err := beforeTool(c, nil, nil); err != nil {
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
		if _, err := beforeTool(ctxB, nil, nil); err != nil {
			t.Errorf("beforeTool(B resume): %v", err)
		}
		close(bDone)
	}()
	mustNotProceed(t, bDone, "B's resume (position 1)")

	if _, err := beforeTool(ctxA, nil, nil); err != nil {
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

// A waiter blocked on an earlier call must be released by context
// cancellation instead of hanging forever.
func TestWaitTurnUnblocksOnContextCancellation(t *testing.T) {
	sess := "sess-cancel"
	registerBatch(sess, []string{"a", "b"})

	ctxA := newFakeCtx(context.Background(), sess, "a")
	if _, err := beforeTool(ctxA, nil, nil); err != nil {
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
		_, err := beforeTool(ctxB, nil, nil)
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
