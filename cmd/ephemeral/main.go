package main

import (
	"encoding/json"
	"ephemeral"
	"fmt"
	"io"
	"os"
	"time"
)

type request struct {
	State                                                    string
	Op, ID, Name, Queue, Token, Failure, Key, Equal, AfterID string
	Version, Limit                                           int
	LeaseMS, After                                           int64
	Value                                                    json.RawMessage
	Steps                                                    []ephemeral.Step
	Attributes                                               map[string]string
}

func dispatch(s *ephemeral.Store, r request) (any, error) {
	switch r.Op {
	case "list":
		return s.List(r.State, r.AfterID, r.Limit)
	case "heartbeat":
		return nil, s.Heartbeat(r.ID, r.Token, time.Duration(r.LeaseMS)*time.Millisecond)
	case "register":
		return nil, s.Register(r.Name, r.Version, r.Steps)
	case "start-once":
		return s.StartOnce(r.ID, r.Name, r.Version, r.Queue, r.Value, r.Attributes)
	case "next-due":
		due, ok, e := s.NextDue(r.Queue)
		if !ok || e != nil {
			return nil, e
		}
		return due, nil
	case "fail-permanent":
		return nil, s.FailPermanent(r.ID, r.Token, r.Failure)
	case "start":
		return nil, s.Start(r.ID, r.Name, r.Version, r.Queue, r.Value, r.Attributes)
	case "signal":
		return nil, s.Signal(r.ID, r.Key, r.Name, r.Value)
	case "cancel":
		return nil, s.Cancel(r.ID)
	case "poll":
		return s.Poll(r.Queue, time.Duration(r.LeaseMS)*time.Millisecond)
	case "finish":
		return nil, s.Finish(r.ID, r.Token, r.Value, r.Failure)
	case "attributes":
		return nil, s.SetAttributes(r.ID, r.Attributes)
	case "describe":
		return s.Describe(r.ID)
	case "history":
		return s.History(r.ID, r.After, r.Limit)
	case "find":
		return s.FindInState(r.Key, r.Equal, r.State, r.AfterID, r.Limit)
	}
	return nil, fmt.Errorf("unknown op %q", r.Op)
}
func run() error {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(ephemeral.Version())
		return nil
	}
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: ephemeral DATABASE < requests.jsonl")
	}
	s, e := ephemeral.Open(os.Args[1])
	if e != nil {
		return e
	}
	defer s.Close()
	d := json.NewDecoder(os.Stdin)
	d.DisallowUnknownFields()
	out := json.NewEncoder(os.Stdout)
	for {
		var r request
		if e = d.Decode(&r); e == io.EOF {
			return nil
		} else if e != nil {
			return e
		}
		v, e := dispatch(s, r)
		if e != nil {
			return e
		}
		if e = out.Encode(v); e != nil {
			return e
		}
	}
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
