package ephemeral

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStartOnceReplay(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db")
	a := open(t, p)
	b := open(t, p)
	defer b.Close()
	check(t, a.Register("job", 1, []Step{{Kind: "signal", Name: "done"}}))
	type outcome struct {
		created bool
		err     error
	}
	out := make(chan outcome, 2)
	for _, s := range []*Store{a, b} {
		go func() { v, e := s.StartOnce("r", "job", 1, "q", input, nil); out <- outcome{v, e} }()
	}
	x, y := <-out, <-out
	check(t, x.err)
	check(t, y.err)
	if x.created == y.created {
		t.Fatal(x, y)
	}
	check(t, a.Signal("r", "1", "done", json.RawMessage("true")))
	check(t, a.SetAttributes("r", map[string]string{"edited": "yes"}))
	a.Close()
	fresh := open(t, p)
	defer fresh.Close()
	v, e := fresh.StartOnce("r", "job", 1, "q", input, map[string]string{})
	check(t, e)
	if v {
		t.Fatal("created duplicate")
	}
	_, e = fresh.StartOnce("r", "job", 1, "new-queue", input, nil)
	if !errors.Is(e, ErrStartConflict) {
		t.Fatal(e)
	}
	check(t, fresh.Start("legacy", "job", 1, "q", input, nil))
	_, e = fresh.StartOnce("legacy", "job", 1, "q", input, nil)
	if !errors.Is(e, ErrStartConflict) {
		t.Fatal(e)
	}
}
func TestIdlePollDuringWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db")
	a := open(t, p)
	defer a.Close()
	b := open(t, p)
	defer b.Close()
	check(t, b.exec("PRAGMA busy_timeout=0"))
	check(t, a.exec("BEGIN IMMEDIATE"))
	defer a.exec("ROLLBACK")
	v, e := b.Poll("q", time.Hour)
	check(t, e)
	if v != nil {
		t.Fatal(v)
	}
}
func TestRetryCalendar(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	check(t, s.Register("job", 1, []Step{{Kind: "activity", Name: "call", Attempts: 3, RetryDelays: []int64{0, 60000}}}))
	check(t, s.Start("r", "job", 1, "q", input, nil))
	for i := 0; i < 2; i++ {
		v, e := s.Poll("q", time.Hour)
		check(t, e)
		if v == nil {
			t.Fatal("missing activity")
		}
		check(t, s.Finish("r", v.Token, nil, "transient"))
	}
	due, ok, e := s.NextDue("q")
	check(t, e)
	if !ok || due < now()+59000 {
		t.Fatal(due)
	}
	check(t, s.Start("p", "job", 1, "q", input, nil))
	v, e := s.Poll("q", time.Hour)
	check(t, e)
	if v == nil || v.Run != "p" {
		t.Fatal(v)
	}
	if s.FailPermanent("p", "wrong", "reject") == nil {
		t.Fatal("unfenced")
	}
	check(t, s.FailPermanent("p", v.Token, "reject"))
	d, e := s.Describe("p")
	check(t, e)
	if d.State != "failed" || d.DueMillis != 0 {
		t.Fatal(d)
	}
}

func TestScopedRecoveryPages(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	check(t, s.Register("job", 1, []Step{{Kind: "signal", Name: "resume"}}))
	for _, id := range []string{"a", "b", "c", "d"} {
		scope := "alpha"
		if id == "b" {
			scope = "beta"
		}
		check(t, s.Start(id, "job", 1, "q", input, map[string]string{"scope": scope}))
	}
	check(t, s.Cancel("a"))
	r, e := s.FindInState("scope", "alpha", "waiting", "", 1)
	check(t, e)
	if len(r) != 1 || r[0] != "c" {
		t.Fatal(r)
	}
	r, e = s.FindInState("scope", "alpha", "waiting", "c", 1)
	check(t, e)
	if len(r) != 1 || r[0] != "d" {
		t.Fatal(r)
	}
}
