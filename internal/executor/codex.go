package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Codex wraps the OpenAI Codex CLI (`codex exec`).
type Codex struct{}

func (Codex) Name() string { return "codex" }

func (Codex) Available() error {
	bin, err := findBinary("codex")
	if err != nil {
		return err
	}
	if out, err := exec.Command(bin, "--version").Output(); err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return errors.New(idioma.T("codex no responde a --version · verifica la instalación"))
	}
	return nil
}

// Models lee el catálogo que codex guarda en $CODEX_HOME/models_cache.json:
// el CLI no tiene un subcomando que liste modelos. Solo los que codex
// muestra en su selector ("visibility": "list"); los ocultos son internos
// (codex-auto-review). Sin caché (codex nunca corrió) devuelve error y
// quien llama sigue sin catálogo, como con cualquier CLI que no lista.
func (Codex) Models(context.Context) ([]string, error) {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		home = filepath.Join(h, ".codex")
	}
	b, err := os.ReadFile(filepath.Join(home, "models_cache.json"))
	if err != nil {
		return nil, fmt.Errorf(idioma.T("codex no tiene catálogo de modelos todavía · corre codex una vez · %s"), err)
	}
	var cache struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &cache); err != nil {
		return nil, fmt.Errorf(idioma.T("codex: models_cache.json ilegible · %s"), err)
	}
	var ids []string
	for _, m := range cache.Models {
		if m.Slug != "" && m.Visibility == "list" {
			ids = append(ids, m.Slug)
		}
	}
	return ids, nil
}

// argsCodex arma la invocación de `codex exec` para un rol.
//
//   - --ignore-user-config: sin el config.toml del usuario (modelo, esfuerzo
//     y MCP suyos); la sesión iniciada sigue valiendo. Mismo criterio que
//     contextoLimpio de claude.
//   - --ephemeral: la sesión no se guarda en ~/.codex/sessions.
//   - --skip-git-repo-check: el cuarto es un worktree y el planificador de
//     constitución puede correr fuera de un repo.
//
// El implementador corre sin sandbox, igual que claude con
// bypassPermissions: el sandbox de codex solo deja escribir en el cuarto
// y /tmp, y `go test`, npm o cargo escriben su caché en el home, así que
// el agente no podía correr sus propias pruebas. El contenedor real es el
// cuarto más la reversión de loom. Los roles que no escriben van en
// read-only: codex no deja quitar herramientas por rol, así que el
// sandbox es lo que impide que un revisor toque el repo.
func argsCodex(req Request) []string {
	args := []string{"exec", "--json", "--ephemeral", "--ignore-user-config", "--skip-git-repo-check", "-C", req.RoomPath}
	if req.Rol == RolImplementador {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else {
		args = append(args, "--sandbox", "read-only")
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Effort != "" {
		args = append(args, "-c", `model_reasoning_effort="`+req.Effort+`"`)
	}
	return append(args, req.Prompt)
}

// ponytail: un límite de uso de la cuenta llega como error y gasta el
// intento; claude espera al reset porque su stream trae la hora. Hacer lo
// mismo aquí cuando se vea el evento real de codex con su reset.
func (e Codex) Run(ctx context.Context, req Request) (Result, error) {
	req.avanceDe = avanceCodex()
	stdout, stderr, code, err := run(ctx, req, "codex", argsCodex(req)...)
	res := Result{Stdout: stdout, Stderr: stderr, ExitCode: code}
	var fallo string
	res.FilesChanged, res.Text, res.Tokens, fallo = parseCodexEvents(stdout)
	if fallo != "" {
		res.Stderr += fallo + "\n"
		if err != nil {
			err = fmt.Errorf(idioma.T("codex: %s"), fallo)
		}
	}
	return res, err
}

// codexEvento es una línea de `codex exec --json`.
type codexEvento struct {
	Type    string `json:"type"`
	Message string `json:"message"` // type "error"
	Error   struct {
		Message string `json:"message"`
	} `json:"error"` // type "turn.failed"
	Item struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Command string `json:"command"`
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	} `json:"item"`
	Usage struct {
		Input  int `json:"input_tokens"`
		Cached int `json:"cached_input_tokens"`
		Write  int `json:"cache_write_input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
}

func leerCodex(linea string) (codexEvento, bool) {
	var ev codexEvento
	linea = strings.TrimSpace(linea)
	if linea == "" || linea[0] != '{' || json.Unmarshal([]byte(linea), &ev) != nil {
		return ev, false
	}
	return ev, true
}

// parseCodexEvents saca del stream los archivos cambiados, el último
// mensaje del agente (la respuesta), el gasto y el error si lo hubo.
//
// input_tokens de codex INCLUYE lo leído de caché (convención de OpenAI);
// Usage.Input es lo que no estaba en caché, como en claude, o el
// presupuesto contaría la caché dos veces.
func parseCodexEvents(stdout string) ([]string, string, Usage, string) {
	var files []string
	var texto, fallo string
	var u Usage
	visto := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		ev, ok := leerCodex(scanner.Text())
		if !ok {
			continue
		}
		switch ev.Type {
		case "item.completed":
			if ev.Item.Type == "agent_message" {
				texto = ev.Item.Text
			}
			for _, c := range ev.Item.Changes {
				if !visto[c.Path] {
					visto[c.Path] = true
					files = append(files, c.Path)
				}
			}
		case "turn.completed":
			us := ev.Usage
			nuevo := max(us.Input-us.Cached-us.Write, 0)
			u.Input += nuevo
			u.Output += us.Output
			u.CacheRead += us.Cached
			u.CacheWrite += us.Write
			u.Turns++
			if u.Turns == 1 {
				u.FirstTurn, u.FirstTurnWrite = us.Input, us.Write
			}
		case "turn.failed":
			fallo = ev.Error.Message
		case "error":
			fallo = ev.Message
		}
	}
	return files, texto, u, fallo
}

// avanceCodex traduce los eventos de codex a avances: comandos, archivos
// editados, texto y el cierre de cada turno.
func avanceCodex() func(string) string {
	turno := 0
	return func(linea string) string {
		ev, ok := leerCodex(linea)
		if !ok {
			return ""
		}
		switch ev.Type {
		case "item.started":
			if ev.Item.Type == "command_execution" {
				// codex envuelve todo en `/bin/bash -lc '…'`
				c := strings.TrimPrefix(ev.Item.Command, "/bin/bash -lc ")
				return "$ " + recortar(strings.Trim(c, "'\""), 100)
			}
		case "item.completed":
			switch ev.Item.Type {
			case "agent_message":
				t := strings.TrimSpace(ev.Item.Text)
				if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") || strings.HasPrefix(t, "```") {
					return fmt.Sprintf("respuesta escrita · %d caracteres", len(t))
				}
				return recortar(t, 100)
			case "file_change":
				var rutas []string
				for _, c := range ev.Item.Changes {
					rutas = append(rutas, filepath.Base(c.Path))
				}
				return "edita " + strings.Join(rutas, ", ")
			}
		case "turn.completed":
			turno++
			return fmt.Sprintf("turno %d · %d tokens escritos", turno, ev.Usage.Output)
		case "turn.failed":
			return "error · " + ev.Error.Message
		case "error":
			return "error · " + ev.Message
		}
		return ""
	}
}
