package loop

import (
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/skills"
	"github.com/Pastranauwu/loom/internal/task"
)

const arquitecturaPlan = `STACK: Go 1.22, sin dependencias.

ÁRBOL DE ARCHIVOS:
 internal/lexer/lexer.go (T-001)  tokeniza
 internal/parser/parser.go (T-002)  arma el árbol
 internal/eval/eval.go (T-003)  evalúa
 Cada paquete lleva sus pruebas al lado.

TIPOS COMPARTIDOS:
 Token = {Kind string, Text string}

FIRMAS PÚBLICAS EXACTAS:
[T-001 internal/lexer/lexer.go] lexer.Tokenize(s string) []Token
[T-002 internal/parser/parser.go] parser.Parse(t []Token) Node
 Node = {Op string, Kids []Node}
[T-003 internal/eval/eval.go] eval.Eval(n Node) float64`

// notasPlan compone las notas como lo hacen plan.Parse y spec.Apply.
func notasPlan(como string) string {
	return plan.MarcaReglas + "- sin dependencias nuevas\n\n" + plan.MarcaArquitectura + arquitecturaPlan + "\n\n" + plan.MarcaImplementacion + como
}

func tareasPlan() []task.Task {
	return []task.Task{
		{ID: "T-001", Titulo: "lexer", ListoCuando: "go test ./internal/lexer/...", TocarSolo: []string{"internal/lexer/**"}, Expone: []string{"lexer.Tokenize(s string) []Token"}, Notas: notasPlan("escribe el lexer")},
		{ID: "T-002", Titulo: "parser", ListoCuando: "go test ./internal/parser/...", TocarSolo: []string{"internal/parser/**"}, Expone: []string{"parser.Parse(t []Token) Node"}, Usa: []string{"lexer.Tokenize(s string) []Token"}, Notas: notasPlan("escribe el parser")},
		{ID: "T-003", Titulo: "evaluador", ListoCuando: "go test ./internal/eval/...", TocarSolo: []string{"internal/eval/**"}, Expone: []string{"eval.Eval(n Node) float64"}, Usa: []string{"parser.Parse(t []Token) Node"}, Notas: notasPlan("escribe el evaluador"), LimiteLineas: 120, Riesgos: "división por cero"},
	}
}

// El caché de prompts solo sirve si el inicio es idéntico byte a byte
// entre agentes: constitución, reglas y arquitectura común van primero, y
// nada de la tarea (id, alcance, árbol recortado) se cuela antes.
func TestPromptsDeUnPlanCompartenElPrefijoComun(t *testing.T) {
	var prompts []string
	for _, tk := range tareasPlan() {
		prompts = append(prompts, promptPara(tk, tk.Usa, "usa gofmt", nil, "", "", false))
	}
	comun := skills.Base + "\n\n" + "Project constitution (conventions every agent must follow):\nusa gofmt\n\n" +
		plan.MarcaReglas + "- sin dependencias nuevas\n\n" +
		plan.MarcaArquitectura + "STACK: Go 1.22, sin dependencias.\n\nTIPOS COMPARTIDOS:\n Token = {Kind string, Text string}\n\n"
	for i, p := range prompts {
		if !strings.HasPrefix(p, comun) {
			t.Fatalf("prompt %d no empieza con la parte común:\n%s", i, p)
		}
		if strings.Contains(p[:len(comun)], "T-00") {
			t.Errorf("la parte común lleva algo de una tarea")
		}
		if strings.Index(p, "ÁRBOL DE ARCHIVOS") < len(comun) || strings.Index(p, "FIRMAS PÚBLICAS") < len(comun) {
			t.Errorf("el árbol y las firmas son de la tarea: van después de la parte común")
		}
	}
	// y el prefijo común termina justo ahí: lo que sigue ya es de la tarea
	if prompts[0][len(comun):len(comun)+len("Task T-001")] != "Task T-001" {
		t.Errorf("después de lo común viene la tarea:\n%s", prompts[0][len(comun):])
	}
}

// Una tarea recibe solo las firmas y el árbol de lo que declara: el
// evaluador usa el parser, no el lexer, así que el bloque del lexer no le
// llega aunque esté en la arquitectura del plan.
func TestPromptRecortaFirmasQueLaTareaNoUsa(t *testing.T) {
	tk := tareasPlan()[2]
	p := promptPara(tk, tk.Usa, "", nil, "", "", false)
	for _, falta := range []string{"lexer.Tokenize", "internal/lexer/lexer.go"} {
		if strings.Contains(p, falta) {
			t.Errorf("el evaluador no usa %q y lo recibió:\n%s", falta, p)
		}
	}
	for _, quiero := range []string{"[T-002 internal/parser/parser.go] parser.Parse", " Node = {Op string", "[T-003 internal/eval/eval.go]", " internal/parser/parser.go (T-002)", " internal/eval/eval.go (T-003)", "Cada paquete lleva sus pruebas", "Risks: división por cero", "Implementation of this task:\nescribe el evaluador"} {
		if !strings.Contains(p, quiero) {
			t.Errorf("falta %q en el prompt:\n%s", quiero, p)
		}
	}
}

// Un contrato escrito a mano, sin marcas, llega igual que antes.
func TestPromptSinMarcasConservaLasNotas(t *testing.T) {
	tk := task.Task{ID: "T-001", Titulo: "x", ListoCuando: "true", Notas: "enfoque libre"}
	if p := promptPara(tk, nil, "", nil, "", "", false); !strings.HasSuffix(p, "\nNotes:\nenfoque libre\n") || !strings.HasPrefix(p, skills.Base+"\n\nTask T-001") {
		t.Errorf("prompt:\n%s", p)
	}
}

// la guía de UI entra por los archivos que toca la tarea, no por su rol:
// las tareas del esqueleto no traen agente
func TestPromptConGuiaDeUISoloSiTocaInterfaz(t *testing.T) {
	ui := task.Task{ID: "T-002", Titulo: "página de inicio", ListoCuando: "npx vitest run", TocarSolo: []string{"frontend/src/pages/HomePage.tsx"}}
	if !strings.Contains(promptPara(ui, nil, "", nil, "", "", false), "Interface:") {
		t.Error("una tarea de .tsx no recibió la guía de UI")
	}
	api := task.Task{ID: "T-003", Titulo: "paleta", ListoCuando: "pytest", TocarSolo: []string{"backend/app/palette.py"}}
	if strings.Contains(promptPara(api, nil, "", nil, "", "", false), "Interface:") {
		t.Error("una tarea de backend recibió la guía de UI")
	}
}
