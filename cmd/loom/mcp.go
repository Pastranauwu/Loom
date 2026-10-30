package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/mcp"
	"github.com/Pastranauwu/loom/internal/skills"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/wrap"
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
constraints:         # globs no task may edit; copied into every contract
  dont_touch: ["vendor/**"]

Interview the human yourself: ask about edge cases, failures and limits they did
not mention, settle them as requirements or rules (prefix an unanswered one with
"Assumption:"), then call loom_spec_write and loom_apply_dry_run. Read the repo
first with loom_map.

If the feature draws screens, settle a design direction before writing: 2-3
products the human wants it to feel like, density (airy or compact), mood, and
what it must NOT look like. Write it as concrete rules, not adjectives: palette as
tokens with hex, type scale with sizes and weights, radius and spacing steps, one
signature element that makes it recognizable, and the layout of the main screen.
"modern and clean" is not a rule: builders and the visual reviewer read these
rules, and vague ones give generic screens.`

// manualAgentes is the full manual for the model driving loom, served by
// loom_manual and `loom manual`.
//
//go:embed manual.md
var manualAgentes string

// instruccionesMCP reach every MCP client on connect, so codex and opencode
// get the same role the loom ide prompt gives claude.
const instruccionesMCP = `loom turns a spec into verified code: it plans tasks and runs coding agents in isolated git worktrees, and only what passes its done_when command ships. You are the tech lead: interview the human, write the spec, set each task's permissions, launch and watch. Never write the builders' code yourself.
Workflow: loom_map → interview (loom_spec_guide) → loom_spec_write → loom_apply_dry_run → loom_apply (if the architect asks, loom_answer and apply again) → loom_task_show/loom_task_set per task → loom_run → loom_board with wait: 600 (never poll: each call re-reads your whole context) → loom_ship (dry_run first) → loom_archive when ship all passed: one spec is one version.
Heavy tools (apply, up, run, ship, redteam, heal) spend the human's quota and run detached: say what you launch first. A red task: read loom_task_logs before retrying. loom_wrap turns a CLI, cURL, HAR capture or OpenAPI spec into bridge_* tools in this server.
Call loom_manual once for the full guide before your first heavy tool.`

// loom mcp: the chat model is the tech lead, loom the builders. Every tool
// is this same binary run with --json, so the CLI stays the one source of
// truth and the server adds no logic of its own.
func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "serve loom to an MCP client (Claude Desktop, Cursor, opencode) over stdio",
		Long: `Speaks the Model Context Protocol on stdin/stdout. Quick tools (map, board,
check, apply dry-run) answer live; heavy ones (up, run, ship, heal, redteam)
start detached and return a pid and a log, so the chat never blocks. Follow them
with loom_board and loom_log. Bridges in .loom/bridges (loom wrap) are served
as bridge_<name>_<tool> and reloaded on every request.`,
		Example: `  claude mcp add loom -- loom mcp`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			mcp.Instructions = instruccionesMCP
			return mcp.Serve(os.Stdin, os.Stdout, "loom", version, mcpTools(exe), bridgeTools)
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
		prop := map[string]any{"type": typ, "description": desc}
		if opciones, ok := strings.CutPrefix(typ, "enum("); ok {
			prop["type"], prop["enum"] = "string", strings.Split(strings.TrimSuffix(opciones, ")"), "|")
		}
		if typ == "array" {
			prop["items"] = map[string]any{"type": "string"}
		}
		p[k] = prop
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
		return mcp.Tool{Name: name, Description: desc + " Runs detached: returns a pid and a log; follow it with loom_board wait: 600, not by polling.", Schema: schema(props, required...),
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
		quick("loom_map", "Map of the repo: modules by layer with their tasks, exposed contracts, gates and active DNA. Read it before writing a spec.",
			map[string]string{"format": "string:json (default), tree for compact ASCII, or mermaid", "all": "boolean:include modules without tasks or contracts"},
			func(a map[string]any) ([]string, error) {
				args := []string{"map", "--json"}
				switch str(a, "format") {
				case "tree":
					args = []string{"map", "--plain"}
				case "mermaid":
					args = []string{"map", "--mermaid", "--plain"}
				}
				if on(a, "all") {
					args = append(args, "--all")
				}
				return args, nil
			}),
		{Name: "loom_board", Description: "Task board, compact. The first call lists every task; later calls return only what changed since the last one (or 'no changes'). Pass full to list everything again. Pass wait (seconds, up to 600) to block until something changes instead of polling: every poll costs you a full turn.",
			Schema: schema(map[string]string{"full": "boolean:list every task again", "wait": "number:seconds to block until a task changes (max 600)"}),
			Call: func(a map[string]any) (string, error) {
				dir, err := projectDir(a)
				if err != nil {
					return "", err
				}
				// esperar aquí es gratis; cada sondeo del modelo relee su contexto entero
				hasta := time.Now().Add(time.Duration(min(max(num(a, "wait"), 0), 600)) * time.Second)
				for {
					raw, err := runQuick(exe, dir, []string{"board", "--json"})
					if err != nil {
						return "", err
					}
					d, err := boardDelta(dir, raw, on(a, "full"))
					// "no tasks" también espera: mientras el arquitecto
					// planea no hay tareas, y volver al instante hacía que
					// el conductor sondeara durante toda la planeación
					fin := terminoVuelo(dir)
					if err != nil || fin != "" || (!strings.HasPrefix(d, "no changes") && d != "no tasks") || time.Now().After(hasta) {
						return d + fin, err
					}
					time.Sleep(esperaBoard)
				}
			}},
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
		quick("loom_apply_dry_run", "Validate a spec (format, ids, rules) without planning or writing anything; it does not show tasks. Use loom_apply to get them.", map[string]string{"file": "string:spec path (defaults to loom.spec.yml)"},
			func(a map[string]any) ([]string, error) {
				args := []string{"apply", "--dry-run", "--json"}
				if f := str(a, "file"); f != "" {
					args = append(args, "-f", f)
				}
				return args, nil
			}),
		quick("loom_stop", "Stop running tasks.", nil, fixed("stop", "--json")),
		{Name: "loom_answer", Description: "Answer the architect's questions (loom_apply stops with them before writing anything). One answer per question, in order; empty = its default. Settle what you can yourself; ask the human only what you cannot. Then call loom_apply again: the architect continues its same session.",
			Schema: schema(map[string]string{"answers": "array:one answer per question"}, "answers"),
			Call: func(a map[string]any) (string, error) {
				dir, err := projectDir(a)
				if err != nil {
					return "", err
				}
				args := []string{"answer", "--json", "--"}
				if l, ok := a["answers"].([]any); ok {
					for _, x := range l {
						args = append(args, fmt.Sprint(x))
					}
				}
				return runQuick(exe, dir, args)
			}},
		quick("loom_archive", "Close the version: after loom_ship all passed every acceptance command, file the spec and its result in .loom/historial, set the shipped tasks aside and leave the spec blank for the next version. Refuses if the last joint delivery did not pass. Never archive by moving .loom files yourself.", nil, fixed("archive", "--json")),
		{Name: "loom_task_show", Description: "Contract of one task: what it may touch, what it may not, exposes, weight, retries, network.",
			Schema: schema(map[string]string{"id": "string:task id"}, "id"),
			Call: func(a map[string]any) (string, error) {
				return editTask(a, false)
			}},
		{Name: "loom_task_set", Description: "Set the contract of a task before it runs: only_touch (globs it may edit), dont_touch (globs it may not), exposes (public signatures it must provide), weight, max_retries, depends_on, network (its tests need the network), skills. Fields you omit stay as they are. Out-of-scope edits are reverted by code, so this is the real boundary.",
			Schema: schema(map[string]string{"id": "string:task id", "only_touch": "array:globs the task may edit", "dont_touch": "array:globs the task may not edit",
				"exposes": "array:public signatures other tasks rely on, e.g. wol.Send(mac, addr string) error",
				"weight":  "enum(light|medium|heavy):model weight of the task", "max_retries": "integer:attempts before the task stops",
				"depends_on": "array:task ids that must finish first", "network": "boolean:its tests need network access (the sandbox keeps only loopback otherwise)",
				"skills": "array:skills injected into the builder's prompt (loom_skills lists them); omitted = its role's, [] = none"}, "id"),
			Call: func(a map[string]any) (string, error) {
				return editTask(a, true)
			}},
		{Name: "loom_skills", Description: "Skill dictionary: what each skill is for, which roles get it by default, which are yours (the orchestrator's) and which are installed. Pick per task with loom_task_set skills; each one costs tokens on every attempt.",
			Schema: schema(nil), Call: func(a map[string]any) (string, error) {
				dir, err := projectDir(a)
				if err != nil {
					return "", err
				}
				root, err := config.RepoRoot(dir)
				if err != nil {
					root = dir
				}
				return diccionarioSkills(root)
			}},
		{Name: "loom_manual", Description: "Full manual for the model driving loom: workflow, every tool and when to use it, reverse engineering apps into tools with loom_wrap, models and providers. Read it once before your first heavy tool.",
			Schema: schema(nil), Call: func(map[string]any) (string, error) { return manualAgentes, nil }},
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
		{Name: "loom_log", Description: "New lines of a detached run's log since your last call (latest log by default; the first call returns the tail). Pass full for the tail again.",
			Schema: schema(map[string]string{"name": "string:log file name from loom_up output", "lines": "number:max lines (default 40)", "full": "boolean:ignore what you already read"}),
			Call: func(a map[string]any) (string, error) {
				dir, err := projectDir(a)
				if err != nil {
					return "", err
				}
				n := num(a, "lines")
				if n <= 0 {
					n = 40
				}
				return tailLog(dir, str(a, "name"), n, on(a, "full"))
			}},
		heavy("loom_apply", "Plan the spec into task contracts WITHOUT running them. Do this first so you can set each task's permissions with loom_task_set, then start them with loom_run. Then call loom_board with wait to block until the tasks appear.",
			map[string]string{"file": "string:spec path (defaults to loom.spec.yml)"},
			func(a map[string]any) ([]string, error) {
				args := []string{"apply"}
				if f := str(a, "file"); f != "" {
					args = append(args, "-f", f)
				}
				return args, nil
			}),
		heavy("loom_up", "Plan AND run in one step, with no chance to set task permissions in between: prefer loom_apply then loom_run. Give a request, or omit it to apply loom.spec.yml.",
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
		heavy("loom_ship", "Deliver one ready task, or all of them in one PR.", map[string]string{"id": "string:task id", "all": "boolean:ship every ready task", "dry_run": "boolean:run the gate only",
			"release": "boolean:formal release: also build the docker compose images (with all)", "tag": "string:release version; like release and leaves a local annotated tag"},
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
				if tag := str(a, "tag"); tag != "" {
					args = append(args, "--tag", tag)
				} else if on(a, "release") {
					args = append(args, "--release")
				}
				return args, nil
			}),
		heavy("loom_redteam", "A saboteur attacks a task; only a reproduced crash counts. The report lands in the log.",
			map[string]string{"id": "string:task id", "all": "boolean:every ready task", "reopen": "boolean:reopen the task with each confirmed crash in its contract",
				"keep_tests": "boolean:keep the attack tests in the repo", "dry_run": "boolean:say what would be attacked, no model call"},
			func(a map[string]any) ([]string, error) {
				args := []string{"redteam"}
				if id := str(a, "id"); id != "" {
					args = append(args, id)
				} else if on(a, "all") {
					args = append(args, "--all")
				} else {
					return nil, errors.New("pass id or all")
				}
				for k, flag := range map[string]string{"reopen": "--reopen", "keep_tests": "--keep-tests", "dry_run": "--dry-run"} {
					if on(a, k) {
						args = append(args, flag)
					}
				}
				return args, nil
			}),
		heavy("loom_heal", "Diagnose a crash log, add the missing rule and acceptance criterion to the spec and run a corrective task.",
			map[string]string{"log": "string:stacktrace or log text", "dry_run": "boolean:only show the diagnosis"},
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
				args := []string{"heal", "--from", f.Name()}
				if on(a, "dry_run") {
					args = append(args, "--dry-run")
				}
				return args, nil
			}, "log"),
		quick("loom_dna", "Design archetypes the agents follow (hexagonal, zero-deps...): list them, show the active block, add or remove one.",
			map[string]string{"action": "string:list, show, add or rm", "name": "string:archetype name for add and rm"},
			func(a map[string]any) ([]string, error) {
				act := str(a, "action")
				switch act {
				case "list", "show":
					return []string{"dna", act, "--json"}, nil
				case "add", "rm":
					if str(a, "name") == "" {
						return nil, errors.New("name is required for " + act)
					}
					return []string{"dna", act, str(a, "name"), "--plain"}, nil
				}
				return nil, errors.New("action must be list, show, add or rm")
			}, "action"),
		{Name: "loom_wrap", Description: "Turn a local CLI, a cURL command, a HAR file or an OpenAPI spec into a bridge: its tools appear in this server as bridge_<name>_<tool> right away. Pass exactly one source.",
			Schema: schema(map[string]string{"cli": "string:binary on the PATH", "curl": "string:full curl command or a file with it", "har": "string:HAR file path",
				"openapi": "string:OpenAPI/Swagger path or URL", "name": "string:bridge name", "dry_run": "boolean:show the manifest without saving"}),
			Call: func(a map[string]any) (string, error) {
				dir, err := projectDir(a)
				if err != nil {
					return "", err
				}
				args := []string{"wrap", "--plain"}
				for _, k := range []string{"cli", "curl", "har", "openapi", "name"} {
					if v := str(a, k); v != "" {
						args = append(args, "--"+k, v)
					}
				}
				if on(a, "dry_run") {
					args = append(args, "--dry-run")
				}
				return runQuickT(exe, dir, args, 15*time.Minute)
			}},
		quick("loom_wrap_list", "Bridges in .loom/bridges with their tool names.", nil, fixed("wrap", "list", "--json")),
		quick("loom_wrap_show", "Manifest of one bridge: tools, input schemas and exec/http templates.", map[string]string{"name": "string:bridge name"},
			func(a map[string]any) ([]string, error) {
				return []string{"wrap", "show", str(a, "name"), "--json"}, nil
			}, "name"),
		quick("loom_wrap_rm", "Delete a bridge; its tools disappear from this server.", map[string]string{"name": "string:bridge name"},
			func(a map[string]any) ([]string, error) {
				return []string{"wrap", "rm", str(a, "name"), "--plain"}, nil
			}, "name"),
	}
}

func corridasDir(dir string) string {
	d := filepath.Join(dir, ".loom", "corridas")
	_ = os.MkdirAll(d, 0o755)
	return d
}

// bridgeTools reads the manifests of the server's repo on every request:
// a bridge written by loom_wrap is callable without restarting the server.
func bridgeTools() []mcp.Tool {
	dir, err := os.Getwd()
	if err != nil {
		return nil
	}
	if root, err := config.RepoRoot(dir); err == nil {
		dir = root
	}
	ms, _ := wrap.List(dir)
	var tools []mcp.Tool
	for _, m := range ms {
		for _, t := range m.Tools {
			t := t
			tools = append(tools, mcp.Tool{Name: m.ToolName(t), Description: t.Description, Schema: t.InputSchema,
				Call: func(a map[string]any) (string, error) { return wrap.Invoke(t, a) }})
		}
	}
	return tools
}

func runQuick(exe, dir string, args []string) (string, error) {
	return runQuickT(exe, dir, args, 2*time.Minute)
}

func runQuickT(exe, dir string, args []string, limite time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limite)
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
	// --json calla las líneas de progreso: sin esto un dry-run válido
	// respondía vacío y el chat no sabía si había pasado
	if text == "" {
		text = "ok"
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
	v := &vuelo{fin: make(chan struct{}), log: log}
	vistoMu.Lock()
	vuelos[dir] = v
	vistoMu.Unlock()
	go func() {
		err := cmd.Wait()
		v.err = err
		close(v.fin)
		done <- err
	}()
	name := filepath.Base(log)
	select {
	case err := <-done:
		out, _ := os.ReadFile(log)
		if err != nil {
			return "", fmt.Errorf("failed at start\n%s", strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	case <-time.After(2 * time.Second):
		return fmt.Sprintf("started · pid %d · log %s · wait with loom_board wait: 600, tail with loom_log", cmd.Process.Pid, name), nil
	}
}

// vuelo es el último comando pesado lanzado en un proyecto: loom_board
// con wait vuelve cuando termina, con la cola de su log, en vez de
// esperar los 600 s sobre un tablero que ya no va a cambiar.
type vuelo struct {
	fin chan struct{}
	err error
	log string
}

var vuelos = map[string]*vuelo{} // dir → último comando pesado

// terminoVuelo devuelve, una sola vez, el aviso de que el comando pesado
// del proyecto terminó con las últimas líneas de su log; "" si sigue o no hay.
func terminoVuelo(dir string) string {
	vistoMu.Lock()
	v := vuelos[dir]
	vistoMu.Unlock()
	if v == nil {
		return ""
	}
	select {
	case <-v.fin:
	default:
		return ""
	}
	vistoMu.Lock()
	delete(vuelos, dir)
	vistoMu.Unlock()
	estado := "ok"
	if v.err != nil {
		estado = "failed: " + v.err.Error()
	}
	b, _ := os.ReadFile(v.log)
	lineas := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lineas) > 12 {
		lineas = lineas[len(lineas)-12:]
	}
	return fmt.Sprintf("\n%s ended (%s) · last lines:\n%s", filepath.Base(v.log), estado, strings.Join(lineas, "\n"))
}

var (
	vistoMu    sync.Mutex
	vistoBoard = map[string]map[string]string{} // dir → id → "estado|detalle"
	vistoLog   = map[string]int{}               // ruta del log → bytes ya leídos
)

// boardDelta turns the board JSON into one line per task and, after the
// first call, only the lines that changed: a polling chat should not pay
// for the whole board every time.
// esperaBoard es cada cuánto mira el tablero un loom_board con wait. Var para pruebas.
var esperaBoard = 5 * time.Second

func boardDelta(dir, raw string, full bool) (string, error) {
	var cols map[string][]map[string]any
	if err := json.Unmarshal([]byte(raw), &cols); err != nil {
		return "", err
	}
	ahora := map[string]string{}
	titulos := map[string]string{}
	for estado, filas := range cols {
		for _, f := range filas {
			id, _ := f["id"].(string)
			detalle, _ := f["detalle"].(string)
			ahora[id] = estado + "|" + detalle
			titulos[id], _ = f["titulo"].(string)
		}
	}
	vistoMu.Lock()
	antes, hubo := vistoBoard[dir]
	vistoBoard[dir] = ahora
	vistoMu.Unlock()

	ids := make([]string, 0, len(ahora))
	for id := range ahora {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	cambios := 0
	for _, id := range ids {
		estado, detalle, _ := strings.Cut(ahora[id], "|")
		if hubo && !full {
			if antes[id] == ahora[id] {
				continue
			}
			prev, _, _ := strings.Cut(antes[id], "|")
			if prev != "" && prev != estado {
				estado = prev + "→" + estado
			}
		}
		cambios++
		fmt.Fprintf(&b, "%s %s", id, estado)
		if !hubo || full {
			fmt.Fprintf(&b, " %s", titulos[id])
		}
		if detalle != "" {
			fmt.Fprintf(&b, " · %s", detalle)
		}
		b.WriteString("\n")
	}
	if hubo && !full {
		for id := range antes {
			if _, sigue := ahora[id]; !sigue {
				cambios++
				fmt.Fprintf(&b, "%s removed\n", id)
			}
		}
	}
	if cambios == 0 {
		if len(ahora) == 0 {
			return "no tasks", nil
		}
		return fmt.Sprintf("no changes · %d tasks", len(ahora)), nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func tailLog(dir, name string, n int, full bool) (string, error) {
	d := corridasDir(dir)
	if name == "" {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".log") {
				name = e.Name() // ordenados por nombre = por hora
			}
		}
		if name == "" {
			return "", errors.New("no runs yet")
		}
	}
	ruta := filepath.Join(d, filepath.Base(name))
	data, err := os.ReadFile(ruta)
	if err != nil {
		return "", err
	}
	vistoMu.Lock()
	leido := vistoLog[ruta]
	vistoLog[ruta] = len(data)
	vistoMu.Unlock()
	if full || leido > len(data) {
		leido = 0
	}
	nuevo := strings.TrimRight(string(data[leido:]), "\n")
	if nuevo == "" {
		return "no new lines", nil
	}
	lines := strings.Split(nuevo, "\n")
	if len(lines) > n {
		lines = append([]string{fmt.Sprintf("… %d earlier lines skipped", len(lines)-n)}, lines[len(lines)-n:]...)
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

func list(a map[string]any, k string) ([]string, bool) {
	raw, ok := a[k].([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out, true
}

func editTask(a map[string]any, set bool) (string, error) {
	dir, err := projectDir(a)
	if err != nil {
		return "", err
	}
	root, err := config.RepoRoot(dir)
	if err != nil {
		return "", err
	}
	tasks := config.TasksDir(root)
	id := str(a, "id")
	if !task.ValidID(id) {
		return "", errors.New("id must look like T-001")
	}
	t, err := task.Load(tasks, id)
	if err != nil {
		return "", err
	}
	if set {
		if v, ok := list(a, "only_touch"); ok {
			t.TocarSolo = v
		}
		if v, ok := list(a, "dont_touch"); ok {
			t.NoTocar = v
		}
		if v, ok := list(a, "depends_on"); ok {
			t.DependeDe = v
		}
		if v, ok := list(a, "exposes"); ok {
			t.Expone = v
		}
		if v, ok := a["network"].(bool); ok {
			t.Red = v
		}
		if w := str(a, "weight"); w != "" {
			t.Peso = task.NormalizarPeso(w)
		}
		if n := num(a, "max_retries"); n > 0 {
			t.LimiteIntentos = n
		}
		if v, ok := list(a, "skills"); ok {
			t.Skills = v
		}
		if errs := t.Validate(); len(errs) > 0 {
			msgs := make([]string, len(errs))
			for i, e := range errs {
				msgs[i] = e.Error()
			}
			return "", errors.New(id + " would be invalid · " + strings.Join(msgs, " · "))
		}
		if err := task.Save(tasks, t); err != nil {
			return "", err
		}
	}
	data, err := json.MarshalIndent(vistaTarea(t), "", "  ")
	return string(data), err
}

// diccionarioSkills es el diccionario tal como lo ve el orquestador.
func diccionarioSkills(root string) (string, error) {
	type fila struct {
		Name         string   `json:"name"`
		For          string   `json:"for"`
		Roles        []string `json:"roles,omitempty"`
		Orchestrator bool     `json:"orchestrator,omitempty"`
		Installed    bool     `json:"installed"`
	}
	var filas []fila
	visto := map[string]bool{}
	for _, s := range skills.DefaultSources() {
		visto[s.Nombre] = true
		filas = append(filas, fila{s.Nombre, s.Para, s.Roles, s.Orquestador, skills.Instalado(root, s.Nombre) || s.Orquestador && skills.InstaladoGlobal(s.Nombre)})
	}
	for _, sk := range skills.Catalogo(root) {
		if !visto[sk.Nombre] {
			filas = append(filas, fila{Name: sk.Nombre, For: sk.Descripcion, Installed: true})
		}
	}
	data, err := json.MarshalIndent(filas, "", "  ")
	return string(data), err
}

// vistaTarea es el contrato con las mismas llaves que acepta loom_task_set.
func vistaTarea(t task.Task) map[string]any {
	return map[string]any{
		"id": t.ID, "title": t.Titulo, "why": t.Porque, "done_when": t.ListoCuando,
		"only_touch": t.TocarSolo, "dont_touch": t.NoTocar, "depends_on": t.DependeDe,
		"exposes": t.Expone, "uses": t.Usa, "weight": task.PesoEn(t.Peso),
		"max_retries": t.LimiteIntentos, "max_lines": t.LimiteLineas, "network": t.Red, "skills": t.Skills,
	}
}

func newManualCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "manual",
		Short: "print the manual for the model driving loom (what loom_manual serves over MCP)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprint(cmd.OutOrStdout(), manualAgentes)
			return err
		},
	}
}
