// Conformance tests state guarantees through the public API only, so that another
// storage engine can be held to the same behavior.
package ephemeral_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rodriveracom/ephemeral"
)

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}

func newStore(t *testing.T, path string) *ephemeral.Store {
	t.Helper()
	s, e := ephemeral.Open(path)
	must(t, e)
	t.Cleanup(s.Close)
	return s
}

func fresh(t *testing.T) *ephemeral.Store {
	t.Helper()
	return newStore(t, filepath.Join(t.TempDir(), "db"))
}

func lease(t *testing.T, s *ephemeral.Store, queue string, d time.Duration) *ephemeral.Task {
	t.Helper()
	task, e := s.Poll(queue, d)
	must(t, e)
	if task == nil {
		t.Fatal("no activity on", queue)
	}
	return task
}

func idle(t *testing.T, s *ephemeral.Store, queue string) {
	t.Helper()
	task, e := s.Poll(queue, time.Hour)
	must(t, e)
	if task != nil {
		t.Fatal("unexpected activity", task)
	}
}

func state(t *testing.T, s *ephemeral.Store, id, want string) {
	t.Helper()
	d, e := s.Describe(id)
	must(t, e)
	if d.State != want {
		t.Fatalf("%s is %s, want %s", id, d.State, want)
	}
}

var one = []ephemeral.Step{{Kind: "activity", Name: "call"}}
var in = json.RawMessage(`{}`)

// With Attempts unset an activity is leased once: an expired lease fails the run.
func TestDefaultIsOneAttempt(t *testing.T) {
	s := fresh(t)
	must(t, s.Register("job", 1, one))
	must(t, s.Start("r", "job", 1, "q", in, nil))
	task := lease(t, s, "q", 20*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	idle(t, s, "q")
	state(t, s, "r", "failed")
	if s.Finish("r", task.Token, in, "") == nil {
		t.Fatal("finished after failure")
	}
}

func TestHeartbeatExtendsLiveLeaseOnly(t *testing.T) {
	s := fresh(t)
	must(t, s.Register("job", 1, one))
	must(t, s.Start("r", "job", 1, "q", in, nil))
	task := lease(t, s, "q", 500*time.Millisecond)
	if s.Heartbeat("r", "wrong", time.Hour) == nil {
		t.Fatal("heartbeat with a wrong token")
	}
	must(t, s.Heartbeat("r", task.Token, time.Hour))
	time.Sleep(800 * time.Millisecond)
	idle(t, s, "q")
	must(t, s.Finish("r", task.Token, in, ""))
	state(t, s, "r", "completed")

	must(t, s.Start("late", "job", 1, "q2", in, nil))
	task = lease(t, s, "q2", 20*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	if s.Heartbeat("late", task.Token, time.Hour) == nil {
		t.Fatal("expired lease renewed")
	}
}

func TestLiveLeaseSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s := newStore(t, path)
	must(t, s.Register("job", 1, one))
	must(t, s.Start("r", "job", 1, "q", in, nil))
	task := lease(t, s, "q", time.Hour)
	s.Close()
	s = newStore(t, path)
	idle(t, s, "q")
	must(t, s.Finish("r", task.Token, in, ""))
	state(t, s, "r", "completed")
}

func TestBufferedSignalsAreConsumedInArrivalOrder(t *testing.T) {
	s := fresh(t)
	must(t, s.Register("job", 1, []ephemeral.Step{
		{Kind: "activity", Name: "first"},
		{Kind: "signal", Name: "go"},
		{Kind: "activity", Name: "second"},
		{Kind: "signal", Name: "go"},
		{Kind: "activity", Name: "third"},
	}))
	must(t, s.Start("r", "job", 1, "q", in, nil))
	task := lease(t, s, "q", time.Hour)
	// Keys sort in the opposite order to arrival.
	must(t, s.Signal("r", "z", "go", json.RawMessage("1")))
	must(t, s.Signal("r", "a", "go", json.RawMessage("2")))
	must(t, s.Finish("r", task.Token, in, ""))
	for _, want := range []string{"1", "2"} {
		task = lease(t, s, "q", time.Hour)
		if string(task.Input) != want {
			t.Fatalf("input %s, want %s", task.Input, want)
		}
		must(t, s.Finish("r", task.Token, in, ""))
	}
	state(t, s, "r", "completed")
}

func TestClosedRunStillRecognizesRepeats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s := newStore(t, path)
	must(t, s.Register("job", 1, []ephemeral.Step{{Kind: "signal", Name: "go"}}))
	created, e := s.StartOnce("r", "job", 1, "q", in, nil)
	must(t, e)
	if !created {
		t.Fatal("not created")
	}
	must(t, s.Signal("r", "k", "go", in))
	state(t, s, "r", "completed")
	s.Close()
	s = newStore(t, path)
	must(t, s.Signal("r", "k", "go", in))
	if s.Signal("r", "k", "go", json.RawMessage("1")) == nil {
		t.Fatal("changed signal accepted")
	}
	if s.Signal("r", "new", "go", in) == nil {
		t.Fatal("new signal accepted by a closed run")
	}
	created, e = s.StartOnce("r", "job", 1, "q", in, nil)
	must(t, e)
	if created {
		t.Fatal("closed run created again")
	}
	_, e = s.StartOnce("r", "job", 1, "other", in, nil)
	if !errors.Is(e, ephemeral.ErrStartConflict) {
		t.Fatal(e)
	}
}

