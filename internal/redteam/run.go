package redteam

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/examiner"
	"github.com/Pastranauwu/loom/internal/task"
)

// Opciones configures one attack on one task.
type Opciones struct {
	Root       string
	Tarea      task.Task
	Ref        string // commit or branch to attack: the integrated set or the task's branch
	Superficie Superficie
	Gen        Generador
	Timeout    time.Duration // per attack run; default 2 min
	Conservar  string        // directory to keep the failing tests in; "" keeps nothing
}

// Atacar spends one model call and one test run per attack, in a throwaway
// worktree that is always removed: the delivery branch is never touched.
func Atacar(ctx context.Context, o Opciones) (Informe, error) {
	inf := Informe{Tarea: o.Tarea.ID}
	leng := lenguajeDe(o.Tarea)
	dir := dirDe(o.Tarea.TocarSolo)
	inf.Lenguaje, inf.Dir = leng, dir
	switch {
	case len(o.Tarea.Expone) == 0:
		inf.Omitido = "the task exposes nothing to attack"
		return inf, nil
	case leng == "":
		inf.Omitido = "no attack runner for this task's language (Go, Python, TypeScript, Rust)"
		return inf, nil
	}
	// lo que deja sin ataque posible se decide antes de pagar al modelo
	var cargoTarget string
	switch leng {
	case "typescript", "javascript":
		pkg := examiner.ToolchainDir(o.Root, path.Join(dir, "x.ts"))
		if o.Superficie.Runner = runnerNode(pkg); o.Superficie.Runner == "" {
			inf.Omitido = "no vitest or jest in package.json"
			return inf, nil
		}
		inf.Runner = o.Superficie.Runner
		if rel, err := filepath.Rel(o.Root, pkg); err == nil {
			inf.Dir = filepath.ToSlash(rel)
		}
	case "rust":
		crate, nombre := examiner.CrateExaminable(o.Root, o.Tarea.TocarSolo)
		if nombre == "" {
			inf.Omitido = "the crate has no library: integration tests cannot import it"
			return inf, nil
		}
		o.Superficie.Crate, inf.Dir = nombre, crate
	}
	if o.Timeout <= 0 {
		o.Timeout = 2 * time.Minute
	}
	o.Superficie.Lenguaje = leng

	texto, err := o.Gen.Generar(ctx, Prompt(o.Tarea, o.Superficie))
	if err != nil {
		return inf, err
	}
	validos, descartados, err := Parse(texto, o.Tarea, o.Superficie)
	inf.Descartados = descartados
	if err != nil {
		return inf, err
	}
	if len(validos) == 0 {
		inf.Omitido = "the attacker proposed no grounded attack"
		return inf, nil
	}

	wt, limpiar, err := worktreeEfimero(ctx, o.Root, o.Ref)
	if err != nil {
		return inf, err
	}
	defer limpiar()
	if leng == "rust" {
		// dentro del worktree: el sandbox solo deja escribir ahí, y se borra con él
		cargoTarget = filepath.Join(wt, ".loom-cargo-target")
	}

	for i, a := range validos {
		r, nombre, contenido := correr(ctx, wt, dir, leng, o.Tarea, o.Superficie, i+1, a, o.Timeout, cargoTarget)
		inf.Resultados = append(inf.Resultados, r)
		if o.Conservar != "" && nombre != "" && (r.Clase == Confirmado || r.Clase == Afirmacion) {
			guardar(o.Conservar, o.Tarea.ID, nombre, contenido)
		}
	}
	return inf, nil
}

// dirDe is the directory a task's non-test scope points to.
func dirDe(tocar []string) string {
	for _, g := range tocar {
		if task.EsArchivoDePrueba(g) {
			continue
		}
		if i := strings.IndexAny(g, "*?["); i >= 0 {
			g = g[:i]
		}
		if !strings.HasSuffix(g, "/") {
			g = path.Dir(g)
		}
		if g = strings.TrimSuffix(g, "/"); g != "" {
			return g
		}
	}
	return "."
}

