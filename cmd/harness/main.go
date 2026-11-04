// Command harness runs a bounded implement -> validate -> review workflow.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type verdict struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type runner func(context.Context, string, string, []string, string, io.Writer) error

type mainBranchPolicy struct {
	refs map[string]bool
}

// Branch inspection is read-only. A policy failure never checks out, deletes,
// resets or recreates a branch, so the user's working files remain untouched.
func readMainBranchPolicy(ctx context.Context, root string, run runner) (mainBranchPolicy, error) {
	if err := ctx.Err(); err != nil {
		return mainBranchPolicy{}, err
	}
	var branch bytes.Buffer
	if err := run(ctx, root, "git", []string{"symbolic-ref", "--quiet", "--short", "HEAD"}, "", &branch); err != nil {
		return mainBranchPolicy{}, fmt.Errorf("o harness exige HEAD na branch main; HEAD destacado ou Git indisponivel: %w", err)
	}
	if current := strings.TrimSpace(branch.String()); current != "main" {
		return mainBranchPolicy{}, fmt.Errorf("o harness exige a branch main; branch atual: %q; nenhuma branch sera criada ou trocada", current)
	}
	var refs bytes.Buffer
	if err := run(ctx, root, "git", []string{"for-each-ref", "--format=%(refname)", "refs/heads/"}, "", &refs); err != nil {
		return mainBranchPolicy{}, fmt.Errorf("nao foi possivel conferir as branches locais: %w", err)
	}
	policy := mainBranchPolicy{refs: make(map[string]bool)}
	for _, ref := range strings.Fields(refs.String()) {
		if !strings.HasPrefix(ref, "refs/heads/") {
			return mainBranchPolicy{}, errors.New("inventario de branches locais invalido")
		}
		policy.refs[ref] = true
	}
	if !policy.refs["refs/heads/main"] {
		return mainBranchPolicy{}, errors.New("o harness exige uma branch main existente; nenhuma branch sera criada")
	}
	return policy, nil
}

func (policy mainBranchPolicy) verify(ctx context.Context, root string, run runner) error {
	current, err := readMainBranchPolicy(ctx, root, run)
	if err != nil {
		return err
	}
	var added, removed []string
	for ref := range current.refs {
		if !policy.refs[ref] {
			added = append(added, ref)
		}
	}
	for ref := range policy.refs {
		if !current.refs[ref] {
			removed = append(removed, ref)
		}
	}
	if len(added) != 0 || len(removed) != 0 {
		sort.Strings(added)
		sort.Strings(removed)
		return fmt.Errorf("politica Git do harness: branches alteradas (criadas: %v; removidas: %v); arquivos preservados, sem correcao automatica de branches", added, removed)
	}
	return nil
}

func resolveCLI(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err == nil || name != "codex" {
		return path, err
	}
	if runtime.GOOS == "windows" {
		if homeDir, homeErr := os.UserHomeDir(); homeErr == nil {
			bundled := filepath.Join(homeDir, ".codex", "plugins", ".plugin-appserver", "codex.exe")
			if info, statErr := os.Stat(bundled); statErr == nil && !info.IsDir() {
				return bundled, nil
			}
		}
	}
	return "", err
}

func process(ctx context.Context, dir, name string, args []string, input string, output io.Writer) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Stdin = strings.NewReader(input)
	c.Stdout, c.Stderr = output, output
	c.WaitDelay = 5 * time.Second
	return c.Run()
}

func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

func readVerdict(path string) (verdict, error) {
	var v verdict
	b, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(b, &v); err != nil {
		return v, err
	}
	if (v.Status != "complete" && v.Status != "blocked") || strings.TrimSpace(v.Summary) == "" {
		return v, errors.New("invalid agent verdict")
	}
	return v, nil
}

func validate(ctx context.Context, root string, run runner, log io.Writer) error {
	if runtime.GOOS == "windows" {
		return run(ctx, root, "powershell.exe", []string{"-NoProfile", "-File", filepath.Join(root, "scripts", "test.ps1")}, "", log)
	}
	return run(ctx, root, "sh", []string{filepath.Join(root, "scripts", "test.sh")}, "", log)
}

// pipeline never considers an agent's success message sufficient: both the
// deterministic test gate and a separate read-only review must succeed.
func pipeline(ctx context.Context, root, dir, cli, task string, attempts int, run runner) (err error) {
	return pipelineWithBranchPolicy(ctx, root, dir, cli, task, attempts, run, nil)
}

