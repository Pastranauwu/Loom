package ship

import (
	"strings"
	"testing"
)

func TestEscanearRuidoDebugYTemporal(t *testing.T) {
	diff := `diff --git a/src/x.go b/src/x.go
--- a/src/x.go
+++ b/src/x.go
@@ -0,0 +1,3 @@
+package x
+import "fmt"
+	fmt.Println("hola")
`
	h := escanearRuido(diff, []string{"src/x.go", "notas.tmp"})
	if len(h) != 2 {
		t.Fatalf("hallazgos = %+v, quiero 2", h)
	}
	tipos := map[string]bool{}
	for _, x := range h {
		tipos[x.Tipo] = true
	}
	if !tipos["print de debug"] || !tipos["archivo temporal"] {
		t.Errorf("tipos = %v", tipos)
	}
}

func TestEscanearRuidoCodigoComentado(t *testing.T) {
	diff := `--- a/x.go
+++ b/x.go
+// x = y;
+// esto es una nota narrativa
`
	h := escanearRuido(diff, nil)
	if len(h) != 1 {
		t.Fatalf("hallazgos = %+v, quiero solo el código comentado", h)
	}
	if h[0].Tipo != "código comentado" {
		t.Errorf("tipo = %q", h[0].Tipo)
	}
}

func TestEscanearRuidoLogNoEsDebug(t *testing.T) {
	diff := `--- a/x.go
+++ b/x.go
+	log.Printf("servidor en %s", addr)
`
	if h := escanearRuido(diff, nil); len(h) != 0 {
		t.Errorf("log.Printf marcado como ruido: %+v", h)
	}
}

func TestEscanearRuidoComentarioConPuntoYComaEnMedio(t *testing.T) {
	diff := `--- a/x.go
+++ b/x.go
+// Defaults to wol.Send; overridable in tests.
`
	if h := escanearRuido(diff, nil); len(h) != 0 {
		t.Errorf("comentario narrativo marcado como código: %+v", h)
	}
}

func TestEscanearRuidoComentarioQueEmpiezaComoPalabraClave(t *testing.T) {
	diff := `--- a/x.go
+++ b/x.go
+// for Echo discovery; add NOTIFY if some controller never probes.
`
	if h := escanearRuido(diff, nil); len(h) != 0 {
		t.Errorf("comentario narrativo marcado como código: %+v", h)
	}
}

func TestEscanearRuidoCodigoComentadoConLlaves(t *testing.T) {
	diff := `--- a/x.go
+++ b/x.go
+// for i := range xs {
`
	h := escanearRuido(diff, nil)
	if len(h) != 1 || h[0].Tipo != "código comentado" {
		t.Errorf("hallazgos = %+v, quiero código comentado", h)
	}
}

func TestEscanearRuidoLimpio(t *testing.T) {
	diff := `--- a/x.go
+++ b/x.go
+package x
+// Config guarda opciones.
+type Config struct{}
`
	if h := escanearRuido(diff, []string{"x.go"}); len(h) != 0 {
		t.Errorf("diff limpio marcado: %+v", h)
	}
}

func TestEscanearSecretos(t *testing.T) {
	diff := `--- a/x.go
+++ b/x.go
+const awsKey = "AKIA1234567890ABCDEF"
+password = "hunter2secret"
+func normal() {}
`
	h := escanearSecretos(diff)
	if len(h) != 2 {
		t.Fatalf("hallazgos = %+v, quiero 2", h)
	}
	if h[0].Tipo != "clave AWS" {
		t.Errorf("primer hallazgo = %q", h[0].Tipo)
	}
}

func TestEscanearSecretosSinSecretos(t *testing.T) {
	diff := `--- a/x.go
+++ b/x.go
+token := os.Getenv("TOKEN")
+const version = "1.2.3"
`
	if h := escanearSecretos(diff); len(h) != 0 {
		t.Errorf("sin secretos pero marcó: %+v", h)
	}
}

func TestTipoCommit(t *testing.T) {
	src := []string{"src/a.ts", "tests/a.test.ts"}
	for _, titulo := range []string{"exportar clientes a CSV", "pantalla de arregla el beat", "romper beat determinista", "esqueleto · base de minijuegos"} {
		if got := tipoCommit(titulo, src, nil); got != "feat" {
			t.Errorf("tipoCommit(%q) = %q, quiero feat", titulo, got)
		}
	}
	for _, titulo := range []string{"arreglar el login", "fix del bug de tildes", "corrige la falla"} {
		if got := tipoCommit(titulo, src, nil); got != "fix" {
			t.Errorf("tipoCommit(%q) = %q, quiero fix", titulo, got)
		}
	}
	if got := tipoCommit("prueba de integración punta a punta", []string{"tests/integracion.test.ts"}, nil); got != "test" {
		t.Errorf("solo pruebas = %q, quiero test", got)
	}
	if got := tipoCommit("guía de uso", []string{"README.md", "docs/uso.md"}, nil); got != "docs" {
		t.Errorf("solo documentación = %q, quiero docs", got)
	}
}

