package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/mcp"
	"github.com/Pastranauwu/loom/internal/spec"
)

const specGuide = `loom.spec.yml declares WHAT must exist; loom builds it with isolated agents.

feature: short lowercase title
why: one or two lines on why it is wanted
requirements:        # atomic, testable behaviors, one per item
  - users can reset their password by email
rules:               # constraints on how it is built, apply to everything
  - no new dependencies
acceptance:          # end-to-end criteria; "command" only if it certainly exists
  - criterion: the suite passes
    command: go test ./...
dont_touch: ["vendor/**"]

Interview the human yourself: ask about edge cases, failures and limits they did
not mention, settle them as requirements or rules (prefix an unanswered one with
"Assumption:"), then call loom_spec_write and loom_apply_dry_run. Read the repo
first with loom_map.`

// loom mcp: the chat model is the tech lead, loom the builders. Every tool
// is this same binary run with --json, so the CLI stays the one source of
// truth and the server adds no logic of its own.
func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "serve loom to an MCP client (Claude Desktop, Cursor, opencode) over stdio",
		Long: `Speaks the Model Context Protocol on stdin/stdout. Quick tools (map, board,
check, apply dry-run) answer live; heavy ones (up, run, ship, heal, redteam)
start detached and return a pid and a log, so the chat never blocks. Poll them
with loom_board and loom_log.`,
		Example: `  claude mcp add loom -- loom mcp`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			return mcp.Serve(os.Stdin, os.Stdout, "loom", version, mcpTools(exe))
		},
	}
}

func str(a map[string]any, k string) string {
	s, _ := a[k].(string)
	return strings.TrimSpace(s)
}

func num(a map[string]any, k string) int {
	n, _ := a[k].(float64)
	return int(n)
}

func on(a map[string]any, k string) bool {
	b, _ := a[k].(bool)
	return b
}

