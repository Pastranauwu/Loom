package esqueleto

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pastranauwu/loom/internal/plan"
)

func escribir(t *testing.T, dir, rel, contenido string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contenido), 0o644); err != nil {
		t.Fatal(err)
	}
}

// un plano de python: tipos declarados, un stub con contrato y casos, y
// uno que el arquitecto implementó cuando no le tocaba
func planoPython(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("sin python3")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	escribir(t, dir, Documento, "# arquitectura\n")
	escribir(t, dir, "calc/__init__.py", "")
	escribir(t, dir, "calc/suma.py", "def suma(a: int, b: int) -> int:\n    \"\"\"Suma a y b.\n\n    Idea: a + b, sin validar.\n    Casos: suma(2, 3) -> 5; suma(-1, 1) -> 0\n    \"\"\"\n    raise NotImplementedError(\""+Marca+"\")\n")
	escribir(t, dir, "calc/resta.py", "def resta(a: int, b: int) -> int:\n    return a - b\n")
	escribir(t, dir, "calc/mult.py", "def mult(a: int, b: int) -> int:\n    raise NotImplementedError(\""+Marca+"\")\n")
	return dir
}

func tarea(titulo, listo string, tocar ...string) plan.Borrador {
	return plan.Borrador{Titulo: titulo, ListoCuando: listo, TocarSolo: tocar}
}

func TestProblemasAceptaUnPlanoConStubsYCasos(t *testing.T) {
	dir := planoPython(t)
	r := Resultado{
		Verificar:   "python3 -m compileall -q calc",
		Integracion: "python3 -m unittest tests/test_e2e.py",
		Tareas: []plan.Borrador{
			tarea("suma", "python3 -m unittest tests/test_suma.py", "calc/suma.py", "tests/test_suma.py"),
			tarea("punta a punta", "python3 -m unittest tests/test_e2e.py", "tests/test_e2e.py"),
		},
	}
	if ps := Problemas(context.Background(), Verificacion{Dir: dir, Timeout: time.Minute}, r); len(ps) > 0 {
		t.Fatalf("plano bueno rechazado: %v", ps)
	}
}

func TestProblemasDetectaCadaFormaDePlanoMalo(t *testing.T) {
	dir := planoPython(t)
	r := Resultado{
		Verificar:   "python3 -c 'raise SystemExit(1)'",
		Integracion: "python3 -m unittest tests/test_e2e.py",
		Tareas: []plan.Borrador{
			tarea("no existe", "python3 -m unittest tests/test_div.py", "calc/div.py", "tests/test_div.py"),
			tarea("ya implementada", "python3 -m unittest tests/test_resta.py", "calc/resta.py", "tests/test_resta.py"),
			tarea("sin casos", "python3 -m unittest tests/test_mult.py", "calc/mult.py", "tests/test_mult.py"),
		},
	}
	todo := strings.Join(Problemas(context.Background(), Verificacion{Dir: dir, Timeout: time.Minute}, r), "\n")
	for _, quiero := range []string{
		"\"verify\"",
		"calc/div.py does not exist",
		"calc/resta.py is new and has no stub",
		"has no \"Cases:\"",
		"has no \"Idea:\"",
		"the final task",
	} {
		if !strings.Contains(todo, quiero) {
			t.Errorf("falta %q en:\n%s", quiero, todo)
		}
	}
	os.Remove(filepath.Join(dir, Documento))
	if ps := Problemas(context.Background(), Verificacion{Dir: dir, Timeout: time.Minute}, r); !strings.Contains(strings.Join(ps, "\n"), Documento) {
		t.Errorf("sin %s no se quejó: %v", Documento, ps)
	}
}

func TestParseLeeVerificarEIntegracion(t *testing.T) {
	r, err := Parse("listo:\n```json\n{\"verificar\": \"go vet ./...\", \"integracion\": \"go test ./e2e/...\", \"arquitectura\": \"hexagonal\nen dos líneas\", \"tareas\": [{\"titulo\": \"a\", \"listo_cuando\": \"go test ./a/...\", \"tocar_solo\": [\"a/a.go\"], \"como\": \"rellena\"}]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if r.Verificar != "go vet ./..." || r.Integracion != "go test ./e2e/..." || len(r.Tareas) != 1 {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.Tareas[0].Como, "hexagonal") {
		t.Errorf("la arquitectura no llegó a las notas: %q", r.Tareas[0].Como)
	}
}

