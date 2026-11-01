package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/skills"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/tui"
)

const rolTechLead = `You are the tech lead of this repository and the human's only voice. Loom builders work in isolated rooms and never see this chat: they only get a task contract.
Workflow: read the repo with loom_map; interview the human about edge cases and limits (loom_spec_guide); write the spec with loom_spec_write, check it with loom_apply_dry_run, then call loom_apply: it plans the tasks without running them.
Before anything runs, decide the permissions of every task: loom_task_show then loom_task_set. Keep only_touch as narrow as the task allows, put secrets, config and other modules in dont_touch, and set weight and max_retries by risk. Code reverts edits outside that scope, so it is the real boundary.
Then launch with loom_run (not loom_up, which skips this step) and wait with loom_board wait: 600 (never poll: each call re-reads your whole context). Heavy tools spend the human's budget: say what you are about to launch first. Never write the builders' code yourself.
If loom_apply stops with the architect's questions, answer what the spec settles with loom_answer and ask the human only the rest, in one short message; relay its "will build" summary in one or two plain lines.
One spec is one version: when loom_ship all passes, close it with loom_archive and start the next spec from blank. Never archive or move .loom files or specs yourself.
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

// ajustesClaude es el settings.json de usuario de Claude Code.
func ajustesClaude() string {
	casa, _ := os.UserHomeDir()
	return filepath.Join(casa, ".claude", "settings.json")
}

// soloCaveman apaga para el tech lead los plugins del usuario que no son
// caveman: cada uno mete su texto en cada turno (ponytail y
// frontend-design eran 3k de 25k) y el tech lead no escribe código.
func soloCaveman(settings string) string {
	var cfg struct {
		Plugins map[string]bool `json:"enabledPlugins"`
	}
	data, err := os.ReadFile(settings)
	if err != nil || json.Unmarshal(data, &cfg) != nil {
		return ""
	}
	apagar := map[string]bool{}
	for p, on := range cfg.Plugins {
		if on && !strings.HasPrefix(p, "caveman@") {
			apagar[p] = false
		}
	}
	if len(apagar) == 0 {
		return ""
	}
	out, _ := json.Marshal(map[string]any{"enabledPlugins": apagar})
	return " --settings " + comillas(string(out))
}

func crearIDE(root, exe, s string) error {
	mcp := fmt.Sprintf(`{"mcpServers":{"loom":{"command":%q,"args":["mcp"]}}}`, exe)
	var tools []string
	for _, t := range mcpSoloLectura {
		tools = append(tools, "mcp__loom__"+t)
	}
	// Sin MCP del usuario (Chrome y conectores de claude.ai) ni sus plugins
	// salvo caveman: la sesión real arrancó en 47k tokens y creció a 308k
	// en 505 pasos sin compactar, releyendo todo en cada paso; el 77% del
	// gasto fue releer contexto. Compactar a los 120k la mantiene chica.
	// Los ajustes de usuario se quedan: ahí viven las skills del
	// orquestador (grilling, domain-modeling) y caveman.
	claude := "CLAUDE_CODE_AUTO_COMPACT_WINDOW=120000 claude --model " + comillas(modeloTechLead(root)) +
		" --strict-mcp-config --no-chrome" + soloCaveman(ajustesClaude()) +
		" --mcp-config " + comillas(mcp) +
		" --allowedTools " + comillas(strings.Join(tools, ",")) +
		" --append-system-prompt " + comillas(rolTechLead)
	panel := func(p string) string { return comillas(exe) + " panel " + p }

	// con paneles flotantes (tmux 3.8) cada caja dibuja su borde entero y
	// la activa se ilumina sin cruces compartidos con sus vecinas. Debajo,
	// el fondo pinta la cabecera y la base. Sin ellos, mosaico
	if err := crearCajas(root, s, claude, panel); err == nil {
		_ = tmux("set-option", "-t", s, "mouse", "on")
		estiloIDE(s, root, exe)
		return nil
	}
	_ = exec.Command("tmux", "kill-session", "-t", s).Run()

	// derecha: el navegador (tareas y mapa) arriba y el consumo, chico,
	// abajo, pegados: comparten la línea con el título del consumo
	chat, err := tmuxID("new-session", "-d", "-s", s, "-c", root, claude)
	var nav string
	if err == nil {
		nav, err = tmuxID("split-window", "-h", "-t", chat, "-l", "34%", "-c", root, panel("nav"))
	}
	if err == nil {
		_, err = tmuxID("split-window", "-v", "-t", nav, "-l", "5", "-c", root, panel("usage"))
	}
	if err != nil {
		_ = exec.Command("tmux", "kill-session", "-t", s).Run()
		return err
	}
	// abajo, la base: loom y las teclas, siempre iluminada. Va antes del
	// marco lateral para quedar entre sus dos líneas: así su borde de
	// arriba se une con ellas (╠═╣) en vez de sobresalir una celda
	if id, err := tmuxID("split-window", "-d", "-vf", "-t", s, "-l", "1", "-c", root, panel("barra")); err == nil {
		_ = tmux("select-pane", "-t", id, "-d")
	}
	// arriba, la cabecera: el título de claude y los agentes activos. Su
	// borde de abajo es la tapa de las cajas
	if id, err := tmuxID("split-window", "-d", "-vbf", "-t", s, "-l", "1", "-c", root, panel("cabecera")); err == nil {
		_ = tmux("select-pane", "-t", id, "-d")
	}
	_ = separar(sepVertical, "-hbf", "-t", s, "-l", "1")
	_ = separar(sepVertical, "-hf", "-t", s, "-l", "1")
	_ = tmux("set-option", "-t", s, "mouse", "on")
	estiloIDE(s, root, exe)
	return nil
}

// crearCajas arma la sesión con claude, navegador y consumo como cajas
// flotantes sobre el fondo. Falla si tmux no tiene paneles flotantes.
func crearCajas(root, s, claude string, panel func(string) string) error {
	if _, err := tmuxID("new-session", "-d", "-s", s, "-c", root, panel("fondo")); err != nil {
		return err
	}
	// el tamaño real llega con client-resized al entrar
	c, n, u := cajasIDE(80, 24)
	for _, caja := range []struct {
		r   [4]int
		cmd string
	}{{n, panel("nav")}, {u, panel("usage")}, {c, claude}} {
		if _, err := tmuxID("new-pane", "-t", s, "-c", root, "-x", strconv.Itoa(caja.r[2]), "-y", strconv.Itoa(caja.r[3]),
			"-X", strconv.Itoa(caja.r[0]), "-Y", strconv.Itoa(caja.r[1]), caja.cmd); err != nil {
			return err
		}
	}
	return nil
}

// loom panel <nav|usage|cabecera|barra|ayuda> es lo que corre en los
// paneles y los atajos de loom ide; no tiene uso fuera de él.
func newPanelCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "panel <nav|usage|fondo|cabecera|barra|ajustar|ayuda>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "ayuda" {
				fmt.Print(ayudaIDE())
				// sin modo crudo la terminal espera un enter
				if st, err := term.MakeRaw(os.Stdin.Fd()); err == nil {
					defer func() { _ = term.Restore(os.Stdin.Fd(), st) }()
				}
				_, _ = os.Stdin.Read(make([]byte, 1))
				return nil
			}
			root, err := projectRoot()
			if err != nil {
				return err
			}
			switch args[0] {
			case "ajustar":
				s := sesionIDE(root)
				acomodarCajas(s, leerPaneles(s))
				return nil
			case "fondo":
				fmt.Print("\033[?25l")
				// al cambiar de tamaño se redibuja ya: la base quedaba partida
				cambio := make(chan os.Signal, 1)
				signal.Notify(cambio, syscall.SIGWINCH)
				for {
					ancho, alto := tui.Terminal(os.Stdout.Fd())
					fmt.Print("\033[H\033[2J", tui.CabeceraIDE(ancho, tituloClaude(), agentesActivos(root)),
						fmt.Sprintf("\033[%d;1H", alto), tui.BarraIDE(ancho, idioma.T("ayuda"), idioma.T("ocultar")))
					select {
					case <-cambio:
					case <-time.After(2 * time.Second):
					}
				}
			case "cabecera":
				fmt.Print("\033[?25l")
				for {
					ancho, _ := tui.Terminal(os.Stdout.Fd())
					fmt.Print("\033[H\033[2J", tui.CabeceraIDE(ancho, tituloClaude(), agentesActivos(root)))
					time.Sleep(2 * time.Second)
				}
			case "barra":
				fmt.Print("\033[?25l")
				for {
					ancho, _ := tui.Terminal(os.Stdout.Fd())
					fmt.Print("\033[H\033[2J", tui.BarraIDE(ancho, idioma.T("ayuda"), idioma.T("ocultar")))
					time.Sleep(2 * time.Second)
				}
			case "nav":
				return tui.Panel(root, func(ancho int) string {
					m, _, err := construirMapa(root, false, 2)
					if err != nil {
						return err.Error()
					}
					// con todo entregado, el bosquejo de lo que hay; mientras
					// se trabaja, las tarjetas con sus tareas
					if !tui.EnObra(m) {
						return tui.VistaArquitectura(m, ancho)
					}
					return tui.VistaMapa(m, ancho)
				})
			case "usage":
				for {
					// tamaño directo: tui.Terminal cae a 100×40 bajo 40 columnas
					// y el panel envolvía hasta sacar el título
					ancho, alto, _ := term.GetSize(os.Stdout.Fd())
					titulo := tui.TituloPanel(strings.ToUpper(idioma.T("uso de recursos")))
					fmt.Print("\033[H\033[2J", titulo+"\n"+tui.Consumo(root, ancho, alto-1))
					time.Sleep(2 * time.Second)
				}
			}
			return fmt.Errorf("panel desconocido: %s", args[0])
		},
	}
}

// tituloClaude es el título que claude le pone a su panel (lo que hace
// ahora); "Claude Code" si no se puede leer.
func tituloClaude() string {
	pane := os.Getenv("TMUX_PANE")
	o, err := exec.Command("tmux", "display-message", "-p", "-t", pane, "#{session_name}").Output()
	if err != nil || pane == "" {
		return "Claude Code"
	}
	if id := leerPaneles(strings.TrimSpace(string(o))).claude; id != "" {
		if t, err := exec.Command("tmux", "display-message", "-p", "-t", id, "#{pane_title}").Output(); err == nil && strings.TrimSpace(string(t)) != "" {
			return strings.TrimSpace(string(t))
		}
	}
	return "Claude Code"
}

// agentesActivos es la derecha de la cabecera: cuántas tareas
// de loom corren ahora; vacío si ninguna.
func agentesActivos(root string) string {
	estados, _ := state.List(root)
	n := 0
	for _, s := range estados {
		if s.Estado == state.EnCurso {
			n++
		}
	}
	switch n {
	case 0:
		return ""
	case 1:
		return idioma.T("1 agente activo")
	}
	return fmt.Sprintf(idioma.T("%d agentes activos"), n)
}

func ayudaIDE() string {
	filas := [][2]string{
		{"F1", idioma.T("esta ayuda")},
		{"F2", idioma.T("agranda o devuelve el panel activo")},
		{"F3", idioma.T("tablero completo de tareas")},
		{"F4", idioma.T("cambia el tema de colores")},
		{"ctrl+b ←→", idioma.T("pasa de un panel a otro (o con el mouse)")},
		{"tab", idioma.T("en el panel derecho: tareas o mapa")},
		{"j/k enter", idioma.T("recorre las tareas y abre su detalle")},
		{"F12", idioma.T("oculta loom ide; sigue corriendo (o ctrl+b d)")},
		{"loom ide", idioma.T("lo vuelve a abrir")},
	}
	var b strings.Builder
	b.WriteString("\n")
	for _, f := range filas {
		fmt.Fprintf(&b, "  %-11s %s\n", f[0], f[1])
	}
	b.WriteString("\n  " + idioma.T("cualquier tecla cierra"))
	return b.String()
}