func pipelineWithBranchPolicy(ctx context.Context, root, dir, cli, task string, attempts int, run runner, initial *mainBranchPolicy) (err error) {
	state := verdict{Status: "blocked", Summary: "execution interrupted"}
	defer func() {
		if err != nil {
			state.Summary = err.Error()
		}
		if saveErr := writeJSON(filepath.Join(dir, "result.json"), state); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
	}()
	var policy mainBranchPolicy
	if initial == nil {
		policy, err = readMainBranchPolicy(ctx, root, run)
	} else {
		policy = *initial
		err = policy.verify(ctx, root, run)
	}
	if err != nil {
		return err
	}
	feedback := ""
	for attempt := 1; attempt <= attempts; attempt++ {
		for _, role := range []string{"implementer", "reviewer"} {
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = policy.verify(ctx, root, run); err != nil {
				return fmt.Errorf("antes de %s: %w", role, err)
			}
			profile, readErr := os.ReadFile(filepath.Join(root, "docs", "harness", role+".md"))
			if readErr != nil {
				return readErr
			}
			output := filepath.Join(dir, fmt.Sprintf("%d-%s.json", attempt, role))
			log, createErr := os.Create(filepath.Join(dir, fmt.Sprintf("%d-%s.log", attempt, role)))
			if createErr != nil {
				return createErr
			}
			sandbox := "read-only"
			if role == "implementer" {
				sandbox = "workspace-write"
			}
			prompt := string(profile) + "\nLeia AGENTS.md e docs/rules.md. Trabalhe exclusivamente na branch main. Nao crie, troque, renomeie ou exclua branches, nem use worktrees para contornar essa regra. Commits do projeto pertencem somente a main; neste harness, nao faca commit, push ou deploy. Nao execute novamente o harness de agentes.\nTarefa:\n" + task + "\nFeedback anterior:\n" + feedback
			args := []string{"exec", "--ephemeral", "--sandbox", sandbox, "--json", "--output-schema", filepath.Join(root, "docs", "harness", "result.schema.json"), "--output-last-message", output, "-C", root, "-"}
			err = run(ctx, root, cli, args, prompt, log)
			closeErr := log.Close()
			if policyErr := policy.verify(ctx, root, run); policyErr != nil {
				return errors.Join(fmt.Errorf("apos %s: %w", role, policyErr), err, closeErr)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", role, err)
			}
			if closeErr != nil {
				return closeErr
			}
			v, readErr := readVerdict(output)
			if readErr != nil {
				return fmt.Errorf("%s response: %w", role, readErr)
			}
			feedback = v.Summary
			if v.Status != "complete" {
				break
			}
			if role == "reviewer" {
				state = v
				return nil
			}
			if err = policy.verify(ctx, root, run); err != nil {
				return fmt.Errorf("antes da validacao: %w", err)
			}
			testLog, createErr := os.Create(filepath.Join(dir, fmt.Sprintf("%d-validation.log", attempt)))
			if createErr != nil {
				return createErr
			}
			testErr := validate(ctx, root, run, testLog)
			closeErr = testLog.Close()
			if policyErr := policy.verify(ctx, root, run); policyErr != nil {
				return errors.Join(fmt.Errorf("apos validacao: %w", policyErr), testErr, closeErr)
			}
			if closeErr != nil {
				return closeErr
			}
			if testErr != nil {
				feedback = fmt.Sprintf("Validacao falhou: %v. Leia %s", testErr, testLog.Name())
				break
			}
		}
	}
	return fmt.Errorf("limite de %d tentativas atingido: %s", attempts, feedback)
}

func execute(args []string) error {
	return executeWithRunner(args, process)
}

func executeWithRunner(args []string, run runner) error {
	f := flag.NewFlagSet("harness", flag.ContinueOnError)
	mode := f.String("mode", "verify", "verify, prepare, run or doctor")
	task := f.String("task", "", "task and acceptance criteria")
	cli := f.String("codex", "codex", "Codex CLI executable or absolute path")
	attempts := f.Int("attempts", 2, "maximum implementation/review cycles (1-5)")
	limit := f.Duration("timeout", 30*time.Minute, "total execution timeout")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *attempts < 1 || *attempts > 5 || *limit <= 0 {
		return errors.New("invalid arguments")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	for _, path := range []string{"AGENTS.md", "go.mod", "docs/rules.md", "docs/harness/implementer.md", "docs/harness/reviewer.md", "docs/harness/result.schema.json"} {
		if _, err = os.Stat(filepath.Join(root, path)); err != nil {
			return fmt.Errorf("execute na raiz do repositorio: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *limit)
	defer cancel()
	switch *mode {
	case "verify":
		return validate(ctx, root, run, os.Stdout)
	case "doctor":
		if *cli, err = resolveCLI(*cli); err != nil {
			return err
		}
		for _, tool := range []string{"go", "git", *cli} {
			if _, err = exec.LookPath(tool); err != nil {
				return err
			}
		}
		return run(ctx, root, *cli, []string{"exec", "--help"}, "", os.Stdout)
	case "prepare", "run":
		if strings.TrimSpace(*task) == "" {
			return errors.New("-task is required")
		}
	default:
		return errors.New("unknown mode")
	}
	policy, err := readMainBranchPolicy(ctx, root, run)
	if err != nil {
		return err
	}
	if *mode == "run" {
		if *cli, err = resolveCLI(*cli); err != nil {
			return fmt.Errorf("Codex CLI unavailable; use -codex with its path: %w", err)
		}
	}
	if err = policy.verify(ctx, root, run); err != nil {
		return err
	}
	base := filepath.Join(root, ".artifacts", "agents")
	if err = os.MkdirAll(base, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(base, "run-")
	if err != nil {
		return err
	}
	fmt.Println("Run artifacts:", dir)
	if err = os.WriteFile(filepath.Join(dir, "task.md"), []byte(*task), 0600); err != nil {
		return err
	}
	status, err := os.Create(filepath.Join(dir, "git-status-before.txt"))
	if err != nil {
		return err
	}
	err = run(ctx, root, "git", []string{"status", "--short"}, "", status)
	closeErr := status.Close()
	if policyErr := policy.verify(ctx, root, run); policyErr != nil {
		return errors.Join(policyErr, err, closeErr)
	}
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if *mode == "prepare" {
		return writeJSON(filepath.Join(dir, "result.json"), verdict{"prepared", "Task prepared; no agent executed"})
	}
	// Only one mutating pipeline may run in this checkout at a time.
	lockPath := filepath.Join(base, "active.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("checkout locked; inspect .artifacts/agents/active.lock: %w", err)
	}
	if err = lock.Close(); err != nil {
		return err
	}
	defer os.Remove(lockPath)
	return pipelineWithBranchPolicy(ctx, root, dir, *cli, *task, *attempts, run, &policy)
}

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "harness:", err)
		os.Exit(1)
	}
}