func schema(props map[string]string, required ...string) map[string]any {
	p := map[string]any{"dir": map[string]any{"type": "string", "description": "project directory (defaults to the server's)"}}
	for k, v := range props {
		typ, desc, _ := strings.Cut(v, ":")
		p[k] = map[string]any{"type": typ, "description": desc}
	}
	s := map[string]any{"type": "object", "properties": p}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func projectDir(a map[string]any) (string, error) {
	dir := str(a, "dir")
	if dir == "" {
		return os.Getwd()
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("dir is not a directory: %s", dir)
	}
	return filepath.Abs(dir)
}

func mcpTools(exe string) []mcp.Tool {
	// quick: run to completion and return the output
	quick := func(name, desc string, props map[string]string, argv func(a map[string]any) ([]string, error), required ...string) mcp.Tool {
		return mcp.Tool{Name: name, Description: desc, Schema: schema(props, required...),
			Call: func(a map[string]any) (string, error) {
				dir, err := projectDir(a)
				if err != nil {
					return "", err
				}
				args, err := argv(a)
				if err != nil {
					return "", err
				}
				return runQuick(exe, dir, args)
			}}
	}
	// heavy: start detached, return pid and log
	heavy := func(name, desc string, props map[string]string, argv func(a map[string]any) ([]string, error), required ...string) mcp.Tool {
		return mcp.Tool{Name: name, Description: desc + " Runs detached: returns a pid and a log; poll with loom_board and loom_log.", Schema: schema(props, required...),
			Call: func(a map[string]any) (string, error) {
				dir, err := projectDir(a)
				if err != nil {
					return "", err
				}
				args, err := argv(a)
				if err != nil {
					return "", err
				}
				return runDetached(exe, dir, args)
			}}
	}
	fixed := func(args ...string) func(map[string]any) ([]string, error) {
		return func(map[string]any) ([]string, error) { return args, nil }
	}
	withID := func(cmd string, extra ...string) func(map[string]any) ([]string, error) {
		return func(a map[string]any) ([]string, error) {
			id := str(a, "id")
			if id == "" {
				return nil, errors.New("id is required, e.g. T-001")
			}
			return append([]string{cmd, id}, extra...), nil
		}
	}
	agents := func(args []string, a map[string]any) []string {
		if n := num(a, "agents"); n > 0 {
			args = append(args, "--agents", fmt.Sprint(n))
		}
		return args
	}

	return []mcp.Tool{
		quick("loom_map", "Map of the repo: modules by layer with their tasks and contracts. Read it before writing a spec.", nil, fixed("map", "--json")),
		quick("loom_board", "Board of every task and its state (pending, running, ready, stopped). Poll it for progress.", nil, fixed("board", "--json")),
		quick("loom_ps", "Tasks with state, agent and branch.", nil, fixed("ps", "--json")),
		quick("loom_check", "Run the done-when command of one task and report.", map[string]string{"id": "string:task id, e.g. T-001"}, withID("check", "--json"), "id"),
		quick("loom_task_logs", "Attempts of one task: what each agent tried and why it failed.", map[string]string{"id": "string:task id"}, withID("logs", "--json"), "id"),
		quick("loom_doctor", "Check git, config, executors, models and keys.", nil, fixed("doctor", "--json")),
		quick("loom_usage", "Token and budget usage per window.", nil, fixed("usage", "--json")),
		quick("loom_standup", "Standup computed from the run artifacts.", nil, fixed("standup", "--json")),
		quick("loom_init", "Create .loom/ in the repo. Pass tests to skip detection.", map[string]string{"tests": "string:test command"},
			func(a map[string]any) ([]string, error) {
				args := []string{"init", "--no-skills", "--json"}
				if t := str(a, "tests"); t != "" {
					args = append(args, "--tests", t)
				}
				return args, nil
			}),
		quick("loom_apply_dry_run", "Validate a spec and show the plan without writing anything.", map[string]string{"file": "string:spec path (defaults to loom.spec.yml)"},
			func(a map[string]any) ([]string, error) {
				args := []string{"apply", "--dry-run", "--json"}
				if f := str(a, "file"); f != "" {
					args = append(args, "-f", f)
				}
				return args, nil
			}),
		quick("loom_stop", "Stop running tasks.", nil, fixed("stop", "--json")),
		{Name: "loom_spec_guide", Description: "Format of loom.spec.yml and how to interview the human to fill it.",
			Schema: schema(nil), Call: func(map[string]any) (string, error) { return specGuide, nil }},
		{Name: "loom_spec_write", Description: "Validate and write a spec you drafted from the interview. Refuses to overwrite unless force.",
			Schema: schema(map[string]string{"content": "string:the YAML", "path": "string:relative path (defaults to loom.spec.yml)", "force": "boolean:overwrite"}, "content"),
			Call: func(a map[string]any) (string, error) {
				dir, err := projectDir(a)
				if err != nil {
					return "", err
				}
				content, _ := a["content"].(string)
				return writeSpec(dir, str(a, "path"), content, on(a, "force"))
			}},
		{Name: "loom_log", Description: "Tail of a detached run's log (latest by default).",
			Schema: schema(map[string]string{"name": "string:log file name from loom_up output", "lines": "number:how many lines (default 40)"}),
			Call: func(a map[string]any) (string, error) {
				dir, err := projectDir(a)
				if err != nil {
					return "", err
				}
				n := num(a, "lines")
				if n <= 0 {
					n = 40
				}
				return tailLog(dir, str(a, "name"), n)
			}},
		heavy("loom_up", "Plan, run in isolated rooms and optionally ship. Give a request, or omit it to apply loom.spec.yml.",
			map[string]string{"request": "string:natural-language request", "agents": "number:parallel workers", "ship": "boolean:open one PR when done"},
			func(a map[string]any) ([]string, error) {
				args := []string{"up"}
				if r := str(a, "request"); r != "" {
					args = append(args, r)
				}
				if on(a, "ship") {
					args = append(args, "--ship")
				}
				return agents(args, a), nil
			}),
		heavy("loom_run", "Run the pending tasks.", map[string]string{"agents": "number:parallel workers", "retry": "boolean:rerun stopped tasks"},
			func(a map[string]any) ([]string, error) {
				args := agents([]string{"run"}, a)
				if on(a, "retry") {
					args = append(args, "--retry")
				}
				return args, nil
			}),
		heavy("loom_ship", "Deliver one ready task, or all of them in one PR.", map[string]string{"id": "string:task id", "all": "boolean:ship every ready task", "dry_run": "boolean:run the gate only"},
			func(a map[string]any) ([]string, error) {
				args := []string{"ship"}
				if id := str(a, "id"); id != "" {
					args = append(args, id)
				} else if on(a, "all") {
					args = append(args, "--all")
				} else {
					return nil, errors.New("pass id or all")
				}
				if on(a, "dry_run") {
					args = append(args, "--dry-run")
				}
				return args, nil
			}),
		heavy("loom_redteam", "A saboteur attacks a task; only a reproduced crash counts.", map[string]string{"id": "string:task id", "all": "boolean:every ready task"},
			func(a map[string]any) ([]string, error) {
				if id := str(a, "id"); id != "" {
					return []string{"redteam", id}, nil
				}
				if on(a, "all") {
					return []string{"redteam", "--all"}, nil
				}
				return nil, errors.New("pass id or all")
			}),
		heavy("loom_heal", "Diagnose a crash log and open a corrective task.", map[string]string{"log": "string:stacktrace or log text"},
			func(a map[string]any) ([]string, error) {
				log := str(a, "log")
				if log == "" {
					return nil, errors.New("log is required")
				}
				dir, err := projectDir(a)
				if err != nil {
					return nil, err
				}
				f, err := os.CreateTemp(corridasDir(dir), "heal-*.txt")
				if err != nil {
					return nil, err
				}
				defer f.Close()
				if _, err := f.WriteString(log); err != nil {
					return nil, err
				}
				return []string{"heal", "--from", f.Name()}, nil
			}, "log"),
	}
}

func corridasDir(dir string) string {
	d := filepath.Join(dir, ".loom", "corridas")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func runQuick(exe, dir string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	text := strings.TrimSpace(buf.String())
	if err != nil {
		return text, errors.New(text + "\n" + err.Error())
	}
	return text, nil
}

// runDetached starts the command in its own session with output to a log,
// and waits two seconds so an immediate failure comes back as an error
// instead of a pid that is already dead.
func runDetached(exe, dir string, args []string) (string, error) {
	log := filepath.Join(corridasDir(dir), time.Now().Format("2006-01-02T15-04-05")+".log")
	f, err := os.Create(log)
	if err != nil {
		return "", err
	}
	defer f.Close()
	cmd := exec.Command(exe, append(args, "--plain")...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = f, f
	cmd.SysProcAttr = desprender()
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	name := filepath.Base(log)
	select {
	case err := <-done:
		out, _ := os.ReadFile(log)
		if err != nil {
			return "", fmt.Errorf("failed at start\n%s", strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	case <-time.After(2 * time.Second):
		return fmt.Sprintf("started · pid %d · log %s · poll loom_board, tail with loom_log", cmd.Process.Pid, name), nil
	}
}

func tailLog(dir, name string, n int) (string, error) {
	d := corridasDir(dir)
	if name == "" {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".log") {
				name = e.Name() // sorted by name = by time
			}
		}
		if name == "" {
			return "", errors.New("no runs yet")
		}
	}
	data, err := os.ReadFile(filepath.Join(d, filepath.Base(name)))
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}

func writeSpec(dir, path, content string, force bool) (string, error) {
	if path == "" {
		path = "loom.spec.yml"
	}
	dest := filepath.Join(dir, path)
	if rel, err := filepath.Rel(dir, dest); err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("path must stay inside the project")
	}
	if _, err := spec.Parse([]byte(content)); err != nil {
		return "", fmt.Errorf("spec does not parse: %w", err)
	}
	if _, err := os.Stat(dest); err == nil && !force {
		return "", fmt.Errorf("%s exists · pass force to overwrite", path)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		return "", err
	}
	return "wrote " + path + " · validate with loom_apply_dry_run", nil
}
