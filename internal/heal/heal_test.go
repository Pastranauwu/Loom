package heal

import (
	"strings"
	"testing"
)

func TestRecortarQuitaColorYConservaLaCola(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("linea\n")
	}
	b.WriteString("\x1b[31mpanic: boom\x1b[0m\n")
	r := Recortar(b.String())
	if strings.Contains(r, "\x1b") || !strings.HasSuffix(r, "panic: boom") || strings.Count(r, "\n") >= maxLineas {
		t.Errorf("recorte inválido: %d líneas, cola %q", strings.Count(r, "\n"), r[len(r)-20:])
	}
}

const diagnosticoOK = "```json\n" + `{"title":"rechaza id vacío","cause":"Get no valida el id","invariant":"Get nunca recibe un id vacío","criterion":"un id vacío se rechaza","command":"go test ./internal/store -run TestGetIDVacio","test_file":"internal/store/vacio_test.go","only_touch":["internal/store/store.go"],"how":"Get(\"\") -> error"}` + "\n```"

func TestParseYTarea(t *testing.T) {
	d, err := Parse(diagnosticoOK)
	if err != nil {
		t.Fatal(err)
	}
	tk := Tarea(d, "T-007", "panic: boom")
	if tk.ID != "T-007" || tk.ListoCuando != d.Comando || tk.Peso != "media" {
		t.Errorf("tarea = %+v", tk)
	}
	if len(tk.TocarSolo) != 2 || tk.TocarSolo[1] != "internal/store/vacio_test.go" {
		t.Errorf("el archivo de prueba debe entrar al alcance: %v", tk.TocarSolo)
	}
	if !strings.Contains(tk.Notas, "Cases: Get(") || !strings.Contains(tk.Notas, "panic: boom") {
		t.Errorf("notas:\n%s", tk.Notas)
	}
	if errs := tk.Validate(); len(errs) > 0 {
		t.Errorf("contrato inválido: %v", errs)
	}
}

func TestParseRechazaDiagnosticosInaccionables(t *testing.T) {
	for _, malo := range []string{
		`{"invariant":"x","command":"","only_touch":["a.go"]}`,
		`{"invariant":"","command":"go test","only_touch":["a.go"]}`,
		`{"invariant":"x","command":"go test"}`,
		"sin json",
	} {
		if _, err := Parse(malo); err == nil {
			t.Errorf("debió rechazar %q", malo)
		}
	}
}
