package ephemeral

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestKilledWriter(t *testing.T) {
	if p := os.Getenv("EPHEMERAL_KILL_TEST_DB"); p != "" {
		s := open(t, p)
		check(t, s.Register("f", 1, []Step{{Kind: "activity", Name: "a"}}))
		check(t, s.Start("r", "f", 1, "q", input, nil))
		// Interrupt a transition after its history insert, before its state update and commit.
		check(t, s.exec("BEGIN IMMEDIATE"))
		check(t, s.event("r", "WorkflowCancelled", 0, ""))
		check(t, syscall.Kill(os.Getpid(), syscall.SIGKILL))
		return
	}
	p := filepath.Join(t.TempDir(), "db")
	c := exec.Command(os.Args[0], "-test.run=^TestKilledWriter$")
	c.Env = append(os.Environ(), "EPHEMERAL_KILL_TEST_DB="+p)
	e := c.Run()
	exit, ok := e.(*exec.ExitError)
	if !ok || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatal(e)
	}
	s := open(t, p)
	defer s.Close()
	h, e := s.History("r", 0, 100)
	check(t, e)
	if len(h) != 2 || h[1].Kind != "ActivityScheduled" {
		t.Fatal(h)
	}
	job, e := s.Poll("q", time.Hour)
	check(t, e)
	if job == nil {
		t.Fatal("lost committed job")
	}
}
