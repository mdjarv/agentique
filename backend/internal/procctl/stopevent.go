package procctl

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
)

// ErrNoStopListener means no running server is listening for stop requests for
// that data dir — it is down, predates this mechanism, or (on unix) stop
// requests ride signals instead. Callers fall back to their platform's
// terminate path.
var ErrNoStopListener = errors.New("no server listening for stop requests")

// stopEventName derives the per-data-dir name of the stop-request kernel event
// (Windows). Keyed by data dir for the same reason the instance lock is: the
// data dir, not the listen address, is what identifies a server instance. The
// `Global\` namespace makes it reachable across logon sessions (an SSH session
// stopping the interactive-session server); the default DACL of a user-created
// event still limits EVENT_MODIFY_STATE to the creating user, SYSTEM and
// administrators, so another local user cannot stop the server with it.
//
// The path is hashed rather than embedded: kernel object names reject
// backslashes past the namespace prefix, and Clean+hash makes trailing-slash
// variants of one dir agree.
func stopEventName(dataDir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(dataDir)))
	return `Global\agentique-stop-` + hex.EncodeToString(sum[:8])
}
