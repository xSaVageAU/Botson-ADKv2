package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"

	"botsonv2/internal/interface/natscore"

	"github.com/nats-io/nats.go"
	"google.golang.org/genai"
)

// Run starts (or resumes) a turn and streams back decoded Events, shaped
// like runner.Runner.Run's own iterator so callers built against that need
// minimal changes. Unlike the plain request/reply calls in client.go, a
// run can yield many events for one request, so this manually manages a
// reply inbox (natscore.Frame per message, terminated by a Done or Error
// frame) instead of using the single-reply nc.Request convenience.
func (c *Client) Run(ctx context.Context, appName, userID, sessionID string, msg *genai.Content) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		body, err := json.Marshal(natscore.RunRequest{
			AppName:    appName,
			UserID:     userID,
			SessionID:  sessionID,
			NewMessage: msg,
		})
		if err != nil {
			yield(nil, err)
			return
		}

		inbox := c.nc.NewInbox()
		sub, err := c.nc.SubscribeSync(inbox)
		if err != nil {
			yield(nil, wrapRequestErr(err))
			return
		}
		defer sub.Unsubscribe()

		if err := c.nc.PublishRequest(natscore.SubjectRun, inbox, body); err != nil {
			yield(nil, wrapRequestErr(err))
			return
		}

		first := true
		for {
			frameMsg, err := sub.NextMsgWithContext(ctx)
			if err != nil {
				yield(nil, wrapRequestErr(err))
				return
			}

			// A manual publish-with-reply doesn't get nats.go's automatic
			// no-responders translation the way nc.Request does -- if
			// nothing was subscribed to SubjectRun at all, the server
			// itself delivers this synthetic status frame to our inbox
			// instead of a real Frame, but only ever as the very first
			// message.
			if first && frameMsg.Header.Get("Status") == "503" {
				yield(nil, wrapRequestErr(nats.ErrNoResponders))
				return
			}
			first = false

			var frame natscore.Frame
			if err := json.Unmarshal(frameMsg.Data, &frame); err != nil {
				yield(nil, err)
				return
			}
			if frame.Error != "" {
				yield(nil, fmt.Errorf("core returned an error: %s", frame.Error))
				return
			}
			if frame.Done {
				return
			}
			if frame.Event != nil && !yield(frame.Event, nil) {
				return
			}
		}
	}
}
