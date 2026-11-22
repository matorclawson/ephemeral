package ephemeral

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestParallelSwitchRecovery(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db")
	s := open(t, p)
	check(t, s.Register("worker", 1, []Step{{Kind: "activity", Name: "echo"}}))
	branches := []Child{{Name: "worker", Version: 1, Queue: "a"}, {Name: "worker", Version: 1, Queue: "b"}}
	def := []Step{{Kind: "switch", Field: "mode", Cases: map[string]string{"both": "many"}, Default: "one"}, {Kind: "child", Label: "one", Children: branches[:1], Next: "$end"}, {Kind: "parallel", Label: "many", Children: branches}}
	check(t, s.Register("root", 1, def))
	check(t, s.Start("r", "root", 1, "parent", json.RawMessage(`{"mode":"both"}`), nil))
	a, e := s.Poll("a", time.Hour)
	check(t, e)
	b, e := s.Poll("b", time.Hour)
	check(t, e)
	if a == nil || b == nil || a.Run == b.Run {
		t.Fatal(a, b)
	}
	check(t, s.Finish(b.Run, b.Token, json.RawMessage("2"), ""))
	s.Close()
	s = open(t, p)
	defer s.Close()
	d, e := s.Describe("r")
	check(t, e)
	if d.State != "joining" {
		t.Fatal(d)
	}
	check(t, s.Finish(a.Run, a.Token, json.RawMessage("1"), ""))
	_, e = s.Poll("parent", time.Hour)
	check(t, e)
	d, e = s.Describe("r")
	check(t, e)
	if d.State != "completed" || string(d.Value) != "[1,2]" {
		t.Fatal(d)
	}
	check(t, s.Start("single", "root", 1, "parent", input, nil))
	a, e = s.Poll("a", time.Hour)
	check(t, e)
	check(t, s.Finish(a.Run, a.Token, input, ""))
	_, e = s.Poll("parent", time.Hour)
	check(t, e)
	d, e = s.Describe("single")
	check(t, e)
	if d.State != "completed" || string(d.Value) != string(input) {
		t.Fatal(d)
	}
}
func TestChildFailureCancellationAndValidation(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	check(t, s.Register("leaf", 1, []Step{{Kind: "activity", Name: "work"}}))
	for _, steps := range [][]Step{{{Kind: "timer", Label: "again", Next: "again"}}, {{Kind: "child", Children: []Child{{Name: "absent", Version: 1}}}}} {
		if s.Register("bad", 1, steps) == nil {
			t.Fatal(steps)
		}
	}
	check(t, s.Register("middle", 1, []Step{{Kind: "child", Children: []Child{{Name: "leaf", Version: 1}}}}))
	check(t, s.Register("root", 1, []Step{{Kind: "child", Children: []Child{{Name: "middle", Version: 1}}}}))
	for _, id := range []string{"cancel", "fail"} {
		check(t, s.Start(id, "root", 1, "q", input, nil))
		v, e := s.Poll("q", time.Hour)
		check(t, e)
		if v == nil {
			t.Fatal("no leaf")
		}
		if id == "cancel" {
			check(t, s.Cancel(id))
			d, e := s.Describe(v.Run)
			check(t, e)
			if d.State != "cancelled" || s.Finish(v.Run, v.Token, input, "") == nil {
				t.Fatal(d)
			}
		} else {
			check(t, s.Finish(v.Run, v.Token, nil, "failed"))
			_, e = s.Poll("q", time.Hour)
			check(t, e)
			d, e := s.Describe(id)
			check(t, e)
			if d.State != "failed" {
				t.Fatal(d)
			}
		}
	}
}
