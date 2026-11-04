package historial

import (
	"strings"
	"testing"
)

func commitVacio(t *testing.T, root, msg string) {
	t.Helper()
	gitEn(t, root, "commit", "-q", "--allow-empty", "-m", msg)
}

func repoConDev(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitEn(t, root, "init", "-q", "-b", "main")
	gitEn(t, root, "config", "user.email", "t@t")
	gitEn(t, root, "config", "user.name", "t")
	escribir(t, root+"/a.txt", "1\n")
	gitEn(t, root, "add", "-A")
	gitEn(t, root, "commit", "-q", "-m", "chore: inicio")
	gitEn(t, root, "checkout", "-q", "-b", "dev")
	escribir(t, root+"/a.txt", "2\n")
	gitEn(t, root, "commit", "-q", "-am", "feat: carrito con total")
	commitVacio(t, root, "wip: T-001 intento")
	commitVacio(t, root, "fix: redondeo del total")
	commitVacio(t, root, "loom: archiva 0001-carrito")
	return root
}

func TestPublicarDejaUnCommitPorVersionConTagYResumen(t *testing.T) {
	root := repoConDev(t)
	r, err := Publicar(root, "main", "carrito", "comprar varias cosas a la vez")
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v0.1.0" || len(r.Mejoras) != 1 || len(r.Reparaciones) != 1 {
		t.Fatalf("release = %+v", r)
	}
	if n := salida(root, "rev-list", "--count", "main"); n != "2" {
		t.Errorf("main lleva el inicio y una release, no el historial de dev: %s commits", n)
	}
	if salida(root, "rev-parse", "main^{tree}") != salida(root, "rev-parse", "dev^{tree}") {
		t.Error("la release es el árbol de la base")
	}
	msg := salida(root, "log", "-1", "--format=%B", "main")
	for _, quiero := range []string{"release: v0.1.0 · carrito", "comprar varias cosas", "- carrito con total", "- redondeo del total", TrailerBase + ": "} {
		if !strings.Contains(msg, quiero) {
			t.Errorf("falta %q en:\n%s", quiero, msg)
		}
	}
	if strings.Contains(msg, "wip") || strings.Contains(msg, "archiva") {
		t.Errorf("wip y commits de loom no van al resumen:\n%s", msg)
	}
	if salida(root, "rev-parse", "v0.1.0^{commit}") != r.Commit {
		t.Error("el tag apunta a la release")
	}
	if salida(root, "symbolic-ref", "--short", "HEAD") != "dev" {
		t.Error("no cambia de rama")
	}

	commitVacio(t, root, "fix: total negativo")
	r2, err := Publicar(root, "main", "carrito", "")
	if err != nil {
		t.Fatal(err)
	}
	if r2.Tag != "v0.1.1" || len(r2.Mejoras) != 0 || len(r2.Reparaciones) != 1 || r2.Reparaciones[0] != "total negativo" {
		t.Errorf("la siguiente resume solo lo nuevo y sube el patch: %+v", r2)
	}
	commitVacio(t, root, "feat!: precios en centavos")
	if r3, _ := Publicar(root, "main", "carrito", ""); r3.Tag != "v0.2.0" {
		t.Errorf("algo que rompe antes de 1.0 sube el minor: %s", r3.Tag)
	}
	gitEn(t, root, "checkout", "-q", "main")
	if _, err := Publicar(root, "main", "x", ""); err == nil {
		t.Error("desde la rama de releases no se publica")
	}
}

func TestSiguienteVersion(t *testing.T) {
	for _, c := range []struct {
		ultima        string
		rompe, mejora bool
		quiero        string
	}{{"", false, true, "v0.1.0"}, {"v1.4.2", false, false, "v1.4.3"}, {"v1.4.2", false, true, "v1.5.0"}, {"v1.4.2", true, true, "v2.0.0"}, {"no-semver", false, false, "v0.1.0"}} {
		if v := siguienteVersion(c.ultima, c.rompe, c.mejora); v != c.quiero {
			t.Errorf("%+v = %s", c, v)
		}
	}
}

func TestAvanzarSoloEnLineaRecta(t *testing.T) {
	root := repoConDev(t)
	gitEn(t, root, "checkout", "-q", "-b", "loom/_entrega")
	commitVacio(t, root, "feat: cupones")
	gitEn(t, root, "checkout", "-q", "dev")
	if ok, err := Avanzar(root, "loom/_entrega"); !ok || err != nil || salida(root, "rev-parse", "dev") != salida(root, "rev-parse", "loom/_entrega") {
		t.Fatalf("dev va detrás: avanza sin merge (%v %v)", ok, err)
	}
	commitVacio(t, root, "chore: aparte")
	gitEn(t, root, "checkout", "-q", "loom/_entrega")
	commitVacio(t, root, "feat: otra")
	gitEn(t, root, "checkout", "-q", "dev")
	if ok, _ := Avanzar(root, "loom/_entrega"); ok {
		t.Error("ramas que divergieron no se avanzan")
	}
	if ok, _ := Avanzar(root, "no-existe"); ok {
		t.Error("sin rama de entrega no hay avance")
	}
}
