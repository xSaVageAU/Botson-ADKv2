package apiclient

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"iter"
	"strings"
)

// parseSSE reads Server-Sent-Event frames from r, yielding a decoded
// Event for each "data: {...}" line. A direct port of the same line-based
// parsing internal/interface/web/webui/static/js/chat.js already does for
// this exact endpoint -- no blank-line frame delimiter is required, only
// a bare "data: " prefix per line, matching what both clients actually
// receive from /api/run_sse.
func parseSSE(ctx context.Context, r io.Reader) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		scanner := bufio.NewScanner(r)
		// SSE lines carrying a large tool result can exceed bufio.Scanner's
		// default 64KB token limit; grow it generously rather than
		// silently truncating or erroring on a long line.
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				yield(nil, err)
				return
			}

			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if payload == "" {
				continue
			}

			var ev Event
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				if !yield(nil, err) {
					return
				}
				continue
			}
			if !yield(&ev, nil) {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			yield(nil, err)
		}
	}
}