func TestHistoryAndListPages(t *testing.T) {
	s := fresh(t)
	must(t, s.Register("job", 1, one))
	for _, id := range []string{"c", "a", "b"} {
		must(t, s.Start(id, "job", 1, "q", in, nil))
	}
	all, e := s.History("a", 0, 1000)
	must(t, e)
	var paged []ephemeral.Event
	var after int64
	for len(paged) <= len(all) {
		page, e := s.History("a", after, 1)
		must(t, e)
		if len(page) == 0 {
			break
		}
		if page[0].Sequence <= after {
			t.Fatal("sequence did not advance", page)
		}
		after = page[0].Sequence
		paged = append(paged, page...)
	}
	if len(all) < 2 || !slices.Equal(all, paged) {
		t.Fatal(all, paged)
	}

	must(t, s.Cancel("b"))
	for _, c := range []struct {
		state, after string
		want         []string
	}{
		{"", "", []string{"a", "b"}},
		{"", "b", []string{"c"}},
		{"ready", "", []string{"a", "c"}},
		{"cancelled", "", []string{"b"}},
	} {
		got, e := s.List(c.state, c.after, 2)
		must(t, e)
		if !slices.Equal(got, c.want) {
			t.Fatal(c.state, c.after, got)
		}
	}
	for _, limit := range []int{0, 1001} {
		if _, e := s.List("", "", limit); e == nil {
			t.Fatal("List limit accepted", limit)
		}
		if _, e := s.History("a", 0, limit); e == nil {
			t.Fatal("History limit accepted", limit)
		}
	}
}

func TestTimerDoesNotFireEarly(t *testing.T) {
	s := fresh(t)
	must(t, s.Register("job", 1, []ephemeral.Step{{Kind: "timer", Millis: 200}}))
	begin := time.Now()
	must(t, s.Start("r", "job", 1, "q", in, nil))
	for {
		idle(t, s, "q")
		d, e := s.Describe("r")
		must(t, e)
		if d.State == "completed" {
			break
		}
		if time.Since(begin) > 10*time.Second {
			t.Fatal("timer did not fire", d)
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Due times have millisecond resolution.
	if elapsed := time.Since(begin); elapsed < 199*time.Millisecond {
		t.Fatal("fired early", elapsed)
	}
}

func TestParallelWaitsForAllChildrenBeforeFailing(t *testing.T) {
	s := fresh(t)
	must(t, s.Register("leaf", 1, one))
	must(t, s.Register("root", 1, []ephemeral.Step{{Kind: "parallel", Children: []ephemeral.Child{
		{Name: "leaf", Version: 1, Queue: "a"},
		{Name: "leaf", Version: 1, Queue: "b"},
	}}}))
	must(t, s.Start("r", "root", 1, "parent", in, nil))
	a := lease(t, s, "a", time.Hour)
	b := lease(t, s, "b", time.Hour)
	must(t, s.Finish(a.Run, a.Token, nil, "broken"))
	idle(t, s, "parent")
	state(t, s, "r", "joining")
	must(t, s.Finish(b.Run, b.Token, in, ""))
	idle(t, s, "parent")
	state(t, s, "r", "failed")
}
