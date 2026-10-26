package examiner

import (
	"context"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/task"
)

// timeoutSintaxis acota al validador externo. Si el intérprete se cuelga,
// el examen degrada en vez de frenar al implementador.
const timeoutSintaxis = 15 * time.Second

// Soportado reporta si loom sabe generar y validar una suite ciega
// para ese stack.
//
// Importa fuera de este paquete porque la veda de pruebas (el implementador
// nunca toca las pruebas) solo tiene sentido si hay un examinador que las
// escriba. Este indicador describe los contratos tradicionales; Node
// solo está habilitado para tareas con contrato de esqueleto.
// Donde no lo hay — Node antiguo, Rust — prohibirle además al
// implementador escribirlas deja la tarea sin nadie que la haga: el
// planificador apunta el listo_cuando a un archivo de prueba, el
// implementador no puede crearlo, y la tarea queda roja para siempre.
func Soportado(lenguaje string) bool {
	switch lenguajeExamen(lenguaje) {
	case "go", "python":
		return true
	default:
		return false
	}
}

// SoportadoEsqueleto incluye Node cuando una tarea trae firmas y Casos:.
func SoportadoEsqueleto(lenguaje string) bool { return lenguajeExamen(lenguaje) != "" }

// lenguajeExamen normaliza el lenguaje detectado al que el examinador
// sabe examinar. Devuelve "" cuando no hay examinador para ese stack: sin
// validador de sintaxis la suite generada es basura que rompe la
// compilación del cuarto, y el implementador no puede tocarla.
func lenguajeExamen(l string) string {
	switch strings.ToLower(strings.TrimSpace(l)) {
	case "", "go":
		// vacío = repo sin manifiesto reconocible. Se asume go, que es lo
		// único que el examinador sabía hacer antes de esta fase.
		return "go"
	case "python", "pytest":
		return "python"
	case "node", "typescript", "javascript":
		return "typescript"
	case "rust":
		// rust pide otro enfoque: la stdlib de Go no parsea rust, así que
		// validar exige el crate syn o `cargo check`, y eso arrastra el
		// toolchain completo dentro del cuarto. Fase aparte.
		return ""
	default:
		// el resto: todavía sin examinador.
		return ""
	}
}

// lenguajeDeTarea decide el lenguaje del examen por los archivos de la
// tarea antes que por el del repo: en un monorepo (backend Python,
// frontend TypeScript) el repo no tiene un lenguaje, y vacío se leía como
// Go, así que ninguna tarea Python de un monorepo se examinaba.
func lenguajeDeTarea(detectado string, t task.Task) string {
	for _, p := range append([]string{t.ExamenVisible}, t.TocarSolo...) {
		switch strings.ToLower(filepath.Ext(p)) {
		case ".py":
			return "python"
		case ".go":
			return "go"
		case ".ts", ".tsx":
			return "typescript"
		case ".js", ".jsx", ".mjs", ".cjs":
			return "javascript"
		}
	}
	return lenguajeExamen(detectado)
}

// nombresSuite devuelve los nombres de archivo de la suite visible y la
// oculta. El nombre importa: el corredor de pruebas del stack tiene que
// descubrirlos sin configuración extra (go test por el sufijo _test.go,
// pytest por el prefijo test_).
func nombresSuite(lenguaje string) (visible, oculta string) {
	switch lenguaje {
	case "python":
		return "test_loom_visible.py", "test_loom_hidden.py"
	case "typescript":
		return "loom_visible.test.ts", "loom_hidden.test.ts"
	case "javascript":
		return "loom_visible.test.js", "loom_hidden.test.js"
	default:
		return VisibleFileName, HiddenFileName
	}
}

// RutasSuite devuelve dónde van la suite visible y la oculta dentro del
// cuarto, relativas a su raíz. El examinador automático la usa al escribir
// y `loom task seal` al sellar a mano: un solo camino para los dos.
func RutasSuite(tocarSolo []string, lenguaje string) (visible, oculta string) {
	_, relDir := inferDirFromTocarSolo(tocarSolo, "")
	v, o := nombresSuite(lenguajeExamen(lenguaje))
	return toRelPath(filepath.Join(relDir, v)), toRelPath(filepath.Join(relDir, o))
}

