package ship

import (
	"context"
	"testing"
	"time"

	"github.com/Pastranauwu/loom/internal/sealed"
)

// Quemar la suite al fallar dejaba el paso saltado en el siguiente ship
// (sin suite sellada se omite en silencio): la misma tarea que la
// compuerta acababa de frenar salía en un PR con solo repetir el comando.
func TestSuiteOcultaNoSeQuemaAlFallar(t *testing.T) {
	root := repoConCommit(t)
	tarea := taskTitulo("una tarea")
	s := sealed.SuiteOculta{Content: "no importa el contenido\n", Archivo: "oculta_test.txt"}
	if err := sealed.Write(root, tarea.ID, s); err != nil {
		t.Fatal(err)
	}

	_, detalle, ok := verificarSuiteOculta(context.Background(), root, root, tarea, "false", time.Minute)
	if ok {
		t.Fatalf("un examen que falla no puede aprobar la compuerta · detalle=%q", detalle)
	}
	if !sealed.Exists(root, tarea.ID) {
		t.Fatal("la suite se quemó al fallar: el siguiente ship saltaría el paso")
	}

	// y el reintento vuelve a examinar, no salta
	if _, _, ok := verificarSuiteOculta(context.Background(), root, root, tarea, "false", time.Minute); ok {
		t.Error("el reintento aprobó sin haber arreglado nada")
	}
}

func TestSuiteOcultaSeQuemaAlAprobar(t *testing.T) {
	root := repoConCommit(t)
	tarea := taskTitulo("una tarea")
	s := sealed.SuiteOculta{Content: "no importa el contenido\n", Archivo: "oculta_test.txt"}
	if err := sealed.Write(root, tarea.ID, s); err != nil {
		t.Fatal(err)
	}

	if _, _, ok := verificarSuiteOculta(context.Background(), root, root, tarea, "true", time.Minute); !ok {
		t.Fatal("el examen pasó y la compuerta lo rechazó")
	}
	if sealed.Exists(root, tarea.ID) {
		t.Error("el examen aprobado debe consumirse, no volver a correr")
	}
}

// Una suite oculta que no compila no juzga nada y el implementador no
// puede arreglarla: el motivo tiene que nombrar qué borrar, o la tarea
// queda frenada para siempre sin pista.
func TestSuiteOcultaQueNoCompilaLoDice(t *testing.T) {
	root := repoConCommit(t)
	tarea := taskTitulo("una tarea")
	s := sealed.SuiteOculta{Content: "contenido\n", Archivo: "cmd/calc/loom_hidden_test.go"}
	if err := sealed.Write(root, tarea.ID, s); err != nil {
		t.Fatal(err)
	}
	salida := "# calc/cmd/calc_test\ncmd/calc/loom_hidden_test.go:8:2: imported as main and not used\nFAIL\tcalc/cmd/calc [build failed]\n"
	if !noCompila(salida, s.Archivo) {
		t.Fatal("un fallo de compilación del examen tiene que reconocerse")
	}
	// un fallo de aserción normal no es lo mismo
	if noCompila("--- FAIL: TestX\nFAIL\tcalc/cmd/calc\t0.01s\n", s.Archivo) {
		t.Error("una prueba que falla no es una suite que no compila")
	}
}