func worktreeEfimero(ctx context.Context, root, ref string) (string, func(), error) {
	if ref == "" {
		ref = "HEAD"
	}
	dir, err := os.MkdirTemp("", "loom-redteam-")
	if err != nil {
		return "", nil, err
	}
	wt := filepath.Join(dir, "wt")
	if out, err := exec.CommandContext(ctx, "git", "-C", root, "worktree", "add", "--detach", "-q", wt, ref).CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", nil, fmt.Errorf("could not create the attack worktree · %s", strings.TrimSpace(string(out)))
	}
	return wt, func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", wt).Run()
		_ = os.RemoveAll(dir)
		_ = exec.Command("git", "-C", root, "worktree", "prune").Run()
	}, nil
}

func correr(ctx context.Context, wt, dir, leng string, t task.Task, s Superficie, n int, a Ataque, timeout time.Duration, cargoTarget string) (Resultado, string, string) {
	r := Resultado{Ataque: a}
	rn, falla := prepararRunner(wt, dir, leng, t, s, n, a, cargoTarget)
	if falla != "" {
		r.Clase, r.Detalle = FallaAtacante, falla
		return r, "", ""
	}
	defer func() {
		os.Remove(rn.archivo)
		for _, e := range rn.extras {
			os.Remove(e)
		}
	}()
	if err := os.MkdirAll(filepath.Dir(rn.archivo), 0o755); err != nil {
		r.Clase, r.Detalle = FallaAtacante, err.Error()
		return r, "", ""
	}
	if err := os.WriteFile(rn.archivo, []byte(rn.contenido), 0o644); err != nil {
		r.Clase, r.Detalle = FallaAtacante, err.Error()
		return r, "", ""
	}
	nombre := filepath.Base(rn.archivo)
	if rn.compilar != nil {
		if salida, ok := ejecutar(ctx, rn.cwd, rn.compilar, rn.env, timeout, t.Red); !ok {
			if mal, det := noCompila(leng, salida, nombre); mal {
				r.Clase, r.Detalle = FallaAtacante, det
				return r, nombre, rn.contenido
			}
		}
	}
	salida, ok := ejecutar(ctx, rn.cwd, rn.correr, rn.env, timeout, t.Red)
	r.Clase, r.Detalle = Clasificar(salida, leng, ok)
	if r.Clase == Confirmado {
		// a crash that does not repeat is not proof
		if _, otra := ejecutar(ctx, rn.cwd, rn.correr, rn.env, timeout, t.Red); otra {
			r.Clase, r.Detalle = Afirmacion, "did not reproduce on the second run"
		}
	}
	return r, nombre, rn.contenido
}

func ejecutar(ctx context.Context, dir string, args, env []string, timeout time.Duration, red bool) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := task.Argv(ctx, dir, red, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func hayRace() bool {
	if _, err := exec.LookPath("gcc"); err != nil {
		return false
	}
	out, err := exec.Command("go", "env", "CGO_ENABLED").Output()
	return err == nil && strings.TrimSpace(string(out)) == "1"
}

func paqueteGo(dir string) string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	fset := token.NewFileSet()
	for _, e := range es {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		if f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.PackageClauseOnly); err == nil && f.Name != nil {
			return f.Name.Name
		}
	}
	return ""
}

// ArmarGo writes a test file in the package of the code under attack. Only
// the imports the test body actually uses are kept: a model that lists one
// too many would otherwise break compilation and lose a good attack.
func ArmarGo(pkg string, imports []string, test string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\nimport (\n\t\"testing\"\n")
	visto := map[string]bool{"testing": true}
	for _, imp := range imports {
		imp = strings.TrimSpace(imp)
		alias, ruta := "", strings.Trim(imp, `"`)
		if i := strings.LastIndex(imp, " "); i > 0 {
			alias, ruta = imp[:i], strings.Trim(strings.TrimSpace(imp[i:]), `"`)
		}
		nombre := alias
		if nombre == "" {
			nombre = path.Base(ruta)
			if reVersion.MatchString(nombre) {
				nombre = path.Base(path.Dir(ruta))
			}
		}
		if ruta == "" || visto[ruta] || !strings.Contains(test, nombre+".") {
			continue
		}
		visto[ruta] = true
		if alias != "" {
			fmt.Fprintf(&b, "\t%s %q\n", alias, ruta)
		} else {
			fmt.Fprintf(&b, "\t%q\n", ruta)
		}
	}
	b.WriteString(")\n\n" + strings.TrimSpace(test) + "\n")
	return b.String()
}

