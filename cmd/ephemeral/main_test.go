package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/rodriveracom/ephemeral"
)

// call decodes one JSON line the way run does, rejecting unknown fields, and dispatches it.
func call(t *testing.T, s *ephemeral.Store, line string) (any, error) {
	t.Helper()
	d := json.NewDecoder(bytes.NewBufferString(line))
	d.DisallowUnknownFields()
	var r request
	if e := d.Decode(&r); e != nil {
		t.Fatal(line, e)
	}
	return dispatch(s, r)
}

func must(t *testing.T, s *ephemeral.Store, line string) any {
	t.Helper()
	v, e := call(t, s, line)
	if e != nil {
		t.Fatal(line, e)
	}
	return v
}

func TestCoordinationOps(t *testing.T) {
	s, e := ephemeral.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	must(t, s, `{"Op":"register","Name":"job","Version":1,"Steps":[{"kind":"activity","name":"call","attempts":3,"retry_delays_ms":[0,60000]}]}`)

	if v := must(t, s, `{"Op":"next-due","Queue":"q"}`); v != nil {
		t.Fatal("empty queue returned", v)
	}
	start := `{"Op":"start-once","ID":"r","Name":"job","Version":1,"Queue":"q","Value":{"ok":true},"Attributes":{"scope":"alpha"}}`
	if v := must(t, s, start); v != true {
		t.Fatal("first start-once", v)
	}
	if v := must(t, s, start); v != false {
		t.Fatal("replayed start-once", v)
	}
	if _, e := call(t, s, `{"Op":"start-once","ID":"r","Name":"job","Version":1,"Queue":"other","Value":{"ok":true},"Attributes":{"scope":"alpha"}}`); e == nil {
		t.Fatal("conflicting start-once accepted")
	}
	if _, ok := must(t, s, `{"Op":"next-due","Queue":"q"}`).(int64); !ok {
		t.Fatal("next-due did not return milliseconds")
	}

	if v := must(t, s, `{"Op":"find","Key":"scope","Equal":"alpha","State":"ready","Limit":10}`); len(v.([]string)) != 1 {
		t.Fatal(v)
	}
	if v := must(t, s, `{"Op":"find","Key":"scope","Equal":"alpha","State":"failed","Limit":10}`); len(v.([]string)) != 0 {
		t.Fatal(v)
	}
	if v := must(t, s, `{"Op":"find","Key":"scope","Equal":"alpha","Limit":10}`); len(v.([]string)) != 1 {
		t.Fatal("find without a state must still match", v)
	}

	task, e := s.Poll("q", time.Hour)
	if e != nil || task == nil {
		t.Fatal(task, e)
	}
	if _, e := call(t, s, `{"Op":"fail-permanent","ID":"r","Token":"wrong","Failure":"reject"}`); e == nil {
		t.Fatal("stale token accepted")
	}
	if _, e := call(t, s, `{"Op":"fail-permanent","ID":"r","Token":"`+task.Token+`"}`); e == nil {
		t.Fatal("empty failure reason accepted")
	}
	must(t, s, `{"Op":"fail-permanent","ID":"r","Token":"`+task.Token+`","Failure":"reject"}`)
	if v := must(t, s, `{"Op":"find","Key":"scope","Equal":"alpha","State":"failed","Limit":10}`); len(v.([]string)) != 1 {
		t.Fatal(v)
	}
}
