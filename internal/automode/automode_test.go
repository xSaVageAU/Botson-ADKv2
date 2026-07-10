package automode

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"

	"botson/internal/management"
)

func newTestSession(t *testing.T) (session.Service, session.Session) {
	t.Helper()
	svc := session.InMemoryService()
	resp, err := svc.Create(context.Background(), &session.CreateRequest{AppName: "app", UserID: "user"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return svc, resp.Session
}

func appendFunctionCall(t *testing.T, svc session.Service, sess session.Session, id, name string, args map[string]any) {
	t.Helper()
	ev := session.NewEvent(context.Background(), "test")
	ev.Author = "model"
	ev.Content = &genai.Content{Role: "model", Parts: []*genai.Part{
		{FunctionCall: &genai.FunctionCall{ID: id, Name: name, Args: args}},
	}}
	if err := svc.AppendEvent(context.Background(), sess, ev); err != nil {
		t.Fatalf("append function call event: %v", err)
	}
}

func appendFunctionResponse(t *testing.T, svc session.Service, sess session.Session, id, name string, response map[string]any) {
	t.Helper()
	ev := session.NewEvent(context.Background(), "test")
	ev.Author = "user"
	ev.Content = &genai.Content{Role: "user", Parts: []*genai.Part{
		{FunctionResponse: &genai.FunctionResponse{ID: id, Name: name, Response: response}},
	}}
	if err := svc.AppendEvent(context.Background(), sess, ev); err != nil {
		t.Fatalf("append function response event: %v", err)
	}
}

func reGet(t *testing.T, svc session.Service, sess session.Session) session.Session {
	t.Helper()
	resp, err := svc.Get(context.Background(), &session.GetRequest{AppName: sess.AppName(), UserID: sess.UserID(), SessionID: sess.ID()})
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	return resp.Session
}

func TestPendingConfirmationsDetectsUnanswered(t *testing.T) {
	svc, sess := newTestSession(t)
	appendFunctionCall(t, svc, sess, "wrap-1", toolconfirmation.FunctionCallName, map[string]any{"hint": "approve?"})

	pending := pendingConfirmations(reGet(t, svc, sess))
	if len(pending) != 1 || pending[0] != "wrap-1" {
		t.Fatalf("pendingConfirmations = %v, want [wrap-1]", pending)
	}
}

func TestPendingConfirmationsExcludesAnswered(t *testing.T) {
	svc, sess := newTestSession(t)
	appendFunctionCall(t, svc, sess, "wrap-1", toolconfirmation.FunctionCallName, nil)
	appendFunctionResponse(t, svc, sess, "wrap-1", toolconfirmation.FunctionCallName, map[string]any{"confirmed": true})

	pending := pendingConfirmations(reGet(t, svc, sess))
	if len(pending) != 0 {
		t.Fatalf("pendingConfirmations = %v, want none (already answered)", pending)
	}
}

func TestPendingConfirmationsIgnoresNonConfirmationCalls(t *testing.T) {
	svc, sess := newTestSession(t)
	appendFunctionCall(t, svc, sess, "call-1", "readFile", map[string]any{"filePath": "a.txt"})

	pending := pendingConfirmations(reGet(t, svc, sess))
	if len(pending) != 0 {
		t.Fatalf("pendingConfirmations = %v, want none (not a confirmation wrapper)", pending)
	}
}

// TestSafetyCapDisablesAutoMode confirms that when a session already has
// more pending confirmations than maxConsecutiveApprovals allows,
// checkSession disables auto mode instead of ever calling autoApprove --
// engineered so the cap check trips before any network call would be
// attempted, since this test has no live NATS connection to answer one.
func TestSafetyCapDisablesAutoMode(t *testing.T) {
	ctx := context.Background()
	svc, sess := newTestSession(t)

	if err := management.SetSessionAutoMode(ctx, svc, sess.AppName(), sess.UserID(), sess.ID(), true, ""); err != nil {
		t.Fatalf("enable auto mode: %v", err)
	}
	for i := range maxConsecutiveApprovals + 1 {
		appendFunctionCall(t, svc, sess, fmt.Sprintf("wrap-%d", i), toolconfirmation.FunctionCallName, nil)
	}

	w := &worker{
		cfg:    &launcher.Config{SessionService: svc},
		streak: make(map[string]int),
		wasOn:  make(map[string]bool),
	}

	listed, err := svc.List(ctx, &session.ListRequest{AppName: sess.AppName()})
	if err != nil || len(listed.Sessions) != 1 {
		t.Fatalf("List: %v, %v", listed, err)
	}
	w.checkSession(ctx, listed.Sessions[0])

	final := reGet(t, svc, sess)
	val, err := final.State().Get(management.AutoModeStateKey)
	if err != nil {
		t.Fatalf("State().Get: %v", err)
	}
	if on, _ := val.(bool); on {
		t.Fatal("auto mode still on after exceeding maxConsecutiveApprovals, want it disabled")
	}

	var sawReason bool
	for evt := range final.Events().All() {
		if evt.Content == nil {
			continue
		}
		for _, p := range evt.Content.Parts {
			if p.Text != "" {
				sawReason = true
			}
		}
	}
	if !sawReason {
		t.Fatal("no text explanation recorded for the safety-cap disable")
	}
}

func TestPendingConfirmationsHandlesMultiplePending(t *testing.T) {
	svc, sess := newTestSession(t)
	appendFunctionCall(t, svc, sess, "wrap-1", toolconfirmation.FunctionCallName, nil)
	appendFunctionCall(t, svc, sess, "wrap-2", toolconfirmation.FunctionCallName, nil)
	appendFunctionResponse(t, svc, sess, "wrap-1", toolconfirmation.FunctionCallName, map[string]any{"confirmed": true})

	pending := pendingConfirmations(reGet(t, svc, sess))
	if len(pending) != 1 || pending[0] != "wrap-2" {
		t.Fatalf("pendingConfirmations = %v, want [wrap-2]", pending)
	}
}
