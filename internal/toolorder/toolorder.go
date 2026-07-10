// Package toolorder is an ADK plugin that serializes the real execution of
// FunctionCalls the model emitted together in one turn (ADK always dispatches
// these in parallel goroutines -- see internal/llminternal/base_flow.go's
// handleFunctionCalls in the vendored ADK v2 source -- there is no config
// knob for sequential dispatch).
//
// Without this, calls resumed together after a batch of HITL confirmations
// are answered (e.g. writeFile + editFile, both approved in the same round
// trip) execute concurrently, racing to touch the same file, and a non-gated
// call emitted after a gated one (e.g. readFile after writeFile) executes
// immediately, before the gated call's real effect has landed. This plugin
// makes every call in the batch execute in the order the model emitted them.
//
// # How it works
//
// AfterModelCallback records the position of every FunctionCall in a fresh
// model response (2+ calls only -- nothing to serialize for one). A gated
// tool's first pass is just a fast, side-effect-free "ask" (RequireConfirmation
// tools return tool.ErrConfirmationRequired instead of running, with no
// ToolConfirmation on the context yet); a later resume pass -- driven by ADK
// once every queued confirmation has been answered -- calls the same tool
// again with ctx.ToolConfirmation() now non-nil, and that call either really
// runs or is rejected; it can never re-ask.
//
// BeforeToolCallback uses that same ToolConfirmation() signal to pick which
// notion of "the call ahead of me is out of the way" applies (see
// readyLocked for the exact predicate):
//   - during an ask (ToolConfirmation() == nil): wait only for earlier calls
//     to reach *some* decision, paused or done -- never their real
//     completion. An earlier gated call's real completion structurally can't
//     happen until a later, separate resume pass, and that resume pass can't
//     even begin until this whole ask pass's wg.Wait() returns -- so
//     blocking on it here would deadlock the turn.
//   - during a resume (ToolConfirmation() != nil): wait for earlier calls to
//     be *actually* done. This is what actually prevents e.g. editFile's
//     real handler from starting before writeFile's has finished -- safe to
//     block on here because, thanks to Botson-TUI's confirmation queue,
//     every gated call in the batch resumes together in this same pass, so
//     nothing this waits on is stuck in some other, later round trip.
//
// # Deferred (synthetic) confirmations
//
// The ask-phase wait alone leaves one hole: a non-gated call (e.g. readFile)
// positioned after a gated call would really run during the ask pass, before
// the gated call's approved effect lands. Blocking it until the gated call
// finishes would deadlock the turn (see above), so instead, once the ask-phase
// wait clears and some earlier call is paused awaiting confirmation, this
// plugin pauses the non-gated call the exact same way a gated tool pauses
// itself: BeforeToolCallback calls ctx.RequestConfirmation with a payload of
// {DeferredPayloadKey: true} and returns tool.ErrConfirmationRequired, so the
// real tool handler never runs in the ask pass. The call then rides ADK's
// normal confirmation resume alongside its gated siblings, where the strict
// wait serializes it into its emitted position.
//
// The payload marker is the contract with clients: a confirmation whose
// payload carries DeferredPayloadKey == true exists only for ordering, not
// for human judgment, and the client must answer it {"confirmed": true}
// immediately, batched with the user's answers to that turn's real
// confirmations (Botson-TUI does this in its confirmation queue). A gated
// call in the same spot is left alone -- its own tool.Run pauses it with a
// real, human-facing confirmation -- which is why New takes the
// requiresConfirmation predicate (wired to internal/agent's registry).
package toolorder

import (
	"errors"
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/tool"
)

// DeferredPayloadKey marks a synthetic, ordering-only confirmation's payload.
// Clients must auto-answer {"confirmed": true} for confirmations whose
// payload has this key set to true, instead of prompting a human -- see the
// package doc and AGENTS.md's "HITL confirmation wire protocol".
const DeferredPayloadKey = "botsonToolOrderDeferred"

// orderPlugin carries the one piece of configuration the callbacks need:
// which tools pause for a real HITL confirmation on their own (and so must
// never be given a synthetic one, or the human approval would be skipped).
type orderPlugin struct {
	requiresConfirmation func(toolName string) bool
}

