package ephemeral

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

func check(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func open(t *testing.T, path string) *Store { t.Helper(); s, e := Open(path); check(t, e); return s }

var input = json.RawMessage(`{"ok":true}`)

func TestRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s := open(t, path)
	steps := []Step{{Kind: "activity", Name: "publish", Attempts: 2}, {Kind: "signal", Name: "confirm"}, {Kind: "timer", Millis: 1}}
	check(t, s.Register("flow", 1, steps))
	check(t, s.Start("r", "flow", 1, "q", input, map[string]string{"org": "zeo"}))
	first, e := s.Poll("q", time.Millisecond)
	check(t, e)
	s.Close()
	s = open(t, path)
	defer s.Close()
	time.Sleep(3 * time.Millisecond)
	second, e := s.Poll("q", time.Hour)
	check(t, e)
	if second == nil || second.Attempt != 2 {
		t.Fatal(second)
	}
	if s.Finish("r", first.Token, input, "") == nil {
		t.Fatal("stale claim")
	}
	check(t, s.Signal("r", "s1", "confirm", input))
	check(t, s.Finish("r", second.Token, input, ""))
	check(t, s.Signal("r", "s1", "confirm", input))
	time.Sleep(3 * time.Millisecond)
	_, e = s.Poll("q", time.Hour)
	check(t, e)
	d, e := s.Describe("r")
	check(t, e)
	if d.State != "completed" || d.Attributes["org"] != "zeo" {
		t.Fatal(d)
	}
	h, e := s.History("r", 0, 100)
	check(t, e)
	if len(h) != 14 || h[len(h)-1].Kind != "WorkflowCompleted" {
		t.Fatal(h)
	}
	ids, e := s.Find("org", "zeo", "", 1)
	check(t, e)
	if len(ids) != 1 || ids[0] != "r" {
		t.Fatal(ids)
	}
}
func TestCompetingWorkers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db")
	s := open(t, p)
	defer s.Close()
	s2 := open(t, p)
	defer s2.Close()
	check(t, s.Register("f", 1, []Step{{Kind: "activity", Name: "a"}}))
	check(t, s.Start("r", "f", 1, "q", input, nil))
	var wg sync.WaitGroup
	c := make(chan *Task, 2)
	for _, w := range []*Store{s, s2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := w.Poll("q", time.Hour)
			if e != nil {
				t.Error(e)
			}
			c <- v
		}()
	}
	wg.Wait()
	close(c)
	n := 0
	for v := range c {
		if v != nil {
			n++
		}
	}
	if n != 1 {
		t.Fatal(n)
	}
	check(t, s.Cancel("r"))
	d, e := s.Describe("r")
	check(t, e)
	if d.State != "cancelled" {
		t.Fatal(d)
	}
}
func TestVersionFailureAndSignals(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	check(t, s.Register("f", 1, []Step{{Kind: "signal", Name: "ok"}, {Kind: "activity", Name: "a"}}))
	if s.Register("f", 1, []Step{{Kind: "timer"}}) == nil {
		t.Fatal("mutable version")
	}
	if s.Start("bad", "f", 1, "q", input, map[string]string{"": "bad"}) == nil {
		t.Fatal("invalid attr")
	}
	if _, e := s.Describe("bad"); e != ErrMissing {
		t.Fatal(e)
	}
	check(t, s.Start("r", "f", 1, "q", input, nil))
	check(t, s.Signal("r", "k", "ok", input))
	if s.Signal("r", "k", "different", input) == nil {
		t.Fatal("signal conflict")
	}
	v, e := s.Poll("q", time.Hour)
	check(t, e)
	check(t, s.Finish("r", v.Token, nil, "error"))
	d, e := s.Describe("r")
	check(t, e)
	if d.State != "failed" {
		t.Fatal(d)
	}
}
func TestMeasurement(t *testing.T) {
	if os.Getenv("MEASURE") == "" {
		t.Skip()
	}
	p := filepath.Join(t.TempDir(), "db")
	s := open(t, p)
	check(t, s.Register("f", 1, []Step{{Kind: "activity", Name: "echo"}}))
	var times []time.Duration
	begin := time.Now()
	for i := 0; i < 1000; i++ {
		start := time.Now()
		id := fmt.Sprintf("r%06d", i)
		check(t, s.Start(id, "f", 1, "q", input, map[string]string{"org": "zeo"}))
		v, e := s.Poll("q", time.Hour)
		check(t, e)
		check(t, s.Finish(id, v.Token, input, ""))
		times = append(times, time.Since(start))
	}
	elapsed := time.Since(begin)
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	s.Close()
	f, e := os.Stat(p)
	check(t, e)
	t.Logf("1000 sequential workflows: %v total, %.1f runs/s, p50 %v, p95 %v, p99 %v, closed database %d bytes", elapsed, 1000/elapsed.Seconds(), times[499], times[949], times[989], f.Size())
}
