package spec

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/task"
)

// Una aceptación que nombra una prueba inexistente solo puede fallar, y
// se sabe antes de pagar una sola tarea.
func TestAceptacionSinPrueba(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("sin git: %s", out)
	}
	for _, f := range []string{"apps/docs/tests/test_viejo.py", "web/src/a.test.ts"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tareas := []task.Task{{ID: "T-001", TocarSolo: []string{"apps/docs/tests/test_nuevo.py", "web/src/*.test.ts"}}}
	avisos := func(cmds ...string) []Issue {
		var s Spec
		for _, c := range cmds {
			s.Acceptance = append(s.Acceptance, Acceptance{Criterion: "x", Command: c})
		}
		return AceptacionSinPrueba(root, s, tareas)
	}

	// lo que existe o alguna tarea escribe, y lo que no nombra una prueba
	if a := avisos(
		"python manage.py test apps.docs.tests.test_viejo",
		"python manage.py test apps.docs.tests.test_nuevo.Clase.test_caso --noinput",
		"npx vitest run web/src/a.test.ts web/src/b.test.ts",
		"python manage.py shell -c \"import apps.docs.views\" && go test ./... && pytest",
	); len(a) != 0 {
		t.Fatalf("avisos de más: %+v", a)
	}
	// el prefijo equivocado: existe, pero en otra ruta
	a := avisos("python manage.py test docs.tests.test_nuevo")
	if len(a) != 1 || a[0].Level != "warning" || !strings.Contains(a[0].Message, "apps/docs/tests/test_nuevo.py") {
		t.Fatalf("quiero el aviso con la ruta real: %+v", a)
	}
	// nadie la escribe
	a = avisos("pytest tests/test_fantasma.py::test_x")
	if len(a) != 1 || !strings.Contains(a[0].Message, "ninguna tarea escribe") {
		t.Fatalf("quiero el aviso de prueba sin autor: %+v", a)
	}
}
