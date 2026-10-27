package ship

import (
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/task"
)

func diffEstricto(archivo string, lineas ...string) string {
	return "+++ b/" + archivo + "\n@@ -0,0 +1 @@\n+" + strings.Join(lineas, "\n+") + "\n"
}

func TestEstrictoTypeScript(t *testing.T) {
	for _, l := range []string{"// @ts-ignore", "const x = y as any", "function f(a: any) {}", "const z = <any>y"} {
		if h := escanearEstricto(diffEstricto("src/a.ts", l), task.Task{}); len(h) != 1 {
			t.Errorf("%q debe frenar, hallazgos=%v", l, h)
		}
	}
	if h := escanearEstricto(diffEstricto("src/a.ts", "const x: number = 1", "const company = 2"), task.Task{}); len(h) != 0 {
		t.Errorf("código tipado no debe frenar: %v", h)
	}
}

func TestEstrictoRustUnsafeNecesitaContrato(t *testing.T) {
	d := diffEstricto("src/lib.rs", "    unsafe { ptr.read() }")
	if h := escanearEstricto(d, task.Task{}); len(h) != 1 {
		t.Fatalf("unsafe sin justificar debe frenar: %v", h)
	}
	if h := escanearEstricto(d, task.Task{Riesgos: "usa unsafe para leer el puntero del FFI"}); len(h) != 0 {
		t.Fatalf("con el contrato justificándolo debe pasar: %v", h)
	}
}

func TestEstrictoPythonTipos(t *testing.T) {
	casos := map[string]bool{
		"def f(a: int, b: str = \"x\") -> int:":        false,
		"def f(a: int):":                               true,
		"def f(a, b: int) -> None:":                    true,
		"def f(self, a: int) -> None:":                 false,
		"def __init__(self, a: int):":                  false,
		"def f(a: dict[str, int], *args: int) -> int:": false,
		"def f(a: int,":                                false,
		"x = 1":                                        false,
	}
	for linea, frena := range casos {
		h := escanearEstricto(diffEstricto("app/m.py", linea), task.Task{})
		if (len(h) > 0) != frena {
			t.Errorf("%q frena=%v, hallazgos=%v", linea, frena, h)
		}
	}
	if h := escanearEstricto(diffEstricto("app/test_m.py", "def test_x():"), task.Task{}); len(h) != 0 {
		t.Errorf("las pruebas están exentas: %v", h)
	}
}