// armarSuite arma el archivo de pruebas completo para el lenguaje.
func armarSuite(lenguaje, pkg, importPath string, imports, funcs []string) string {
	if lenguaje == "python" {
		return buildPyFile(imports, funcs)
	}
	if lenguaje == "typescript" || lenguaje == "javascript" {
		return buildNodeFile(imports, funcs)
	}
	return buildGoFile(pkg, importPath, imports, funcs)
}

func buildNodeFile(imports, funcs []string) string {
	var b strings.Builder
	for _, imp := range dedup(imports) {
		b.WriteString(strings.TrimSpace(imp) + "\n")
	}
	b.WriteString("\n")
	for _, f := range funcs {
		b.WriteString(strings.TrimSpace(f) + "\n\n")
	}
	return b.String()
}

// buildPyFile arma un archivo de pytest. A diferencia de Go no lleva
// declaración de paquete, y los imports van tal cual los escribió el
// examinador: la ruta de import del módulo bajo prueba depende del layout
// del proyecto (src/, plano, paquete instalado) y adivinarla rompe más de
// lo que arregla. Python tampoco falla por import sin usar, así que no hay
// que filtrarlos como en Go.
func buildPyFile(imports, funcs []string) string {
	var b strings.Builder
	for _, imp := range dedup(imports) {
		linea := strings.TrimSpace(imp)
		if linea == "" {
			continue
		}
		if !strings.HasPrefix(linea, "import ") && !strings.HasPrefix(linea, "from ") {
			linea = "import " + linea
		}
		b.WriteString(linea + "\n")
	}
	b.WriteString("\n")
	for _, f := range funcs {
		b.WriteString(strings.TrimRight(f, "\n"))
		b.WriteString("\n\n")
	}
	return b.String()
}

// validarSintaxis reporta si la suite al menos parsea. No verifica tipos
// ni que la implementación exista (que no exista todavía es TDD legítimo),
// solo que el examinador no devolvió basura. Un lenguaje sin validador
// devuelve nil: degradar es mejor que frenar.
func validarSintaxis(lenguaje, contenido string) error {
	switch lenguaje {
	case "python":
		return validarPython(contenido)
	default:
		_, err := parser.ParseFile(token.NewFileSet(), "loom_test.go", contenido, parser.AllErrors)
		return err
	}
}

// validarSintaxisEnProyecto usa el parser del TypeScript instalado en el
// cuarto. No ejecuta las aserciones: los stubs todavía deben fallarlas.
func validarSintaxisEnProyecto(roomPath, lenguaje, nombre, contenido string) error {
	if lenguaje != "typescript" && lenguaje != "javascript" {
		return validarSintaxis(lenguaje, contenido)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutSintaxis)
	defer cancel()
	var cmd *exec.Cmd
	if lenguaje == "javascript" {
		cmd = exec.CommandContext(ctx, "node", "--check", "--input-type=module")
	} else {
		const script = `const ts = require('typescript'); let s=''; process.stdin.on('data', c => s+=c); process.stdin.on('end', () => { const f=ts.createSourceFile(process.argv[1], s, ts.ScriptTarget.Latest, true); if (f.parseDiagnostics.length) { console.error(ts.flattenDiagnosticMessageText(f.parseDiagnostics[0].messageText, '\n')); process.exitCode=1; } });`
		cmd = exec.CommandContext(ctx, "node", "-e", script, nombre)
	}
	cmd.Dir = toolchainDir(roomPath, nombre)
	cmd.Stdin = strings.NewReader(contenido)
	salida, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf(idioma.T("el parser tardó más de %s"), timeoutSintaxis)
	}
	if err != nil {
		return fmt.Errorf(idioma.T("la suite no parsea con el toolchain del proyecto · %s"), ultimaLinea(string(salida)))
	}
	return nil
}

