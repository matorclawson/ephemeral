// A reply can be lost after the operation committed. These tests make the call,
// discard its result, reopen and repeat, to pin what the caller then sees.
package ephemeral_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rodriveracom/ephemeral"
)

func reopen(t *testing.T, s *ephemeral.Store, path string) *ephemeral.Store {
	t.Helper()
	s.Close()
	return newStore(t, path)
}

func events(t *testing.T, s *ephemeral.Store, id, kind string) (data []string) {
	t.Helper()
	history, e := s.History(id, 0, 1000)
	must(t, e)
	for _, event := range history {
		if event.Kind == kind {
			data = append(data, event.Data)
		}
	}
	return data
}

func TestLostStartOnceReply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s := newStore(t, path)
	must(t, s.Register("job", 1, one))
	_, e := s.StartOnce("r", "job", 1, "q", in, nil)
	must(t, e)
	s = reopen(t, s, path)
	created, e := s.StartOnce("r", "job", 1, "q", in, nil)
	must(t, e)
	if created || len(events(t, s, "r", "WorkflowStarted")) != 1 {
		t.Fatal("started again")
	}
}

func describe(t *testing.T, s *ephemeral.Store, id string) ephemeral.Description {
	t.Helper()
	d, e := s.Describe(id)
	must(t, e)
	return d
}

// unchanged compares the progress of a run, which a reopen or a rejected call
// must not move.
func unchanged(t *testing.T, got, want ephemeral.Description) {
	t.Helper()
	if got.State != want.State || got.Step != want.Step || got.Attempt != want.Attempt ||
		got.DueMillis != want.DueMillis || !slices.Equal(got.Value, want.Value) {
		t.Fatal("run moved", got, want)
	}
}

// The token of a lost lease cannot be read back, so the attempt is spent when it
// expires. The lease is long first, so a slow reopen cannot expire it.
func TestLostPollReply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s := newStore(t, path)
	must(t, s.Register("job", 1, one))
	must(t, s.Start("r", "job", 1, "q", in, nil))
	lease(t, s, "q", time.Hour) // reply discarded
	before := describe(t, s, "r")
	s = reopen(t, s, path)
	unchanged(t, describe(t, s, "r"), before)
	idle(t, s, "q")
	unchanged(t, describe(t, s, "r"), before)
}

func TestLostPollReplyExpiry(t *testing.T) {
	for _, attempts := range []int{1, 2} {
		path := filepath.Join(t.TempDir(), "db")
		s := newStore(t, path)
		must(t, s.Register("job", 1, []ephemeral.Step{{Kind: "activity", Name: "call", Attempts: attempts}}))
		must(t, s.Start("r", "job", 1, "q", in, nil))
		lease(t, s, "q", 50*time.Millisecond) // reply discarded
		due := describe(t, s, "r").DueMillis
		s = reopen(t, s, path)
		time.Sleep(time.Until(time.UnixMilli(due)) + 50*time.Millisecond)
		if attempts == 1 {
			idle(t, s, "q")
			state(t, s, "r", "failed")
			continue
		}
		if task := lease(t, s, "q", time.Hour); task.Attempt != 2 {
			t.Fatal(task)
		}
	}
}

func TestLostFinishReply(t *testing.T) {
	for _, c := range []struct {
		failure, event string
		step, attempt  int
	}{{"", "ActivityCompleted", 1, 1}, {"broken", "ActivityFailed", 0, 2}} {
		path := filepath.Join(t.TempDir(), "db")
		s := newStore(t, path)
		step := ephemeral.Step{Kind: "activity", Name: "call", Attempts: 2}
		must(t, s.Register("job", 1, []ephemeral.Step{step, step}))
		must(t, s.Start("r", "job", 1, "q", in, nil))
		task := lease(t, s, "q", time.Hour)
		result := json.RawMessage(`"done"`)
		must(t, s.Finish("r", task.Token, result, c.failure))
		s = reopen(t, s, path)
		recovered := describe(t, s, "r")
		history, e := s.History("r", 0, 1000)
		must(t, e)
		if s.Finish("r", task.Token, result, c.failure) == nil {
			t.Fatal("repeat accepted", c.event)
		}
		unchanged(t, describe(t, s, "r"), recovered)
		after, e := s.History("r", 0, 1000)
		must(t, e)
		if !slices.Equal(after, history) {
			t.Fatal("rejected finish recorded", history, after)
		}
		want := string(result)
		if c.failure != "" {
			want = c.failure
		}
		if got := events(t, s, "r", c.event); !slices.Equal(got, []string{want}) {
			t.Fatal(c.event, got)
		}
		retries := len(events(t, s, "r", "RetryScheduled"))
		if (c.failure == "") != (retries == 0) || retries > 1 {
			t.Fatal("retries", c.event, retries)
		}
		// A success moves on with the result; a failure retries with the old input.
		input := result
		if c.failure != "" {
			input = in
		}
		next := lease(t, s, "q", time.Hour)
		if next.Step != c.step || next.Attempt != c.attempt || !slices.Equal(next.Input, input) {
			t.Fatal("recovered progress", c.event, next)
		}
	}
}
