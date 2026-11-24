package ephemeral

import (
	"encoding/json"
	"errors"
	"fmt"
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

func TestStartOnceExactBytes(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	check(t, s.Register("job", 1, []Step{{Kind: "signal", Name: "done"}}))
	// json.Valid accepts malformed UTF-8 inside strings. These two bodies differ only
	// in such bytes and must not be recognized as the same request.
	first := json.RawMessage("\"\xff\"")
	second := json.RawMessage("\"\xfe\"")
	if !json.Valid(first) || !json.Valid(second) {
		t.Fatal("test premise: json.Valid must accept these bodies")
	}
	v, e := s.StartOnce("r", "job", 1, "q", first, nil)
	check(t, e)
	if !v {
		t.Fatal("not created")
	}
	v, e = s.StartOnce("r", "job", 1, "q", first, nil)
	check(t, e)
	if v {
		t.Fatal("replay created a duplicate")
	}
	_, e = s.StartOnce("r", "job", 1, "q", second, nil)
	if !errors.Is(e, ErrStartConflict) {
		t.Fatal(e)
	}
	_, e = s.StartOnce("r", "job", 1, "q", first, map[string]string{"k": "\xff"})
	if !errors.Is(e, ErrStartConflict) {
		t.Fatal(e)
	}
}

func TestFixedBackoffAndRetryValidation(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	// Old definitions: Attempts plus a fixed BackoffMillis, no RetryDelays.
	check(t, s.Register("fixed", 1, []Step{{Kind: "activity", Name: "call", Attempts: 2, BackoffMillis: 60000}}))
	check(t, s.Start("f", "fixed", 1, "fixed-q", input, nil))
	v, e := s.Poll("fixed-q", time.Hour)
	check(t, e)
	if v == nil {
		t.Fatal("missing activity")
	}
	check(t, s.Finish("f", v.Token, nil, "transient"))
	due, ok, e := s.NextDue("fixed-q")
	check(t, e)
	if !ok || due < now()+59000 || due > now()+61000 {
		t.Fatal(due, ok)
	}
	// Lease expiry uses the same per-attempt delay as an explicit failure.
	check(t, s.Register("lease", 1, []Step{{Kind: "activity", Name: "call", Attempts: 2, RetryDelays: []int64{60000}}}))
	check(t, s.Start("l", "lease", 1, "lease-q", input, nil))
	v, e = s.Poll("lease-q", time.Millisecond)
	check(t, e)
	if v == nil {
		t.Fatal("missing activity")
	}
	time.Sleep(20 * time.Millisecond)
	v, e = s.Poll("lease-q", time.Hour)
	check(t, e)
	if v != nil {
		t.Fatal("retry must wait for its delay", v)
	}
	due, ok, e = s.NextDue("lease-q")
	check(t, e)
	if !ok || due < now()+59000 {
		t.Fatal(due, ok)
	}
	for name, step := range map[string]Step{
		"too few delays":    {Kind: "activity", Name: "a", Attempts: 3, RetryDelays: []int64{1}},
		"too many delays":   {Kind: "activity", Name: "a", Attempts: 2, RetryDelays: []int64{1, 2}},
		"with fixed":        {Kind: "activity", Name: "a", Attempts: 2, RetryDelays: []int64{1}, BackoffMillis: 5},
		"negative":          {Kind: "activity", Name: "a", Attempts: 2, RetryDelays: []int64{-1}},
		"not an activity":   {Kind: "timer", Millis: 1, Attempts: 2, RetryDelays: []int64{1}},
		"beyond the cutoff": {Kind: "activity", Name: "a", Attempts: 2, RetryDelays: []int64{315360000001}},
	} {
		if s.Register("bad-"+name, 1, []Step{step}) == nil {
			t.Fatal("accepted", name)
		}
	}
}

func TestTwoWorkersOneLease(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db")
	a := open(t, p)
	defer a.Close()
	b := open(t, p)
	defer b.Close()
	check(t, a.Register("job", 1, []Step{{Kind: "activity", Name: "call"}}))
	for round := 0; round < 20; round++ {
		id := fmt.Sprint("r", round)
		queue := fmt.Sprint("q", round)
		check(t, a.Start(id, "job", 1, queue, input, nil))
		tasks := make(chan *Task, 2)
		errs := make(chan error, 2)
		for _, s := range []*Store{a, b} {
			go func(s *Store) {
				v, e := s.Poll(queue, time.Hour)
				tasks <- v
				errs <- e
			}(s)
		}
		got := 0
		for i := 0; i < 2; i++ {
			v := <-tasks
			check(t, <-errs)
			if v != nil {
				got++
			}
		}
		if got != 1 {
			t.Fatal("round", round, "leases", got)
		}
	}
}

func TestNextDueEmptyAndOrdered(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	_, ok, e := s.NextDue("none")
	check(t, e)
	if ok {
		t.Fatal("empty queue reported work")
	}
	check(t, s.Register("late", 1, []Step{{Kind: "timer", Millis: 60000}}))
	check(t, s.Register("now", 1, []Step{{Kind: "activity", Name: "call"}}))
	check(t, s.Start("late", "late", 1, "q", input, nil))
	due, ok, e := s.NextDue("q")
	check(t, e)
	if !ok || due < now()+59000 {
		t.Fatal(due, ok)
	}
	check(t, s.Start("soon", "now", 1, "q", input, nil))
	due, ok, e = s.NextDue("q")
	check(t, e)
	if !ok || due > now()+1000 {
		t.Fatal(due, ok)
	}
}

// A database written by 0.2.1 has no start_requests table and carries the old index.
func TestOldDatabaseResumes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db")
	s := open(t, p)
	check(t, s.Register("wait", 1, []Step{{Kind: "signal", Name: "go"}}))
	check(t, s.Register("act", 1, []Step{{Kind: "activity", Name: "call"}}))
	check(t, s.Start("waiting", "wait", 1, "q", input, nil))
	check(t, s.Start("working", "act", 1, "q", input, nil))
	task, e := s.Poll("q", time.Hour)
	check(t, e)
	if task == nil || task.Run != "working" {
		t.Fatal(task)
	}
	check(t, s.exec("DROP TABLE start_requests"))
	check(t, s.exec("DROP INDEX runnable_due"))
	check(t, s.exec("CREATE INDEX ready ON runs(queue,state,due,id)"))
	s.Close()

	s = open(t, p)
	defer s.Close()
	r, e := s.query("SELECT name FROM sqlite_master WHERE type='index' AND name IN ('ready','runnable_due') ORDER BY name")
	check(t, e)
	if len(r) != 1 || r[0][0] != "runnable_due" {
		t.Fatal(r)
	}
	check(t, s.Signal("waiting", "1", "go", json.RawMessage("true")))
	check(t, s.Finish("working", task.Token, json.RawMessage("1"), ""))
	for _, id := range []string{"waiting", "working"} {
		d, e := s.Describe(id)
		check(t, e)
		if d.State != "completed" {
			t.Fatal(id, d.State)
		}
	}
	v, e := s.StartOnce("fresh", "wait", 1, "q", input, nil)
	check(t, e)
	if !v {
		t.Fatal("StartOnce unavailable after upgrade")
	}
	_, e = s.StartOnce("waiting", "wait", 1, "q", input, nil)
	if !errors.Is(e, ErrStartConflict) {
		t.Fatal(e)
	}
}
