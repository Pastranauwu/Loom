package examiner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/sealed"
	"github.com/Pastranauwu/loom/internal/task"
)

type agenteSuite struct{ pedido, cuarto string }

func (a *agenteSuite) Name() string { return "suite" }
func (a *agenteSuite) Run(_ context.Context, req loop.Request) (loop.Result, error) {
	a.pedido = req.Prompt
	a.cuarto = req.RoomPath
	return loop.Result{Text: `{"imports":["import { test } from 'node:test';","import { strict as assert } from 'node:assert';","import { doble } from '../src/doble.js';"],"visible":["test('caso', () => { assert.equal(doble(2), 4); });"],"hidden":["test('borde', () => { assert.equal(doble(0), 0); });"]}`}, nil
}

func TestContratoDelEsqueletoSoloFirmaYCasos(t *testing.T) {
	root := t.TempDir()
	src := `/**
 * Duplica un número.
 * Idea: secreto del algoritmo.
 * Casos:
 * - doble(2) -> 4
 * - doble(0) -> 0
 */
export function doble(n: number): number {
  const secretoImplementacion = 77;
  throw new Error("loom: sin implementar");
}`
	path := filepath.Join(root, "src", "doble.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	contrato, err := contratoDelEsqueleto(root, task.Task{TocarSolo: []string{"src/doble.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"doble(n: number): number", "doble(2) -> 4"} {
		if !strings.Contains(contrato, want) {
			t.Errorf("falta %q en %q", want, contrato)
		}
	}
	for _, forbidden := range []string{"secretoImplementacion", "secreto del algoritmo"} {
		if strings.Contains(contrato, forbidden) {
			t.Errorf("el contrato filtró %q", forbidden)
		}
	}
}

func TestExaminadorEsqueletoJavaScriptEscribeYSella(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node no instalado")
	}
	root := t.TempDir()
	src := `/**
 * Casos:
 * - doble(2) -> 4
 * - doble(0) -> 0
 */
export function doble(n) {
  throw new Error("loom: sin implementar");
}`
	path := filepath.Join(root, "src", "doble.js")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ag := &agenteSuite{}
	tk := task.Task{ID: "T-001", Titulo: "doblar", TocarSolo: []string{"src/doble.js"}, ExamenEsqueleto: true, ExamenVisible: "tests/doble.test.js", ListoCuando: "node --test tests/doble.test.js"}
	ok, err := Run(context.Background(), root, Options{Agent: ag, Task: tk, Root: root, Lenguaje: "node"})
	if err != nil || !ok {
		t.Fatalf("sellada=%v err=%v", ok, err)
	}
	if !strings.Contains(ag.pedido, "doble(2) -> 4") || strings.Contains(ag.pedido, "throw new Error") {
		t.Errorf("el prompt no es ciego: %q", ag.pedido)
	}
	if ag.cuarto == root {
		t.Fatal("el examinador pudo leer el cuarto de implementación")
	}
	if _, err := os.Stat(filepath.Join(ag.cuarto, "src", "doble.js")); !os.IsNotExist(err) {
		t.Fatalf("el directorio del examinador contiene la implementación: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "tests", "doble.test.js")); err != nil {
		t.Fatal(err)
	}
	s, err := sealed.Read(root, tk.ID)
	if err != nil || s.Archivo != "tests/loom_hidden.test.js" {
		t.Fatalf("suite oculta=%+v err=%v", s, err)
	}
}

func TestSintaxisTypeScriptConToolchainDelProyecto(t *testing.T) {
	dir := os.Getenv("LOOM_TS_TOOLCHAIN")
	if dir == "" {
		t.Skip("define LOOM_TS_TOOLCHAIN con un proyecto que tenga TypeScript instalado")
	}
	if err := validarSintaxisEnProyecto(dir, "typescript", "tests/caso.test.ts", "import { test } from 'vitest'; test('ok', () => {});\n"); err != nil {
		t.Fatalf("suite válida: %v", err)
	}
	if err := validarSintaxisEnProyecto(dir, "typescript", "tests/caso.test.ts", "test('roto', () => {\n"); err == nil {
		t.Fatal("TypeScript debía rechazar sintaxis rota")
	}
}

