package loop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/sealed"
)

// examinadorFalso cuenta invocaciones: lo que se verifica es si el bucle
// lo llamó o no, no lo que produce.
type examinadorFalso struct{ veces int }

func (e *examinadorFalso) Run(context.Context, string) (bool, error) {
	e.veces++
	return true, nil
}

type examinadorSinSuite struct{}

func (examinadorSinSuite) Run(context.Context, string) (bool, error) {
	return false, errors.New("parser del proyecto no disponible")
}

func TestEsqueletoRecuperaPruebaPropiaSiExaminadorFalla(t *testing.T) {
	root := repoConCommit(t)
	tk := tareaDePrueba()
	tk.TocarSolo = []string{"src/done.txt"}
	tk.ListoCuando = "test -f src/done.test.js"
	tk.ExamenEsqueleto = true
	tk.ExamenVisible = "src/done.test.js"
	ag := &agenteFalso{nombre: "falso", hacer: func(_ int, req Request) (string, int, error) {
		if !strings.Contains(strings.Join(req.AllowedGlobs, ","), tk.ExamenVisible) {
			t.Errorf("el agente no recuperó el alcance de la prueba: %v", req.AllowedGlobs)
		}
		escribir(t, req.RoomPath, tk.ExamenVisible, "prueba propia\n")
		return "", 0, nil
	}}
	o := optsDePrueba(t, root, ag, tk)
	o.Examinador = examinadorSinSuite{}
	resultado, err := Run(context.Background(), o)
	if err != nil || !resultado.Verde {
		t.Fatalf("resultado=%+v err=%v", resultado, err)
	}
	if _, err := os.Stat(filepath.Join(o.Room.Path, tk.ExamenVisible)); err != nil {
		t.Fatalf("la prueba propia fue revertida: %v", err)
	}
}

func agenteQueTermina(t *testing.T) *agenteFalso {
	t.Helper()
	ag := &agenteFalso{nombre: "falso"}
	ag.hacer = func(_ int, req Request) (string, int, error) {
		escribir(t, req.RoomPath, "src/done.txt", "ok")
		return "", 0, nil
	}
	return ag
}

// una suite sellada a mano manda: el examinador automático no corre y no
// la pisa.
func TestRunNoExaminaConSuiteSelladaAMano(t *testing.T) {
	root := repoConCommit(t)
	tk := tareaDePrueba()
	if err := sealed.Write(root, tk.ID, sealed.SuiteOculta{
		Content:        "def test_oculta():\n    assert True\n",
		Archivo:        "src/test_loom_hidden.py",
		Visible:        "def test_visible():\n    assert True\n",
		ArchivoVisible: "src/test_loom_visible.py",
	}); err != nil {
		t.Fatal(err)
	}

	o := optsDePrueba(t, root, agenteQueTermina(t), tk)
	exam := &examinadorFalso{}
	o.Examinador = exam

	res, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Verde {
		t.Fatalf("la tarea debió quedar verde: %+v", res)
	}
	if exam.veces != 0 {
		t.Errorf("el examinador corrió %d veces pese a la suite sellada a mano", exam.veces)
	}

	// la suite visible manual aterriza en el cuarto, en la ruta sellada
	visible := filepath.Join(o.Room.Path, "src", "test_loom_visible.py")
	data, err := os.ReadFile(visible)
	if err != nil {
		t.Fatalf("la suite visible manual no llegó al cuarto: %v", err)
	}
	if string(data) != "def test_visible():\n    assert True\n" {
		t.Errorf("contenido de la visible = %q", data)
	}

	// y queda commiteada, o revertFueraDeAlcance la borraría en el
	// primer intento
	if salida, err := gitRun(o.Room.Path, "log", "--oneline", "--", "src/test_loom_visible.py"); err != nil || salida == "" {
		t.Errorf("la suite visible manual no quedó commiteada: %q %v", salida, err)
	}

	// sellar a mano no toca la oculta: ship la lee igual que la automática
	s, err := sealed.Read(root, tk.ID)
	if err != nil {
		t.Fatalf("sealed.Read: %v", err)
	}
	if s.Archivo != "src/test_loom_hidden.py" {
		t.Errorf("la oculta cambió de ruta: %q", s.Archivo)
	}
}

// sin suite sellada el examinador automático corre como siempre.
func TestRunExaminaSinSuiteSellada(t *testing.T) {
	root := repoConCommit(t)
	o := optsDePrueba(t, root, agenteQueTermina(t), tareaDePrueba())
	exam := &examinadorFalso{}
	o.Examinador = exam

	if _, err := Run(context.Background(), o); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exam.veces != 1 {
		t.Errorf("el examinador debió correr una vez, corrió %d", exam.veces)
	}
}

// examinadorQueDegrada devuelve el motivo, como el real cuando la suite
// no compila o el modelo no responde JSON.
type examinadorQueDegrada struct{}

func (examinadorQueDegrada) Run(context.Context, string) (bool, error) {
	return false, errors.New("la suite visible del examinador no compila · expected declaration")
}

// una tarea que queda sin suite tiene que decir POR QUÉ. Antes moría con
// "no tiene suite que lo juzgue" y el motivo del examinador se tiraba: no
// había forma de saber si el modelo no respondió, si la suite no
// compilaba o si faltaban imports sin volver a correr la tarea entera.
func TestRunDicePorQueNoHuboSuite(t *testing.T) {
	root := repoConCommit(t)
	tk := tareaDePrueba()
	tk.ListoCuando = "echo '?   snake/internal/game [no test files]'"

	o := optsDePrueba(t, root, agenteQueTermina(t), tk)
	o.Examinador = examinadorQueDegrada{}

	res, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Verde {
		t.Fatal("salir con 0 sin ejecutar pruebas no es verde")
	}
	if !strings.Contains(res.UltimoError, "no tiene suite que lo juzgue") {
		t.Errorf("motivo sin el diagnóstico de siempre: %q", res.UltimoError)
	}
	if !strings.Contains(res.UltimoError, "no compila") {
		t.Errorf("el motivo no dice por qué falló el examinador: %q", res.UltimoError)
	}

	log := filepath.Join(RunsDir(root), tk.ID, "examinador.log")
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("sin examinador.log: %v", err)
	}
	if !strings.Contains(string(data), "no compila") {
		t.Errorf("examinador.log = %q", data)
	}
}
