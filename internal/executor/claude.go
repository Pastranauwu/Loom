package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Claude wraps the claude CLI (Claude Code) in print mode.
type Claude struct{}

func (Claude) Name() string { return "claude" }

func (Claude) Available() error {
	bin, err := findBinary("claude")
	if err != nil {
		return err
	}
	if out, err := exec.Command(bin, "--version").Output(); err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return errors.New(idioma.T("claude no responde a --version · verifica la instalación"))
	}
	return nil
}

// Models devuelve los modelos que acepta `claude --model`. El CLI no
// expone un subcomando para listarlos, así que van fijos: primero los ids
// de versión —el alias "opus" apunta a Opus 5, no a 5.5— y después los
// alias, que siguen valiendo para los config.yml que ya los tienen.
func (Claude) Models(context.Context) ([]string, error) {
	return []string{"claude-opus-5-5", "claude-fable-5-1", "claude-sonnet-5-5", "claude-sonnet-5", "claude-haiku-5-5", "claude-haiku-4-5", "opus", "sonnet", "haiku"}, nil
}

// contextoLimpio deja fuera del agente lo que el usuario configuró para
// su propio Claude Code. Medido en un cuarto real: el contexto base de un
// "responde ok" baja de 23,4k a 12k tokens.
//   - --setting-sources project,local: sin settings de usuario, o sea sin
//     sus hooks (caveman entraba como estilo de prosa), plugins ni skills.
//     El .claude/ y el CLAUDE.md del repo siguen cargando.
//   - --strict-mcp-config: sin MCP, tampoco los conectores de claude.ai.
//   - --exclude-dynamic-system-prompt-sections: el cwd y el git status
//     pasan del prompt de sistema al primer mensaje, así el prompt de
//     sistema es igual en todos los cuartos y el caché se comparte.
//   - --system-prompt: reemplaza el prompt de sistema de Claude Code, que
//     está escrito para una sesión interactiva con un humano. Medido con
//     las 4 herramientas, "responde ok" baja de 11,9k a 5,8k tokens, y
//     eso se reenvía en cada turno de cada intento. Es fijo, igual para
//     todos los roles y cuartos: el caché se comparte. Cómo trabajar lo
//     dice el prompt de loom (skills.Base).
//
// --disable-slash-commands no va: también apaga las skills del proyecto
// (y medido no ahorra nada).
var contextoLimpio = []string{"--setting-sources", "project,local", "--strict-mcp-config", "--exclude-dynamic-system-prompt-sections", "--system-prompt", sistemaClaude}

const sistemaClaude = "You are a coding agent working unsupervised in the current directory, a git repository. If you have tools, use them to read, edit and run commands; edit existing files with Edit instead of rewriting them. Do not ask for confirmation or questions: nobody answers. Do not explain what you are going to do: act and answer at the end only what is asked."

// herramientasClaude es el --tools de cada rol. Sin Glob ni Grep: Bash
// los cubre. "" apaga todas: medido, un "responde ok" arranca con 6,5k
// tokens sin herramientas contra 11k con Read y Bash.
var herramientasClaude = map[Rol]string{
	RolImplementador: "Bash,Read,Edit,Write",
	RolTexto:         "",
	RolPlanificador:  "Read,Bash",
	RolVisual:        "Read",
}

