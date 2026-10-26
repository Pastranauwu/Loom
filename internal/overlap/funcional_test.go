package overlap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// repoDosTareasVerdes arma un repo donde cada rama pasa su propia suite.
// cambioA/cambioB son "ruta=contenido" igual que en repoConRamas.
func repoDosTareasVerdes(t *testing.T, cambioA, cambioB string) string {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
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

	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	escribir("valor.txt=hola")
	git("add", "-A")
	git("commit", "-qm", "init")

	git("checkout", "-qb", "loom/T-001")
	escribir(cambioA)
	git("add", "-A")
	git("commit", "-qm", "a")

	git("checkout", "-q", "main")
	git("checkout", "-qb", "loom/T-002")
	escribir(cambioB)
	git("add", "-A")
	git("commit", "-qm", "b")
	git("checkout", "-q", "main")

	return root
}

// El caso que justifica todo el nivel 3: las dos ramas pasan su suite por
// separado, no chocan en texto y no comparten símbolo — y al fusionarlas
// una rompe. T-001 cambia el valor; T-002 trae una prueba que lo exige
// tal como estaba.
func TestDosVerdesQueRompenAlFusionarse(t *testing.T) {
	root := repoDosTareasVerdes(t,
		"valor.txt=HOLA",
		"prueba_b.sh=grep -qx hola valor.txt")

	res := CheckPar(root, "T-001", "T-002", nil, nil)
	if res.Textual {
		t.Fatalf("el par no debía chocar en texto: %v", res.Conflictos)
	}
	if res.Arbol == "" {
		t.Fatal("la fusión limpia no dejó árbol")
	}

	rf := CheckFuncional(context.Background(), root, res,
		Suite{ID: "T-001", ListoCuando: "test -s valor.txt"},
		Suite{ID: "T-002", ListoCuando: "sh prueba_b.sh"},
		30*time.Second)

	if !rf.Corrio {
		t.Fatalf("no corrió: %s", rf.Motivo)
	}
	if len(rf.Rompen) != 1 || rf.Rompen[0] != "T-002" {
		t.Fatalf("Rompen = %v, quiero solo T-002", rf.Rompen)
	}
	if !strings.Contains(rf.Alerta(), "verdes por separado") {
		t.Errorf("Alerta = %q", rf.Alerta())
	}
}

// Y el contrario: dos tareas que de verdad no se pisan no levantan nada.
func TestFusionLimpiaNoLevantaNada(t *testing.T) {
	root := repoDosTareasVerdes(t,
		"a.txt=de T-001",
		"b.txt=de T-002")

	res := CheckPar(root, "T-001", "T-002", nil, nil)
	rf := CheckFuncional(context.Background(), root, res,
		Suite{ID: "T-001", ListoCuando: "test -s a.txt"},
		Suite{ID: "T-002", ListoCuando: "test -s b.txt"},
		30*time.Second)

	if !rf.Corrio {
		t.Fatalf("no corrió: %s", rf.Motivo)
	}
	if len(rf.Rompen) != 0 {
		t.Errorf("Rompen = %v, quiero vacío · salida:\n%s", rf.Rompen, rf.Salida)
	}
	if rf.Alerta() != "" {
		t.Errorf("Alerta = %q, quiero vacía", rf.Alerta())
	}
}

// El worktree de la fusión es de usar y tirar: si quedara montado, la
// comprobación del próximo par chocaría contra él.
func TestElWorktreeDeLaFusionNoSobrevive(t *testing.T) {
	root := repoDosTareasVerdes(t, "a.txt=uno", "b.txt=dos")
	res := CheckPar(root, "T-001", "T-002", nil, nil)
	suiteA := Suite{ID: "T-001", ListoCuando: "true"}
	suiteB := Suite{ID: "T-002", ListoCuando: "true"}

	for i := 0; i < 2; i++ {
		rf := CheckFuncional(context.Background(), root, res, suiteA, suiteB, 30*time.Second)
		if !rf.Corrio {
			t.Fatalf("pasada %d no corrió: %s", i+1, rf.Motivo)
		}
	}
	out, err := exec.Command("git", "-C", root, "worktree", "list").Output()
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimSpace(string(out)), "\n"); n != 0 {
		t.Errorf("quedaron worktrees montados:\n%s", out)
	}
}

// Sin árbol no se corre nada y no se inventa un veredicto: un conflicto
// de texto ya lo reporta el nivel 1, y callar es correcto ahí.
func TestSinArbolNoCorreYNoAlerta(t *testing.T) {
	rf := CheckFuncional(context.Background(), t.TempDir(), Resultado{},
		Suite{ID: "T-001", ListoCuando: "true"},
		Suite{ID: "T-002", ListoCuando: "true"},
		time.Second)
	if rf.Corrio {
		t.Error("corrió sin árbol que montar")
	}
	if rf.Alerta() != "" {
		t.Errorf("Alerta = %q, quiero vacía", rf.Alerta())
	}
}

// Pero "no se pudo comprobar" sí habla. Decir limpio sin haber mirado es
// justo lo que esta comprobación existe para no hacer.
func TestSinListoCuandoAvisaQueNoSePudo(t *testing.T) {
	root := repoDosTareasVerdes(t, "a.txt=uno", "b.txt=dos")
	res := CheckPar(root, "T-001", "T-002", nil, nil)
	rf := CheckFuncional(context.Background(), root, res,
		Suite{ID: "T-001", ListoCuando: "true"},
		Suite{ID: "T-002"},
		time.Second)
	if rf.Corrio {
		t.Error("corrió sin listo_cuando de T-002")
	}
	if !strings.Contains(rf.Alerta(), "no se pudo comprobar") {
		t.Errorf("Alerta = %q", rf.Alerta())
	}
}

// El nivel 3 cuesta caro porque ejecuta código, así que el llamador
// necesita saber si el par lo amerita.
func TestSospechosoSoloConTextualOSemantico(t *testing.T) {
	casos := []struct {
		nombre string
		res    Resultado
		quiero bool
	}{
		{"limpio", Resultado{}, false},
		{"textual", Resultado{Textual: true}, true},
		{"semántico", Resultado{Semantico: true}, true},
		{"indeterminado", Resultado{Indeterminado: "git viejo"}, false},
	}
	for _, c := range casos {
		if got := c.res.Sospechoso(); got != c.quiero {
			t.Errorf("%s: Sospechoso() = %v, quiero %v", c.nombre, got, c.quiero)
		}
	}
}
