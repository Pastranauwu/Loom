package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Pastranauwu/loom/internal/capturas"
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/skills"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

// revisorVisualEnBucle levanta la interfaz del cuarto, la captura en
// tamaño celular y le pide a un modelo que ve imágenes que juzgue si se
// nota lo que la tarea pide. Las pruebas de UI solo saben que una clase
// existe: en una corrida real, 9 tareas "liquid glass" salieron verdes y en la
// pantalla no había vidrio. Degrada en abierto: sin capturas o sin
// respuesta, aprueba.
type revisorVisualEnBucle struct {
	ex        executor.Executor
	modelo    string
	root      string
	pantallas capturas.Pantallas
}

// revisorVisualPara arma el revisor visual con el rol `revisor_visual`, o
// el modelo medio: con opus y 38 capturas fue un tercio del gasto de una
// tarea, y sonnet ve lo mismo en pocas fotos. El tope de críticas por
// tarea (loop.TopeVisual) y de fotos por crítica (Pantallas.Tope) acotan
// el gasto. nil si el proyecto no declara cómo levantar su interfaz.
func revisorVisualPara(root string, cfg config.Config, ex executor.Executor) loop.RevisorVisual {
	if cfg.Pantallas.Vacia() || ex == nil {
		return nil
	}
	modelo := config.ModeloRol(cfg, "revisor_visual")
	if modelo == "" {
		modelo = cfg.ModeloPeso("media")
	}
	// un modelo sin visión rechaza con "no puedo ver imágenes" y quema un
	// intento: se busca otro peso que sí vea, o se apaga con aviso
	if !veImagenes(modelo, ex.Name()) {
		otro := ""
		for _, peso := range []string{"media", "pesada", "liviana"} {
			if m := cfg.ModeloPeso(peso); m != "" && veImagenes(m, ex.Name()) {
				otro = m
				break
			}
		}
		avisoVisual.Do(func() {
			if otro == "" {
				out.Line(idioma.T("· revisión visual apagada · %s no ve imágenes y ningún peso de modelos: ve · pon un modelo claude en el rol revisor_visual"), modelo)
			} else {
				out.Line(idioma.T("· revisión visual con %s · %s no ve imágenes"), otro, modelo)
			}
		})
		if otro == "" {
			return nil
		}
		modelo = otro
	}
	return revisorVisualEnBucle{ex: ejecutorPara(ex, modelo), modelo: modelo, root: root, pantallas: cfg.Pantallas}
}

var avisoVisual sync.Once

// veImagenes dice si el modelo acepta capturas. claude y codex sí; de
// opencode solo las familias conocidas con visión (deepseek, por ejemplo,
// es solo texto). Sin modelo decide el CLI.
func veImagenes(modelo, cli string) bool {
	m := strings.ToLower(modelo)
	if m == "" {
		return cli != "opencode"
	}
	if !strings.Contains(m, "/") {
		return true
	}
	for _, f := range []string{"claude", "gpt", "gemini", "vision", "-vl"} {
		if strings.Contains(m, f) {
			return true
		}
	}
	return false
}

func (r revisorVisualEnBucle) Revisar(ctx context.Context, cuarto room.Room, t task.Task, intento int) (bool, string, loop.Tokens) {
	dir := filepath.Join(loop.RunsDir(r.root), t.ID, fmt.Sprintf("visual-%d", intento))
	todas, repetidas := capturas.SinRepetir(tomarCapturas(ctx, cuarto.Path, r.pantallas, cuarto.Puerto, dir))
	if len(repetidas) > 0 {
		out.Line(idioma.T("· %s · %d capturas iguales a otra pantalla (¿redirige al login?) · fuera de la revisión · captura con sesión en screens_script"), t.ID, len(repetidas))
	}
	fotos := capturas.Elegir(todas, r.pantallas.Tope())
	if len(fotos) < len(todas) {
		out.Line(idioma.T("· %s · revisión visual con %d de %d capturas · sube pantallas_max en config.yml para ver más"), t.ID, len(fotos), len(todas))
	}
	pide := t.Titulo
	if t.Porque != "" {
		pide += "\nPor qué: " + t.Porque
	}
	notas := plan.SepararNotas(t.Notas)
	if notas.Tarea != "" {
		pide += "\n" + notas.Tarea
	}
	// las reglas del spec traen la dirección de diseño (paleta, tono,
	// referencias): sin ellas el revisor juzgaba contra su gusto genérico
	if notas.Reglas != "" {
		pide += "\n" + notas.Reglas
	}
	if len(repetidas) > 0 {
		// sin esto el revisor pide "la captura del tablero", que el agente
		// no puede dar: es la configuración de capturas la que falla
		pide += "\nThese screens could not be captured (they showed another screen, likely a login redirect); do not ask for them: " + strings.Join(repetidas, ", ")
	}
	return r.juzgar(ctx, cuarto.Path, fotos, pide)
}

