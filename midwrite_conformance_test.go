// This test kills a process while it keeps writing, so the kill is not aligned
// with a return from the store. Every acknowledged call must be there afterwards,
// and at most the one call that followed may be there too, whole. It is a process
// kill, not a power loss. Where each kill lands is left to timing: no trial is
// known to have interrupted a transaction.
package ephemeral_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/rodriveracom/ephemeral"
)

const midwriteEnv = "EPHEMERAL_CONFORMANCE_MIDWRITE_DB"

// Each run takes three calls: StartOnce, Poll and Finish.
const callsPerRun = 3

func midwriteRun(i int) string { return fmt.Sprintf("r%04d", i) }

// writeUntilKilled prints one line after each call returns. The cap only stops a
// child whose parent has gone.
func writeUntilKilled(t *testing.T, path string) {
	s, e := ephemeral.Open(path)
	must(t, e)
	must(t, s.Register("job", 1, one))
	for i := range 900 {
		_, e = s.StartOnce(midwriteRun(i), "job", 1, "q", in, nil)
		must(t, e)
		fmt.Println("ack")
		task := lease(t, s, "q", time.Hour)
		fmt.Println("ack")
		must(t, s.Finish(task.Run, task.Token, done, ""))
		fmt.Println("ack")
	}
	t.Fatal("not killed")
}

// killAfter returns how many calls the child acknowledged before it died.
func killAfter(t *testing.T, path string, calls int, delay time.Duration) int {
	t.Helper()
	child := exec.Command(os.Args[0], "-test.run=^TestKillDuringWrites$")
	child.Env = append(os.Environ(), midwriteEnv+"="+path)
	var stderr bytes.Buffer
	child.Stderr = &stderr
	pipe, e := child.StdoutPipe()
	must(t, e)
	must(t, child.Start())
	t.Cleanup(func() { _ = child.Process.Kill() })

	out := bufio.NewReader(pipe)
	acked := 0
	killed := false
	for {
		line, e := out.ReadString('\n')
		if line == "ack\n" {
			acked++
		} else if line != "" {
			t.Fatalf("child printed %q\n%s", line, &stderr)
		}
		if acked == calls && !killed {
			time.Sleep(delay)
			must(t, child.Process.Kill())
			killed = true
		}
		if errors.Is(e, io.EOF) {
			break
		}
		must(t, e)
	}
	e = child.Wait()
	var exit *exec.ExitError
	if !killed || !errors.As(e, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("child was not killed after %d calls: %v\n%s", acked, e, &stderr)
	}
	return acked
}

// recordedCalls checks that a run's state and history describe the same whole
// number of calls, and returns that number.
func recordedCalls(t *testing.T, s *ephemeral.Store, id string) int {
	t.Helper()
	d, e := s.Describe(id)
	if errors.Is(e, ephemeral.ErrMissing) {
		return 0
	}
	must(t, e)
	history, e := s.History(id, 0, 100)
	must(t, e)
	var kinds []string
	for _, event := range history {
		kinds = append(kinds, event.Kind)
	}
	all := []string{"WorkflowStarted", "ActivityScheduled", "ActivityStarted", "ActivityCompleted", "WorkflowCompleted"}
	for calls, want := range []struct {
		state  string
		events int
	}{{"ready", 2}, {"leased", 3}, {"completed", 5}} {
		if d.State == want.state && slices.Equal(kinds, all[:want.events]) {
			return calls + 1
		}
	}
	t.Fatal("state and history disagree", id, d.State, kinds)
	return 0
}

func TestKillDuringWrites(t *testing.T) {
	if path := os.Getenv(midwriteEnv); path != "" {
		writeUntilKilled(t, path)
		return
	}
	extra := 0
	const trials = 20
	for trial := range trials {
		path := filepath.Join(t.TempDir(), "db")
		acked := killAfter(t, path, 1+trial*7, time.Duration(trial%5)*200*time.Microsecond)

		s := newStore(t, path)
		// The child may have started the run after the last acknowledged one, and
		// no run beyond that.
		runs := acked/callsPerRun + 2
		recorded := make([]int, runs)
		total := 0
		for i := range runs {
			recorded[i] = recordedCalls(t, s, midwriteRun(i))
			total += recorded[i]
		}
		if total != acked && total != acked+1 {
			t.Fatal("trial", trial, "acknowledged", acked, "recorded", total, recorded)
		}
		// The recorded calls are the first ones made, with no gap.
		var started []string
		for i := range runs {
			if want := max(0, min(callsPerRun, total-i*callsPerRun)); recorded[i] != want {
				t.Fatal("trial", trial, "run", i, "has", recorded[i], "calls, want", want, recorded)
			}
			if recorded[i] > 0 {
				started = append(started, midwriteRun(i))
			}
		}
		listed, e := s.List("", "", 1000)
		must(t, e)
		if !slices.Equal(listed, started) {
			t.Fatal("trial", trial, "listed", listed, "want", started)
		}
		extra += total - acked

		// The reopened store still takes writes.
		must(t, s.Start("after", "job", 1, "after", in, nil))
		must(t, s.Finish("after", lease(t, s, "after", time.Hour).Token, done, ""))
		state(t, s, "after", "completed")
		s.Close()
	}
	t.Logf("%d of %d trials kept one call the child had not acknowledged", extra, trials)
}