func TestVerificarPresupuesto(t *testing.T) {
	tk := taskTitulo("x")
	tk.LimiteLineas = 200

	d, ok := verificarPresupuesto(118, 12, 0, 3, tk)
	if !ok || !strings.Contains(d, "118") || !strings.Contains(d, "200") {
		t.Errorf("dentro de presupuesto: %q, %v", d, ok)
	}

	// el límite lo estima un modelo sin ver el código: pasarse por poco
	// es ruido de estimación y no debe frenar trabajo ya verificado
	d, ok = verificarPresupuesto(260, 0, 0, 3, tk)
	if !ok || !strings.Contains(d, "tolerancia") {
		t.Errorf("dentro de la tolerancia: %q, %v", d, ok)
	}

	// las pruebas del examinador no las escribe el agente: cobrárselas
	// hacía que casi toda tarea con examen se pasara del límite
	d, ok = verificarPresupuesto(600, 0, 450, 6, tk)
	if !ok {
		t.Errorf("las líneas de prueba no deben contra el presupuesto: %q", d)
	}
	if !strings.Contains(d, "150") || !strings.Contains(d, "450") {
		t.Errorf("debe reportar código y pruebas por separado: %q", d)
	}

	// pasarse de largo sigue siendo desbordamiento
	d, ok = verificarPresupuesto(700, 0, 0, 9, tk)
	if ok {
		t.Error("exceder la tolerancia debió fallar")
	}
	if !strings.Contains(d, "700") || !strings.Contains(d, tk.ID) {
		t.Errorf("el motivo debe decir el número a poner y dónde: %q", d)
	}
}

func TestGenerarHandoff(t *testing.T) {
	tk := taskTitulo("exportar")
	tk.ListoCuando = "go test ./..."
	tk.NoTocar = []string{"migrations/**"}
	tk.Riesgos = "archivos grandes"
	cuerpo := generarHandoff(tk, []string{"src/export/writer.go"}, 10, 2)
	for _, want := range []string{
		"## Qué cambió",
		"src/export/writer.go",
		"## Qué no se hizo",
		"migrations/**",
		"archivos grandes",
		"## Cómo verificar",
		"go test ./...",
	} {
		if !strings.Contains(cuerpo, want) {
			t.Errorf("handoff sin %q:\n%s", want, cuerpo)
		}
	}
}

func TestVerificarExpone(t *testing.T) {
	diff := `--- a/internal/wol/wol.go
+++ b/internal/wol/wol.go
+func Send(macStr, addr string) error {
+	return nil
+}
`
	// el nombre está aunque los parámetros se llamen distinto
	if faltan, _ := verificarExpone([]string{"wol.Send(mac, addr string) error"}, diff); len(faltan) != 0 {
		t.Errorf("faltan = %v, la firma sí está entregada", faltan)
	}
	// sin firmas declaradas, no hay nada que verificar
	if faltan, _ := verificarExpone(nil, diff); len(faltan) != 0 {
		t.Errorf("faltan = %v, quiero vacío", faltan)
	}
	// prometida y no entregada
	faltan, _ := verificarExpone([]string{"POST /wake"}, diff)
	if len(faltan) != 1 || faltan[0] != "POST /wake" {
		t.Errorf("faltan = %v, quiero [POST /wake]", faltan)
	}
}

// El planificador es un modelo y a veces escribe descripciones donde va
// una firma. Eso no se puede buscar en un diff, y frenar la entrega por
// ello dejaba la tarea muerta: el único arreglo era editar el contrato a
// mano, justo lo que loom evita.
func TestVerificarExponeNoBloqueaPorProsa(t *testing.T) {
	diff := `--- a/cmd/sum/main.go
+++ b/cmd/sum/main.go
+func main() {}
`
	prosa := []string{"cmd/sum main package", "loom --mac <MAC>"}
	faltan, sinForma := verificarExpone(prosa, diff)
	if len(faltan) != 0 {
		t.Errorf("faltan = %v · la prosa no se puede verificar, no debe bloquear", faltan)
	}
	if len(sinForma) != 2 {
		t.Errorf("sinForma = %v, quiero las dos reportadas", sinForma)
	}
}

// Pero una firma de verdad que no se entregó sigue frenando.
func TestVerificarExponeSigueBloqueandoFirmaReal(t *testing.T) {
	diff := `--- a/x.go
+++ b/x.go
+func Otra() {}
`
	faltan, sinForma := verificarExpone([]string{"wol.Send(mac string) error"}, diff)
	if len(faltan) != 1 {
		t.Errorf("faltan = %v, quiero la firma incumplida", faltan)
	}
	if len(sinForma) != 0 {
		t.Errorf("sinForma = %v, quiero vacío", sinForma)
	}
}

