// Package examiner implements the blind test examiner.
// The examiner sees only the task contract and the public boundary
// (expone signatures, endpoints, CLI). It never sees function bodies.
package examiner

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/sealed"
	"github.com/Pastranauwu/loom/internal/task"
)

const (
	VisibleFileName = "loom_visible_test.go"
	HiddenFileName  = "loom_hidden_test.go"
	DefaultTimeout  = 3 * time.Minute
)

// Options configures one examiner run.
type Options struct {
	Agent   loop.Agent
	Task    task.Task
	Root    string
	Model   string
	Timeout time.Duration
	OnUsage func(loop.Tokens)

	// Lenguaje es el stack del proyecto (config.DetectLanguage), y decide
	// cómo se arma y se valida la suite. Vacío se trata como go. Un stack
	// sin examinador (rust, node) salta el examen.
	Lenguaje string
}

// Runner implements loop.Examinador so cmd/run can wire it without a cycle.
type Runner struct{ Options }

func (r Runner) Run(ctx context.Context, roomPath string) (bool, error) {
	return Run(ctx, roomPath, r.Options)
}

// Run invokes the examiner agent, parses the response, writes the visible
// suite to the worktree and seals the hidden suite.
// El bool dice si la prueba visible del cuarto es del examinador: es lo
// que decide si el implementador tiene vedadas las pruebas.
// Returns (true, nil) when a hidden suite was sealed.
// Returns (true, err) cuando la visible quedó escrita y la oculta no se
// pudo sellar: la tarea tiene examen, el paso suite_oculta se omite.
// Returns (false, nil) when la tarea no es examinable: no hay examen que
// hacer y no falló nada.
// Returns (false, err) on graceful degradation, con el motivo: el error
// NO debe frenar al implementador, solo queda registrado. Degradar en
// silencio dejaba la tarea sin suite y sin rastro de por qué: la veda de
// rutas de prueba sigue activa, la reversión de alcance le quita al
// implementador las suyas y `listo_cuando` pasa sin ejecutar nada.
func Run(ctx context.Context, roomPath string, o Options) (bool, error) {
	if o.Agent == nil {
		return false, nil
	}
	// examen de caja negra: sin interfaz pública declarada no hay
	// frontera que probar. Tareas de andamiaje (init, wiring) no exponen
	// nada; examinarlas solo produce un test file de relleno que después
	// dispara falsos solapamientos entre ramas.
	if len(o.Task.Expone) == 0 && !o.Task.ExamenEsqueleto {
		return false, nil
	}
	// un stack sin examinador (rust, node) no se examina: emitir un
	// archivo de otro lenguaje solo rompería la compilación del cuarto.
	lenguaje := lenguajeDeTarea(o.Lenguaje, o.Task)
	if lenguaje == "" {
		return false, nil
	}
	if (lenguaje == "typescript" || lenguaje == "javascript") && !o.Task.ExamenEsqueleto {
		return false, nil // los contratos Node antiguos conservan sus propias pruebas
	}
	var contrato string
	if o.Task.ExamenEsqueleto {
		var err error
		contrato, err = contratoDelEsqueleto(roomPath, o.Task)
		if err != nil {
			return false, fmt.Errorf("no se pudo leer el contrato ciego del esqueleto · %w", err)
		}
		if contrato == "" {
			return false, fmt.Errorf("el esqueleto no tiene firmas y Casos: para esta tarea")
		}
		v := o.Task.ExamenVisible
		if v == "" || filepath.IsAbs(v) || strings.HasPrefix(filepath.Clean(v), "..") {
			return false, fmt.Errorf("ruta de prueba visible inválida: %q", v)
		}
		// al retomar o escalar la prueba ya está: no se paga otro examen
		// para descartarlo. Si la escribió el examinador sigue vedada; si
		// la escribió el implementador (el examen no se pudo preparar),
		// sigue siendo suya.
		if _, err := os.Stat(filepath.Join(roomPath, v)); err == nil {
			return visibleDelExaminador(roomPath, v), nil
		}
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}

	dir, pkg := inferDirPkg(o.Task.TocarSolo, roomPath)

	// Todo lo que puede invalidar el examen se resuelve ANTES de invocar
	// al modelo: descubrirlo después es pagar una llamada entera para
	// tirarla a la basura.
	var importPath string
	if lenguaje == "go" {
		// El paquete real manda sobre el nombre del directorio. Adivinarlo
		// rompía en el caso más común de Go: `cmd/algo` declara `package
		// main`, no `package algo`, y la suite terminaba con dos paquetes
		// en el mismo directorio — un error que el implementador no puede
		// arreglar, porque las rutas de prueba le están vedadas.
		real := paqueteReal(dir)
		switch real {
		case "":
			// todavía no hay código ahí (tarea de paquete nuevo, el caso
			// más común al arrancar). Renunciar al examen dejaba a esas
			// tareas sin suite: el implementador escribe sus propias
			// pruebas, la reversión de alcance se las quita y
			// `go test ./pkg/...` pasa sin ejecutar nada. El nombre sale
			// del contrato —`expone: ["numeros.Media(...)"]` ya lo
			// declara—, así que el implementador que sigue el contrato
			// coincide con la suite.
			if n := paqueteDeExpone(o.Task.Expone); n != "" {
				pkg = n
			}
		case "main":
			// Go no deja importar un paquete main: no hay examen de caja
			// negra posible sobre un binario desde otro paquete
			return false, nil
		default:
			// el paquete que ya vive en el directorio manda sobre
			// cualquier inferencia
			pkg = real
		}
		// sin nombre no hay suite posible: `package _test` en el mismo
		// directorio rompe la compilación del cuarto y el implementador
		// no puede arreglarlo. Y un `main` inferido cae en la misma
		// pared que el real: `cmd/algo` con `expone: ["main.run(...)"]`
		// daba una suite `package main_test` que importa el binario, y Go
		// responde "imported as main and not used".
		if pkg == "" || pkg == "main" {
			return false, nil
		}

		// El examinador corre ANTES que el implementador, así que en un
		// repo recién nacido todavía no hay go.mod. Sin ruta de import la
		// suite llama a `pkg.Func()` sin importar `pkg`: no compila nunca
		// y nadie puede tocarla. Sin ruta, no hay examen.
		importPath = resolveImportPath(roomPath, dir)
		if importPath == "" {
			return false, nil
		}
	}

	prompt := buildPrompt(o.Task, pkg, lenguaje)
	if contrato != "" {
		prompt += "\nFIRMAS Y CASOS DEL ESQUELETO (sin cuerpos de implementación):\n" + contrato + "\n"
	}
	// Un rol de texto no garantiza ceguera: algunos CLIs conservan acceso de
	// lectura al cwd. En el camino del esqueleto el modelo recibe únicamente
	// el contrato, desde un directorio vacío ajeno al cuarto de trabajo.
	cuartoExamen := roomPath
	if o.Task.ExamenEsqueleto {
		var err error
		cuartoExamen, err = os.MkdirTemp("", "loom-examen-")
		if err != nil {
			return false, fmt.Errorf("no se pudo aislar al examinador · %w", err)
		}
		defer os.RemoveAll(cuartoExamen)
	}
	req := loop.Request{
		RoomPath: cuartoExamen,
		Prompt:   prompt,
		Model:    o.Model,
		Timeout:  o.Timeout,
		Texto:    true,
	}
	res, err := o.Agent.Run(ctx, req)
	if o.OnUsage != nil {
		o.OnUsage(res.Tokens)
	}
	if o.Root != "" {
		if recordErr := guardarUso(o.Root, o.Task.ID, res.Tokens); recordErr != nil {
			return false, fmt.Errorf("no se pudo registrar el gasto del examinador · %w", recordErr)
		}
	}
	if err != nil {
		return false, fmt.Errorf("el examinador no pudo invocar al modelo · %s", err)
	}

	text := res.Text
	if strings.TrimSpace(text) == "" {
		text = res.Stdout
	}
	visible, hidden, imports, err := parseRespText(text)
	if err != nil {
		return false, fmt.Errorf("la respuesta del examinador no se pudo leer · %s", err)
	}
	if len(visible) == 0 {
		return false, fmt.Errorf("el examinador no devolvió ninguna prueba visible")
	}

	visibleRelPath, hiddenRelPath := RutasSuite(o.Task.TocarSolo, lenguaje)
	if o.Task.ExamenEsqueleto {
		visibleRelPath = o.Task.ExamenVisible
		_, hiddenName := nombresSuite(lenguaje)
		hiddenRelPath = toRelPath(filepath.Join(filepath.Dir(visibleRelPath), hiddenName))
		// el implementador no puede tocar esta prueba: si importa un
		// archivo que no existe no pasa nunca y la tarea quema sus intentos
		if roto := importRelativoRoto(roomPath, visibleRelPath, imports); roto != "" {
			return false, fmt.Errorf("la suite del examinador importa %s, que no existe desde %s", roto, visibleRelPath)
		}
	}

	visibleContent, okVisible := suiteCompleta(lenguaje, roomPath, pkg, importPath, imports, visible)
	if !okVisible {
		return false, fmt.Errorf("no se pudieron resolver los imports de la suite visible")
	}
	// un examinador que emite pruebas que no compilan bloquea al
	// implementador: no puede tocar el archivo y su impl correcta
	// igual da "build failed". Si la suite ni siquiera parsea, se
	// descarta y el implementador corre sin suite ciega.
	if err := validarSintaxisEnProyecto(roomPath, lenguaje, visibleRelPath, visibleContent); err != nil {
		return false, fmt.Errorf("la suite visible del examinador no compila · %s", err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(roomPath, visibleRelPath)), 0o755); err != nil {
		return false, fmt.Errorf("no se pudo preparar la suite visible · %s", err)
	}
	visiblePath := filepath.Join(roomPath, filepath.FromSlash(visibleRelPath))
	if err := os.WriteFile(visiblePath, []byte(visibleContent), 0o644); err != nil {
		return false, fmt.Errorf("no se pudo escribir la suite visible · %s", err)
	}
	// commit visible tests so the loop's revertFueraDeAlcance does not
	// undo them — git status won't list committed files as "changed".
	commitVisible(roomPath, visiblePath)

	// Desde aquí la visible ya existe y es la que juzga la tarea: lo que
	// falle con la oculta deja el paso suite_oculta sin correr, no a la
	// tarea sin examen. (Esperar a tener las dos para escribir la visible
	// dejaba sin suite a toda tarea cuyo examinador no devolvía "hidden",
	// y con las rutas de prueba vedadas nadie más podía escribirla.)
	if len(hidden) == 0 {
		return true, fmt.Errorf("el examinador no devolvió suite oculta · el paso suite_oculta se omite")
	}
	hiddenContent, okHidden := suiteCompleta(lenguaje, roomPath, pkg, importPath, imports, hidden)
	if !okHidden {
		return true, fmt.Errorf("no se pudieron resolver los imports de la suite oculta · solo queda la visible")
	}
	if err := validarSintaxisEnProyecto(roomPath, lenguaje, hiddenRelPath, hiddenContent); err != nil {
		return true, fmt.Errorf("la suite oculta del examinador no compila · solo queda la visible · %s", err)
	}
	s := sealed.SuiteOculta{
		Content: hiddenContent,
		Archivo: hiddenRelPath,
	}
	if err := sealed.Write(o.Root, o.Task.ID, s); err != nil {
		return true, fmt.Errorf("no se pudo sellar la suite oculta · solo queda la visible · %s", err)
	}
	return true, nil
}

