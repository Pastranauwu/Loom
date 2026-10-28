package examiner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/task"
)

func TestBuildGoFileImports(t *testing.T) {
	visible := []string{
		"func TestLen(t *testing.T) {\n\tif !bytes.Equal(nil, nil) { t.Fatal(\"x\") }\n}",
	}
	src := buildGoFile("wol", "wakeup/internal/wol", []string{"bytes", "time", "github.com/x/y"}, visible)

	if !strings.Contains(src, `"bytes"`) {
		t.Error("falta import usado: bytes")
	}
	if strings.Contains(src, `"time"`) {
		t.Error("import no usado no debe aparecer: time")
	}
	if strings.Contains(src, "github.com/x/y") {
		t.Error("import externo no debe aparecer")
	}
	if !strings.Contains(src, `"wakeup/internal/wol"`) {
		t.Error("falta el paquete bajo prueba")
	}
	if err := validarSintaxis("go", src); err != nil {
		t.Errorf("el archivo armado no parsea: %v\n%s", err, src)
	}
}

func TestValidarSintaxisGoRechazaBasura(t *testing.T) {
	if validarSintaxis("go", "package x_test\n\nimport (\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) {\n\tnet.Foo(\n") == nil {
		t.Error("validarSintaxis debió rechazar código con paréntesis sin cerrar")
	}
}

func TestRunSaltaSinExpone(t *testing.T) {
	sellada, err := Run(nil, t.TempDir(), Options{
		Agent: stubAgent{},
		Task:  task.Task{ID: "T-001", Titulo: "init", TocarSolo: []string{"go.mod"}},
	})
	if err != nil || sellada {
		t.Errorf("tarea sin expone no debe examinarse: sellada=%v err=%v", sellada, err)
	}
}

type stubAgent struct{}

func (stubAgent) Name() string { return "stub" }
func (stubAgent) Run(context.Context, loop.Request) (loop.Result, error) {
	return loop.Result{}, nil
}

func TestParseRespTextImports(t *testing.T) {
	v, h, imp, err := parseRespText("```json\n{\"imports\":[\"net\"],\"visible\":[\"func TestA(t *testing.T){}\"],\"hidden\":[]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 1 || len(h) != 0 || len(imp) != 1 || imp[0] != "net" {
		t.Errorf("parse = v%v h%v imp%v", v, h, imp)
	}
}

func TestPaqueteReal(t *testing.T) {
	dir := t.TempDir()
	if p := paqueteReal(dir); p != "" {
		t.Errorf("directorio vacío = %q, quiero \"\"", p)
	}
	if p := paqueteReal(filepath.Join(dir, "nope")); p != "" {
		t.Errorf("directorio inexistente = %q", p)
	}

	// cmd/<algo> declara package main, no package <algo>: adivinarlo por
	// el nombre del directorio dejaba dos paquetes en la misma carpeta
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := paqueteReal(dir); p != "main" {
		t.Errorf("paqueteReal = %q, quiero main", p)
	}

	// los _test.go no cuentan: declaran pkg_test, no el paquete real
	otro := t.TempDir()
	if err := os.WriteFile(filepath.Join(otro, "x_test.go"), []byte("package wol_test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := paqueteReal(otro); p != "" {
		t.Errorf("solo con _test.go = %q, quiero \"\"", p)
	}
	if err := os.WriteFile(filepath.Join(otro, "wol.go"), []byte("package wol\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := paqueteReal(otro); p != "wol" {
		t.Errorf("paqueteReal = %q, quiero wol", p)
	}
}

// Sin paquete real en el directorio no se emite suite: escribirla
// garantizaba un rojo permanente que nadie podía arreglar.
func TestRunSaltaSinPaqueteReal(t *testing.T) {
	dir := t.TempDir()
	sellada, err := Run(context.Background(), dir, Options{
		Agent: agenteQueFalla{},
		Task:  task.Task{ID: "T-001", Expone: []string{"wol.Send(m string) error"}, TocarSolo: []string{"internal/wol/**"}},
		Root:  dir,
	})
	if err != nil || sellada {
		t.Errorf("sellada = %v, err = %v · debe degradar sin examen", sellada, err)
	}
}

// agenteQueFalla verifica de paso que ni siquiera se invoca al modelo
// cuando el examen es imposible.
type agenteQueFalla struct{}

func (agenteQueFalla) Name() string { return "falla" }
func (agenteQueFalla) Run(context.Context, loop.Request) (loop.Result, error) {
	panic("no se debe invocar al modelo si el examen es imposible")
}

func TestSoportado(t *testing.T) {
	// donde hay examinador ciego, la veda de pruebas tiene sentido
	for _, l := range []string{"go", "", "python", "pytest", "rust"} {
		if !Soportado(l) {
			t.Errorf("Soportado(%q) = false, quiero true", l)
		}
	}
	// donde no lo hay, prohibirle al implementador escribir pruebas deja
	// la tarea sin nadie que la haga
	for _, l := range []string{"node", "elixir"} {
		if Soportado(l) {
			t.Errorf("Soportado(%q) = true, quiero false", l)
		}
	}
	if !SoportadoEsqueleto("node") {
		t.Error("el esqueleto Node sí debe poder examinarse")
	}
}

func TestPaqueteDeExpone(t *testing.T) {
	casos := []struct{ in, quiero string }{
		{"numeros.Media(xs []float64) (float64, error)", "numeros"},
		{"  slug.Slugify(s string) string", "slug"},
		{"Slugify(s string) string", ""},
		{"src/export/ToCSV", ""},
		{"", ""},
	}
	for _, c := range casos {
		if got := paqueteDeExpone([]string{c.in}); got != c.quiero {
			t.Errorf("paqueteDeExpone(%q) = %q, quiero %q", c.in, got, c.quiero)
		}
	}
	// la primera firma calificada manda
	if got := paqueteDeExpone([]string{"Suma(a int) int", "stats.Media(xs []int) float64"}); got != "stats" {
		t.Errorf("con firma sin calificar primero = %q, quiero stats", got)
	}
}
