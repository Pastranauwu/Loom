package sketch

import (
	"strings"
	"testing"
)

func TestParsePreguntasTopaYValida(t *testing.T) {
	qs, err := ParsePreguntas("aquí van:\n```json\n{\"questions\":[{\"question\":\"a?\",\"why\":\"x\"},{\"question\":\"b?\"},{\"question\":\"c?\"},{\"question\":\"d?\"},{\"question\":\"e?\"},{\"question\":\"  \"}]}\n```")
	if err != nil || len(qs) != NumPreguntas || qs[0].Porque != "x" {
		t.Fatalf("qs = %+v, err = %v", qs, err)
	}
	if _, err := ParsePreguntas(`{"questions":[]}`); err == nil {
		t.Error("sin preguntas debe fallar")
	}
	if _, err := ParsePreguntas("no es json"); err == nil {
		t.Error("sin JSON debe fallar")
	}
}

func TestPromptSpecMarcaLasSinRespuesta(t *testing.T) {
	p := PromptSpec("cola de trabajos", []Respuesta{{"¿reintentos?", "3 veces"}, {"¿orden?", ""}}, "")
	for _, quiero := range []string{"cola de trabajos", "A: 3 veces", "Assumption:", "conservative"} {
		if !strings.Contains(p, quiero) {
			t.Errorf("falta %q:\n%s", quiero, p)
		}
	}
	if strings.Contains(PromptPreguntas("x", ""), "REPOSITORY") || !strings.Contains(PromptPreguntas("x", "Language: go."), "REPOSITORY") {
		t.Error("el contexto del repo solo entra si existe")
	}
}

func TestParseSpecArmaElSpec(t *testing.T) {
	s, err := ParseSpec(`{"feature":"cola","why":"procesar en segundo plano","requirements":["encola","reintenta 3 veces"," "],"rules":["Assumption: orden FIFO"],"acceptance":[{"criterion":"flujo completo","command":"go test ./..."},{"criterion":"sin comando","command":""}],"dont_touch":["vendor/**"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if s.Feature != "cola" || len(s.Requirements) != 2 || len(s.Reglas) != 1 || len(s.Acceptance) != 2 || s.Acceptance[0].Command != "go test ./..." || s.Constraints.NoTocar[0] != "vendor/**" {
		t.Errorf("spec = %+v", s)
	}
	if _, err := ParseSpec(`{"feature":"x","requirements":[]}`); err == nil {
		t.Error("sin requirements debe fallar")
	}
}
