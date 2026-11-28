// A process can die without closing the store. This test kills one after its
// calls returned and pins what the next process sees. It is a process kill, not
// a power loss, and no call is in flight when it happens.
package ephemeral_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rodriveracom/ephemeral"
)

const killEnv = "EPHEMERAL_CONFORMANCE_KILL_DB"

var twice = ephemeral.Step{Kind: "activity", Name: "call", Attempts: 2}
var done = json.RawMessage(`"done"`)
var signalled = json.RawMessage(`1`)

// acknowledge makes three runs, prints the lease tokens a later process needs,
// and kills itself.
func acknowledge(t *testing.T, path string) {
	s, e := ephemeral.Open(path)
	must(t, e)
	must(t, s.Register("pair", 1, []ephemeral.Step{twice, twice}))
	must(t, s.Register("wait", 1, []ephemeral.Step{twice, {Kind: "signal", Name: "go"}, twice}))

	_, e = s.StartOnce("finished", "pair", 1, "a", in, nil)
	must(t, e)
	must(t, s.Finish("finished", lease(t, s, "a", time.Hour).Token, done, ""))

	must(t, s.Start("held", "pair", 1, "b", in, nil))
	fmt.Println("held", lease(t, s, "b", time.Hour).Token)

	must(t, s.Start("buffered", "wait", 1, "c", in, nil))
	fmt.Println("buffered", lease(t, s, "c", time.Hour).Token)
	must(t, s.Signal("buffered", "id-1", "go", signalled))

	must(t, syscall.Kill(os.Getpid(), syscall.SIGKILL))
}

func TestAcknowledgedCallsSurviveKill(t *testing.T) {
	if path := os.Getenv(killEnv); path != "" {
		acknowledge(t, path)
		return
	}
	path := filepath.Join(t.TempDir(), "db")
	child := exec.Command(os.Args[0], "-test.run=^TestAcknowledgedCallsSurviveKill$")
	child.Env = append(os.Environ(), killEnv+"="+path)
	out, e := child.Output()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("child was not killed: %v\n%s", e, out)
	}
	tokens := map[string]string{}
	for lines := bufio.NewScanner(bytes.NewReader(out)); lines.Scan(); {
		if run, token, ok := strings.Cut(lines.Text(), " "); ok {
			tokens[run] = token
		}
	}

	s := newStore(t, path)
	created, e := s.StartOnce("finished", "pair", 1, "a", in, nil)
	must(t, e)
	if created {
		t.Fatal("start receipt lost")
	}
	if task := lease(t, s, "a", time.Hour); task.Run != "finished" || task.Step != 1 || !bytes.Equal(task.Input, done) {
		t.Fatal("finished step lost", task)
	}

	idle(t, s, "b")
	must(t, s.Finish("held", tokens["held"], done, ""))

	must(t, s.Signal("buffered", "id-1", "go", signalled))
	idle(t, s, "c")
	must(t, s.Finish("buffered", tokens["buffered"], done, ""))
	if task := lease(t, s, "c", time.Hour); task.Run != "buffered" || task.Step != 2 || !bytes.Equal(task.Input, signalled) {
		t.Fatal("buffered signal lost", task)
	}
}