// cambiar código que ya funciona no exige stub: el contrato va en "como"
func TestProblemasAceptaCambiosACodigoExistente(t *testing.T) {
	dir := planoPython(t)
	for _, c := range [][]string{{"add", "calc/resta.py"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, c...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	cambio := tarea("resta con negativos", "python3 -m unittest tests/test_resta_neg.py", "calc/resta.py", "tests/test_resta_neg.py")
	cambio.Como = "resta acepta y devuelve negativos. Casos: resta(1, 3) -> -2"
	r := Resultado{Verificar: "python3 -m compileall -q calc", Tareas: []plan.Borrador{cambio}}
	v := Verificacion{Dir: dir, Base: "HEAD", Timeout: time.Minute}
	if ps := Problemas(context.Background(), v, r); len(ps) > 0 {
		t.Fatalf("un cambio con casos fue rechazado: %v", ps)
	}
	r.Tareas[0].Como = "resta acepta negativos"
	if ps := strings.Join(Problemas(context.Background(), v, r), "\n"); !strings.Contains(ps, "already exists") {
		t.Errorf("un cambio sin casos pasó: %q", ps)
	}
}

// una prueba que ya existe solo se descongela diciendo qué quedó obsoleto
func TestProblemasExigeObsoletoParaTocarUnaPruebaExistente(t *testing.T) {
	dir := planoPython(t)
	escribir(t, dir, "tests/test_resta.py", "# casos viejos\n")
	for _, c := range [][]string{{"add", "calc/resta.py", "tests/test_resta.py"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, c...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	cambio := tarea("resta con negativos", "python3 -m unittest tests/test_resta_neg.py", "calc/resta.py", "tests/test_resta_neg.py", "tests/test_resta.py")
	cambio.Como = "resta acepta y devuelve negativos. Casos: resta(1, 3) -> -2"
	r := Resultado{Verificar: "python3 -m compileall -q calc", Tareas: []plan.Borrador{cambio}}
	v := Verificacion{Dir: dir, Base: "HEAD", Timeout: time.Minute}
	if ps := strings.Join(Problemas(context.Background(), v, r), "\n"); !strings.Contains(ps, "Obsolete:") || !strings.Contains(ps, "tests/test_resta.py") {
		t.Errorf("dejó tocar una prueba existente sin decir qué queda obsoleto: %q", ps)
	}
	r.Tareas[0].Como += " Obsoleto: test_resta_rechaza_negativos ya no aplica, ahora devuelve el negativo."
	if ps := Problemas(context.Background(), v, r); len(ps) > 0 {
		t.Errorf("con Obsoleto: declarado debía pasar: %v", ps)
	}
}

func TestProblemasExigeComposeYDockerignore(t *testing.T) {
	dir := planoPython(t)
	r := Resultado{Verificar: "true", Tareas: []plan.Borrador{tarea("suma", "true", "calc/suma.py")}}
	todo := strings.Join(Problemas(context.Background(), Verificacion{Dir: dir, Docker: true, Timeout: time.Minute}, r), "\n")
	if !strings.Contains(todo, "compose.yaml") || !strings.Contains(todo, ".dockerignore") {
		t.Errorf("sin compose ni .dockerignore no se quejó: %q", todo)
	}
}

// en un proyecto existente la prueba de punta a punta ya pasa: usarla
// como listo_cuando deja a la tarea final rechazada por la esclusa; sin
// tarea final vale como regresión (un cambio solo visual)
func TestIntegracionQueYaPasaEsRegresionNoTareaFinal(t *testing.T) {
	dir := planoPython(t)
	r := Resultado{
		Verificar:   "python3 -m compileall -q calc",
		Integracion: "true",
		Tareas:      []plan.Borrador{tarea("punta a punta", "true", "tests/test_e2e.py")},
	}
	v := Verificacion{Dir: dir, Timeout: time.Minute}
	if ps := strings.Join(Problemas(context.Background(), v, r), "\n"); !strings.Contains(ps, "already passes today") {
		t.Errorf("una tarea final con una integración que ya pasa no se rechazó: %q", ps)
	}
	r.Tareas = []plan.Borrador{tarea("suma", "python3 -m unittest tests/test_suma.py", "calc/suma.py", "tests/test_suma.py")}
	if ps := strings.Join(Problemas(context.Background(), v, r), "\n"); strings.Contains(ps, "integration") {
		t.Errorf("una integración que ya pasa, sin tarea final, es regresión válida: %q", ps)
	}
}

// "no hay nada que cambiar" es una respuesta válida del arquitecto
func TestParseAceptaUnPlanSinTareas(t *testing.T) {
	r, err := Parse(`{"verificar": "true", "arquitectura": "igual", "tareas": []}`)
	if err != nil || len(r.Tareas) != 0 || r.Verificar != "true" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestTieneMarcaAceptaLaAnterior(t *testing.T) {
	if !TieneMarca("raise NotImplementedError(\""+Marca+"\")") || !TieneMarca("throw new Error('devclean: sin implementar')") || TieneMarca("return 1") {
		t.Error("TieneMarca debe aceptar ambos nombres")
	}
}

func TestProblemasRechazaBloquesNarrativosYTsconfigObsoleto(t *testing.T) {
	dir := planoPython(t)
	escribir(t, dir, "web/parse.ts", "/**\n * Parses the input.\n * Idea: split.\n * Cases: parse(\"\") -> []\n */\nexport function parse(s: string): string[] { throw new Error(\""+Marca+"\") }\n")
	escribir(t, dir, "tsconfig.json", `{"compilerOptions": {"moduleResolution": "node"}}`)
	r := Resultado{Verificar: "true", Tareas: []plan.Borrador{tarea("parse", "npx vitest run web", "web/parse.ts")}}
	todo := strings.Join(Problemas(context.Background(), Verificacion{Dir: dir, Timeout: time.Minute}, r), "\n")
	for _, quiero := range []string{"web/parse.ts documents its stubs with a /** ... */ block", `deprecated TypeScript config: tsconfig.json: moduleResolution "node"`} {
		if !strings.Contains(todo, quiero) {
			t.Errorf("falta %q en:\n%s", quiero, todo)
		}
	}
}

func TestSinCambiosYEsCodigo(t *testing.T) {
	if !SinCambios("Arreglé el guion.\n```json\n{\"unchanged\": true}\n```") {
		t.Error("{unchanged: true} es sin cambios")
	}
	if SinCambios(`{"unchanged": true, "tasks": []}`) || SinCambios(`{"verify": "go vet ./..."}`) {
		t.Error("con tareas o sin la llave es un JSON completo")
	}
	if EsCodigo("templates/base.html") || EsCodigo("static/a.css") || !EsCodigo("static/js/panel.js") {
		t.Error("EsCodigo mal clasificado")
	}
}
