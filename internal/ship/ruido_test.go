package ship

import (
	"os"
	"path/filepath"
	"testing"
)

func tiposDe(h []Hallazgo) []string {
	var out []string
	for _, x := range h {
		out = append(out, x.Tipo)
	}
	return out
}

func TestRuidoMarcadoresHumanos(t *testing.T) {
	for _, c := range []string{"// TODO: arreglar", "# FIXME luego", "x := 1 // HACK temporal", "// NOTE: ojo", "// DO NOT TOUCH", "// no tocar esto"} {
		h := escanearRuido(diffDe("x.go", c), nil)
		if len(h) != 1 || h[0].Tipo != "comentario humano" {
			t.Errorf("%q: hallazgos = %+v, quiero comentario humano", c, h)
		}
	}
}

func TestRuidoComentarioNarrativoBloquea(t *testing.T) {
	h := escanearRuido(diffDe("x.go", "// uno", "// dos", "// tres", "func f() {}"), nil)
	if len(h) != 1 || h[0].Tipo != "comentario narrativo" {
		t.Errorf("hallazgos = %+v", h)
	}
}

func TestRuidoDosLineasPasan(t *testing.T) {
	if h := escanearRuido(diffDe("x.go", "// uno", "// dos", "func f() {}"), nil); len(h) != 0 {
		t.Errorf("hallazgos = %+v, quiero ninguno", h)
	}
}

func TestRuidoDirectivasPasan(t *testing.T) {
	h := escanearRuido(diffDe("x.go", "//go:build linux", "//nolint:errcheck", "// +build linux", "package x"), nil)
	if len(h) != 0 {
		t.Errorf("hallazgos = %+v, quiero ninguno", h)
	}
}

func TestRuidoContratoDelEsqueletoSoloAvisa(t *testing.T) {
	h := escanearRuido(diffDe("x.go", "// Idea: una", "// dos", "// tres", "// Casos: a -> b"), nil)
	for _, x := range h {
		if x.Tipo != "contrato del esqueleto" {
			t.Errorf("tipo = %q, quiero solo contrato del esqueleto", x.Tipo)
		}
	}
}

func TestRuidoHunksDistintosNoSeJuntan(t *testing.T) {
	diff := `--- a/x.go
+++ b/x.go
@@ -1,2 +1,4 @@
+// uno
+// dos
 func f() {}
+// tres
`
	if h := escanearRuido(diff, nil); len(h) != 0 {
		t.Errorf("hallazgos = %+v, comentarios separados por contexto no son un bloque", h)
	}
}

func TestRuidoDocumentacionExenta(t *testing.T) {
	if h := escanearRuido(diffDe("README.md", "// TODO", "// a", "// b", "// c"), nil); len(h) != 0 {
		t.Errorf("hallazgos = %+v", h)
	}
}

func TestRuidoPuntero(t *testing.T) {
	if h := escanearRuido(diffDe("x.go", "*p = 1", "*q = 2", "*r = 3"), nil); len(h) != 0 {
		t.Errorf("hallazgos = %v, un puntero no es comentario", tiposDe(h))
	}
}

func TestRuidoSinFalsosPositivos(t *testing.T) {
	diff := "+++ b/static/estilos.css\n@@ -0,0 +1,3 @@\n+:root {\n+  --sombra-1: 0 1px 2px rgba(0, 0, 0, .1);\n+}\n" +
		"+++ b/tests/test_js.py\n@@ -0,0 +1,1 @@\n+        self.assertNotIn(\"debugger\", js)\n"
	if h := escanearRuido(diff, nil); len(h) > 0 {
		t.Fatalf("falsos positivos: %+v", h)
	}
	real := "+++ b/app.js\n@@ -0,0 +1,2 @@\n+debugger;\n+// x = foo();\n"
	if h := escanearRuido(real, nil); len(h) != 2 {
		t.Fatalf("debía marcar el depurador y el código comentado: %+v", h)
	}
}

func TestRuidoVigente(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.js"), []byte("const x = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := escanearRuido("+++ b/a.js\n@@ -0,0 +1,1 @@\n+  console.log(x);\n", []string{"a.js.bak"})
	if len(h) != 2 {
		t.Fatalf("hallazgos = %+v", h)
	}
	if v := ruidoVigente(dir, h); len(v) != 0 {
		t.Fatalf("lo corregido sigue contando: %+v", v)
	}
	_ = os.WriteFile(filepath.Join(dir, "a.js"), []byte("console.log(x);\n"), 0o644)
	if v := ruidoVigente(dir, h); len(v) != 1 {
		t.Fatalf("el ruido que quedó no cuenta: %+v", v)
	}
}