// juzgar le muestra las capturas a un modelo que ve imágenes y le pide
// un veredicto contra lo pedido. Sin capturas o sin respuesta, aprueba.
func (r revisorVisualEnBucle) juzgar(ctx context.Context, dir string, fotos []string, pide string) (bool, string, loop.Tokens) {
	if len(fotos) == 0 {
		return true, "", loop.Tokens{}
	}
	res, err := r.ex.Run(ctx, executor.Request{
		Rol:      executor.RolVisual,
		RoomPath: dir,
		Prompt:   promptVisual(pide, fotos),
		Model:    r.modelo,
		Timeout:  5 * time.Minute,
	})
	tk := tokensDe(res.Tokens)
	if err != nil {
		return true, "", tk
	}
	cumple, cambios, ok := parseVisual(res.Text)
	if !ok || cumple {
		return true, "", tk
	}
	return false, strings.Join(cambios, "\n") + "\n(capturas en " + filepath.Dir(fotos[0]) + ")", tk
}

func promptVisual(pide string, fotos []string) string {
	var b strings.Builder
	b.WriteString("You are loom's VISUAL REVIEWER. Open these screenshots of the app (phone and desktop) with your file-reading tool, all of them in ONE turn. They are from now, of the current code:\n")
	for _, f := range fotos {
		b.WriteString("- " + f + "\n")
	}
	if strings.HasPrefix(filepath.Base(fotos[0]), "00-muestra-") {
		b.WriteString("The first ones are the component sheet: the visual identity (palette, typography, buttons, fields, cards, table, alerts) and a basic layout with everything together. Judge the identity there; the rest show it applied to real screens.\n")
	}
	fmt.Fprintf(&b, "\nWhat was asked:\n%s\n", pide)
	b.WriteString("\nInterface criteria:\n" + skills.UI + "\n")
	b.WriteString(`
Judge ONLY what shows in the screenshots, not the code:
- Is what the task asks for visible at a glance? An effect that is barely visible counts as not done.
- Does anything look broken or unfinished? Overlapping or clipped text, unstyled controls, unreadable contrast, misaligned spacing.
- Does it look generic: a template or framework default with flat hierarchy, no focal point and uniform spacing? If the task draws or redesigns screens, that counts as not done: say what would give each screen a point of view.
Do not ask for anything outside this task or for minor taste changes.

Answer at the end with only this JSON:
{"complies": true, "changes": []}
or, if it does not comply, with concrete visual changes (which element, what it looks like today, how it should look):
{"complies": false, "changes": ["..."]}
`)
	b.WriteString(idioma.Instruccion())
	return b.String()
}

// parseVisual reads the last JSON with "complies" (or "cumple") from the response.
func parseVisual(texto string) (cumple bool, cambios []string, ok bool) {
	i := max(strings.LastIndex(texto, `"complies"`), strings.LastIndex(texto, `"cumple"`))
	if i < 0 {
		return false, nil, false
	}
	j := strings.LastIndex(texto[:i], "{")
	if j < 0 {
		return false, nil, false
	}
	var crudo json.RawMessage
	if err := json.NewDecoder(strings.NewReader(texto[j:])).Decode(&crudo); err != nil {
		return false, nil, false
	}
	var v struct {
		Cumple  bool     `json:"cumple"`
		Cambios []string `json:"cambios"`
	}
	if json.Unmarshal(idioma.NormalizarClaves(crudo, map[string]string{"complies": "cumple", "changes": "cambios"}), &v) != nil {
		return false, nil, false
	}
	return v.Cumple, v.Cambios, true
}

// ultimaDeUI dice si la tarea es la última de interfaz de su cadena:
// ninguna otra tarea de interfaz sin terminar depende de ella. Solo esa
// pasa por la revisión visual: las intermedias se juzgaban sobre
// pantallas a medio hacer ("Pantalla en construcción"), y cada revisión
// levanta la app entera. La de diseño se revisa cuando nadie la sigue.
func ultimaDeUI(root string, t task.Task) bool {
	todas, err := task.List(config.TasksDir(root))
	if err != nil {
		return true
	}
	for _, o := range todas {
		if o.ID == t.ID || !slices.Contains(o.DependeDe, t.ID) || !skills.TocaUI(o.TocarSolo) {
			continue
		}
		if st, _ := state.Get(root, o.ID); st.Estado != state.Lista {
			return false
		}
	}
	return true
}
