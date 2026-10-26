package redteam

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/task"
)

const goPanic = `--- FAIL: TestRedTeam1 (0.00s)
panic: runtime error: invalid memory address or nil pointer dereference [recovered]
goroutine 7 [running]:
testing.tRunner.func1.2({0x1, 0x2})
	/usr/lib/go/src/testing/testing.go:1631 +0x24a
example.com/m/pkg.Boom(...)
	/tmp/wt/pkg/pkg.go:4
example.com/m/pkg.TestRedTeam1(0x0)
	/tmp/wt/pkg/loom_redteam_1_test.go:9 +0x18
FAIL	example.com/m/pkg	0.004s`

func TestClasificarGo(t *testing.T) {
	casos := []struct {
		nombre string
		salida string
		quiero Clase
	}{
		{"panic en el código de la tarea", goPanic, Confirmado},
		{"el test no compila", "# example.com/m/pkg [example.com/m/pkg.test]\n./loom_redteam_1_test.go:9:2: undefined: x\nFAIL\texample.com/m/pkg [build failed]", FallaAtacante},
		{"panic en el propio test", "panic: boom\ngoroutine 7 [running]:\nexample.com/m/pkg.TestRedTeam1(0x0)\n\t/tmp/wt/pkg/loom_redteam_1_test.go:9 +0x18\nFAIL", FallaAtacante},
		{"aserción", "--- FAIL: TestRedTeam3 (0.00s)\n    loom_redteam_3_test.go:10: wrong\nFAIL", Afirmacion},
		{"carrera en el código", "WARNING: DATA RACE\nWrite at 0x1 by goroutine 8:\n  example.com/m/pkg.Inc()\n      /tmp/wt/pkg/pkg.go:7 +0x10\n--- FAIL: TestRedTeam2", Confirmado},
		{"se colgó", "panic: test timed out after 30s\ngoroutine 1 [running]:\ntesting.tRunner\n\t/usr/lib/go/src/testing/testing.go:1 +0x1", Afirmacion},
		{"raro", "algo inesperado", FallaAtacante},
	}
	for _, c := range casos {
		if got, det := Clasificar(c.salida, "go", false); got != c.quiero {
			t.Errorf("%s: %s (%s), quiero %s", c.nombre, got, det, c.quiero)
		}
	}
	if got, _ := Clasificar("ok", "go", true); got != Resistio {
		t.Error("una prueba verde resistió")
	}
}

func TestClasificarPython(t *testing.T) {
	if c, _ := Clasificar("pkg/mod.py:12: ZeroDivisionError\n", "python", false); c != Confirmado {
		t.Errorf("excepción en código de la tarea: %s", c)
	}
	if c, _ := Clasificar("E   NameError: name 'x' is not defined\n", "python", false); c != FallaAtacante {
		t.Errorf("NameError es del test: %s", c)
	}
	if c, _ := Clasificar("pkg/mod.py:3: AssertionError\n", "python", false); c != Afirmacion {
		t.Errorf("aserción: %s", c)
	}
}

func tareaDePrueba() task.Task {
	return task.Task{ID: "T-001", Titulo: "boom", ListoCuando: "go test ./pkg", TocarSolo: []string{"pkg/**"}, Expone: []string{"pkg.Boom(p *int) int"}}
}

func superficie() Superficie {
	return Superficie{Requisitos: map[string]string{"R-1": "never crash"}, Reglas: []string{"no panics"}, Aceptacion: []string{"flow"}}
}

func TestParseSoloAtacaLoQueExisteDeVerdad(t *testing.T) {
	json := `{"attacks":[
	 {"name":"ok","invariant":"nil_edge","target":"pkg.Boom(p *int) int","test":"func TestRedTeam1(t *testing.T) { Boom(nil) }"},
	 {"name":"inventado","invariant":"spec:R-9","target":"pkg.Boom(p *int) int","test":"func TestRedTeam2(t *testing.T) { Boom(nil) }"},
	 {"name":"otra firma","invariant":"rule:1","target":"pkg.Otra() int","test":"func TestRedTeam3(t *testing.T) { Otra() }"},
	 {"name":"no llama","invariant":"boundary","target":"pkg.Boom(p *int) int","test":"func TestRedTeam4(t *testing.T) {}"},
	 {"name":"sin test","invariant":"boundary","target":"pkg.Boom(p *int) int","test":" "},
	 {"name":"regla","invariant":"rule:1","target":"pkg.Boom(p *int) int","test":"func TestRedTeam5(t *testing.T) { Boom(nil) }"}]}`
	validos, desc, err := Parse(json, tareaDePrueba(), superficie())
	if err != nil || len(validos) != 2 || len(desc) != 4 {
		t.Fatalf("validos=%d descartados=%v err=%v", len(validos), desc, err)
	}
	for _, quiero := range []string{"spec:R-9", "not an exposed", "never calls", "empty test"} {
		if !strings.Contains(strings.Join(desc, "\n"), quiero) {
			t.Errorf("falta el motivo %q:\n%v", quiero, desc)
		}
	}
}

