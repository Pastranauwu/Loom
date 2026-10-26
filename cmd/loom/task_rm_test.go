package main

import (
	"slices"
	"testing"

	"github.com/Pastranauwu/loom/internal/task"
)

func TestQuitarDependenciaLiberaALasQueEsperaban(t *testing.T) {
	dir := t.TempDir()
	nueva := func(id string, deps ...string) {
		tk := task.Task{Version: task.Version, ID: id, Titulo: "tarea " + id, ListoCuando: "true", TocarSolo: []string{id + ".go"}, DependeDe: deps, LimiteIntentos: 3, LimiteLineas: 200}
		if err := task.Save(dir, tk); err != nil {
			t.Fatal(err)
		}
	}
	nueva("T-001")
	nueva("T-002", "T-001", "T-009")
	nueva("T-003", "T-009")

	liberadas, err := quitarDependencia(dir, "T-009")
	if err != nil || !slices.Equal(liberadas, []string{"T-002", "T-003"}) {
		t.Fatalf("liberadas = %v err = %v", liberadas, err)
	}
	t2, _ := task.Load(dir, "T-002")
	t3, _ := task.Load(dir, "T-003")
	if !slices.Equal(t2.DependeDe, []string{"T-001"}) || len(t3.DependeDe) != 0 {
		t.Errorf("depende_de: T-002 = %v, T-003 = %v", t2.DependeDe, t3.DependeDe)
	}
}

func TestPruebaVisibleDeTareaEnMonorepo(t *testing.T) {
	alcance := []string{"backend/calc/suma.py", "backend/tests/test_suma.py"}
	for cmd, quiero := range map[string]string{
		"cd backend && python3 -m pytest tests/test_suma.py": "backend/tests/test_suma.py",
		"python3 -m pytest backend/tests/test_suma.py":       "backend/tests/test_suma.py",
		"npm --prefix backend test -- tests/test_suma.py":    "backend/tests/test_suma.py",
		"cd frontend && npx vitest run tests/test_suma.py":   "",
		"cd backend && python3 -m pytest tests/test_otra.py": "",
	} {
		if got := pruebaVisibleDeTarea(alcance, cmd); got != quiero {
			t.Errorf("%q → %q, quiero %q", cmd, got, quiero)
		}
	}
}
