package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// All branch-policy cases use this runner state, never real branch creation or
// checkout commands, including when the simulated agent violates the policy.
type simulatedGitState struct {
	branch   string
	detached bool
	refs     []string
}

func mainGitState() *simulatedGitState {
	return &simulatedGitState{branch: "main", refs: []string{"refs/heads/main", "refs/heads/preexisting"}}
}

func (state *simulatedGitState) handle(t *testing.T, name string, args []string, output io.Writer) (bool, error) {
	t.Helper()
	if name != "git" {
		return false, nil
	}
	switch strings.Join(args, " ") {
	case "symbolic-ref --quiet --short HEAD":
		if state.detached {
			return true, errors.New("detached HEAD")
		}
		_, err := io.WriteString(output, state.branch+"\n")
		return true, err
	case "for-each-ref --format=%(refname) refs/heads/":
		_, err := io.WriteString(output, strings.Join(state.refs, "\n")+"\n")
		return true, err
	case "status --short":
		_, err := io.WriteString(output, " M user-work.txt\n")
		return true, err
	default:
		t.Fatalf("unexpected or mutating Git command: %v", args)
		return true, errors.New("unexpected Git command")
	}
}

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
			git := mainGitState()
			run := func(ctx context.Context, cwd, name string, args []string, input string, out io.Writer) error {
				if handled, err := git.handle(t, name, args, out); handled {
					return err
				}
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
				for _, policy := range []string{"Trabalhe exclusivamente na branch main", "Nao crie, troque, renomeie ou exclua branches", "Commits do projeto pertencem somente a main", "nao faca commit, push ou deploy"} {
					if !strings.Contains(input, policy) {
						t.Fatalf("agent prompt omitted branch/commit policy: %s", policy)
					}
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

func harnessRootFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"AGENTS.md", "go.mod", "docs/rules.md", "docs/harness/implementer.md", "docs/harness/reviewer.md", "docs/harness/result.schema.json"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestWorkModesRejectNonMainBeforeArtifactsOrAgents(t *testing.T) {
	for _, mode := range []string{"prepare", "run"} {
		for _, stateName := range []string{"feature", "detached", "missing main ref"} {
			t.Run(mode+"/"+stateName, func(t *testing.T) {
				root := harnessRootFixture(t)
				t.Chdir(root)
				git := mainGitState()
				switch stateName {
				case "feature":
					git.branch = "feature/example"
				case "detached":
					git.detached = true
				case "missing main ref":
					git.refs = nil
				}
				run := func(ctx context.Context, cwd, name string, args []string, input string, out io.Writer) error {
					if handled, err := git.handle(t, name, args, out); handled {
						return err
					}
					t.Fatal("started an agent or validation outside main")
					return nil
				}
				err := executeWithRunner([]string{"-mode", mode, "-task", "change", "-codex", "missing-agent-must-not-be-resolved"}, run)
				if err == nil || !strings.Contains(err.Error(), "main") {
					t.Fatalf("non-main work mode was not refused clearly: %v", err)
				}
				if _, err := os.Stat(filepath.Join(root, ".artifacts")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("policy failure created work artifacts: %v", err)
				}
			})
		}
	}
}

func TestPrepareOnMainDoesNotStartAgentsOrChangeBranches(t *testing.T) {
	root := harnessRootFixture(t)
	t.Chdir(root)
	git := mainGitState()
	run := func(ctx context.Context, cwd, name string, args []string, input string, out io.Writer) error {
		if handled, err := git.handle(t, name, args, out); handled {
			return err
		}
		t.Fatal("prepare started a process other than read-only Git inspection")
		return nil
	}
	if err := executeWithRunner([]string{"-mode", "prepare", "-task", "change"}, run); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, ".artifacts", "agents", "run-*", "result.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("prepare artifacts missing: %v %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var result verdict
	if json.Unmarshal(data, &result) != nil || result.Status != "prepared" {
		t.Fatalf("prepare contract changed: %s", data)
	}
	if git.branch != "main" || len(git.refs) != 2 {
		t.Fatal("prepare changed the branch state")
	}
}

func TestPrepareDetectsBranchChangeDuringStatusWithoutAutomaticCheckout(t *testing.T) {
	root := harnessRootFixture(t)
	t.Chdir(root)
	git := mainGitState()
	run := func(ctx context.Context, cwd, name string, args []string, input string, out io.Writer) error {
		handled, err := git.handle(t, name, args, out)
		if !handled {
			t.Fatal("unexpected process")
		}
		if strings.Join(args, " ") == "status --short" {
			git.branch = "feature/changed-externally"
		}
		return err
	}
	if err := executeWithRunner([]string{"-mode", "prepare", "-task", "change"}, run); err == nil || !strings.Contains(err.Error(), "main") {
		t.Fatalf("branch change during preparation was ignored: %v", err)
	}
	if git.branch != "feature/changed-externally" {
		t.Fatal("harness automatically checked out main")
	}
	paths, err := filepath.Glob(filepath.Join(root, ".artifacts", "agents", "run-*", "result.json"))
	if err != nil || len(paths) != 0 {
		t.Fatalf("preparation reported success after branch violation: %v %v", paths, err)
	}
}

func TestPipelineStopsAfterBranchViolationAndPreservesAgentWork(t *testing.T) {
	for _, stage := range []string{"implementer", "validation", "reviewer"} {
		for _, change := range []string{"switch", "detached", "new branch", "removed branch"} {
			t.Run(stage+"/"+change, func(t *testing.T) {
				root := harnessRootFixture(t)
				dir := t.TempDir()
				git := mainGitState()
				calls := 0
				run := func(ctx context.Context, cwd, name string, args []string, input string, out io.Writer) error {
					if handled, err := git.handle(t, name, args, out); handled {
						return err
					}
					calls++
					currentStage := "validation"
					if name == "fake-codex" {
						var output string
						for i, arg := range args {
							if arg == "--output-last-message" {
								output = args[i+1]
							}
						}
						currentStage = "implementer"
						if strings.Contains(output, "reviewer") {
							currentStage = "reviewer"
						}
						if err := writeJSON(output, verdict{"complete", "checked"}); err != nil {
							return err
						}
						if err := os.WriteFile(filepath.Join(root, "user-work.txt"), []byte("preserve this work"), 0600); err != nil {
							return err
						}
					}
					if currentStage == stage {
						switch change {
						case "switch":
							git.branch = "feature/unauthorized"
						case "detached":
							git.detached = true
						case "new branch":
							git.refs = append(git.refs, "refs/heads/unauthorized")
						case "removed branch":
							git.refs = []string{"refs/heads/main"}
						}
					}
					return nil
				}
				err := pipeline(context.Background(), root, dir, "fake-codex", "change", 2, run)
				if err == nil || (!strings.Contains(err.Error(), "main") && !strings.Contains(err.Error(), "branches alteradas")) {
					t.Fatalf("branch violation was not detected: %v", err)
				}
				wantCalls := map[string]int{"implementer": 1, "validation": 2, "reviewer": 3}[stage]
				if calls != wantCalls {
					t.Fatalf("pipeline continued after policy failure: calls=%d want=%d", calls, wantCalls)
				}
				result, readErr := readVerdict(filepath.Join(dir, "result.json"))
				if readErr != nil || result.Status != "blocked" {
					t.Fatalf("branch violation was reported as success: result=%+v err=%v", result, readErr)
				}
				work, readErr := os.ReadFile(filepath.Join(root, "user-work.txt"))
				if readErr != nil || string(work) != "preserve this work" {
					t.Fatalf("agent work was discarded: %q %v", work, readErr)
				}
			})
		}
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
