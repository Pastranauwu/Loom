package redteam

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Pastranauwu/loom/internal/examiner"
	"github.com/Pastranauwu/loom/internal/task"
)

// runner is how one attack is written, compiled and run in its language.
type runner struct {
	archivo   string   // absolute path of the attack file
	contenido string   // its source
	extras    []string // helper files removed with the attack
	compilar  []string // static check before running; nil when the runner compiles itself
	correr    []string
	cwd       string
	env       []string
}

// objetivoAtaque is the file and cargo target name of a task's attacks.
func objetivoAtaque(id string) string {
	return "loom_adversarial_" + strings.ToLower(strings.ReplaceAll(id, "-", "_"))
}

func lenguajeDe(t task.Task) string {
	sinPruebas := task.Task{}
	for _, f := range t.TocarSolo {
		if !task.EsArchivoDePrueba(f) {
			sinPruebas.TocarSolo = append(sinPruebas.TocarSolo, strings.TrimRight(f, "*?["))
		}
	}
	if l := examiner.LenguajeDeTarea("ninguno", sinPruebas); l != "" {
		return l
	}
	for _, e := range t.Expone {
		switch {
		case strings.Contains(e, "def "):
			return "python"
		case strings.Contains(e, "fn ") || strings.HasPrefix(strings.TrimSpace(e), "pub "):
			return "rust"
		case strings.Contains(e, "func "):
			return "go"
		case strings.Contains(e, "function ") || strings.Contains(e, "=>") || strings.Contains(e, "export ") || strings.Contains(e, "): "):
			return "typescript"
		case strings.Contains(e, "(") && !strings.Contains(e, ":"):
			return "go"
		}
	}
	return ""
}

// runnerNode reads package.json and names the test runner an attack can use.
func runnerNode(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return ""
	}
	var p struct {
		Scripts map[string]string `json:"scripts"`
		Deps    map[string]string `json:"dependencies"`
		DevDeps map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(data, &p) != nil {
		return ""
	}
	for _, r := range []string{"vitest", "jest"} {
		_, dep := p.Deps[r]
		_, dev := p.DevDeps[r]
		if dep || dev || strings.Contains(p.Scripts["test"], r) {
			return r
		}
	}
	return ""
}

// comandoNode runs one test file with the project's runner, never installing.
func comandoNode(runner, archivo string) []string {
	if runner == "jest" {
		return []string{"npx", "--no-install", "jest", archivo}
	}
	return []string{"npx", "--no-install", "vitest", "run", archivo}
}

func prepararRunner(wt, dir, leng string, t task.Task, s Superficie, n int, a Ataque, cargoTarget string) (runner, string) {
	abs := filepath.Join(wt, filepath.FromSlash(dir))
	switch leng {
	case "python":
		return runner{
			archivo:   filepath.Join(abs, fmt.Sprintf("test_loom_redteam_%d.py", n)),
			contenido: ArmarPython(a.Imports, a.Test),
			correr:    []string{"python3", "-m", "pytest", "-q", "-x", "--no-header", "-p", "no:cacheprovider", filepath.Join(abs, fmt.Sprintf("test_loom_redteam_%d.py", n))},
			cwd:       wt,
			env:       []string{"PYTHONPATH=" + wt},
		}, ""
	case "typescript", "javascript":
		pkg := examiner.ToolchainDir(wt, path.Join(dir, "x.ts"))
		ext := ".test.ts"
		if leng == "javascript" {
			ext = ".test.js"
		}
		base := fmt.Sprintf("%s_%d%s", objetivoAtaque(t.ID), n, ext)
		r := runner{
			archivo:   filepath.Join(pkg, "tests", base),
			contenido: ArmarNode(a.Imports, a.Test),
			correr:    comandoNode(s.Runner, "tests/"+base),
			cwd:       pkg,
		}
		if leng == "typescript" {
			if _, err := os.Stat(filepath.Join(pkg, "tsconfig.json")); err == nil {
				cfg := filepath.Join(pkg, fmt.Sprintf("tsconfig.loom-redteam-%d.json", n))
				_ = os.WriteFile(cfg, []byte(`{"extends":"./tsconfig.json","compilerOptions":{"noEmit":true},"include":["tests/`+base+`"]}`), 0o644)
				r.extras = append(r.extras, cfg)
				r.compilar = []string{"npx", "--no-install", "tsc", "-p", filepath.Base(cfg), "--pretty", "false"}
			}
		}
		return r, ""
	case "rust":
		crate, nombre := examiner.CrateExaminable(wt, t.TocarSolo)
		if nombre == "" {
			return runner{}, "no Rust library crate at " + crate
		}
		obj := fmt.Sprintf("%s_%d", objetivoAtaque(t.ID), n)
		env := []string{"CARGO_TERM_COLOR=never"}
		if cargoTarget != "" {
			env = append(env, "CARGO_TARGET_DIR="+cargoTarget)
		}
		return runner{
			archivo:   filepath.Join(wt, crate, "tests", obj+".rs"),
			contenido: examiner.ArmarRust(nombre, a.Imports, []string{a.Test}),
			compilar:  append([]string{"cargo"}, examiner.ArgsCargo(crate, obj, true)...),
			correr:    append([]string{"cargo"}, examiner.ArgsCargo(crate, obj, false)...),
			cwd:       wt,
			env:       env,
		}, ""
	}
	pkg := paqueteGo(abs)
	if pkg == "" {
		return runner{}, "no Go package at " + dir
	}
	args := []string{"go", "test", "-count=1", "-run", "TestRedTeam"}
	if a.Invariante == "concurrency" && hayRace() {
		args = append(args, "-race")
	}
	return runner{
		archivo:   filepath.Join(abs, fmt.Sprintf("loom_redteam_%d_test.go", n)),
		contenido: ArmarGo(pkg, a.Imports, a.Test),
		correr:    append(args, "./"+strings.TrimPrefix(dir, "./")),
		cwd:       wt,
	}, ""
}