// Un expone puede ser un archivo entero (un interaction-model de alexa,
// un README, un servicio systemd): su nombre está en el encabezado del
// diff, no en las líneas añadidas. Antes eso frenaba con "no expone lo
// prometido" pese a que el archivo se entregó.
func TestVerificarExponeAceptaArchivos(t *testing.T) {
	diff := `diff --git a/skill/interaction-model.json b/skill/interaction-model.json
new file mode 100644
--- /dev/null
+++ b/skill/interaction-model.json
@@ -0,0 +1,3 @@
+{"interactionModel": {"languageModel": {"invocationName": "wake pc"}}}
`
	faltan, _ := verificarExpone([]string{"skill/interaction-model.json"}, diff)
	if len(faltan) != 0 {
		t.Errorf("faltan = %v, el archivo sí se entregó", faltan)
	}
}

// Un CLI cuyo trabajo es imprimir no puede quedar bloqueado para siempre
// porque su main.go llama a fmt.Println.
func TestEscanearRuidoNoMarcaLaSalidaDelPrograma(t *testing.T) {
	diff := `diff --git a/cmd/sum/main.go b/cmd/sum/main.go
--- a/cmd/sum/main.go
+++ b/cmd/sum/main.go
@@ -0,0 +1,3 @@
+func main() {
+	fmt.Println(calc.Sum(a, b))
+}
`
	if h := escanearRuido(diff, []string{"cmd/sum/main.go"}); len(h) != 0 {
		t.Errorf("hallazgos = %+v · imprimir es lo que hace un CLI", h)
	}
}

// Fuera del punto de entrada, el print sigue siendo ruido.
func TestEscanearRuidoMarcaPrintEnLibreria(t *testing.T) {
	diff := `diff --git a/internal/calc/sum.go b/internal/calc/sum.go
--- a/internal/calc/sum.go
+++ b/internal/calc/sum.go
@@ -0,0 +1,2 @@
+func Sum(a, b int) int {
+	fmt.Println("debug:", a, b)
`
	h := escanearRuido(diff, []string{"internal/calc/sum.go"})
	if len(h) != 1 || h[0].Tipo != "print de debug" {
		t.Errorf("hallazgos = %+v, quiero un print de debug", h)
	}
}

// Un depurador es ruido en cualquier archivo, punto de entrada incluido.
func TestEscanearRuidoMarcaDepuradorEnPuntoDeEntrada(t *testing.T) {
	diff := `diff --git a/cmd/app/main.py b/cmd/app/main.py
--- a/cmd/app/main.py
+++ b/cmd/app/main.py
@@ -0,0 +1,2 @@
+print("resultado", x)
+breakpoint()
`
	h := escanearRuido(diff, []string{"cmd/app/main.py"})
	if len(h) != 1 {
		t.Fatalf("hallazgos = %+v, quiero solo el breakpoint", h)
	}
	if !strings.Contains(h[0].Detalle, "breakpoint") {
		t.Errorf("detalle = %q", h[0].Detalle)
	}
}

func TestEsPuntoDeEntrada(t *testing.T) {
	for _, ruta := range []string{"cmd/sum/main.go", "main.go", "bin/tool.js", "src/cli/run.ts", "app/__main__.py"} {
		if !esPuntoDeEntrada(ruta) {
			t.Errorf("%q debería ser punto de entrada", ruta)
		}
	}
	for _, ruta := range []string{"internal/calc/sum.go", "src/lib/helper.js", "pkg/wol/send.go", "commander/x.go"} {
		if esPuntoDeEntrada(ruta) {
			t.Errorf("%q NO debería ser punto de entrada", ruta)
		}
	}
}

// un script que se declara ejecutable imprime su resultado aunque no viva
// en cmd/ o scripts/; el mismo print en un módulo sigue siendo ruido
func TestRuidoDejaImprimirAlArchivoQueSeDeclaraEntrada(t *testing.T) {
	for archivo, lineas := range map[string][]string{
		"tools/reporte.py": {"def main():", `    print("listo")`, `if __name__ == "__main__":`, "    main()"},
		"tools/gen.go":     {"func main() {", `	fmt.Println("listo")`, "}"},
		"tools/correr":     {"#!/usr/bin/env python3", `print("listo")`},
	} {
		if h := escanearRuido(diffDe(archivo, lineas...), nil); len(h) != 0 {
			t.Errorf("%s: %+v", archivo, h)
		}
	}
	if h := escanearRuido(diffDe("app/servicio.py", "def f():", `    print("aquí")`), nil); len(h) != 1 {
		t.Errorf("un print en un módulo debe frenar: %+v", h)
	}
	// depuradores: ruido también en un punto de entrada
	if h := escanearRuido(diffDe("tools/x.py", `if __name__ == "__main__":`, "    breakpoint()"), nil); len(h) != 1 {
		t.Errorf("breakpoint() debe frenar: %+v", h)
	}
}

func TestPresupuestoSinTope(t *testing.T) {
	tk := taskTitulo("módulo completo")
	tk.LimiteLineas = 0
	d, ok := verificarPresupuesto(10000, 0, 2000, 20, tk)
	if !ok || !strings.Contains(d, "sin límite") || !strings.Contains(d, "8000") {
		t.Fatalf("%s: %v", d, ok)
	}
}
