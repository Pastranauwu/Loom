package ship

import "testing"

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
