package toolorder

import (
	"sync"
	"time"

	"google.golang.org/adk/v2/agent"
)

// registryTTL bounds how long a registered batch can sit unresolved (e.g.
// the core process restarted between a call pausing for confirmation and
// the resume that never came) before its tickets are swept away.
const registryTTL = 10 * time.Minute

var (
	registryMu sync.Mutex
	registry   = map[string]*ticket{}
)

// batch tracks, for one group of FunctionCalls the model emitted together,
// which positions have merely paused (asked for confirmation, not yet
// resolved) versus reached a final outcome. See readyLocked for why this
// two-state model -- not a simple "done" flag -- is what lets the same
// gate work safely across both ADK's confirmation ask pass and its later
// resume pass without deadlocking either one.
type batch struct {
	mu        sync.Mutex
	cond      *sync.Cond
	paused    []bool
	done      []bool
	createdAt time.Time
}

func newBatch(total int) *batch {
	b := &batch{
		paused:    make([]bool, total),
		done:      make([]bool, total),
		createdAt: time.Now(),
	}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// readyLocked reports whether ticket position may proceed. strict selects
// which notion of "the call ahead of me is out of the way" applies:
//
//   - strict=false (an ask-phase attempt, or a never-gated call's only
//     attempt): proceed once every earlier call has reached *some* decision,
//     paused or done. paused is enough here because an earlier gated call's
//     real completion structurally cannot happen until a later, separate
//     resume pass -- waiting for done here would deadlock this pass's
//     wg.Wait() (see package doc).
//   - strict=true (a resume-phase attempt, i.e. ctx.ToolConfirmation() != nil):
//     proceed only once every earlier call is actually done. paused alone
//     isn't enough -- that's stale state left over from the ask phase, not
//     evidence the earlier call's real work has run yet in *this* pass. This
//     is what actually prevents e.g. editFile's real handler from starting
//     before writeFile's real handler has finished.
//
// Callers must hold b.mu.
func (b *batch) readyLocked(position int, strict bool) bool {
	for j := range position {
		if b.done[j] {
			continue
		}
		if !strict && b.paused[j] {
			continue
		}
		return false
	}
	return true
}

// ticket is one call's claim on a position within a batch.
type ticket struct {
	b        *batch
	position int
}

func registryKey(sessionID, functionCallID string) string {
	return sessionID + "\x00" + functionCallID
}

// registerBatch records the ordering for a freshly-observed model response
// containing 2+ FunctionCalls, keyed by session + call ID rather than
// InvocationID -- a confirmation resume is plausibly a new invocation, but
// FunctionCall.ID and SessionID are stable across it. Also opportunistically
// evicts anything older than registryTTL so an abandoned batch doesn't leak.
func registerBatch(sessionID string, callIDs []string) {
	registryMu.Lock()
	defer registryMu.Unlock()

	now := time.Now()
	for key, t := range registry {
		if now.Sub(t.b.createdAt) > registryTTL {
			delete(registry, key)
		}
	}

	b := newBatch(len(callIDs))
	for i, id := range callIDs {
		registry[registryKey(sessionID, id)] = &ticket{b: b, position: i}
	}
}

func lookupTicket(ctx agent.Context) *ticket {
	registryMu.Lock()
	defer registryMu.Unlock()
	return registry[registryKey(ctx.SessionID(), ctx.FunctionCallID())]
}

func deleteTicket(ctx agent.Context) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, registryKey(ctx.SessionID(), ctx.FunctionCallID()))
}

// waitTurn blocks until t's position is ready under strict (see
// readyLocked), or ctx is done first. sync.Cond has no native context
// support, so cancellation is wired up with a short-lived watcher goroutine
// that broadcasts when ctx.Done() fires; it's torn down via stop once the
// wait loop exits either way.
func waitTurn(ctx agent.Context, t *ticket, strict bool) error {
	b := t.b
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			b.mu.Lock()
			b.cond.Broadcast()
			b.mu.Unlock()
		case <-stop:
		}
	}()

	b.mu.Lock()
	defer b.mu.Unlock()
	for !b.readyLocked(t.position, strict) {
		if err := ctx.Err(); err != nil {
			return err
		}
		b.cond.Wait()
	}
	return nil
}

func markPaused(t *ticket) {
	b := t.b
	b.mu.Lock()
	b.paused[t.position] = true
	b.cond.Broadcast()
	b.mu.Unlock()
}

func markDone(t *ticket) {
	b := t.b
	b.mu.Lock()
	b.done[t.position] = true
	b.cond.Broadcast()
	b.mu.Unlock()
}
