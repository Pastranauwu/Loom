package examiner

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/task"
)

func TestLenguajeExamen(t *testing.T) {
	casos := map[string]string{
		"go":     "go",
		"":       "go", // repo sin manifiesto: se asume go, como antes
		"python": "python",
		"pytest": "python",
		"PYTHON": "python",
		"rust":   "rust",
		"node":   "typescript",
	}
	for entrada, quiere := range casos {
		if got := lenguajeExamen(entrada); got != quiere {
			t.Errorf("lenguajeExamen(%q) = %q, quiere %q", entrada, got, quiere)
		}
	}
}

func TestRutasSuitePorLenguaje(t *testing.T) {
	tocarSolo := []string{"src/export/**"}

	visible, oculta := RutasSuite(task.Task{TocarSolo: tocarSolo}, "python")
	if visible != "src/export/test_loom_visible.py" {
		t.Errorf("visible python = %q", visible)
	}
	if oculta != "src/export/test_loom_hidden.py" {
		t.Errorf("oculta python = %q", oculta)
	}
	// pytest descubre por el prefijo test_, sin configuración extra
	if !strings.HasPrefix("test_loom_visible.py", "test_") {
		t.Error("el nombre de python debe empezar con test_")
	}

	visible, oculta = RutasSuite(task.Task{TocarSolo: tocarSolo}, "go")
	if visible != "src/export/"+VisibleFileName {
		t.Errorf("visible go = %q", visible)
	}
	if oculta != "src/export/"+HiddenFileName {
		t.Errorf("oculta go = %q", oculta)
	}
}

func TestBuildPyFileImports(t *testing.T) {
	src := buildPyFile(
		[]string{"json", "from exportador import a_csv", "json", ""},
		[]string{"def test_vacio():\n    assert a_csv([]) == \"\""},
	)
	if !strings.Contains(src, "import json\n") {
		t.Errorf("falta el import simple normalizado:\n%s", src)
	}
	if !strings.Contains(src, "from exportador import a_csv\n") {
		t.Errorf("la línea from debe ir tal cual:\n%s", src)
	}
	if strings.Count(src, "import json") != 1 {
		t.Errorf("import duplicado:\n%s", src)
	}
	if !strings.Contains(src, "def test_vacio():") {
		t.Errorf("falta el cuerpo del test:\n%s", src)
	}
	if strings.Contains(src, "package ") {
		t.Errorf("python no lleva declaración de paquete:\n%s", src)
	}
}

func TestValidarSintaxisPython(t *testing.T) {
	sinPython3(t)

	bueno := buildPyFile([]string{"json"}, []string{"def test_ok():\n    assert json.dumps([]) == \"[]\""})
	if err := validarSintaxis("python", bueno); err != nil {
		t.Errorf("suite válida rechazada: %v\n%s", err, bueno)
	}

	malo := "def test_roto(:\n    assert True\n"
	if validarSintaxis("python", malo) == nil {
		t.Error("validarSintaxis debió rechazar python con paréntesis roto")
	}
}

// sin python3 en el PATH el examinador degrada: deja pasar la suite en vez
// de frenar al implementador.
func TestValidarSintaxisPythonSinInterpreteDegrada(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := validarSintaxis("python", "def test_roto(:\n"); err != nil {
		t.Errorf("sin python3 debe degradar, devolvió: %v", err)
	}
}

// el validador por defecto es el de go.
func TestValidarSintaxisLenguajeSinValidador(t *testing.T) {
	if err := validarSintaxis("", "fn main() {}"); err == nil {
		t.Error("el default valida como go y debió rechazar rust")
	}
}

func TestBuildPromptDiceElLenguaje(t *testing.T) {
	tk := task.Task{ID: "T-001", Titulo: "exportar", Expone: []string{"a_csv(rows) -> str"}}

	py := buildPrompt(tk, "export", "python")
	if !strings.Contains(py, "Test language: python") {
		t.Errorf("el prompt de python no declara el lenguaje:\n%s", py)
	}
	if !strings.Contains(py, "pytest") {
		t.Errorf("el prompt de python no menciona pytest:\n%s", py)
	}
	for _, deGo := range []string{"Go stdlib", "Go package", "*testing.T"} {
		if strings.Contains(py, deGo) {
			t.Errorf("el prompt de python arrastra %q:\n%s", deGo, py)
		}
	}

	golang := buildPrompt(tk, "export", "go")
	if !strings.Contains(golang, "Test language: go") {
		t.Errorf("el prompt de go no declara el lenguaje:\n%s", golang)
	}
	if !strings.Contains(golang, "Go package of the tests: export_test") {
		t.Errorf("el prompt de go perdió el package:\n%s", golang)
	}
	if !strings.Contains(golang, "Go stdlib") {
		t.Errorf("el prompt de go perdió sus reglas:\n%s", golang)
	}
}

// un stack sin examinador salta el examen aunque la tarea declare expone;
// rust sin Cargo.toml en el cuarto, también.
func TestRunSaltaLenguajeSinExaminador(t *testing.T) {
	for _, lenguaje := range []string{"rust", "node", "elixir"} {
		sellada, err := Run(nil, t.TempDir(), Options{
			Agent:    stubAgent{},
			Lenguaje: lenguaje,
			Task: task.Task{
				ID: "T-001", Titulo: "wol", TocarSolo: []string{"src/**"},
				Expone: []string{"fn send(mac: &str)"},
			},
		})
		if err != nil || sellada {
			t.Errorf("%s no debe examinarse: sellada=%v err=%v", lenguaje, sellada, err)
		}
	}
}

func sinPython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("sin python3 en el PATH")
	}
}
