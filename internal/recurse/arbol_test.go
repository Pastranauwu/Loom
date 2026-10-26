package recurse

import (
	"context"
	"testing"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/room"
)

func TestLeerArbolSinArchivo(t *testing.T) {
	got, err := LeerArbol(t.TempDir(), "T-100")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("LeerArbol sin archivo debió devolver nil, dio %v", got)
	}
}

func TestAgregarNodoEscribeYActualiza(t *testing.T) {
	root := t.TempDir()
	if err := AgregarNodo(root, "T-100", NodoArbol{ID: "T-100001", Titulo: "uno", Padre: "T-100", Verde: false, Intentos: 1}); err != nil {
		t.Fatal(err)
	}
	nodos, err := LeerArbol(root, "T-100")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodos) != 1 || nodos[0].Verde {
		t.Fatalf("nodos = %+v, quiero 1 nodo rojo", nodos)
	}

	// un reintento de la misma subtarea pisa el nodo, no lo duplica
	if err := AgregarNodo(root, "T-100", NodoArbol{ID: "T-100001", Titulo: "uno", Padre: "T-100", Verde: true, Intentos: 2}); err != nil {
		t.Fatal(err)
	}
	nodos, err = LeerArbol(root, "T-100")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodos) != 1 || !nodos[0].Verde || nodos[0].Intentos != 2 {
		t.Fatalf("nodos = %+v, quiero 1 nodo verde en el segundo intento", nodos)
	}
}

func TestAgentRunRegistraNodoVerdeEnElArbol(t *testing.T) {
	root := repoConCommit(t)
	parentTask := tareaRecursivaDePrueba()
	parentRoom, err := room.Create(context.Background(), root, parentTask.ID, "main")
	if err != nil {
		t.Fatalf("room.Create padre: %v", err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, parentTask.ID) })

	texto := `[{"titulo": "crear done.txt", "listo_cuando": "test -f src/done.txt", "tocar_solo": ["src/**"]}]`
	a := Agent{
		Planificador: generadorFalso{texto: texto},
		Ejecutor:     ejecutorFalso{},
		Task:         parentTask,
		Root:         root,
		RaizID:       parentTask.ID,
	}
	if _, err := a.Run(context.Background(), loop.Request{RoomPath: parentRoom.Path}); err != nil {
		t.Fatalf("Agent.Run: %v", err)
	}

	nodos, err := LeerArbol(root, parentTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodos) != 1 || !nodos[0].Verde || nodos[0].Padre != parentTask.ID {
		t.Fatalf("arbol.json = %+v, quiero 1 nodo verde con padre %s", nodos, parentTask.ID)
	}
}
