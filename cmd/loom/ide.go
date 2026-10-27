package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/idioma"
)

const rolTechLead = `You are the tech lead of this repository and the human's only voice. Loom builders work in isolated rooms and never see this chat: they only get a task contract.
Workflow: read the repo with loom_map; interview the human about edge cases and limits (loom_spec_guide); write the spec with loom_spec_write and check it with loom_apply_dry_run.
Before anything runs, decide the permissions of every task: loom_task_show then loom_task_set. Keep only_touch as narrow as the task allows, put secrets, config and other modules in dont_touch, and set weight and max_retries by risk. Code reverts edits outside that scope, so it is the real boundary.
Then launch with loom_run or loom_up and poll loom_board and loom_log. Heavy tools spend the human's budget: say what you are about to launch first. Never write the builders' code yourself.`

// herramientas de solo lectura: las que el chat puede llamar sin preguntar
var mcpSoloLectura = []string{"loom_map", "loom_board", "loom_ps", "loom_check", "loom_task_logs", "loom_task_show",
	"loom_doctor", "loom_usage", "loom_standup", "loom_spec_guide", "loom_log", "loom_apply_dry_run"}

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

func crearIDE(root, exe, s string) error {
	mcp := fmt.Sprintf(`{"mcpServers":{"loom":{"command":%q,"args":["mcp"]}}}`, exe)
	var tools []string
	for _, t := range mcpSoloLectura {
		tools = append(tools, "mcp__loom__"+t)
	}
	claude := "claude --mcp-config " + comillas(mcp) +
		" --allowedTools " + comillas(strings.Join(tools, ",")) +
		" --append-system-prompt " + comillas(rolTechLead)
	panel := func(p string) string { return comillas(exe) + " panel " + p }

	// el panel derecho se parte de arriba abajo: tablero, mapa, consumo
	pasos := [][]string{
		{"new-session", "-d", "-s", s, "-c", root, claude},
		{"split-window", "-h", "-t", s, "-l", "34%", "-c", root, panel("board")},
		{"split-window", "-v", "-t", s + ":.1", "-l", "55%", "-c", root, panel("map")},
		{"split-window", "-v", "-t", s + ":.2", "-l", "36%", "-c", root, panel("usage")},
		{"set-option", "-t", s, "mouse", "on"},
		{"set-option", "-t", s, "pane-border-status", "top"},
		{"set-option", "-t", s, "status-right", " ctrl+b d · " + idioma.T("ocultar") + "   loom ide · " + idioma.T("volver") + " "},
		{"select-pane", "-t", s + ":.0", "-T", "claude · loom mcp"},
		{"select-pane", "-t", s + ":.1", "-T", "board"},
		{"select-pane", "-t", s + ":.2", "-T", "map"},
		{"select-pane", "-t", s + ":.3", "-T", "usage"},
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

// loom panel <board|map|usage> redibuja un comando cada 2 s. Es lo que
// corre en los paneles de la derecha; no tiene uso fuera de loom ide.
func newPanelCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "panel <board|map|usage>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "board" && args[0] != "map" && args[0] != "usage" {
				return fmt.Errorf("panel desconocido: %s", args[0])
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			for {
				salida, _ := exec.Command(exe, args[0], "--plain").CombinedOutput()
				fmt.Print("\033[H\033[2J", string(salida))
				time.Sleep(2 * time.Second)
			}
		},
	}
}
