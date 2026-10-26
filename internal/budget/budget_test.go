package budget

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContadorGastaYSabeCuantoQueda(t *testing.T) {
	c := Nuevo(100)
	if !c.Gastar(30) {
		t.Fatal("gastar 30 con tope 100 debía caber")
	}
	if c.Usado() != 30 || c.Restante() != 70 {
		t.Errorf("usado=%d restante=%d, quiero 30/70", c.Usado(), c.Restante())
	}
	if !c.Gastar(70) {
		t.Fatal("gastar 70 con 70 libres debía caber exacto")
	}
	if !c.Agotado() {
		t.Errorf("en el tope exacto ya no queda margen, Agotado=%v", c.Agotado())
	}
	if c.Gastar(1) {
		t.Fatal("gastar 1 sin margen debía rechazarse")
	}
	if c.Usado() != 100 || !c.Agotado() {
		t.Errorf("usado=%d Agotado=%v, quiero 100/true", c.Usado(), c.Agotado())
	}
}

func TestContadorSinTopeNoRechaza(t *testing.T) {
	c := Nuevo(0)
	if !c.Gastar(1 << 20) {
		t.Fatal("sin tope nada se rechaza")
	}
	if c.Limite() != 0 {
		t.Errorf("Limite = %d, quiero 0", c.Limite())
	}
	// sin tope nunca está agotado: Agotado() true con presupuesto 0
	// cortaba todas las tareas con "presupuesto agotado" apenas arrancaba
	if c.Agotado() {
		t.Fatal("sin tope no puede estar agotado")
	}
	if c.Restante() != 0 {
		t.Errorf("Restante = %d, quiero 0 (no hay tope que medir)", c.Restante())
	}
}

func TestAgotadoConTope(t *testing.T) {
	c := Nuevo(10)
	if c.Agotado() {
		t.Fatal("recién creado no puede estar agotado")
	}
	if !c.Gastar(10) {
		t.Fatal("gastar 10 de 10 debía caber")
	}
	if !c.Agotado() {
		t.Fatal("en el tope exacto ya está agotado")
	}
}

func TestGastoEnDiscoSumaIntentos(t *testing.T) {
	root := t.TempDir()
	runs := filepath.Join(root, ".loom", "runs")
	if err := os.MkdirAll(filepath.Join(runs, "T-001"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(runs, "T-100001"), 0o755); err != nil {
		t.Fatal(err)
	}
	// attempts.jsonl de una tarea raíz y de una subtarea, como deja la
	// recursión real
	escribe := func(id, entrada, salida string) {
		data := `{"intento":1,"salida_codigo":0,"tokens":{"entrada":` + entrada + `,"salida":` + salida + `}}` + "\n"
		if err := os.WriteFile(filepath.Join(runs, id, "attempts.jsonl"), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	escribe("T-001", "100", "50")
	escribe("T-100001", "200", "80")
	if err := os.WriteFile(filepath.Join(runs, "T-001", "examinador-usage.jsonl"), []byte(`{"entrada":10,"salida":5}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := GastoEnDisco(root); got != 100+50+200+80+10+5 {
		t.Errorf("GastoEnDisco = %d, quiero 445", got)
	}
}

func TestFormatearGasto(t *testing.T) {
	casos := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1234, "1.2k"},
		{40000, "40k"},
		{1250000, "1.2M"},
	}
	for _, c := range casos {
		if got := FormatearGasto(c.n); got != c.want {
			t.Errorf("FormatearGasto(%d) = %q, quiero %q", c.n, got, c.want)
		}
	}
}

func TestBarraMuestraLlenoYRestante(t *testing.T) {
	s := Barra(2500, 10000)
	if len(s) == 0 {
		t.Fatal("barra vacía")
	}
	if !strings.Contains(s, "█") || !strings.Contains(s, "░") {
		t.Errorf("barra sin relleno ni vacío: %q", s)
	}
	if !strings.Contains(s, "2.5k") || !strings.Contains(s, "10k") || !strings.Contains(s, "25%") || !strings.Contains(s, "7.5k") {
		t.Errorf("barra sin cifras: %q", s)
	}
}
