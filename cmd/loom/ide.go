package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/skills"
	"github.com/Pastranauwu/loom/internal/tui"
)

const rolTechLead = `You are the tech lead of this repository and the human's only voice. Loom builders work in isolated rooms and never see this chat: they only get a task contract.
Workflow: read the repo with loom_map; interview the human about edge cases and limits (loom_spec_guide); write the spec with loom_spec_write, check it with loom_apply_dry_run, then call loom_apply: it plans the tasks without running them.
Before anything runs, decide the permissions of every task: loom_task_show then loom_task_set. Keep only_touch as narrow as the task allows, put secrets, config and other modules in dont_touch, and set weight and max_retries by risk. Code reverts edits outside that scope, so it is the real boundary.
Then launch with loom_run (not loom_up, which skips this step) and wait with loom_board wait: 600 (never poll: each call re-reads your whole context). Heavy tools spend the human's budget: say what you are about to launch first. Never write the builders' code yourself.
Before you call interface work done, open its screenshots in .loom/runs/<task>/visual-*: green tests do not mean it looks right.
Skills: talk to the human with your caveman skill (terse, every turn costs tokens) and interview with grill-with-docs (grilling and domain-modeling) before writing the spec. Builders get skills per task: loom_skills is the dictionary, set them with loom_task_set skills, and only what the task needs, since each one costs tokens on every attempt. If the dictionary lacks one, find it with find-skills (npx skills find <topic>).
How you talk to the human: never show code. Say what task runs, what it is for, and in one or two lines why it was split or decided that way, with a plain everyday analogy (a kitchen, a building site, a relay race) instead of jargon. Short answers: a few lines, no lists of files or flags unless asked. Skip technical detail that does not change a decision.`

// herramientas de solo lectura: las que el chat puede llamar sin preguntar
var mcpSoloLectura = []string{"loom_map", "loom_board", "loom_ps", "loom_check", "loom_task_logs", "loom_task_show",
	"loom_doctor", "loom_usage", "loom_standup", "loom_spec_guide", "loom_manual", "loom_log", "loom_apply_dry_run", "loom_skills"}

func newIDECmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ide",
		Short: "open the loom workspace: Claude (2/3) with board, map and usage (1/3)",
		Long: `Opens a tmux session: your Claude session, already wired to loom over MCP, on
the left (2/3); on the right (1/3) the task board, the map and the usage,
live. Detach with ctrl+b d and the work keeps running; run loom ide again to
come back to the same session.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			return runIDE(root)
		},
	}
}

var noNombre = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func sesionIDE(root string) string {
	return "loom-" + noNombre.ReplaceAllString(filepath.Base(root), "_")
}

func comillas(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func tmux(args ...string) error {
	if o, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux %s: %s", args[0], strings.TrimSpace(string(o)))
	}
	return nil
}

func runIDE(root string) error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return errors.New(idioma.T("falta tmux · instálalo (pacman -S tmux, apt install tmux, brew install tmux) y repite loom ide"))
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return errors.New(idioma.T("falta claude · instala Claude Code y repite loom ide"))
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	s := sesionIDE(root)
	if exec.Command("tmux", "has-session", "-t", s).Run() != nil {
		// las skills del orquestador (hablar corto, entrevistar, buscar skills) van antes de abrir el chat
		for _, err := range skills.AsegurarOrquestador(context.Background()) {
			out.Line("· %s", err)
		}
		if err := crearIDE(root, exe, s); err != nil {
			return err
		}
	}
	verbo := "attach-session"
	if os.Getenv("TMUX") != "" {
		verbo = "switch-client"
	}
	cmd := exec.Command("tmux", verbo, "-t", s)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// modeloTechLead is the model of the chat: the medium one, never the heavy.
// An id claude does not know (an opencode one) falls back to the sonnet alias.
func modeloTechLead(root string) string {
	if cfg, err := config.Load(root); err == nil {
		m := config.ModeloRol(cfg, "techlead")
		if strings.HasPrefix(m, "claude-") || m == "sonnet" || m == "opus" || m == "haiku" {
			if !strings.Contains(m, "opus") {
				return m
			}
		}
	}
	return "sonnet"
}

func crearIDE(root, exe, s string) error {
	mcp := fmt.Sprintf(`{"mcpServers":{"loom":{"command":%q,"args":["mcp"]}}}`, exe)
	var tools []string
	for _, t := range mcpSoloLectura {
		tools = append(tools, "mcp__loom__"+t)
	}
	claude := "claude --model " + comillas(modeloTechLead(root)) + " --mcp-config " + comillas(mcp) +
		" --allowedTools " + comillas(strings.Join(tools, ",")) +
		" --append-system-prompt " + comillas(rolTechLead)
	panel := func(p string) string { return comillas(exe) + " panel " + p }

	// derecha: el navegador (tareas y mapa) arriba y el consumo, chico, abajo
	pasos := [][]string{
		{"new-session", "-d", "-s", s, "-c", root, claude},
		{"split-window", "-h", "-t", s, "-l", "34%", "-c", root, panel("nav")},
		{"split-window", "-v", "-t", s + ":.1", "-l", "9", "-c", root, panel("usage")},
		{"set-option", "-t", s, "mouse", "on"},
		{"set-option", "-t", s, "pane-border-status", "top"},
		{"set-option", "-t", s, "pane-border-format", " #{pane_title} "},
		{"set-option", "-t", s, "pane-border-style", "fg=#3a3f42"},
		{"set-option", "-t", s, "pane-active-border-style", "fg=#4FB3A2"},
		{"set-option", "-t", s, "status-style", "bg=#1c1f21,fg=#6B6F72"},
		{"set-option", "-t", s, "status-left", "#[fg=#4FB3A2,bold] loom #[default]· "},
		{"set-option", "-t", s, "window-status-current-format", ""},
		{"set-option", "-t", s, "window-status-format", ""},
		{"set-option", "-t", s, "status-right", " ctrl+b d · " + idioma.T("ocultar") + "   loom ide · " + idioma.T("volver") + " "},
		{"select-pane", "-t", s + ":.0", "-T", "claude"},
		{"select-pane", "-t", s + ":.1", "-T", idioma.T("tareas · mapa")},
		{"select-pane", "-t", s + ":.2", "-T", idioma.T("uso")},
		{"select-pane", "-t", s + ":.0"},
	}
	for _, p := range pasos {
		if err := tmux(p...); err != nil {
			_ = exec.Command("tmux", "kill-session", "-t", s).Run()
			return err
		}
	}
	return nil
}

// loom panel <nav|usage> es lo que corre en los paneles de la derecha;
// no tiene uso fuera de loom ide.
func newPanelCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "panel <nav|usage>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			switch args[0] {
			case "nav":
				return tui.Panel(root, func(ancho int) string {
					m, _, err := construirMapa(root, false, 2)
					if err != nil {
						return err.Error()
					}
					return tui.VistaMapa(m, ancho)
				})
			case "usage":
				for {
					ancho, alto := tui.Terminal(os.Stdout.Fd())
					fmt.Print("\033[H\033[2J", tui.Consumo(root, ancho, alto))
					time.Sleep(2 * time.Second)
				}
			}
			return fmt.Errorf("panel desconocido: %s", args[0])
		},
	}
}