func TestArmarGoSoloConservaLosImportsQueUsa(t *testing.T) {
	src := ArmarGo("pkg", []string{"os", "strings", "example.com/m/v2/util"}, "func TestRedTeam1(t *testing.T) { _ = os.Args }")
	if !strings.Contains(src, `"os"`) || strings.Contains(src, `"strings"`) || strings.Contains(src, "util") || !strings.HasPrefix(src, "package pkg") {
		t.Errorf("src:\n%s", src)
	}
}

type generadorFijo string

func (g generadorFijo) Generar(context.Context, string) (string, error) { return string(g), nil }

func repoGo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(root, "pkg", "pkg.go"), []byte("package pkg\n\nfunc Boom(p *int) int {\n\treturn *p\n}\n"), 0o644)
	for _, a := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", a, out)
		}
	}
	return root
}

func TestAtacarDeVerdadEnUnWorktreeEfimero(t *testing.T) {
	root := repoGo(t)
	respuesta := `{"attacks":[
	 {"name":"nil","invariant":"nil_edge","target":"pkg.Boom(p *int) int","test":"func TestRedTeam1(t *testing.T) { Boom(nil) }"},
	 {"name":"rota","invariant":"boundary","target":"pkg.Boom(p *int) int","test":"func TestRedTeam2(t *testing.T) { Boom(variableQueNoExiste) }"},
	 {"name":"aserción","invariant":"rule:1","target":"pkg.Boom(p *int) int","test":"func TestRedTeam3(t *testing.T) { v := new(int); if Boom(v) != 99 { t.Fatal(\"x\") } }"},
	 {"name":"resiste","invariant":"boundary","target":"pkg.Boom(p *int) int","test":"func TestRedTeam4(t *testing.T) { v := new(int); *v = 2; if Boom(v) != 2 { t.Fatal(\"x\") } }"}]}`
	keep := t.TempDir()
	inf, err := Atacar(context.Background(), Opciones{Root: root, Tarea: tareaDePrueba(), Superficie: superficie(), Gen: generadorFijo(respuesta), Conservar: keep})
	if err != nil || inf.Omitido != "" {
		t.Fatalf("informe: %+v err=%v", inf, err)
	}
	clases := map[string]Clase{}
	for _, r := range inf.Resultados {
		clases[r.Ataque.Nombre] = r.Clase
	}
	if clases["nil"] != Confirmado || clases["rota"] != FallaAtacante || clases["aserción"] != Afirmacion || clases["resiste"] != Resistio {
		t.Fatalf("clases = %v", clases)
	}
	if len(inf.Confirmados()) != 1 {
		t.Error("solo el crash confirmado bloquea")
	}
	if es, _ := os.ReadDir(filepath.Join(keep, "T-001")); len(es) != 2 {
		t.Errorf("se guardan los tests que fallaron (crash y aserción), no los demás: %d", len(es))
	}
	// el repo y sus worktrees quedan como estaban
	if out, _ := exec.Command("git", "-C", root, "status", "--porcelain").Output(); strings.TrimSpace(string(out)) != "" {
		t.Errorf("el ataque ensució el repo:\n%s", out)
	}
	if out, _ := exec.Command("git", "-C", root, "worktree", "list").Output(); strings.Count(string(out), "\n") != 1 {
		t.Errorf("quedó un worktree colgado:\n%s", out)
	}
}

func TestReabrirMeteElTestEnElContrato(t *testing.T) {
	r := Resultado{Ataque: Ataque{Objetivo: "pkg.Boom(p *int) int", Invariante: "nil_edge", Test: "func TestRedTeam1(t *testing.T) { Boom(nil) }"}, Detalle: "panic · pkg.go:4"}
	n := Reabrir(tareaDePrueba(), r, "go")
	if !strings.Contains(n.ListoCuando, "go test ./pkg -run '^TestRedTeam1$'") || n.Peso != "media" {
		t.Errorf("contrato: %+v", n)
	}
	if len(n.TocarSolo) != 2 || !strings.HasSuffix(n.TocarSolo[1], "_test.go") || !strings.Contains(n.Notas, "RED TEAM") || !strings.Contains(n.Notas, "Boom(nil)") {
		t.Errorf("alcance/notas: %v\n%s", n.TocarSolo, n.Notas)
	}
}
