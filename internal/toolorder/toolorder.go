// Package toolorder is an ADK plugin that serializes the real execution of
// FunctionCalls the model emitted together in one turn (ADK always dispatches
// these in parallel goroutines -- see internal/llminternal/base_flow.go's
// handleFunctionCalls in the vendored ADK v2 source -- there is no config
// knob for sequential dispatch).
//
// Without this, calls resumed together after a batch of HITL confirmations
// are answered (e.g. writeFile + editFile, both approved in the same round
// trip) execute concurrently, racing to touch the same file. This plugin
// makes them execute in the order the model emitted them instead.
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
// # Known limitation
//
// A non-gated, read-only call (e.g. readFile) placed after a gated call in
// the same turn can still run before that gated call's real effect lands,
// because deferring it past the ask pass would deadlock the turn (see above).
// Every mutating Botson tool requires confirmation, so this can only ever
// produce a stale read within the same turn -- never a corrupted write.
// Fixing it fully would require forking ADK's own dispatch loop.
package toolorder

import (
	"errors"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/tool"
)

// New returns the ToolOrderPlugin, ready to add to a runner.PluginConfig.
func New() *plugin.Plugin {
	p, err := plugin.New(plugin.Config{
		Name:               "ToolOrderPlugin",
		AfterModelCallback: afterModel,
		BeforeToolCallback: beforeTool,
		AfterToolCallback:  afterTool,
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

func beforeTool(ctx agent.Context, _ tool.Tool, _ map[string]any) (map[string]any, error) {
	t := lookupTicket(ctx)
	if t == nil {
		return nil, nil
	}
	// A non-nil ToolConfirmation means this call is being resumed after
	// approval/rejection (the same signal tool/functiontool.Run itself
	// switches on) -- i.e. this is a real, side-effecting attempt, not an
	// ask. See readyLocked for why that distinction changes what "the call
	// ahead of me is out of the way" means.
	strict := ctx.ToolConfirmation() != nil
	if err := waitTurn(ctx, t, strict); err != nil {
		return nil, err
	}
	return nil, nil
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
