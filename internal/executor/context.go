package executor

import (
	"context"
	"strings"
	"sync"
)

// ContextWindow is the context size of a model in tokens, from the static
// table. It is the fallback when the real limit is unknown: `[1m]` or a
// gemini id mean 1M, everything else 200k. Most opencode models are much
// smaller (32k–128k), so prefer CatalogoVentanas when it is available.
func ContextWindow(model string) int {
	m := strings.ToLower(model)
	if strings.Contains(m, "[1m]") || strings.HasSuffix(m, "-1m") || strings.Contains(m, "gemini") {
		return 1_000_000
	}
	return 200_000
}

// VentanaDe devuelve la ventana de contexto de un modelo. Si el catálogo
// del CLI trae el límite real, manda; si no, cae a la tabla. catalogo es
// la salida de un ejecutor que sabe leerlo (opencode lo da en `models
// --verbose`), indexada por el id tal como lo acepta el CLI
// ("provider/model") y también por el id pelado.
//
// ContextWindow sola sobreestima modelos chicos: con 200k por defecto,
// puedeReanudar creía que una sesión de 100k de un modelo de 128k cabía,
// y continuaba una conversación que el proveedor ya no sostiene.
func VentanaDe(model string, catalogo map[string]int) int {
	if catalogo != nil {
		if n := catalogo[model]; n > 0 {
			return n
		}
		// el catálogo indexa por "provider/model"; quien pregunta puede usar
		// el id pelado o el prefijo normalizado de opencode (google/gemini…).
		// Se compara el último segmento de ambos lados.
		base := model
		if i := strings.LastIndex(model, "/"); i >= 0 {
			base = model[i+1:]
		}
		for id, n := range catalogo {
			ultimo := id
			if i := strings.LastIndex(id, "/"); i >= 0 {
				ultimo = id[i+1:]
			}
			if ultimo == base && n > 0 {
				return n
			}
		}
	}
	return ContextWindow(model)
}

// ventanasPorCLI cachea por nombre de CLI el catálogo de ventanas que
// reporta su propio comando de modelos. `opencode models --verbose` tarda
// y devuelve siempre lo mismo dentro de una corrida: consultarlo por
// tarea multiplicaría esa latencia sin cambiar el resultado.
var (
	ventanasMu    sync.Mutex
	ventanasCache = map[string]map[string]int{}
)

// CatalogoVentanas consulta una vez por CLI las ventanas reales de sus
// modelos y devuelve el mapa cacheado. ex solo participa si expone
// Ventanas(ctx); cualquier otro ejecutor devuelve nil y el llamador cae a
// ContextWindow. El cache dura lo que el proceso: una corrida, no más.
func CatalogoVentanas(ctx context.Context, ex Executor) map[string]int {
	if ex == nil {
		return nil
	}
	// la clave incluye la ruta del binario: dos CLIs del mismo nombre en
	// PATH distintos (un fake de prueba, el real) no comparten catálogo.
	nombre := ex.Name()
	if bin, err := findBinary(nombre); err == nil {
		nombre += "\x00" + bin
	}
	ventanasMu.Lock()
	if v, ok := ventanasCache[nombre]; ok {
		ventanasMu.Unlock()
		return v
	}
	ventanasMu.Unlock()

	lector, ok := ex.(interface {
		Ventanas(context.Context) map[string]int
	})
	if !ok {
		ventanasMu.Lock()
		ventanasCache[nombre] = nil // negativo: no se reintenta por tarea
		ventanasMu.Unlock()
		return nil
	}
	v := lector.Ventanas(ctx)
	ventanasMu.Lock()
	ventanasCache[nombre] = v
	ventanasMu.Unlock()
	return v
}