// mensajeExamen es el commit con que el examinador fija su prueba visible.
const mensajeExamen = "exam: suite visible"

// visibleDelExaminador reporta si la prueba en rel la escribió el
// examinador: su primer commit es el del examen.
func visibleDelExaminador(roomPath, rel string) bool {
	out, err := exec.Command("git", "-C", roomPath, "log", "--diff-filter=A", "--format=%s", "--", rel).Output()
	if err != nil {
		return false
	}
	l := strings.Split(strings.TrimSpace(string(out)), "\n")
	return l[len(l)-1] == mensajeExamen
}

// importRelativoRoto devuelve el primer import relativo de la suite que
// no lleva a ningún archivo del cuarto, o "". Los de paquete (vitest,
// node:test) no se miran.
func importRelativoRoto(roomPath, visibleRel string, imports []string) string {
	dir := filepath.Dir(filepath.Join(roomPath, filepath.FromSlash(visibleRel)))
	for _, imp := range imports {
		i := strings.LastIndexAny(imp, "'\"")
		if i < 0 {
			continue
		}
		j := strings.LastIndexAny(imp[:i], "'\"")
		if j < 0 {
			continue
		}
		ruta := imp[j+1 : i]
		if !strings.HasPrefix(ruta, ".") {
			continue
		}
		base := filepath.Join(dir, filepath.FromSlash(ruta))
		// TypeScript importa "./x.js" y el archivo es x.ts
		sinExt := strings.TrimSuffix(base, filepath.Ext(base))
		existe := false
		for _, c := range []string{base, base + ".ts", base + ".tsx", base + ".js", base + ".jsx", base + ".mjs", sinExt + ".ts", sinExt + ".tsx", filepath.Join(base, "index.ts"), filepath.Join(base, "index.js")} {
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				existe = true
				break
			}
		}
		if !existe {
			return ruta
		}
	}
	return ""
}

