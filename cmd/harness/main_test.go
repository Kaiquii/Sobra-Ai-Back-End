package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPipelineGatesAndRetries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failure   string
		wantOK    bool
		wantCalls int
	}{
		{"success", "", true, 3},
		{"agent crash", "crash", false, 1},
		{"missing verdict", "missing", false, 1},
		{"invalid verdict", "invalid", false, 1},
		{"implementation blocked", "implementation", false, 2},
		{"test failure", "tests", false, 4},
		{"review rejection", "review", false, 6},
		{"repair test failure", "repair", true, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "docs", "harness"), 0700); err != nil {
				t.Fatal(err)
			}
			for _, role := range []string{"implementer", "reviewer"} {
				if err := os.WriteFile(filepath.Join(root, "docs", "harness", role+".md"), []byte(role), 0600); err != nil {
					t.Fatal(err)
				}
			}
			calls, tests := 0, 0
			run := func(ctx context.Context, cwd, name string, args []string, input string, out io.Writer) error {
				calls++
				if name != "fake-codex" {
					tests++
					if tc.failure == "tests" || (tc.failure == "repair" && tests == 1) {
						return errors.New("test failed")
					}
					return nil
				}
				if tc.failure == "crash" {
					return errors.New("process failed")
				}
				if tc.failure == "missing" {
					return nil
				}
				var output, sandbox string
				for i, arg := range args {
					if arg == "--output-last-message" {
						output = args[i+1]
					}
					if arg == "--sandbox" {
						sandbox = args[i+1]
					}
				}
				review := strings.Contains(output, "reviewer")
				if review && sandbox != "read-only" {
					t.Fatal("review must be read-only")
				}
				if !review && sandbox != "workspace-write" {
					t.Fatal("implementation sandbox missing")
				}
				if tc.failure == "invalid" {
					return os.WriteFile(output, []byte(`{"status":"whatever","summary":"x"}`), 0600)
				}
				v := verdict{"complete", "checked"}
				if (review && tc.failure == "review") || (!review && tc.failure == "implementation") {
					v.Status = "blocked"
				}
				return writeJSON(output, v)
			}
			err := pipeline(context.Background(), root, dir, "fake-codex", "task", 2, run)
			if (err == nil) != tc.wantOK {
				t.Fatalf("err=%v", err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("calls=%d want=%d", calls, tc.wantCalls)
			}
			result, readErr := readVerdict(filepath.Join(dir, "result.json"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if (result.Status == "complete") != tc.wantOK {
				t.Fatalf("false success: %+v", result)
			}
		})
	}
}

func TestCanceledPipelineDoesNotStartAgent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := pipeline(ctx, t.TempDir(), t.TempDir(), "unused", "task", 2,
		func(context.Context, string, string, []string, string, io.Writer) error {
			t.Fatal("started after cancellation")
			return nil
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestProcessPropagatesExitCode(t *testing.T) {
	err := process(context.Background(), t.TempDir(), "go", []string{"invalid-harness-subcommand"}, "", io.Discard)
	if err == nil {
		t.Fatal("expected subprocess failure")
	}
}