// New returns the ToolOrderPlugin, ready to add to a runner.PluginConfig.
// requiresConfirmation reports whether the named tool is built with
// RequireConfirmation: true (wire it to internal/agent.RequiresConfirmation);
// nil is treated as "no tool is gated".
func New(requiresConfirmation func(toolName string) bool) *plugin.Plugin {
	op := &orderPlugin{requiresConfirmation: requiresConfirmation}
	p, err := plugin.New(plugin.Config{
		Name:                "ToolOrderPlugin",
		AfterModelCallback:  afterModel,
		BeforeToolCallback:  op.beforeTool,
		AfterToolCallback:   afterTool,
		OnToolErrorCallback: onToolError,
	})
	if err != nil {
		// plugin.New has no error path in the current ADK version; a panic
		// here would only ever fire from a mistake in this file's Config.
		panic(err)
	}
	return p
}

func afterModel(ctx agent.Context, resp *model.LLMResponse, respErr error) (*model.LLMResponse, error) {
	if respErr != nil || resp == nil || resp.Content == nil {
		return nil, nil
	}
	var callIDs []string
	for _, part := range resp.Content.Parts {
		if part.FunctionCall != nil {
			callIDs = append(callIDs, part.FunctionCall.ID)
		}
	}
	if len(callIDs) < 2 {
		return nil, nil
	}
	registerBatch(ctx.SessionID(), callIDs)
	return nil, nil
}

func (op *orderPlugin) beforeTool(ctx agent.Context, t tool.Tool, _ map[string]any) (map[string]any, error) {
	tk := lookupTicket(ctx)
	if tk == nil {
		return nil, nil
	}
	// A non-nil ToolConfirmation means this call is being resumed after
	// approval/rejection (the same signal tool/functiontool.Run itself
	// switches on) -- i.e. this is a real, side-effecting attempt, not an
	// ask. See readyLocked for why that distinction changes what "the call
	// ahead of me is out of the way" means.
	strict := ctx.ToolConfirmation() != nil
	if err := waitTurn(ctx, tk, strict); err != nil {
		return nil, err
	}
	if strict {
		return nil, nil
	}
	// Ask phase, and every earlier call has now reached a decision. If none
	// of them is paused awaiting confirmation, this call's real execution is
	// truly next in line and may proceed. Otherwise, running it now would
	// land its effect before the paused calls' approved effects (the
	// readFile-after-writeFile case) -- so it must move to the resume pass.
	if !anyEarlierPaused(tk) {
		return nil, nil
	}
	var name string
	if t != nil {
		name = t.Name()
	}
	if op.requiresConfirmation != nil && op.requiresConfirmation(name) {
		// Gated: its own tool.Run is about to pause it with a real,
		// human-facing confirmation. Don't preempt that with a synthetic
		// one, or the human approval would be silently skipped.
		return nil, nil
	}
	if err := ctx.RequestConfirmation(
		fmt.Sprintf("Tool call %s() is deferred until this turn's earlier tool calls finish; no human approval is needed -- answer confirmed: true.", name),
		map[string]any{DeferredPayloadKey: true},
	); err != nil {
		return nil, err
	}
	// The wrapped sentinel's text ("requires confirmation, ...") is part of
	// the wire contract: clients detect ADK's bookkeeping functionResponse
	// by that substring (see AGENTS.md step 2 of the HITL sequence).
	return nil, fmt.Errorf("error tool %q deferred behind earlier calls in this turn: %w", name, tool.ErrConfirmationRequired)
}

func afterTool(ctx agent.Context, _ tool.Tool, _, _ map[string]any, err error) (map[string]any, error) {
	t := lookupTicket(ctx)
	if t == nil {
		return nil, nil
	}
	if errors.Is(err, tool.ErrConfirmationRequired) {
		markPaused(t)
		return nil, nil
	}
	markDone(t)
	deleteTicket(ctx)
	return nil, nil
}

// onToolError marks a call's position done when its error is final. This is
// belt-and-braces for the one dispatch path that never reaches the
// after-tool callbacks at all -- ADK's tool-not-found handling
// (base_flow.go's handleFunctionCalls) runs only the on-error callbacks --
// where an unresolved position would otherwise block every later sibling's
// ask until the turn's context deadline. ErrConfirmationRequired is not
// final (the call pauses and resumes later), so it stays with afterTool's
// markPaused. On the normal callTool path this runs before afterTool and
// deleteTicket makes the later afterTool a no-op.
func onToolError(ctx agent.Context, _ tool.Tool, _ map[string]any, err error) (map[string]any, error) {
	if errors.Is(err, tool.ErrConfirmationRequired) {
		return nil, nil
	}
	if t := lookupTicket(ctx); t != nil {
		markDone(t)
		deleteTicket(ctx)
	}
	return nil, nil
}