func (e Claude) Run(ctx context.Context, req Request) (Result, error) {
	// bypassPermissions: el agente no puede preguntar nada (modo -p) y
	// el contenedor real es el cuarto + la reversión de loom
	// stream-json y no json: el resultado final trae lo mismo, y los
	// eventos de cada turno son la única forma de saber cuántos turnos
	// hubo y con cuánto contexto arrancó el primero
	args := []string{"-p", req.Prompt, "--output-format", "stream-json", "--verbose", "--permission-mode", "bypassPermissions"}
	args = append(args, contextoLimpio...)
	args = append(args, "--tools", herramientasClaude[req.Rol])
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Effort != "" {
		args = append(args, "--effort", req.Effort)
	}
	if req.Sesion != "" {
		args = append(args, "--resume", req.Sesion)
	}
	if req.TopeUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.Itoa(req.TopeUSD))
	}
	var res Result
	espera := esperaSinReset
	req.avanceDe = avanceClaude(req.RoomPath)
	for {
		stdout, stderr, code, err := run(ctx, req, "claude", args...)
		text, uso := parseClaudeStream(stdout)
		fin := finClaude(stdout)
		res = Result{Stdout: res.Stdout + stdout, Stderr: res.Stderr + stderr, ExitCode: code, Text: text, Tokens: sumarUso(res.Tokens, uso), Sesion: fin.SessionID}
		reset, agotada := cuotaAgotada(stdout)
		if !agotada {
			if fin.Subtype == "error_max_budget_usd" {
				return res, fmt.Errorf("%w · $%d", ErrTope, req.TopeUSD)
			}
			return res, err
		}
		hasta := reset.Add(margenReset)
		if reset.IsZero() {
			hasta = time.Now().Add(espera)
			espera = min(2*espera, time.Hour)
		}
		res.Stderr += fmt.Sprintf("cuota agotada · se retoma a las %s\n", hasta.Format("15:04:05"))
		if req.Avance != nil {
			req.Avance("cuota agotada · se retoma a las " + hasta.Format("15:04:05"))
		}
		if err := dormir(ctx, time.Until(hasta)); err != nil {
			return res, err
		}
	}
}

// Un 429 no es un intento del agente: es la cuota del proveedor. Run
// espera al reset que trae la respuesta (o, sin él, un backoff que dobla
// hasta una hora) y relanza la misma invocación. Así el bucle no gasta
// intentos ni escala de modelo contra una cuota agotada, y el revisor no
// aprueba sin revisar por "degradar en abierto". Lo que el agente alcanzó
// a escribir antes del corte queda en el cuarto y el gasto se suma.
var (
	esperaSinReset = 5 * time.Minute
	margenReset    = 30 * time.Second
	dormir         = func(ctx context.Context, d time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
			return nil
		}
	}
)