var reErrorTS = regexp.MustCompile(`(?m)^.*error TS\d+:.*$`)

// noCompila reads the static check: in TypeScript only errors in the attack
// file count (the project may already carry its own); in Rust any error does,
// because the code under attack already exists.
func noCompila(leng, salida, archivo string) (bool, string) {
	if leng == "rust" {
		for _, l := range strings.Split(salida, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "error") {
				return true, "does not compile: " + recortarLinea(l)
			}
		}
		return true, "does not compile"
	}
	for _, l := range reErrorTS.FindAllString(salida, -1) {
		if strings.Contains(l, archivo) {
			return true, "does not typecheck: " + recortarLinea(l)
		}
	}
	return false, ""
}

func recortarLinea(l string) string {
	l = strings.TrimSpace(l)
	if len(l) > 200 {
		return l[:200]
	}
	return l
}

// ArmarNode writes a Vitest/Jest file: import lines, then the test block.
func ArmarNode(imports []string, test string) string {
	var b strings.Builder
	for _, i := range imports {
		if i = strings.TrimSpace(i); i != "" {
			b.WriteString(i + "\n")
		}
	}
	return b.String() + "\n" + strings.TrimSpace(test) + "\n"
}

var (
	reFrameJS     = regexp.MustCompile(`at (?:.*? \()?(?:file://)?([^\s()]+\.(?:ts|tsx|js|jsx|mjs|cjs|mts|cts)):(\d+):\d+\)?`)
	reErrorJS     = regexp.MustCompile(`(?m)\b(\w*Error): .*$`)
	rePanicRust   = regexp.MustCompile(`panicked at (?:'.*?', )?([^\s:'"]+\.rs):(\d+)`)
	reGlobalFalta = regexp.MustCompile(`ReferenceError: (?:it|test|describe|expect|vi|jest) is not defined`)
)

func clasificarNode(s string) (Clase, string) {
	for _, malo := range []string{"Cannot find module", "Failed to load", "Transform failed", "SyntaxError", "No test files found", "No tests found", "Failed to resolve import"} {
		if strings.Contains(s, malo) {
			return FallaAtacante, "the test does not run: " + malo
		}
	}
	if reGlobalFalta.MatchString(s) {
		return FallaAtacante, "the test does not run: " + reGlobalFalta.FindString(s)
	}
	if strings.Contains(s, "AssertionError") || strings.Contains(s, "expect(received)") {
		return Afirmacion, strings.TrimSpace(primeraLinea(s, "Error"))
	}
	for _, m := range reFrameJS.FindAllStringSubmatch(s, -1) {
		f := m[1]
		if strings.Contains(f, "node_modules") || strings.Contains(f, "/tests/") || strings.HasPrefix(f, "tests/") ||
			strings.Contains(f, "loom_adversarial") || strings.Contains(f, ".test.") || strings.HasPrefix(f, "node:") {
			continue
		}
		err := "uncaught error"
		if e := reErrorJS.FindString(s); e != "" {
			err = recortarLinea(e)
		}
		return Confirmado, err + " · " + f + ":" + m[2]
	}
	if strings.Contains(s, "FAIL") || strings.Contains(s, "failed") {
		return Afirmacion, strings.TrimSpace(primeraLinea(s, "FAIL"))
	}
	return FallaAtacante, "unrecognized failure"
}

func clasificarRust(s string) (Clase, string) {
	if strings.Contains(s, "error[E") || strings.Contains(s, "could not compile") {
		return FallaAtacante, "the test does not compile"
	}
	for _, m := range rePanicRust.FindAllStringSubmatch(s, -1) {
		f := m[1]
		if strings.HasPrefix(f, "tests/") || strings.Contains(f, "/tests/") {
			return Afirmacion, "assertion · " + f + ":" + m[2]
		}
		if strings.Contains(f, "/rustc/") || strings.Contains(f, ".cargo/registry") || strings.HasPrefix(f, "library/") {
			continue
		}
		return Confirmado, strings.TrimSpace(primeraLinea(s, "panicked at")) + " · " + f + ":" + m[2]
	}
	if strings.Contains(s, "test result: FAILED") {
		return Afirmacion, strings.TrimSpace(primeraLinea(s, "test result"))
	}
	return FallaAtacante, "unrecognized failure"
}

// reglasAtaque is the language-specific shape of one attack.
func reglasAtaque(s Superficie) string {
	switch s.Lenguaje {
	case "python":
		return "Each is ONE pytest function named test_redteam_<n>(); \"imports\" holds complete import lines."
	case "typescript", "javascript":
		return "Each is ONE " + s.Runner + " block `it('redteam <n> ...', () => { ... })` (async if needed). \"imports\" holds complete import lines, including the runner (`import { it, expect } from '" + s.Runner + "'` for vitest) and the module under attack with a path relative to the package's tests/ folder."
	case "rust":
		return "Each is ONE `#[test] fn redteam_<n>()` function. loom adds `use " + s.Crate + "::*;`; \"imports\" holds only extra `use` lines from std or the crate. Prefer inputs that make the code panic (unwrap on bad data, overflow, out-of-bounds)."
	}
	return "Each is ONE test function named TestRedTeam<N>(t *testing.T) in the package under attack; \"imports\" lists import paths."
}
