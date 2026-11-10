package ship

import (
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/task"
)

func TestSinTiposDiceLasDosFaltasALaVez(t *testing.T) {
	if f := sinTipos("def save(self, *args, **kwargs):"); !strings.Contains(f, "retorno") || !strings.Contains(f, "argumento") {
		t.Errorf("faltas = %q, quiero retorno y argumento juntos", f)
	}
	if f := sinTipos("def suma(a: int, b: int) -> int:"); f != "" {
		t.Errorf("def completo marcado: %q", f)
	}
}

func TestEstrictoListaCadaHallazgoYExentaMigraciones(t *testing.T) {
	diff := "+++ b/core/migrations/0004_x.py\n+def sembrar(apps, schema_editor):\n" +
		"+++ b/core/models.py\n+def a(x):\n+def b(y):\n"
	h := Estricto(diff, task.Task{})
	if strings.Count(h, "\n") != 2 || strings.Contains(h, "migrations") || !strings.Contains(h, "core/models.py") {
		t.Errorf("hallazgos:\n%s", h)
	}
}
