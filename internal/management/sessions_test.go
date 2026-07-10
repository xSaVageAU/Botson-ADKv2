package management

import (
	"context"
	"testing"

	"google.golang.org/adk/v2/session"
)

func TestSetSessionAutoMode(t *testing.T) {
	ctx := context.Background()
	svc := session.InMemoryService()

	createResp, err := svc.Create(ctx, &session.CreateRequest{AppName: "app", UserID: "user"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess := createResp.Session

	if err := SetSessionAutoMode(ctx, svc, sess.AppName(), sess.UserID(), sess.ID(), true, ""); err != nil {
		t.Fatalf("SetSessionAutoMode(true): %v", err)
	}

	getResp, err := svc.Get(ctx, &session.GetRequest{AppName: sess.AppName(), UserID: sess.UserID(), SessionID: sess.ID()})
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	val, err := getResp.Session.State().Get(AutoModeStateKey)
	if err != nil {
		t.Fatalf("State().Get(%q): %v", AutoModeStateKey, err)
	}
	if on, _ := val.(bool); !on {
		t.Fatalf("AutoModeStateKey = %v, want true", val)
	}

	const reason = "safety cap reached"
	if err := SetSessionAutoMode(ctx, svc, sess.AppName(), sess.UserID(), sess.ID(), false, reason); err != nil {
		t.Fatalf("SetSessionAutoMode(false, reason): %v", err)
	}

	getResp, err = svc.Get(ctx, &session.GetRequest{AppName: sess.AppName(), UserID: sess.UserID(), SessionID: sess.ID()})
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	val, err = getResp.Session.State().Get(AutoModeStateKey)
	if err != nil {
		t.Fatalf("State().Get(%q): %v", AutoModeStateKey, err)
	}
	if on, _ := val.(bool); on {
		t.Fatalf("AutoModeStateKey = %v, want false", val)
	}

	var found bool
	for evt := range getResp.Session.Events().All() {
		if evt.Content == nil {
			continue
		}
		for _, part := range evt.Content.Parts {
			if part.Text == reason {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("disable reason %q not found in session history", reason)
	}
}