// buildPrompt constructs the examiner instruction. lenguaje va explícito
// en el prompt: sin decirlo, el modelo asume Go y devuelve una suite que
// el proyecto no puede correr.
func buildPrompt(t task.Task, pkg, lenguaje string) string {
	var b strings.Builder
	b.WriteString("You are loom's blind examiner. You write tests WITHOUT seeing the implementation.\n\n")
	fmt.Fprintf(&b, "Task: %s — %s\n", t.ID, t.Titulo)
	if t.Porque != "" {
		fmt.Fprintf(&b, "Why: %s\n", t.Porque)
	}
	fmt.Fprintf(&b, "Done when: %s\n", t.ListoCuando)
	if len(t.Expone) > 0 {
		fmt.Fprintf(&b, "Public contract (signatures it must expose): %s\n", strings.Join(t.Expone, "; "))
	}
	if t.ExamenEsqueleto {
		fmt.Fprintf(&b, "The visible test must live in %s and be discovered by done_when.\n", t.ExamenVisible)
	}
	if len(t.TocarSolo) > 0 {
		fmt.Fprintf(&b, "Files it may touch: %s\n", strings.Join(t.TocarSolo, ", "))
	}
	if t.Riesgos != "" {
		fmt.Fprintf(&b, "Risks: %s\n", t.Riesgos)
	}
	fmt.Fprintf(&b, "Test language: %s · write ONLY in that language.\n", lenguaje)
	if lenguaje == "go" {
		fmt.Fprintf(&b, "Go package of the tests: %s_test\n", pkg)
	}
	b.WriteString(`
Rules:
- BLACK-BOX tests: test only the public interface declared in the contract.
- Do NOT write implementation code.
- 70% go in "visible": the implementer will see them as acceptance criteria.
- 30% go in "hidden": edge cases and limits the implementer will NOT see.
- "hidden" tests must fail if the implementation is superficial or only tuned for "visible".
- Each test function is independent.
`)
	b.WriteString(reglasDe(lenguaje))
	return b.String()
}

