package loop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Un latido recién escrito está vivo; el mismo archivo sin refrescar más
// allá del umbral está muerto. Es la premisa que un SIGKILL rompía: el
// archivo quedaba en disco y todo el mundo lo leía como "corriendo".
func TestLatidoVivoSoloMientrasLoRefrescan(t *testing.T) {
	root := t.TempDir()
	EscribirLatido(root, Latido{ID: "T-001", Intento: 1, Fase: FaseAgente})

	if _, vivo := LeerLatido(root, "T-001"); !vivo {
		t.Fatal("recién escrito debería estar vivo")
	}
	if _, muerta := Interrumpida(root, "T-001"); muerta {
		t.Error("recién escrito no está interrumpido")
	}

	// envejecerlo a mano: es lo que deja un SIGKILL
	l, _ := LeerLatidoCrudo(root, "T-001")
	l.Visto = time.Now().UTC().Add(-LatidoRancioTras - time.Second)
	escribirCrudo(t, root, l)

	if _, vivo := LeerLatido(root, "T-001"); vivo {
		t.Error("un latido que nadie refresca no está vivo")
	}
	lat, muerta := Interrumpida(root, "T-001")
	if !muerta {
		t.Fatal("un latido rancio es una corrida interrumpida")
	}
	if lat.Intento != 1 || lat.Fase != FaseAgente {
		t.Errorf("el latido rancio perdió su contenido: %+v", lat)
	}
}

// Sin archivo no hay ni corrida viva ni corrida muerta: la tarea nunca
// arrancó. Distinguirlo importa porque solo la segunda se retoma.
func TestSinLatidoNoHayCorridaNiInterrupcion(t *testing.T) {
	root := t.TempDir()
	if _, vivo := LeerLatido(root, "T-404"); vivo {
		t.Error("sin archivo no hay corrida viva")
	}
	if _, muerta := Interrumpida(root, "T-404"); muerta {
		t.Error("sin archivo no hay corrida interrumpida")
	}
}

// Un latido de un binario anterior no trae Visto. Se da por muerto: es
// la dirección segura, porque la alternativa deja la tarea atascada para
// siempre, que es justo el problema que el campo vino a resolver.
func TestLatidoSinVistoSeDaPorMuerto(t *testing.T) {
	root := t.TempDir()
	escribirCrudo(t, root, Latido{ID: "T-001", Intento: 1, Fase: FaseAgente}) // Visto en cero
	if _, vivo := LeerLatido(root, "T-001"); vivo {
		t.Error("sin Visto no se puede afirmar que corre")
	}
	if _, muerta := Interrumpida(root, "T-001"); !muerta {
		t.Error("sin Visto se retoma, no se abandona")
	}
}

// Lo que el agente va haciendo dentro del intento tiene que llegar al
// que mira: al callback OnAvance en vivo y, por la cola, al latido que
// pinta el tablero. Sin esto una fase "agente" de veinte minutos se veía
// igual que una invocación colgada.
func TestAvanceDelAgenteLlegaAlLatidoYAlCallback(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso"}
	var vistoLatido []string
	ag.hacer = func(veces int, req Request) (string, int, error) {
		if req.Avance != nil {
			req.Avance("$ go test ./...")
			req.Avance("lee src/a.go")
			// el latido en disco lo refresca el latidor cada 15s; aquí se
			// mira la memoria del bucle a través del archivo tras un aviso
		}
		if l, ok := LeerLatidoCrudo(root, "T-001"); ok {
			vistoLatido = append(vistoLatido, l.Actividad)
		}
		if veces == 1 {
			escribir(t, req.RoomPath, "src/done.txt", "x\n")
		}
		return "", 0, nil
	}
	var onAvance []string
	opts := optsDePrueba(t, root, ag, tareaDePrueba())
	opts.OnAvance = func(msg string) { onAvance = append(onAvance, msg) }

	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"$ go test ./...", "lee src/a.go"}
	if len(onAvance) != len(want) || onAvance[0] != want[0] || onAvance[1] != want[1] {
		t.Errorf("OnAvance = %v, quiero %v", onAvance, want)
	}
	if len(vistoLatido) == 0 {
		t.Fatal("el latido no registró actividad")
	}
}

// escribirCrudo guarda un latido tal cual, sin estampar Visto. Solo sirve
// para fabricar latidos viejos en las pruebas; EscribirLatido siempre lo
// estampa, que es lo correcto en producción.
func escribirCrudo(t *testing.T, root string, l Latido) {
	t.Helper()
	p := latidoPath(root, l.ID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	datos, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, datos, 0o644); err != nil {
		t.Fatal(err)
	}
}