// Cada forma de comentario en que el arquitecto deja los Casos:. La
// versión anterior solo leía bloques /* */: Go con casos en la misma
// línea y Python no daban contrato, y un comentario de línea se llevaba
// código hasta el siguiente bloque del archivo.
func TestCasosYFirmaPorTipoDeComentario(t *testing.T) {
	for nombre, c := range map[string]struct {
		src    string
		quiero []string
		nunca  []string
	}{
		"go en la misma línea": {
			"// Suma a y b.\n// Idea: a+b.\n// Casos: Suma(2,3) -> 5; Suma(-1,1) -> 0\nfunc Suma(a, b int) int {\n\tpanic(\"loom: sin implementar\")\n}\n",
			[]string{"Firma: func Suma(a, b int) int", "Suma(2,3) -> 5"}, []string{"panic", "Idea"},
		},
		"python docstring": {
			"def suma(a: int, b: int) -> int:\n    \"\"\"Suma a y b.\n\n    Idea: a + b.\n    Casos: suma(2, 3) -> 5; suma(-1, 1) -> 0\n    \"\"\"\n    raise NotImplementedError(\"loom: sin implementar\")\n",
			[]string{"Firma: def suma(a: int, b: int) -> int", "suma(2, 3) -> 5"}, []string{"raise", "Idea"},
		},
		"comentario de línea con un bloque más abajo": {
			"// Casos: a(1) -> 2\nexport function a(n: number): number {\n  throw new Error(\"loom: sin implementar\");\n}\n\nconst SECRETO = 42;\n/** otra cosa */\nexport function b() {}\n",
			[]string{"Firma: export function a(n: number): number", "a(1) -> 2"}, []string{"SECRETO", "function b", "throw"},
		},
		"método de una clase": {
			"export class Carrito {\n  /**\n   * Casos:\n   * - total([]) -> 0\n   */\n  total(items: Item[]): number {\n    throw new Error(\"loom: sin implementar\");\n  }\n}\n",
			[]string{"class Carrito · total(items: Item[]): number", "total([]) -> 0"}, []string{"throw"},
		},
	} {
		got := strings.Join(casosYFirma(c.src), "\n")
		for _, q := range c.quiero {
			if !strings.Contains(got, q) {
				t.Errorf("%s: falta %q en %q", nombre, q, got)
			}
		}
		for _, n := range c.nunca {
			if strings.Contains(got, n) {
				t.Errorf("%s: se filtró %q en %q", nombre, n, got)
			}
		}
	}
}

type agenteContado struct {
	veces int
	texto string
}

func (a *agenteContado) Name() string { return "contado" }
func (a *agenteContado) Run(context.Context, loop.Request) (loop.Result, error) {
	a.veces++
	return loop.Result{Text: a.texto}, nil
}

