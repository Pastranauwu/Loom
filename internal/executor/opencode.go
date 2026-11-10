package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os/exec"
	"regexp"
	"strings"
)

// OpenCode wraps the opencode CLI (https://opencode.ai).
type OpenCode struct{}

// agenteOpenCode es el --agent de cada rol, definido en entornoOpenCode.
var agenteOpenCode = map[Rol]string{
	RolImplementador: "loom-implementador",
	RolTexto:         "loom-texto",
	RolPlanificador:  "loom-planificador",
	// ponytail: opencode no tiene un agente de solo lectura; el del
	// planificador lee y corre comandos. Uno propio si hace falta cerrarlo.
	RolVisual: "loom-planificador",
}

// entornoOpenCode trae los agentes de loom sin tocar la config del
// usuario (proveedores y credenciales siguen siendo los suyos). Medido
// con un "responde ok": el agente por defecto arranca con 10,3k tokens,
// el implementador con 5,8k y el de texto con 2,1k. Las skills del
// usuario (~/.config/opencode/skills, token-saver-caveman incluida)
// viajan en la descripción de la herramienta skill: apagarla las saca;
// las variables DISABLE cubren las de .claude y las externas.
//
// opencode topa la salida en min(limit.output del modelo, 32000). Un
// modelo que razona (deepseek-v4-flash, 384k de salida real) gastó los
// 32000 pensando el plan y cortó con finish "length" sin escribir el
// JSON. El min con el límite del modelo mantiene válido el tope alto.
var entornoOpenCode = []string{
	`OPENCODE_CONFIG_CONTENT={"agent":{` +
		`"loom-implementador":{"mode":"primary","tools":{"webfetch":false,"task":false,"todowrite":false,"todoread":false,"skill":false}},` +
		`"loom-texto":{"mode":"primary","tools":{"*":false}},` +
		`"loom-planificador":{"mode":"primary","tools":{"*":false,"read":true,"bash":true}}}}`,
	"OPENCODE_DISABLE_CLAUDE_CODE_SKILLS=1",
	"OPENCODE_DISABLE_EXTERNAL_SKILLS=1",
	"OPENCODE_EXPERIMENTAL_OUTPUT_TOKEN_MAX=128000",
}

func (OpenCode) Name() string { return "opencode" }

func (OpenCode) Available() error {
	bin, err := findBinary("opencode")
	if err != nil {
		return err
	}
	if out, err := exec.Command(bin, "--version").Output(); err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return errors.New(idioma.T("opencode no responde a --version · verifica la instalación"))
	}
	return nil
}

// Models devuelve el catálogo real de `opencode models`, en el formato
// provider/model que el CLI exige. Sin esto loom inventaba ids como
// "glm-5.2" que el servidor rechaza: cada invocación moría en dos
// segundos sin gastar un token y sin dejar rastro.
func (OpenCode) Models(ctx context.Context) ([]string, error) {
	return modelosDeCLI(ctx, "opencode", "models")
}

// Ventanas lee la ventana de contexto real de cada modelo desde
// `opencode models --verbose`. La salida es la id "provider/model" en una
// línea y, debajo, un objeto JSON con "limit": {"context": N}. Sin esto
// loom medía todos los modelos contra la tabla de 200k y sobreestimaba
// los chicos (32k–128k), así que puedeReanudar continuaba sesiones que el
// proveedor ya no sostiene.
//
// Degrada en abierto: si el CLI no responde o cambia el formato, el mapa
// queda vacío y SubVentana cae a la tabla. Es información para decidir,
// no un oráculo que deba frenar la corrida.
func (OpenCode) Ventanas(ctx context.Context) map[string]int {
	bin, err := findBinary("opencode")
	if err != nil {
		return nil
	}
	out, err := exec.CommandContext(ctx, bin, "models", "--verbose").Output()
	if err != nil {
		return nil
	}
	return parseOpenCodeVentanas(string(out))
}

// parseOpenCodeVentanas recorre la salida de `models --verbose`: una id
// "provider/model" seguida de un objeto JSON. Se queda con limit.context
// de cada modelo, indexado por su id completo.
//
// El bloque se delimita contando llaves, no buscando una línea "{" o "}":
// los objetos anidados (limit, cost, capabilities) abren con su propia
// llave en la misma línea ("limit": {), y cortar en la primera "}" daba
// un JSON truncado y sin ventana.
func parseOpenCodeVentanas(salida string) map[string]int {
	ventanas := map[string]int{}
	var id string
	var buf strings.Builder
	profundidad := 0
	for _, linea := range strings.Split(salida, "\n") {
		t := strings.TrimSpace(linea)
		if profundidad == 0 {
			if t == "" {
				continue
			}
			if strings.HasPrefix(t, "{") {
				// abre el bloque del modelo
				buf.Reset()
				buf.WriteString(linea + "\n")
				profundidad += strings.Count(linea, "{") - strings.Count(linea, "}")
				if profundidad == 0 && id != "" { // JSON compacto en una línea
					if n := ventanaDelJSON(buf.String()); n > 0 {
						ventanas[id] = n
					}
					id = ""
				}
				continue
			}
			id = t // la id del modelo, antes de su JSON
			continue
		}
		buf.WriteString(linea + "\n")
		profundidad += strings.Count(linea, "{") - strings.Count(linea, "}")
		if profundidad == 0 && id != "" {
			if n := ventanaDelJSON(buf.String()); n > 0 {
				ventanas[id] = n
			}
			id = ""
			buf.Reset()
		}
	}
	return ventanas
}