var reVersion = regexp.MustCompile(`^v\d+$`)

// ArmarPython writes a pytest file.
func ArmarPython(imports []string, test string) string {
	var b strings.Builder
	for _, i := range imports {
		b.WriteString(strings.TrimSpace(i) + "\n")
	}
	return b.String() + "\n\n" + strings.TrimSpace(test) + "\n"
}

func guardar(dir, id, nombre, contenido string) {
	d := filepath.Join(dir, id)
	if os.MkdirAll(d, 0o755) != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(d, nombre), []byte(contenido), 0o644)
}

var reNombreTest = regexp.MustCompile(`(?m)^(?:func (Test\w+)\(|def (test_\w+)\()`)

// Reabrir turns a confirmed attack into the contract of the task that owns the
// target: the hostile test joins its scope and its done_when, and its source
// goes in the notes so the agent can write it before fixing the code. inf
// carries the language, runner and directory the attack ran with.
func Reabrir(t task.Task, r Resultado, inf Informe) task.Task {
	dir := dirDe(t.TocarSolo)
	n := reNombreTest.FindStringSubmatch(r.Ataque.Test)
	nombre := ""
	if n != nil {
		nombre = n[1] + n[2]
	}
	var ruta, comando string
	switch inf.Lenguaje {
	case "python":
		ruta = path.Join(dir, "test_loom_redteam_"+t.ID+".py")
		comando = "python3 -m pytest -q " + ruta
		if nombre != "" {
			comando += " -k " + nombre
		}
	case "typescript", "javascript":
		ext := ".test.ts"
		if inf.Lenguaje == "javascript" {
			ext = ".test.js"
		}
		rel := "tests/" + objetivoAtaque(t.ID) + ext
		ruta = path.Join(inf.Dir, rel)
		comando = strings.Join(comandoNode(inf.Runner, rel), " ")
		comando = strings.Replace(comando, "npx --no-install", "npx", 1)
		if inf.Dir != "" && inf.Dir != "." {
			comando = "cd " + inf.Dir + " && " + comando
		}
	case "rust":
		obj := objetivoAtaque(t.ID)
		ruta = path.Join(inf.Dir, "tests", obj+".rs")
		comando = "cargo " + strings.Join(examiner.ArgsCargo(inf.Dir, obj, false), " ")
	default:
		ruta = path.Join(dir, "loom_redteam_"+strings.ToLower(strings.ReplaceAll(t.ID, "-", "_"))+"_test.go")
		comando = "go test ./" + dir + " -run '^" + nombre + "$'"
	}
	t.TocarSolo = append(t.TocarSolo, ruta)
	if !strings.Contains(t.ListoCuando, comando) {
		t.ListoCuando += " && (" + comando + ")"
	}
	if t.Peso == "" || t.Peso == "liviana" {
		t.Peso = "media"
	}
	fuente := strings.TrimSpace(r.Ataque.Test)
	if inf.Lenguaje != "go" && inf.Lenguaje != "" && len(r.Ataque.Imports) > 0 {
		fuente = strings.Join(r.Ataque.Imports, "\n") + "\n\n" + fuente
	}
	t.Notas = strings.TrimSpace(t.Notas) + "\n\nRED TEAM. An attack broke what this task exposes (" + r.Ataque.Objetivo + ").\n" +
		"Invariant: " + r.Ataque.Invariante + "\nFailure: " + r.Detalle + "\n" +
		"Write this test in " + ruta + " first (it must fail for that reason), then fix the code until it passes. Do not weaken the test or any existing one.\n" +
		"```\n" + fuente + "\n```"
	return t
}
