package overlap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoConRamas crea un repo con dos ramas. cambioA y cambioB son
// "ruta=contenido"; si tocan el mismo archivo con contenido distinto hay
// conflicto real entre las ramas.
func repoConRamas(t *testing.T, cambioA, cambioB string) (root, ramaA, ramaB string) {
	t.Helper()
	root = t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "init")

	escribir := func(cambio string) {
		t.Helper()
		ruta, contenido, _ := strings.Cut(cambio, "=")
		p := filepath.Join(root, filepath.FromSlash(ruta))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(contenido+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("checkout", "-qb", "loom/T-001")
	escribir(cambioA)
	git("add", "-A")
	git("commit", "-qm", "a")

	git("checkout", "-q", "main")
	git("checkout", "-qb", "loom/T-002")
	escribir(cambioB)
	git("add", "-A")
	git("commit", "-qm", "b")

	return root, "loom/T-001", "loom/T-002"
}

// Cada tarea toca su propio archivo: fusión limpia, sin alerta.
func TestMergeTreeLimpio(t *testing.T) {
	root, a, b := repoConRamas(t, "src/a.txt=aaa", "docs/b.txt=bbb")
	arbol, conflictos, err := mergeTree(root, a, b)
	if err != nil {
		t.Fatalf("mergeTree: %v", err)
	}
	if len(conflictos) != 0 {
		t.Errorf("conflictos = %v, quiero ninguno", conflictos)
	}
	// el árbol de la fusión limpia es lo que monta el nivel funcional
	if arbol == "" {
		t.Error("fusión limpia sin árbol: el nivel funcional se queda sin qué montar")
	}
}

// Las dos ramas editan el mismo archivo: conflicto real, con la RUTA del
// archivo (antes salía "ramaA ↔ ramaB", que no dice nada).
func TestMergeTreeConflictoRealConRuta(t *testing.T) {
	root, a, b := repoConRamas(t, "f.txt=version a", "f.txt=version b")
	arbol, conflictos, err := mergeTree(root, a, b)
	if err != nil {
		t.Fatalf("mergeTree: %v", err)
	}
	if arbol != "" {
		t.Errorf("con conflicto no debe salir árbol utilizable, salió %q", arbol)
	}
	if len(conflictos) != 1 || conflictos[0] != "f.txt" {
		t.Errorf("conflictos = %v, quiero [f.txt]", conflictos)
	}
}

// Una rama que todavía no existe no es un conflicto: es el falso
// positivo que se veía al arrancar la oleada, antes del primer wip:.
func TestMergeTreeRamaInexistenteNoEsConflicto(t *testing.T) {
	root, a, _ := repoConRamas(t, "src/a.txt=aaa", "docs/b.txt=bbb")
	_, conflictos, err := mergeTree(root, a, "loom/T-999")
	if err != nil {
		t.Fatalf("mergeTree: %v", err)
	}
	if len(conflictos) != 0 {
		t.Errorf("rama inexistente reportó conflicto: %v", conflictos)
	}
}

// Las líneas de etapa llevan la ruta; el resto de la salida (OID del
// árbol, mensajes localizados) no es una etapa y no debe confundirse.
func TestParseLineaEtapa(t *testing.T) {
	conRuta := map[string]string{
		"100644 df967b96a579e45a18b8251732d16804b2e56a55 1\tf.txt":                       "f.txt",
		"100644 0809ef163cd3b01c24087c94153bfc63489d4f9b 2\tsrc/api/login.go":            "src/api/login.go",
		"100644 6c560784be09725cf2e7ab1918a5dc704a19f610 3\tcon espacios/mi archivo.txt": "con espacios/mi archivo.txt",
	}
	for linea, quiero := range conRuta {
		ruta, ok := parseLineaEtapa(linea)
		if !ok || ruta != quiero {
			t.Errorf("parseLineaEtapa(%q) = %q, %v · quiero %q, true", linea, ruta, ok, quiero)
		}
	}
	sinRuta := []string{
		"d424b56c114db04d17c5e423355f1125f030f1fa", // OID del árbol
		"", // línea vacía
		"CONFLICT (content): Merge conflict in f.txt",         // mensaje, no etapa
		"CONFLICTO (contenido): Conflicto de fusión en f.txt", // idem localizado
		"Auto-fusionando f.txt",                               // mensaje informativo
	}
	for _, linea := range sinRuta {
		if ruta, ok := parseLineaEtapa(linea); ok {
			t.Errorf("parseLineaEtapa(%q) = %q, true · no es una etapa", linea, ruta)
		}
	}
}