// ventanaDelJSON saca limit.context de un bloque JSON de modelo.
func ventanaDelJSON(bloque string) int {
	var m struct {
		Limit struct {
			Context int `json:"context"`
		} `json:"limit"`
	}
	if json.Unmarshal([]byte(bloque), &m) != nil {
		return 0
	}
	return m.Limit.Context
}

func (e OpenCode) Run(ctx context.Context, req Request) (Result, error) {
	model := req.Model
	if strings.HasPrefix(model, "gemini-") {
		model = "google/" + model
	}
	args := []string{"run", req.Prompt, "--dir", req.RoomPath, "--format", "json", "--auto", "--agent", agenteOpenCode[req.Rol]}
	if model != "" {
		args = append(args, "--model", model)
	}
	// Sin --variant deepseek razona con su esfuerzo por defecto en cada
	// turno: medido con el prompt real del revisor en flash, low tardó
	// 32 s y 5k tokens razonando; sin variante, 96 s y 15k. Una variante
	// que el modelo no define (deepseek no tiene medium) cae a su default.
	if req.Effort != "" {
		args = append(args, "--variant", req.Effort)
	}
	if req.Sesion != "" {
		args = append(args, "--session", req.Sesion)
	}
	req.Env = append(append([]string(nil), req.Env...), entornoOpenCode...)
	req.avanceDe = avanceOpenCode(req.RoomPath)
	stdout, stderr, code, err := run(ctx, req, "opencode", args...)
	res := Result{Stdout: stdout, Stderr: stderr, ExitCode: code}
	res.FilesChanged, res.Text, res.Tokens = parseOpenCodeEvents(stdout)
	res.Sesion = sesionOpenCode(stdout)
	if msg := errorDeOpenCode(stdout); msg != "" {
		res.Stderr += msg + "\n"
		if err != nil {
			err = fmt.Errorf(idioma.T("opencode: %s"), msg)
		}
	}
	return res, err
}

// sesionOpenCode es el id de la conversación, que opencode repite en cada
// evento: con él una corrección o un reintento continúa (--session) en vez
// de releer el repo (un arquitecto sin sesión: 71, 42 y 53 turnos en tres
// vueltas del mismo plano).
var reSesionOpenCode = regexp.MustCompile(`"sessionID":"(ses_[A-Za-z0-9]+)"`)

func sesionOpenCode(stdout string) string {
	if m := reSesionOpenCode.FindStringSubmatch(stdout); m != nil {
		return m[1]
	}
	return ""
}

// errorDeOpenCode saca el error del proveedor que opencode reporta como
// evento en stdout (con --format json no escribe nada en stderr). Sin
// esto un 402 de saldo agotado llegaba al usuario como "exit status 1".
func errorDeOpenCode(stdout string) string {
	for _, line := range strings.Split(stdout, "\n") {
		var ev struct {
			Type  string `json:"type"`
			Error struct {
				Name string `json:"name"`
				Data struct {
					Message    string `json:"message"`
					StatusCode int    `json:"statusCode"`
				} `json:"data"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &ev) != nil || ev.Type != "error" {
			continue
		}
		msg := ev.Error.Data.Message
		if msg == "" {
			msg = ev.Error.Name
		}
		if ev.Error.Data.StatusCode != 0 {
			msg = fmt.Sprintf("%d %s", ev.Error.Data.StatusCode, msg)
		}
		return msg
	}
	return ""
}

// avanceOpenCode traduce los eventos de opencode a avances legibles: qué
// comando corre, qué archivo lee y cuánto escribió cada turno. Es lo que
// deja ver si el planificador trabaja sobre el repo o está alucinando.
func avanceOpenCode(dir string) func(string) string {
	turno := 0
	return func(linea string) string {
		var ev struct {
			Type string `json:"type"`
			Part struct {
				Tool   string `json:"tool"`
				Text   string `json:"text"`
				Reason string `json:"reason"`
				State  struct {
					Input map[string]any `json:"input"`
				} `json:"state"`
				Tokens struct {
					Output    int `json:"output"`
					Reasoning int `json:"reasoning"`
				} `json:"tokens"`
			} `json:"part"`
		}
		if json.Unmarshal([]byte(linea), &ev) != nil {
			return ""
		}
		switch ev.Type {
		case "tool_use":
			in := ev.Part.State.Input
			if c, ok := in["command"].(string); ok {
				return "$ " + recortar(c, 100)
			}
			if f, ok := in["filePath"].(string); ok {
				return "lee " + strings.TrimPrefix(strings.TrimPrefix(f, dir), "/")
			}
			return ev.Part.Tool
		case "text":
			t := strings.TrimSpace(ev.Part.Text)
			if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") || strings.HasPrefix(t, "```") {
				return fmt.Sprintf("respuesta escrita · %d caracteres", len(t))
			}
			return recortar(t, 100)
		case "step_finish":
			turno++
			msg := fmt.Sprintf("turno %d · %d tokens escritos", turno, ev.Part.Tokens.Output)
			if ev.Part.Tokens.Reasoning > 0 {
				msg += fmt.Sprintf(" · %d razonando", ev.Part.Tokens.Reasoning)
			}
			if ev.Part.Reason == "length" {
				msg += " · se agotó la salida"
			}
			return msg
		case "error":
			return "error · " + errorDeOpenCode(linea)
		}
		return ""
	}
}

