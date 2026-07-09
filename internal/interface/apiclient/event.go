package apiclient

import "botsonv2/internal/interface/natscore"

// Event and SessionInfo are the wire types internal/interface/natscore
// defines, aliased here so callers built against this package's exported
// API don't need to import natscore directly.
type Event = natscore.Event
type SessionInfo = natscore.SessionInfo