type suiteJSON struct {
	Imports []string `json:"imports"`
	Visible []string `json:"visible"`
	Hidden  []string `json:"hidden"`
}

func parseRespText(text string) (visible, hidden, imports []string, err error) {
	t := strings.TrimSpace(text)
	t = strings.TrimPrefix(t, "```json")
	t = strings.TrimPrefix(t, "```")
	t = strings.TrimSuffix(t, "```")
	t = strings.TrimSpace(t)

	ini := strings.Index(t, "{")
	fin := strings.LastIndex(t, "}")
	if ini == -1 || fin <= ini {
		return nil, nil, nil, fmt.Errorf("no JSON in response")
	}
	var s suiteJSON
	if err := json.Unmarshal([]byte(t[ini:fin+1]), &s); err != nil {
		return nil, nil, nil, err
	}
	return s.Visible, s.Hidden, s.Imports, nil
}

// buildGoFile assembles a complete Go external test file.
// importPath is the full import path of the package under test (e.g.
// "mymod/calculator"). Empty string means no package import is added.
// extra are stdlib packages declared by the examiner; only those a test
// body actually references are kept, so a package used only by the other
// suite does not turn into an "imported and not used" build error.
func buildGoFile(pkg, importPath string, extra, funcs []string) string {
	body := strings.Join(funcs, "\n")

	var b strings.Builder
	// el encabezado le dice al implementador cómo tiene que llamarse su
	// paquete: la suite es externa (pkg_test) y, si él elige otro nombre,
	// Go responde "found packages X and pkg (…_test.go)" — un error que
	// no puede arreglar, porque esta ruta le está vedada.
	fmt.Fprintf(&b, "// loom · suite del examinador ciego: criterio de aceptación, no editable.\n")
	fmt.Fprintf(&b, "// El paquete de este directorio tiene que llamarse %q.\n\n", pkg)
	fmt.Fprintf(&b, "package %s_test\n\nimport (\n\t\"testing\"\n", pkg)
	if importPath != "" {
		fmt.Fprintf(&b, "\t%q\n", importPath)
	}
	for _, imp := range dedup(extra) {
		if !importPermitido(imp, importPath) || imp == "testing" {
			continue
		}
		if !strings.Contains(body, selector(imp)+".") {
			continue
		}
		fmt.Fprintf(&b, "\t%q\n", imp)
	}
	b.WriteString(")\n\n")
	for _, f := range funcs {
		b.WriteString(f)
		if !strings.HasSuffix(strings.TrimSpace(f), "}") {
			b.WriteString("\n}")
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

// stdlibImport descarta paquetes externos: el primer segmento de la
// stdlib nunca lleva punto (dominio).
func stdlibImport(path string) bool {
	if path == "" {
		return false
	}
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	return !strings.Contains(first, ".")
}

// importPermitido acepta la stdlib y los paquetes del mismo módulo que el
// paquete bajo prueba. Filtrar por stdlibImport a secas tiraba el import
// hermano cuando la ruta del módulo lleva punto ("github.com/x/y"), y la
// suite quedaba con `ast.Number` sin importar ast: no compila y el
// implementador no puede tocarla.
func importPermitido(imp, importPath string) bool {
	if stdlibImport(imp) {
		return true
	}
	a, b := primerSegmento(imp), primerSegmento(importPath)
	return a != "" && a == b
}

func primerSegmento(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}

// selector devuelve el identificador con que se referencia un import:
// el último segmento de la ruta ("encoding/json" → "json").
func selector(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func dedup(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

// resolveImportPath reads go.mod to find the module name, then computes
// the import path for the package in dir. Returns "" on any failure.
func resolveImportPath(roomPath, dir string) string {
	data, err := os.ReadFile(filepath.Join(roomPath, "go.mod"))
	if err != nil {
		return ""
	}
	// find "module <name>" line
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		mod := strings.TrimSpace(strings.TrimPrefix(line, "module"))
		rel, err := filepath.Rel(roomPath, dir)
		if err != nil || rel == "." || rel == "" {
			return mod
		}
		return mod + "/" + filepath.ToSlash(rel)
	}
	return ""
}

// inferDirPkg returns the absolute worktree directory and the Go package
// name inferred from tocar_solo, skipping file-like entries (e.g. go.mod).
// Falls back to roomPath and "main".
func inferDirPkg(tocarSolo []string, roomPath string) (dir, pkg string) {
	absDir, relDir := inferDirFromTocarSolo(tocarSolo, roomPath)
	if relDir == "." {
		return absDir, "main"
	}
	parts := strings.Split(relDir, "/")
	pkg = parts[len(parts)-1]
	if pkg == "" || pkg == "." {
		pkg = "main"
	}
	return absDir, pkg
}

// paqueteDeExpone saca el nombre de paquete del prefijo de las firmas
// prometidas ("numeros.Media(xs []float64) (float64, error)" → numeros).
// Devuelve "" si ninguna firma viene calificada.
func paqueteDeExpone(expone []string) string {
	for _, f := range expone {
		f = strings.TrimSpace(f)
		i := strings.Index(f, ".")
		if i <= 0 {
			continue
		}
		n := f[:i]
		if !esIdentificador(n) {
			continue
		}
		return n
	}
	return ""
}

// esIdentificador filtra prefijos que no son un nombre de paquete Go
// (rutas, firmas sin calificar, ruido del planificador).
func esIdentificador(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// paqueteReal lee el nombre de paquete declarado en los .go que ya viven
// en dir. Devuelve "" si el directorio no existe o todavía no tiene
// código.
//
// Adivinarlo desde el nombre del directorio rompía en el caso más común
// de Go: `cmd/algo` declara `package main`, no `package algo`. La suite
// salía como `package algo_test` en el mismo directorio y `go build`
// respondía "found packages algo (…_test.go) and main (main.go)" — un
// error que el implementador no puede arreglar, porque las rutas de
// prueba le están vedadas. La tarea quedaba roja para siempre.
func paqueteReal(dir string) string {
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	fset := token.NewFileSet()
	for _, e := range entradas {
		nombre := e.Name()
		if e.IsDir() || !strings.HasSuffix(nombre, ".go") || strings.HasSuffix(nombre, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, nombre), nil, parser.PackageClauseOnly)
		if err != nil || f.Name == nil {
			continue
		}
		return f.Name.Name
	}
	return ""
}

func toRelPath(p string) string { return filepath.ToSlash(p) }

// commitVisible commits the visible test file to the worktree so the
// implementer loop won't revert it (the revert applies to changes, not
// to already-committed files).
func commitVisible(roomPath, absPath string) {
	rel, err := filepath.Rel(roomPath, absPath)
	if err != nil {
		return
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", roomPath}, args...)...)
		_ = cmd.Run()
	}
	git("-c", "user.name=loom", "-c", "user.email=loom@local", "add", rel)
	git("-c", "user.name=loom", "-c", "user.email=loom@local", "commit", "-m", mensajeExamen)
}

// inferDirFromTocarSolo finds the best directory from tocar_solo:
//   - "internal/wol/**" → "internal/wol" (strip glob)
//   - "calculator/calculator.go" → "calculator" (take parent of a specific file)
//   - "go.mod" → skip (top-level dotfile, not a package dir)
//
// Falls back to roomPath when no useful entry is found.
func inferDirFromTocarSolo(tocarSolo []string, roomPath string) (absDir, relDir string) {
	for _, g := range tocarSolo {
		rel := strings.TrimRight(g, "/*")
		if rel == "" || rel == "." {
			return roomPath, "."
		}
		base := filepath.Base(rel)
		if strings.Contains(base, ".") {
			// specific file — use its parent directory
			parent := filepath.Dir(rel)
			if parent == "." || parent == "" {
				// top-level file like "go.mod" — skip to next entry
				continue
			}
			if roomPath != "" {
				return filepath.Join(roomPath, parent), parent
			}
			return parent, parent
		}
		if roomPath != "" {
			return filepath.Join(roomPath, rel), rel
		}
		return rel, rel
	}
	return roomPath, "."
}
