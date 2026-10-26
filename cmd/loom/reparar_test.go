package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/ship"
	"github.com/Pastranauwu/loom/internal/task"
)

// T-037 cambió el intro de la pantalla como pedía
// su contrato y una prueba vieja de esa pantalla, que nadie tenía en su
// alcance, falló al integrar.
func TestRepararReabreLaTareaQueTocaElCodigoDeLaPruebaFallida(t *testing.T) {
	tareas := []task.Task{
		{ID: "T-036", TocarSolo: []string{"src/games/arregla-el-beat/tutorial.ts", "tests/games/arregla-el-beat/tutorial-escucha.test.ts"}},
		{ID: "T-037", Peso: "liviana", ListoCuando: "npx tsc --noEmit && (npx vitest run tests/screens/tutoriales-teoria.test.ts)",
			TocarSolo: []string{"src/screens/tutoriales.ts", "tests/screens/tutoriales-teoria.test.ts"}, Notas: "contrato"},
	}
	f := ship.Fallo{Paso: "integradas", Comando: "npx vitest run", Pruebas: []string{"tests/screens/tutoriales.test.ts"},
		Salida: "FAIL  tests/screens/tutoriales.test.ts > muestra intro · Expected: \"Cada tutorial dura\""}

	if ids := responsables(tareas, f.Pruebas); !slices.Equal(ids, []string{"T-037"}) {
		t.Fatalf("responsables = %v, quiero [T-037]", ids)
	}
	if ids := responsables(tareas, []string{"tests/integracion.test.ts"}); len(ids) != 0 {
		t.Errorf("una prueba que no se llama como ningún módulo no culpa a nadie: %v", ids)
	}

	r := reabierta(tareas[1], f)
	if !strings.HasSuffix(r.ListoCuando, "&& (npx vitest run tests/screens/tutoriales.test.ts)") {
		t.Errorf("listo_cuando debe correr solo la prueba que falló, no la suite: %q", r.ListoCuando)
	}
	if !slices.Contains(r.TocarSolo, "tests/screens/tutoriales.test.ts") {
		t.Errorf("la prueba vieja debe entrar al alcance con su ruta exacta: %v", r.TocarSolo)
	}
	if r.Peso != "media" {
		t.Errorf("peso = %q: decidir entre código y prueba vieja no es del liviano", r.Peso)
	}
	for _, q := range []string{"REPARACIÓN", "Obsoleto:", "Expected: \"Cada tutorial dura\""} {
		if !strings.Contains(r.Notas, q) {
			t.Errorf("falta %q en las notas", q)
		}
	}
	// reparar dos veces no encadena el mismo comando
	if otra := reabierta(r, f); strings.Count(otra.ListoCuando, "tutoriales.test.ts") != 1 {
		t.Errorf("listo_cuando repetido: %q", otra.ListoCuando)
	}
	// un comando que ya nombra su prueba se usa tal cual
	g := ship.Fallo{Paso: "aceptación", Comando: "npx vitest run tests/integracion-escucha.test.ts"}
	if r := reabierta(tareas[0], g); !strings.HasSuffix(r.ListoCuando, "&& (npx vitest run tests/integracion-escucha.test.ts)") {
		t.Errorf("listo_cuando = %q", r.ListoCuando)
	}
}

// Una prueba de integración no se llama como ningún módulo: responde la
// tarea que la escribió, y lo que se vuelve a correr es el id que entiende
// su runner, no una ruta de archivo.
func TestRepararReabreALaAutoraDeUnaPruebaDeIntegracion(t *testing.T) {
	tareas := []task.Task{
		{ID: "T-001", TocarSolo: []string{"apps/docs/views.py"}},
		{ID: "T-002", ListoCuando: "python manage.py test apps.docs.tests.test_flujo", TocarSolo: []string{"apps/docs/tests/test_flujo.py"}},
	}
	f := ship.Fallo{Paso: "integradas", Comando: "python manage.py test", Salida: "AssertionError: 404 != 200",
		Pruebas:    []string{"apps/docs/tests/test_flujo.py"},
		Nombres:    []string{"test_pantalla (apps.docs.tests.test_flujo.PantallaTest.test_pantalla)"},
		Selectores: []string{"apps.docs.tests.test_flujo.PantallaTest.test_pantalla"}}

	if ids := responsables(tareas, f.Pruebas); !slices.Equal(ids, []string{"T-002"}) {
		t.Fatalf("responsables = %v, quiero [T-002]", ids)
	}
	r := reabierta(tareas[1], f)
	if !strings.HasSuffix(r.ListoCuando, "&& (python manage.py test apps.docs.tests.test_flujo.PantallaTest.test_pantalla)") {
		t.Errorf("listo_cuando = %q", r.ListoCuando)
	}
	if !strings.Contains(r.Notas, "Pruebas que fallaron: test_pantalla (") {
		t.Errorf("las notas no nombran la prueba: %q", r.Notas)
	}
}