func cuartoConStubJS(t *testing.T) (string, task.Task) {
	t.Helper()
	root := t.TempDir()
	for _, c := range [][]string{{"init", "-q"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, c...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "/**\n * Casos:\n * - doble(2) -> 4\n */\nexport function doble(n) {\n  throw new Error(\"loom: sin implementar\");\n}\n"
	if err := os.WriteFile(filepath.Join(root, "src", "doble.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, task.Task{ID: "T-001", Titulo: "doblar", TocarSolo: []string{"src/doble.js"}, ExamenEsqueleto: true, ExamenVisible: "tests/doble.test.js", ListoCuando: "node --test tests/doble.test.js"}
}

// Sin bloque "hidden" la tarea igual tiene examen: la visible queda
// escrita y vedada. Solo se omite el paso suite_oculta.
func TestExaminadorSinOcultaDejaLaVisible(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node no instalado")
	}
	root, tk := cuartoConStubJS(t)
	ag := &agenteContado{texto: `{"imports":["import { test } from 'node:test';","import { doble } from '../src/doble.js';"],"visible":["test('caso', () => { doble(2); });"],"hidden":[]}`}
	hay, err := Run(context.Background(), root, Options{Agent: ag, Task: tk, Root: root, Lenguaje: "node"})
	if !hay || err == nil || !strings.Contains(err.Error(), "suite oculta") {
		t.Fatalf("hay=%v err=%v, quiero examen visible con aviso de la oculta", hay, err)
	}
	if _, err := os.Stat(filepath.Join(root, tk.ExamenVisible)); err != nil {
		t.Fatalf("la visible no quedó escrita: %v", err)
	}

	// al retomar la prueba ya está: no se paga otro examen y sigue
	// siendo del examinador
	hay, err = Run(context.Background(), root, Options{Agent: ag, Task: tk, Root: root, Lenguaje: "node"})
	if !hay || err != nil || ag.veces != 1 {
		t.Errorf("hay=%v err=%v llamadas=%d, quiero reusar la visible sin llamar al modelo", hay, err, ag.veces)
	}
}

// Una prueba que el implementador escribió (el examen no se pudo
// preparar) sigue siendo suya al retomar.
func TestExaminadorNoSeApropiaDeLaPruebaDelImplementador(t *testing.T) {
	root, tk := cuartoConStubJS(t)
	if err := os.MkdirAll(filepath.Join(root, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, tk.ExamenVisible), []byte("// del implementador\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ag := &agenteContado{}
	hay, err := Run(context.Background(), root, Options{Agent: ag, Task: tk, Root: root, Lenguaje: "node"})
	if hay || err != nil || ag.veces != 0 {
		t.Errorf("hay=%v err=%v llamadas=%d", hay, err, ag.veces)
	}
}

// El implementador no puede tocar la prueba del examinador: si importa
// un archivo que no existe, la tarea no cerraría nunca.
func TestExaminadorDescartaSuiteConImportRoto(t *testing.T) {
	root, tk := cuartoConStubJS(t)
	ag := &agenteContado{texto: `{"imports":["import { test } from 'node:test';","import { doble } from '../lib/doble.js';"],"visible":["test('caso', () => { doble(2); });"],"hidden":["test('b', () => {});"]}`}
	hay, err := Run(context.Background(), root, Options{Agent: ag, Task: tk, Root: root, Lenguaje: "node"})
	if hay || err == nil || !strings.Contains(err.Error(), "../lib/doble.js") {
		t.Fatalf("hay=%v err=%v", hay, err)
	}
	if _, err := os.Stat(filepath.Join(root, tk.ExamenVisible)); !os.IsNotExist(err) {
		t.Error("una suite con import roto no debe quedar en el cuarto")
	}
}

// Monorepo: el repo no tiene un lenguaje detectable y la tarea es Python.
// El lenguaje sale de sus archivos; antes caía al camino Go y no había examen.
func TestExaminadorEsqueletoPythonEnMonorepo(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 no instalado")
	}
	root := t.TempDir()
	for _, c := range [][]string{{"init", "-q"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, c...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	src := "def suma(a: int, b: int) -> int:\n    \"\"\"Suma a y b.\n\n    Idea: a + b.\n    Casos: suma(2, 3) -> 5; suma(-1, 1) -> 0\n    \"\"\"\n    raise NotImplementedError(\"loom: sin implementar\")\n"
	if err := os.MkdirAll(filepath.Join(root, "backend", "calc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "calc", "suma.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	tk := task.Task{ID: "T-001", Titulo: "sumar", TocarSolo: []string{"backend/calc/suma.py"}, ExamenEsqueleto: true,
		ExamenVisible: "backend/tests/test_suma.py", ListoCuando: "cd backend && python3 -m pytest tests/test_suma.py"}
	if !Examinable(root, tk, "") {
		t.Fatal("una tarea Python de un monorepo debe ser examinable")
	}
	ag := &agenteContado{texto: `{"imports":["from calc.suma import suma"],"visible":["def test_suma():\n    assert suma(2, 3) == 5"],"hidden":["def test_negativos():\n    assert suma(-1, 1) == 0"]}`}
	hay, err := Run(context.Background(), root, Options{Agent: ag, Task: tk, Root: root, Lenguaje: ""})
	if !hay || err != nil {
		t.Fatalf("hay=%v err=%v", hay, err)
	}
	visible, err := os.ReadFile(filepath.Join(root, tk.ExamenVisible))
	if err != nil || !strings.Contains(string(visible), "def test_suma") || strings.Contains(string(visible), "package ") {
		t.Fatalf("la visible debe ser pytest: %q err=%v", visible, err)
	}
	if s, err := sealed.Read(root, tk.ID); err != nil || s.Archivo != "backend/tests/test_loom_hidden.py" {
		t.Fatalf("suite oculta=%+v err=%v", s, err)
	}
}
