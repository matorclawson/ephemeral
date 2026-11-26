package ephemeral

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

// Every compared field must conflict on its own, and the original request must still
// be recognized afterwards.
func TestStartOnceComparedFields(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	steps := []Step{{Kind: "signal", Name: "done"}}
	check(t, s.Register("job", 1, steps))
	check(t, s.Register("job", 2, steps))
	check(t, s.Register("other", 1, steps))
	attrs := map[string]string{"scope": "alpha", "owner": "a"}
	v, e := s.StartOnce("r", "job", 1, "q", input, attrs)
	check(t, e)
	if !v {
		t.Fatal("not created")
	}

	for label, c := range map[string]struct {
		name    string
		version int
		input   json.RawMessage
		attrs   map[string]string
	}{
		"name":              {"other", 1, input, attrs},
		"version":           {"job", 2, input, attrs},
		"input value":       {"job", 1, json.RawMessage(`{"ok":false}`), attrs},
		"input whitespace":  {"job", 1, json.RawMessage(`{"ok": true}`), attrs},
		"attribute value":   {"job", 1, input, map[string]string{"scope": "beta", "owner": "a"}},
		"attribute missing": {"job", 1, input, map[string]string{"scope": "alpha"}},
		"attribute added":   {"job", 1, input, map[string]string{"scope": "alpha", "owner": "a", "x": ""}},
		"no attributes":     {"job", 1, input, nil},
	} {
		v, e = s.StartOnce("r", c.name, c.version, "q", c.input, c.attrs)
		if v || !errors.Is(e, ErrStartConflict) {
			t.Fatal(label, v, e)
		}
	}

	v, e = s.StartOnce("r", "job", 1, "q", input, map[string]string{"owner": "a", "scope": "alpha"})
	check(t, e)
	if v {
		t.Fatal("created duplicate")
	}
}

// A rejected submission must not bind its run ID.
func TestStartOnceFailureLeavesNoReceipt(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	v, e := s.StartOnce("r", "job", 1, "q", input, nil)
	if v || e == nil || errors.Is(e, ErrStartConflict) {
		t.Fatal("unregistered definition", v, e)
	}
	check(t, s.Register("job", 1, []Step{{Kind: "signal", Name: "done"}}))
	v, e = s.StartOnce("r", "job", 1, "q", input, map[string]string{"": "x"})
	if v || e == nil || errors.Is(e, ErrStartConflict) {
		t.Fatal("empty attribute key", v, e)
	}
	v, e = s.StartOnce("r", "job", 1, "q", input, nil)
	check(t, e)
	if !v {
		t.Fatal("run ID was bound by a failed submission")
	}
}
