package procctl

import (
	"strings"
	"testing"
)

func TestStopEventName(t *testing.T) {
	a := stopEventName(`/home/u/.local/share/agentique`)
	b := stopEventName(`/home/u/.local/share/agentique/`)
	if a != b {
		t.Errorf("trailing slash changed the name: %q vs %q", a, b)
	}
	if a == stopEventName(`/tmp/other`) {
		t.Error("different data dirs derived the same event name")
	}
	if !strings.HasPrefix(a, `Global\agentique-stop-`) {
		t.Errorf("name %q missing the Global namespace prefix", a)
	}
	// Kernel object names reject backslashes past the namespace prefix; the
	// hashed suffix must stay hex.
	suffix := strings.TrimPrefix(a, `Global\agentique-stop-`)
	if len(suffix) != 16 || strings.ContainsAny(suffix, `\/`) {
		t.Errorf("suffix %q is not a 16-char hex hash", suffix)
	}
}
