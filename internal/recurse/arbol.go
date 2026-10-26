package recurse

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/Pastranauwu/loom/internal/loop"
)

// muArbol serializa las escrituras del árbol: desde que las subtareas
// corren en paralelo, varias hojas de la misma ola pueden registrar su
// nodo al mismo tiempo, y un read-modify-write suelto perdería nodos.
var muArbol sync.Mutex

// NodoArbol es una subtarea ya resuelta, para que board/standup/TUI
// puedan mostrar el árbol sin volver a correr nada — mismo criterio que
// attempts.jsonl: el estado sale de un archivo, nunca de recalcular.
type NodoArbol struct {
	ID          string      `json:"id"`
	Titulo      string      `json:"titulo"`
	Padre       string      `json:"padre"`
	Profundidad int         `json:"profundidad"`
	Verde       bool        `json:"verde"`
	Intentos    int         `json:"intentos"`
	Motivo      string      `json:"motivo,omitempty"`
	Tokens      loop.Tokens `json:"tokens"`
	// Modelo es el modelo que resolvió esta subtarea. En una hoja que
	// escaló de modelo muestra el pesado, así board/standup dejan ver
	// cuándo el barato no alcanzó sin volver a correr nada.
	Modelo string `json:"modelo,omitempty"`
}

// arbolPath es donde vive el árbol de una tarea raíz recursiva —
// sobrevive a que el cuarto de la subtarea se destruya, porque cuelga de
// la raíz del proyecto, no del cuarto (mismo lugar que attempts.jsonl).
func arbolPath(root, raizID string) string {
	return filepath.Join(loop.RunsDir(root), raizID, "arbol.json")
}

// AgregarNodo escribe o actualiza un nodo del árbol. Reemplaza cualquier
// nodo previo con el mismo ID: un reintento de la misma subtarea pisa su
// resultado anterior, el árbol siempre muestra la última corrida, no un
// historial acumulado que nadie pidió.
func AgregarNodo(root, raizID string, n NodoArbol) error {
	muArbol.Lock()
	defer muArbol.Unlock()

	nodos, err := LeerArbol(root, raizID)
	if err != nil {
		return err
	}
	reemplazado := false
	for i, existente := range nodos {
		if existente.ID == n.ID {
			nodos[i] = n
			reemplazado = true
			break
		}
	}
	if !reemplazado {
		nodos = append(nodos, n)
	}

	p := arbolPath(root, raizID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(nodos, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// LeerArbol devuelve los nodos guardados de una tarea raíz. Una tarea sin
// recursión (o que nunca llegó a decomponer nada) no tiene archivo: eso
// no es un error, es "sin árbol".
func LeerArbol(root, raizID string) ([]NodoArbol, error) {
	data, err := os.ReadFile(arbolPath(root, raizID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var nodos []NodoArbol
	if err := json.Unmarshal(data, &nodos); err != nil {
		return nil, err
	}
	return nodos, nil
}