// cuotaAgotada dice si la invocación terminó por límite de cuota (el
// resultado trae api_error_status 429) y cuándo se reinicia la ventana
// rechazada, si el stream lo reportó.
func cuotaAgotada(stdout string) (time.Time, bool) {
	var reset time.Time
	agotada := false
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var ev struct {
			Type           string `json:"type"`
			APIErrorStatus int    `json:"api_error_status"`
			RateLimitInfo  struct {
				Status   string `json:"status"`
				ResetsAt int64  `json:"resetsAt"`
			} `json:"rate_limit_info"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		switch {
		case ev.Type == "rate_limit_event" && ev.RateLimitInfo.Status == "rejected" && ev.RateLimitInfo.ResetsAt > 0:
			reset = time.Unix(ev.RateLimitInfo.ResetsAt, 0)
		case ev.Type == "result" && ev.APIErrorStatus == 429:
			agotada = true
		}
	}
	return reset, agotada
}

// sumarUso junta el gasto de dos invocaciones. El primer turno es el de
// la primera: es la base con que arrancó el agente.
func sumarUso(a, b Usage) Usage {
	a.Input += b.Input
	a.Output += b.Output
	a.CacheRead += b.CacheRead
	a.CacheWrite += b.CacheWrite
	a.Turns += b.Turns
	a.CostUSD += b.CostUSD
	if a.FirstTurn == 0 {
		a.FirstTurn, a.FirstTurnWrite = b.FirstTurn, b.FirstTurnWrite
	}
	if b.LastTurn > 0 {
		a.LastTurn = b.LastTurn
	}
	return a
}

// claudeUsage es el gasto como lo reporta la API: en `usage` de cada
// mensaje y en `usage` del resultado.
type claudeUsage struct {
	Input      int `json:"input_tokens"`
	Output     int `json:"output_tokens"`
	CacheRead  int `json:"cache_read_input_tokens"`
	CacheWrite int `json:"cache_creation_input_tokens"`
}

// claudeEvento es una línea de --output-format stream-json. La salida de
// --output-format json es un solo evento de resultado, así que se lee igual.
type claudeEvento struct {
	Type            string  `json:"type"`
	ParentToolUseID *string `json:"parent_tool_use_id"`
	Message         struct {
		ID    string      `json:"id"`
		Usage claudeUsage `json:"usage"`
	} `json:"message"`
	Result     string      `json:"result"`
	NumTurns   int         `json:"num_turns"`
	CostUSD    float64     `json:"total_cost_usd"`
	Usage      claudeUsage `json:"usage"`
	ModelUsage map[string]struct {
		Input      int `json:"inputTokens"`
		Output     int `json:"outputTokens"`
		CacheRead  int `json:"cacheReadInputTokens"`
		CacheWrite int `json:"cacheCreationInputTokens"`
	} `json:"modelUsage"`
}

// finClaude es el evento `result` del stream: la sesión, para continuarla,
// y el subtipo, que distingue un corte por tope de un fallo.
func finClaude(stdout string) (fin struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
}) {
	// el orden de las claves no es fijo: "type" llega al final del objeto
	lineas := strings.Split(strings.TrimSpace(stdout), "\n")
	for i := len(lineas) - 1; i >= 0; i-- {
		if !strings.Contains(lineas[i], `"type":"result"`) {
			continue
		}
		if json.Unmarshal([]byte(lineas[i]), &fin) == nil && fin.Type == "result" {
			return fin
		}
	}
	fin.Type, fin.Subtype, fin.SessionID = "", "", ""
	return fin
}

// ParseClaudeUsage lee el gasto de una salida de claude ya guardada
// (stream-json o json). Lo usa el benchmark para medir corridas viejas.
func ParseClaudeUsage(stdout string) Usage {
	_, u := parseClaudeStream(stdout)
	return u
}

// parseClaudeStream saca el texto final y el gasto del stream de claude.
//
// Los totales salen de `modelUsage` y no de `usage`: el `usage` del
// resultado se queda corto. Medido en una tarea real con haiku: `usage`
// decía 789k de caché leída y `modelUsage` 4,58M, y solo este último
// cuadra con el `total_cost_usd` que el mismo resultado reporta.
//
// Los turnos se cuentan por id de mensaje del hilo principal: claude
// emite un evento `assistant` por cada bloque de contenido, todos con el
// mismo id y el mismo usage.
func parseClaudeStream(stdout string) (string, Usage) {
	var u Usage
	var texto string
	vistos := map[string]bool{}
	numTurns := 0
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var ev claudeEvento
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "assistant":
			if ev.ParentToolUseID != nil || ev.Message.ID == "" || vistos[ev.Message.ID] {
				continue
			}
			vistos[ev.Message.ID] = true
			m := ev.Message.Usage
			u.LastTurn = m.Input + m.CacheRead + m.CacheWrite
			if len(vistos) == 1 {
				u.FirstTurn, u.FirstTurnWrite = u.LastTurn, m.CacheWrite
			}
		case "result":
			texto, numTurns, u.CostUSD = ev.Result, ev.NumTurns, ev.CostUSD
			if len(ev.ModelUsage) > 0 {
				for _, m := range ev.ModelUsage {
					u.Input += m.Input
					u.Output += m.Output
					u.CacheRead += m.CacheRead
					u.CacheWrite += m.CacheWrite
				}
			} else {
				u.Input, u.Output, u.CacheRead, u.CacheWrite = ev.Usage.Input, ev.Usage.Output, ev.Usage.CacheRead, ev.Usage.CacheWrite
			}
		}
	}
	u.Turns = len(vistos)
	if u.Turns == 0 {
		u.Turns = numTurns // salida sin eventos por turno
	}
	return texto, u
}

// avanceClaude saca de cada mensaje del asistente lo que hace: comandos,
// archivos leídos o editados y su texto, recortado.
func avanceClaude(dir string) func(string) string {
	return func(linea string) string {
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type  string         `json:"type"`
					Name  string         `json:"name"`
					Text  string         `json:"text"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(linea), &ev) != nil || ev.Type != "assistant" {
			return ""
		}
		var partes []string
		for _, c := range ev.Message.Content {
			switch c.Type {
			case "tool_use":
				if cmd, ok := c.Input["command"].(string); ok {
					partes = append(partes, "$ "+recortar(cmd, 100))
				} else if f, ok := c.Input["file_path"].(string); ok {
					partes = append(partes, strings.ToLower(c.Name)+" "+strings.TrimPrefix(strings.TrimPrefix(f, dir), "/"))
				} else {
					partes = append(partes, c.Name)
				}
			case "text":
				t := strings.TrimSpace(c.Text)
				if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") || strings.HasPrefix(t, "```") {
					partes = append(partes, fmt.Sprintf("respuesta escrita · %d caracteres", len(t)))
				} else if t != "" {
					partes = append(partes, recortar(t, 100))
				}
			}
		}
		return strings.Join(partes, " · ")
	}
}
