package redteam

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/task"
)

func escribir(t *testing.T, base string, archivos map[string]string) {
	t.Helper()
	for ruta, contenido := range archivos {
		p := filepath.Join(base, ruta)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(contenido), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunnerPorLenguajeTier1(t *testing.T) {
	wt := t.TempDir()
	escribir(t, wt, map[string]string{
		"go.mod":            "module example.com/m\n",
		"pkg/pkg.go":        "package pkg\n",
		"app/m.py":          "def f(x: int) -> int: ...\n",
		"web/package.json":  `{"devDependencies":{"vitest":"^2"}}`,
		"web/tsconfig.json": `{"compilerOptions":{"strict":true}}`,
		"web/src/parse.ts":  "export function parse(s: string): number { return 0 }\n",
		"api/package.json":  `{"scripts":{"test":"jest"}}`,
		"core/Cargo.toml":   "[package]\nname = \"wake-core\"\n",
		"core/src/lib.rs":   "pub fn send(mac: &str) -> u8 { 0 }\n",
	})
	a := Ataque{Invariante: "nil_edge", Test: "x"}
	casos := []struct {
		nombre, leng string
		tarea        task.Task
		s            Superficie
		archivo      string
		correr       string
		compila      string
	}{
		{"go", "go", task.Task{ID: "T-001", TocarSolo: []string{"pkg/pkg.go"}}, Superficie{}, "pkg/loom_redteam_1_test.go", "go test -count=1 -run TestRedTeam ./pkg", ""},
		{"python", "python", task.Task{ID: "T-001", TocarSolo: []string{"app/m.py"}}, Superficie{}, "app/test_loom_redteam_1.py", "python3 -m pytest", ""},
		{"vitest", "typescript", task.Task{ID: "T-002", TocarSolo: []string{"web/src/parse.ts"}}, Superficie{Runner: "vitest"},
			"web/tests/loom_adversarial_t_002_1.test.ts", "npx --no-install vitest run tests/loom_adversarial_t_002_1.test.ts", "npx --no-install tsc -p tsconfig.loom-redteam-1.json"},
		{"jest", "javascript", task.Task{ID: "T-003", TocarSolo: []string{"api/src/x.js"}}, Superficie{Runner: "jest"},
			"api/tests/loom_adversarial_t_003_1.test.js", "npx --no-install jest tests/loom_adversarial_t_003_1.test.js", ""},
		{"rust", "rust", task.Task{ID: "T-004", TocarSolo: []string{"core/src/lib.rs"}}, Superficie{},
			"core/tests/loom_adversarial_t_004_1.rs", "cargo test --test loom_adversarial_t_004_1 --manifest-path core/Cargo.toml", "cargo test --test loom_adversarial_t_004_1 --manifest-path core/Cargo.toml --no-run"},
	}
	for _, c := range casos {
		if got := lenguajeDe(c.tarea); got != c.leng {
			t.Errorf("%s: lenguajeDe = %q", c.nombre, got)
		}
		r, falla := prepararRunner(wt, dirDe(c.tarea.TocarSolo), c.leng, c.tarea, c.s, 1, a, "")
		if falla != "" {
			t.Fatalf("%s: %s", c.nombre, falla)
		}
		if rel, _ := filepath.Rel(wt, r.archivo); filepath.ToSlash(rel) != c.archivo {
			t.Errorf("%s: archivo = %s", c.nombre, rel)
		}
		if got := strings.Join(r.correr, " "); !strings.HasPrefix(got, c.correr) {
			t.Errorf("%s: correr = %q", c.nombre, got)
		}
		if got := strings.Join(r.compilar, " "); !strings.HasPrefix(got, c.compila) || (c.compila == "") != (got == "") {
			t.Errorf("%s: compilar = %q", c.nombre, got)
		}
		for _, e := range r.extras {
			os.Remove(e)
		}
	}
	if runnerNode(filepath.Join(wt, "web")) != "vitest" || runnerNode(filepath.Join(wt, "api")) != "jest" || runnerNode(filepath.Join(wt, "pkg")) != "" {
		t.Error("runnerNode lee devDependencies y scripts")
	}
	if l := lenguajeDe(task.Task{TocarSolo: []string{"src/**"}, Expone: []string{"pub fn send(mac: &str) -> Result<(), Error>"}}); l != "rust" {
		t.Errorf("firma rust sin extensión = %q", l)
	}
	if l := lenguajeDe(task.Task{TocarSolo: []string{"src/**"}, Expone: []string{"export function parse(s: string): Item[]"}}); l != "typescript" {
		t.Errorf("firma ts sin extensión = %q", l)
	}
}

func TestClasificarTSyRust(t *testing.T) {
	casos := []struct {
		leng, salida string
		quiero       Clase
	}{
		{"typescript", "TypeError: Cannot read properties of null (reading 'length')\n ❯ parse src/parse.ts:3:12\n    at parse (/w/web/src/parse.ts:3:12)\n    at /w/web/tests/loom_adversarial_t_002_1.test.ts:5:3", Confirmado},
		{"typescript", "AssertionError: expected 1 to be 2\n    at /w/web/tests/loom_adversarial_t_002_1.test.ts:5:3", Afirmacion},
		{"typescript", "Error: Cannot find module '../src/nada'", FallaAtacante},
		{"javascript", "ReferenceError: describe is not defined", FallaAtacante},
		{"rust", "thread 'redteam_1' panicked at src/lib.rs:4:5:\ncalled `Option::unwrap()` on a `None` value\ntest result: FAILED", Confirmado},
		{"rust", "thread 'redteam_1' panicked at tests/loom_adversarial_t_004_1.rs:7:5:\nassertion failed\ntest result: FAILED", Afirmacion},
		{"rust", "error[E0425]: cannot find function `nope`\nerror: could not compile `core`", FallaAtacante},
		{"rust", "thread 'x' panicked at 'index out of bounds', core/src/buf.rs:12:9", Confirmado},
	}
	for _, c := range casos {
		if got, det := Clasificar(c.salida, c.leng, false); got != c.quiero {
			t.Errorf("%s %q = %s (%s), quiero %s", c.leng, c.salida[:30], got, det, c.quiero)
		}
	}
	if mal, _ := noCompila("typescript", "src/otro.ts(1,1): error TS2322: x", "loom_adversarial_t_002_1.test.ts"); mal {
		t.Error("errores previos del proyecto no descartan el ataque")
	}
	if mal, _ := noCompila("typescript", "tests/loom_adversarial_t_002_1.test.ts(3,7): error TS2345: x", "loom_adversarial_t_002_1.test.ts"); !mal {
		t.Error("un ataque que no tipa se descarta")
	}
}

func TestReabrirTSyRust(t *testing.T) {
	r := Resultado{Ataque: Ataque{Objetivo: "parse(s: string): number", Invariante: "nil_edge", Imports: []string{"import { it } from 'vitest'"}, Test: "it('redteam 1', () => { parse(null as unknown as string) })"}, Detalle: "TypeError · src/parse.ts:3"}
	ts := Reabrir(task.Task{ID: "T-002", ListoCuando: "npm test", TocarSolo: []string{"web/src/**"}}, r, Informe{Lenguaje: "typescript", Runner: "vitest", Dir: "web"})
	if !strings.Contains(ts.ListoCuando, "cd web && npx vitest run tests/loom_adversarial_t_002.test.ts") || ts.TocarSolo[1] != "web/tests/loom_adversarial_t_002.test.ts" || !strings.Contains(ts.Notas, "from 'vitest'") {
		t.Errorf("ts: %+v", ts)
	}
	rs := Reabrir(task.Task{ID: "T-004", ListoCuando: "cargo test", TocarSolo: []string{"core/src/**"}}, Resultado{Ataque: Ataque{Test: "#[test]\nfn redteam_1() {}"}}, Informe{Lenguaje: "rust", Dir: "core"})
	if !strings.Contains(rs.ListoCuando, "cargo test --test loom_adversarial_t_004 --manifest-path core/Cargo.toml") || rs.TocarSolo[1] != "core/tests/loom_adversarial_t_004.rs" {
		t.Errorf("rust: %+v", rs)
	}
}

// Rust de punta a punta con un cargo falso: compila, el código de la tarea
// entra en pánico dos veces y el ataque queda confirmado.
func TestAtacarRustConCargoFalso(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cargo falso en shell")
	}
	root := t.TempDir()
	escribir(t, root, map[string]string{"Cargo.toml": "[package]\nname = \"wol\"\n", "src/lib.rs": "pub fn send(mac: &str) -> u8 { mac.len() as u8 }\n"})
	for _, a := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", a, out)
		}
	}
	bin := t.TempDir()
	cargo := "#!/bin/sh\ncase \"$*\" in *--no-run*) exit 0;; esac\necho \"thread 'redteam_1' panicked at src/lib.rs:1:40:\"\necho 'test result: FAILED'\nexit 101\n"
	if err := os.WriteFile(filepath.Join(bin, "cargo"), []byte(cargo), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	tk := task.Task{ID: "T-001", Titulo: "wol", TocarSolo: []string{"src/**"}, Expone: []string{"pub fn send(mac: &str) -> u8"}}
	gen := generadorFijo(`{"attacks":[{"name":"vacío","invariant":"nil_edge","target":"pub fn send(mac: &str) -> u8","imports":[],"test":"#[test]\nfn redteam_1() { send(\"\"); }"}]}`)
	inf, err := Atacar(context.Background(), Opciones{Root: root, Tarea: tk, Gen: gen})
	if err != nil {
		t.Fatal(err)
	}
	if inf.Omitido != "" || inf.Lenguaje != "rust" || len(inf.Resultados) != 1 || inf.Resultados[0].Clase != Confirmado {
		t.Fatalf("informe = %+v", inf)
	}
	if _, err := os.Stat(filepath.Join(root, "tests")); err == nil {
		t.Error("el ataque no deja archivos en el repo")
	}
}
