package tutorial

import (
	"strings"
	"testing"
)

func TestAmbosIdiomasTienenLasMismasPaginas(t *testing.T) {
	en, es := Paginas(false), Paginas(true)
	if len(en) != len(es) || len(en) < 6 {
		t.Fatalf("páginas en=%d es=%d", len(en), len(es))
	}
	for i := range en {
		if strings.TrimSpace(en[i].Titulo) == "" || strings.TrimSpace(en[i].Cuerpo) == "" || strings.TrimSpace(es[i].Cuerpo) == "" {
			t.Errorf("página %d vacía", i)
		}
	}
}

func TestElTutorialNombraLosComandosQueEnsena(t *testing.T) {
	for _, es := range []bool{false, true} {
		txt := Texto(Paginas(es))
		for _, cmd := range []string{"loom sketch", "loom apply --dry-run", "loom run", "loom board", "loom ship --all", "loom map", "loom dna", "loom heal --from", "loom language", "loom doctor"} {
			if !strings.Contains(txt, cmd) {
				t.Errorf("es=%v: el tutorial no menciona %q", es, cmd)
			}
		}
	}
}
