package loop

import (
	"strings"
	"testing"
)

func TestResumenFalloComandoSilencioso(t *testing.T) {
	code := 1
	// el caso real: `grep -q` no imprime nada, y el agente recibía
	// "sin salida" como único dato del intento anterior
	cmd := "go run . --help 2>&1 | grep -q -- --mac"
	got := resumenFallo(cmd, &code, "")

	for _, quiero := range []string{"code 1", cmd, "silences its output"} {
		if !strings.Contains(got, quiero) {
			t.Errorf("resumen %q no menciona %q", got, quiero)
		}
	}
	if strings.Contains(got, "sin salida") {
		t.Errorf("resumen %q sigue sin decir nada útil", got)
	}
}

func TestResumenFalloSinCodigo(t *testing.T) {
	got := resumenFallo("make test", nil, "")
	if !strings.Contains(got, "make test") || strings.Contains(got, "código") {
		t.Errorf("resumen = %q", got)
	}
}

func TestResumenFalloConSalida(t *testing.T) {
	code := 1
	salida := "compilando\n\nFAIL wol_test.go:12: quiero 6 bytes\nFAIL\texit status 1\n"
	got := resumenFallo("go test ./...", &code, salida)

	// manda la salida real, y con contexto: la última línea sola no
	// alcanza para arreglar un test
	if !strings.Contains(got, "wol_test.go:12") {
		t.Errorf("resumen %q perdió la línea que importa", got)
	}
	if strings.Contains(got, "go test ./...") {
		t.Errorf("con salida real no hace falta repetir el comando · %q", got)
	}
	// las líneas vacías no gastan cupo
	if strings.Contains(got, "\n\n") {
		t.Errorf("resumen %q conserva líneas vacías", got)
	}
}

func TestUltimasLineasAcota(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("linea de relleno bastante larga para pasar el tope\n")
	}
	got := ultimasLineas(b.String(), 6, 200)
	if tope := 200 + len("…"); len(got) > tope {
		t.Errorf("largo = %d, quiero <= %d", len(got), tope)
	}
	if !strings.HasPrefix(got, "…") {
		t.Errorf("un recorte debe avisarse · %q", got)
	}
	if ultimasLineas("   \n\n  ", 6, 200) != "" {
		t.Error("solo espacios en blanco es nada")
	}
}

// jest termina con marcos y el conteo: lo que sirve es Expected/Received
func TestResumenFalloTomaLineasDeFalloSinMarcos(t *testing.T) {
	salida := `FAIL src/suma.test.ts
  ● suma › suma dos números

    expect(received).toBe(expected)

    Expected: 4
    Received: 5

      at Object.<anonymous> (src/suma.test.ts:4:22)
      at Promise.then.completed (node_modules/jest-circus/build/utils.js:298:28)
      at new Promise (<anonymous>)

Tests:       1 failed, 1 total
Time:        0.5 s`
	got := resumenFallo("npx jest", nil, salida)
	for _, quiero := range []string{"Expected: 4", "Received: 5"} {
		if !strings.Contains(got, quiero) {
			t.Errorf("falta %q en %q", quiero, got)
		}
	}
	if strings.Contains(got, "at Object") {
		t.Errorf("se coló un marco del stack: %q", got)
	}
}

// en go la línea útil no dice "error": viene de t.Errorf con archivo:línea
func TestResumenFalloGoConservaElMensaje(t *testing.T) {
	salida := "--- FAIL: TestSuma (0.00s)\n    suma_test.go:8: Suma(2, 2) = 5, quiero 4\nFAIL\nFAIL\tej/suma\t0.01s"
	if got := resumenFallo("go test ./...", nil, salida); !strings.Contains(got, "quiero 4") {
		t.Errorf("perdió el mensaje de la prueba: %q", got)
	}
}
