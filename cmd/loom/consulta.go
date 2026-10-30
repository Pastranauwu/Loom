package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/tui"
)

// consulta es el paso 1 del arquitecto: antes de escribir nada dice en
// pocas líneas qué va a hacer y, solo si un mal supuesto tiraría el
// trabajo, pregunta. El esqueleto continúa la misma sesión, así que lo
// que leyó para contestar no se paga dos veces. Sin esto el modelo más
// caro adivinaba en silencio y una corrección de rumbo costaba un plan entero.
type consulta struct {
	Pedido     string   `json:"pedido"` // sha256 del pedido: otro spec, otra consulta
	Sesion     string   `json:"sesion"`
	Resumen    string   `json:"resumen"`
	Preguntas  []string `json:"preguntas,omitempty"`
	Respuestas []string `json:"respuestas,omitempty"`
}

const maxPreguntas = 3

const pasoConsulta = `

STEP 1 OF 2. Do not create or change any file yet. Read only what you need to understand the request, then answer ONLY this JSON:
{"summary": "what you will build, at most 3 short lines a non-technical person understands", "questions": []}
Ask a question only if a wrong guess would throw away the work: scope, a behavior the request leaves open or contradicts. At most 3, one line each, each ending with your default in parentheses. Usually there are none.`

func rutaConsulta(root, id string) string {
	return filepath.Join(loop.RunsDir(root), id, "consulta.json")
}

func leerConsulta(p string) (consulta, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return consulta{}, false
	}
	var c consulta
	return c, json.Unmarshal(b, &c) == nil
}

func guardarConsulta(p string, c consulta) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// continuacion es el paso 2: las respuestas, si hubo, y la orden de hacer el trabajo.
func (c consulta) continuacion() string {
	var b strings.Builder
	b.WriteString("STEP 2 OF 2.")
	if len(c.Preguntas) > 0 {
		b.WriteString(" Answers to your questions:\n")
		for i, q := range c.Preguntas {
			r := "(use your default)"
			if i < len(c.Respuestas) && strings.TrimSpace(c.Respuestas[i]) != "" {
				r = c.Respuestas[i]
			}
			fmt.Fprintf(&b, "- %s → %s\n", q, r)
		}
	}
	b.WriteString(" Now do the work as the first message asks: write the skeleton in the repository and answer with the complete JSON.\n")
	return b.String()
}

// consultarArquitecto corre el paso 1 (o lo retoma) y devuelve la sesión
// y el prompt del paso 2. Sesión vacía sin error = no se pudo consultar
// (respuesta ilegible, CLI sin sesiones): el esqueleto sigue como antes.
func consultarArquitecto(ctx context.Context, root, id string, ex executor.Executor, base executor.Request, pedido string, registrar func(executor.Result)) (sesion, inicio string, err error) {
	p := rutaConsulta(root, id)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(pedido)))
	c, hay := leerConsulta(p)
	if !hay || c.Pedido != hash {
		var res executor.Result
		err := esperarPlan("el arquitecto revisa el pedido y dice qué hará · "+id, func(avance func(string)) error {
			req := base
			req.Prompt = base.Prompt + pasoConsulta
			req.Avance = avance
			var err error
			res, err = ex.Run(ctx, req)
			registrar(res)
			return err
		})
		if err != nil {
			return "", "", err
		}
		var v struct {
			Summary   string   `json:"summary"`
			Questions []string `json:"questions"`
		}
		t := plan.CuerpoJSON(res.Text)
		if res.Sesion == "" || json.NewDecoder(strings.NewReader(t)).Decode(&v) != nil || strings.TrimSpace(v.Summary) == "" {
			out.Line(idioma.T("· el arquitecto no dio su resumen · sigue sin consulta"))
			return "", "", nil
		}
		if len(v.Questions) > maxPreguntas {
			v.Questions = v.Questions[:maxPreguntas]
		}
		c = consulta{Pedido: hash, Sesion: res.Sesion, Resumen: strings.TrimSpace(v.Summary), Preguntas: v.Questions}
		if err := guardarConsulta(p, c); err != nil {
			return "", "", err
		}
	}
	out.Line(idioma.T("· el arquitecto hará: %s"), strings.Join(strings.Fields(c.Resumen), " "))
	if len(c.Preguntas) > 0 && len(c.Respuestas) == 0 {
		if !esTUI() {
			for i, q := range c.Preguntas {
				out.Line("  %d. %s", i+1, q)
			}
			return "", "", errors.New(idioma.T("el arquitecto tiene preguntas antes de escribir · contesta con loom answer \"<r1>\" \"<r2>\" (vacío = su default) y vuelve a correr apply"))
		}
		for i, q := range c.Preguntas {
			r, ok, err := tui.Preguntar(fmt.Sprintf("%d/%d · %s", i+1, len(c.Preguntas), q), "enter envía · vacío = su default", "")
			if err != nil || !ok {
				r = ""
			}
			c.Respuestas = append(c.Respuestas, r)
		}
		if err := guardarConsulta(p, c); err != nil {
			return "", "", err
		}
	}
	return c.Sesion, c.continuacion(), nil
}

func newAnswerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "answer [respuesta...]",
		Short: "contesta las preguntas del arquitecto; vacío o faltante = su default",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			p, c, err := consultaPendiente(root)
			if err != nil {
				return err
			}
			c.Respuestas = make([]string, len(c.Preguntas))
			copy(c.Respuestas, args)
			if err := guardarConsulta(p, c); err != nil {
				return err
			}
			if err := out.Data(map[string]any{"preguntas": c.Preguntas, "respuestas": c.Respuestas}); err != nil {
				return err
			}
			out.Line(idioma.T("✓ respuestas guardadas · vuelve a correr apply: el arquitecto sigue su misma sesión"))
			return nil
		},
	}
}

// consultaPendiente es la última consulta con preguntas sin contestar.
func consultaPendiente(root string) (string, consulta, error) {
	ps, _ := filepath.Glob(filepath.Join(loop.RunsDir(root), "*", "consulta.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(ps)))
	for _, p := range ps {
		if c, ok := leerConsulta(p); ok && len(c.Preguntas) > 0 && len(c.Respuestas) == 0 {
			return p, c, nil
		}
	}
	return "", consulta{}, errors.New(idioma.T("no hay preguntas pendientes del arquitecto"))
}