// parseOpenCodeEvents walks the JSONL event stream best-effort:
// file paths from tool events, tokens from step finish events, and the
// assistant text from text parts.
func parseOpenCodeEvents(stdout string) ([]string, string, Usage) {
	seen := map[string]bool{}
	var files []string
	var usage Usage
	var textos []string

	scanner := bufio.NewScanner(strings.NewReader(stdout))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		collectPaths(event, "", 0, seen, &files)
		collectText(event, &textos)
		collectTokens(event, &usage)
	}
	return files, strings.Join(textos, "\n"), usage
}

// collectTokens suma el gasto de cada evento step_finish. opencode lo
// anida bajo "part", no en la raíz del evento: mirando solo la raíz el
// gasto salía siempre 0, lo que además hacía indistinguible una
// invocación que nunca llegó al modelo de una que sí trabajó.
//
// Cada step_finish es un turno: el primero da el contexto base.
//
// LastTurn es el tamaño del contexto del último turno, que opencode
// informa en tokens.total. Antes se calculaba como input+caché, pero en
// opencode input es solo el delta del turno: subestimaba el contexto y
// puedeReanudar creía que había espacio cuando la sesión ya estaba llena,
// así que --session recargaba una sesión al límite. Se prefiere total y,
// si no viene, se cae a la suma (que es lo que hacía el código antes).
func collectTokens(m map[string]any, usage *Usage) {
	if tokens, ok := m["tokens"].(map[string]any); ok {
		in, leida, escrita := intValue(tokens, "input"), 0, 0
		if cache, ok := tokens["cache"].(map[string]any); ok {
			leida, escrita = intValue(cache, "read"), intValue(cache, "write")
		}
		usage.Input += in
		usage.Output += intValue(tokens, "output") + intValue(tokens, "reasoning")
		usage.CacheRead += leida
		usage.CacheWrite += escrita
		usage.Turns++
		if c, ok := m["cost"].(float64); ok {
			usage.CostUSD += c
		}
		turno := in + leida + escrita
		if total := intValue(tokens, "total"); total > 0 {
			turno = total
		}
		if usage.Turns == 1 {
			usage.FirstTurn, usage.FirstTurnWrite = turno, escrita
		}
		usage.LastTurn = turno
		return
	}
	if part, ok := m["part"].(map[string]any); ok {
		collectTokens(part, usage)
	}
}

// collectText finds every "text" string field in the event, best-effort.
func collectText(m map[string]any, out *[]string) {
	if v, ok := m["text"].(string); ok && strings.TrimSpace(v) != "" {
		*out = append(*out, v)
	}
	for _, key := range []string{"part", "message", "state", "input"} {
		if nested, ok := m[key].(map[string]any); ok {
			collectText(nested, out)
		}
	}
}

// collectPaths finds file paths under well-known keys, one level deep.
func collectPaths(m map[string]any, _ string, depth int, seen map[string]bool, files *[]string) {
	if depth > 1 {
		return
	}
	for _, key := range []string{"path", "file", "filePath", "filename"} {
		if v, ok := m[key].(string); ok && looksLikePath(v) && !seen[v] {
			seen[v] = true
			*files = append(*files, v)
		}
	}
	for _, key := range []string{"part", "tool", "state", "input"} {
		if nested, ok := m[key].(map[string]any); ok {
			collectPaths(nested, key, depth+1, seen, files)
		}
	}
}

func looksLikePath(s string) bool {
	return strings.Contains(s, "/") || strings.Contains(s, ".")
}

func intValue(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}