func toolchainDir(root, nombre string) string {
	dir := filepath.Dir(filepath.Join(root, nombre))
	for {
		if _, err := os.Stat(filepath.Join(dir, "node_modules", "typescript")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
			return dir
		}
		if dir == root || dir == filepath.Dir(dir) {
			return root
		}
		dir = filepath.Dir(dir)
	}
}

// validarPython delega en el intérprete del sistema: ast.parse es el mismo
// parser que después usará pytest. Sin python3 instalado no valida y deja
// pasar — el examinador nunca bloquea al implementador.
func validarPython(contenido string) error {
	if _, err := exec.LookPath("python3"); err != nil {
		return nil
	}
	f, err := os.CreateTemp("", "loom-examen-*.py")
	if err != nil {
		return nil
	}
	defer os.Remove(f.Name())
	_, errW := f.WriteString(contenido)
	if errC := f.Close(); errW != nil || errC != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutSintaxis)
	defer cancel()
	// el archivo llega por argv, no interpolado en el -c: una ruta con
	// comillas o espacios rompería el fuente de Python.
	cmd := exec.CommandContext(ctx, "python3", "-c",
		"import ast, sys; ast.parse(open(sys.argv[1]).read())", f.Name())
	salida, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil
	}
	if err != nil {
		return fmt.Errorf(idioma.T("la suite no parsea · %s"), ultimaLinea(string(salida)))
	}
	return nil
}

// ultimaLinea reduce el traceback de python a la línea que dice qué pasó.
func ultimaLinea(s string) string {
	lineas := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lineas) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lineas[i]); l != "" {
			return l
		}
	}
	return "sin salida"
}

// reglasDe devuelve las reglas y el JSON de respuesta propios del
// lenguaje. Lo común (caja negra, 70/30, no implementar) vive en
// buildPrompt.
func reglasDe(lenguaje string) string {
	if lenguaje == "typescript" || lenguaje == "javascript" {
		return `- Use the project's test runner (Vitest/Jest/node:test per package.json). Import only the module's public API.
- In "imports" write complete import lines; the relative path is computed from the indicated visible test file.
- In "visible" and "hidden" return complete, independent test blocks (it/test), with no imports inside the block.
- Do not copy implementation bodies or test private details.

Return ONLY this JSON (no markdown):
{"imports":["import { describe, it, expect } from 'vitest';", "import { funcion } from '../../src/modulo';"], "visible":["it('caso', () => { expect(funcion(1)).toBe(2); });"], "hidden":["it('borde', () => { expect(funcion(0)).toBe(0); });"]}`
	}
	if lenguaje == "python" {
		return `- Write pytest tests: module-level functions ` + "`def test_xxx():`" + `, assertions with assert.
- Use only the Python stdlib and the module under test. No pip, no external dependencies.
- "imports" holds COMPLETE import lines, including the one for the module under test (e.g. "from exportador import a_csv", "import json").
- Do not put imports inside function bodies: loom writes them at the top of the file.

Return ONLY this JSON (no text around it, no markdown):
{
  "imports": ["import json", "from exportador import a_csv"],
  "visible": ["def test_xxx():\n    assert a_csv([]) == \"\"", "..."],
  "hidden":  ["def test_yyy():\n    assert a_csv(None) == \"\"", "..."]
}`
	}
	return `- Use only the Go stdlib. No external imports.
- In "imports" list EVERY stdlib package your tests use besides "testing" (e.g. "net", "time", "bytes", "encoding/json"). If you forget one, the suite does not compile and is discarded.
- Do not put the import block in the code: only the functions. loom builds the file.

Return ONLY this JSON (no text around it, no markdown):
{
  "imports": ["net", "time", "bytes"],
  "visible": ["func TestXxx(t *testing.T) {\n\t// ...\n}", "..."],
  "hidden":  ["func TestYyy(t *testing.T) {\n\t// ...\n}", "..."]
}`
}
