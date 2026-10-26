package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/sealed"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ui"
)

// repoConTarea deja un repo inicializado con una tarea T-001 que declara
// tocar_solo, que es de donde sale la ruta de la suite.
func repoConTarea(t *testing.T, manifiesto string) string {
	t.Helper()
	root := repoTemporal(t)
	if manifiesto != "" {
		if err := os.WriteFile(filepath.Join(root, manifiesto), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out = ui.New(io.Discard, false)
	if err := runInit(root, "", "", "", nil, true); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	tk := task.Task{
		Version:        task.Version,
		ID:             "T-001",
		Titulo:         "exportar",
		ListoCuando:    "pytest",
		TocarSolo:      []string{"src/export/**"},
		NoTocar:        []string{},
		LimiteIntentos: task.DefaultLimiteIntentos,
		LimiteLineas:   task.DefaultLimiteLineas,
	}
	if err := task.Save(config.TasksDir(root), tk); err != nil {
		t.Fatal(err)
	}
	return root
}

func escribirSuite(t *testing.T, root, rel, contenido string) string {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(contenido), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestTaskSealSellaSuiteManual(t *testing.T) {
	root := repoConTarea(t, "pyproject.toml")
	v := escribirSuite(t, root, "pruebas/visible.py", "def test_visible():\n    assert True\n")
	o := escribirSuite(t, root, "pruebas/oculta.py", "def test_oculta():\n    assert True\n")

	out = ui.New(io.Discard, false)
	if err := runTaskSeal(root, "T-001", v, o, false); err != nil {
		t.Fatalf("runTaskSeal: %v", err)
	}

	s, err := sealed.Read(root, "T-001")
	if err != nil {
		t.Fatalf("sealed.Read: %v", err)
	}
	// la oculta se guarda igual que la del examinador automático: ship
	// lee Content y Archivo y no pregunta de dónde salió.
	if !strings.Contains(s.Content, "def test_oculta()") {
		t.Errorf("la oculta no se selló: %q", s.Content)
	}
	if s.Archivo != "src/export/test_loom_hidden.py" {
		t.Errorf("archivo oculto = %q", s.Archivo)
	}
	if s.Hash == "" {
		t.Error("la oculta se selló sin hash")
	}
	if !strings.Contains(s.Visible, "def test_visible()") {
		t.Errorf("la visible no se guardó: %q", s.Visible)
	}
	if s.ArchivoVisible != "src/export/test_loom_visible.py" {
		t.Errorf("archivo visible = %q", s.ArchivoVisible)
	}
}

func TestTaskSealUsaElLenguajeDelRepo(t *testing.T) {
	root := repoConTarea(t, "go.mod")
	v := escribirSuite(t, root, "pruebas/visible_test.go", "package x\n")
	o := escribirSuite(t, root, "pruebas/oculta_test.go", "package x\n")

	out = ui.New(io.Discard, false)
	if err := runTaskSeal(root, "T-001", v, o, false); err != nil {
		t.Fatalf("runTaskSeal: %v", err)
	}
	s, err := sealed.Read(root, "T-001")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(s.Archivo, "loom_hidden_test.go") {
		t.Errorf("en un repo go la oculta debe ser *_test.go, es %q", s.Archivo)
	}
	if !strings.HasSuffix(s.ArchivoVisible, "loom_visible_test.go") {
		t.Errorf("en un repo go la visible debe ser *_test.go, es %q", s.ArchivoVisible)
	}
}

func TestTaskSealNoPisaSinForzar(t *testing.T) {
	root := repoConTarea(t, "pyproject.toml")
	v := escribirSuite(t, root, "pruebas/visible.py", "def test_v():\n    assert True\n")
	o := escribirSuite(t, root, "pruebas/oculta.py", "def test_o():\n    assert True\n")

	out = ui.New(io.Discard, false)
	if err := runTaskSeal(root, "T-001", v, o, false); err != nil {
		t.Fatalf("primer sellado: %v", err)
	}

	escribirSuite(t, root, "pruebas/oculta.py", "def test_o2():\n    assert True\n")
	err := runTaskSeal(root, "T-001", v, o, false)
	if err == nil {
		t.Fatal("sellar dos veces sin --forzar debió fallar")
	}
	if !strings.Contains(err.Error(), "--forzar") {
		t.Errorf("el error debe decir cómo seguir, dice: %v", err)
	}
	s, _ := sealed.Read(root, "T-001")
	if !strings.Contains(s.Content, "def test_o()") {
		t.Error("la suite previa se pisó pese al rechazo")
	}

	if err := runTaskSeal(root, "T-001", v, o, true); err != nil {
		t.Fatalf("con --forzar debió sellar: %v", err)
	}
	s, _ = sealed.Read(root, "T-001")
	if !strings.Contains(s.Content, "def test_o2()") {
		t.Errorf("--forzar no reemplazó la suite: %q", s.Content)
	}
}

func TestTaskSealArchivoInvalido(t *testing.T) {
	root := repoConTarea(t, "pyproject.toml")
	v := escribirSuite(t, root, "pruebas/visible.py", "def test_v():\n    assert True\n")
	vacio := escribirSuite(t, root, "pruebas/vacia.py", "   \n")

	out = ui.New(io.Discard, false)
	casos := []struct {
		nombre          string
		visible, oculta string
		esperaEnMensaje string
	}{
		{"oculta que no existe", v, "pruebas/nope.py", "no se pudo leer"},
		{"oculta vacía", v, vacio, "está vacío"},
		{"visible que no existe", "pruebas/nope.py", v, "no se pudo leer"},
	}
	for _, c := range casos {
		err := runTaskSeal(root, "T-001", c.visible, c.oculta, false)
		if err == nil {
			t.Errorf("%s: debió fallar", c.nombre)
			continue
		}
		if !strings.Contains(err.Error(), c.esperaEnMensaje) {
			t.Errorf("%s: error = %v, quiere que mencione %q", c.nombre, err, c.esperaEnMensaje)
		}
		if sealed.Exists(root, "T-001") {
			t.Errorf("%s: no debe quedar nada sellado", c.nombre)
		}
	}
}

func TestTaskSealTareaInexistente(t *testing.T) {
	root := repoConTarea(t, "pyproject.toml")
	v := escribirSuite(t, root, "pruebas/visible.py", "def test_v():\n    assert True\n")

	out = ui.New(io.Discard, false)
	err := runTaskSeal(root, "T-404", v, v, false)
	if err == nil || !strings.Contains(err.Error(), "no existe la tarea T-404") {
		t.Errorf("sellar una tarea inexistente = %v", err)
	}
}
