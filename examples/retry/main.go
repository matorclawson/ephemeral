// A worker records a temporary failure. A fresh Store resumes the queued retry.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rodriveracom/ephemeral"
)

func firstAttempt(path string) error {
	store, err := ephemeral.Open(path)
	if err != nil {
		return err
	}
	defer store.Close()
	steps := []ephemeral.Step{{Kind: "activity", Name: "uppercase", Attempts: 2}}
	if err := store.Register("format", 1, steps); err != nil {
		return err
	}
	if err := store.Start("draft-42", "format", 1, "editor", json.RawMessage(`"hello"`), nil); err != nil {
		return err
	}
	task, err := store.Poll("editor", time.Minute)
	if err != nil {
		return err
	}
	if task == nil || task.Attempt != 1 {
		return fmt.Errorf("expected first attempt, got %v", task)
	}
	fmt.Println("Attempt 1 failed. Closing the store.")
	return store.Finish(task.Run, task.Token, nil, "temporary failure")
}

func resume(path string) error {
	store, err := ephemeral.Open(path)
	if err != nil {
		return err
	}
	defer store.Close()
	task, err := store.Poll("editor", time.Minute)
	if err != nil {
		return err
	}
	if task == nil || task.Activity != "uppercase" || task.Attempt != 2 {
		return fmt.Errorf("expected retry, got %v", task)
	}
	var input string
	if err := json.Unmarshal(task.Input, &input); err != nil {
		return err
	}
	output, err := json.Marshal(strings.ToUpper(input))
	if err != nil {
		return err
	}
	if err := store.Finish(task.Run, task.Token, output, ""); err != nil {
		return err
	}
	result, err := store.Describe(task.Run)
	if err != nil {
		return err
	}
	if result.State != "completed" || string(result.Value) != `"HELLO"` {
		return fmt.Errorf("unexpected outcome: %+v", result)
	}
	fmt.Printf("Attempt %d completed after reopen. Result: %s\n", task.Attempt, result.Value)
	return nil
}
func run() error {
	dir, err := os.MkdirTemp("", "ephemeral-retry-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "example.db")
	if err := firstAttempt(path); err != nil {
		return err
	}
	return resume(path)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
