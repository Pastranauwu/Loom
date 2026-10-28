package ship

import (
	"context"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/sealed"
	"github.com/Pastranauwu/loom/internal/task"
)

// verificarSuiteOculta runs the hidden test suite against the task's worktree.
// Returns:
//   - (nil, "", true) when there is no sealed suite — step skipped gracefully
//   - (&brecha, detalle, true) when hidden suite passed
//   - (&brecha, detalle, false) when hidden suite failed — block PR
func verificarSuiteOculta(ctx context.Context, root, roomPath string, t task.Task, pruebas string, timeout time.Duration) (brecha *float64, detalle string, ok bool) {
	s, err := sealed.Read(root, t.ID)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", true // no suite sealed, skip
	}
	if err != nil {
		return nil, "error leyendo suite sellada · " + err.Error(), false
	}

	hiddenPath := filepath.Join(roomPath, filepath.FromSlash(s.Archivo))
	if err := os.MkdirAll(filepath.Dir(hiddenPath), 0o755); err != nil {
		return nil, "no se pudo preparar la suite oculta · " + err.Error(), false
	}
	if err := os.WriteFile(hiddenPath, []byte(s.Content), 0o644); err != nil {
		return nil, "no se pudo escribir la suite oculta · " + err.Error(), false
	}
	// el cuarto no se queda con el examen; la suite sellada sigue en el
	// repo principal salvo que este examen la consuma (ver abajo)
	defer func() { _ = os.Remove(hiddenPath) }()

	salida, code := runComando(ctx, roomPath, pruebas, timeout)
	pasaron, fallaron := loop.ParseTestCounts(salida)
	brechaVal := calcularBrecha(root, t.ID, pasaron, fallaron)

	if code != nil && *code == 0 {
		// examen aprobado: se consume, no se vuelve a correr
		_ = sealed.Burn(root, t.ID)
		detalle = "suite oculta superada"
		if brechaVal != nil {
			detalle = fmt.Sprintf(idioma.T("suite oculta superada · brecha=%.1f%%"), *brechaVal)
		}
		return brechaVal, detalle, true
	}

	brechaStr := "sin datos"
	if brechaVal != nil {
		brechaStr = fmt.Sprintf("%.1f%%", *brechaVal)
	}
	// La suite NO se quema cuando falla: quemarla dejaba el paso saltado
	// en el siguiente `ship` —sin suite sellada se omite en silencio— y la
	// misma tarea que la compuerta acababa de frenar salía en un PR con
	// solo repetir el comando. Y con el examen borrado no quedaba nada que
	// mirar, así que el detalle también guarda la salida completa.
	// Una suite que no compila no juzga nada, y el implementador no puede
	// arreglarla: sin nombrar la salida, la tarea quedaba frenada para
	// siempre sin pista de qué borrar.
	if noCompila(salida, s.Archivo) {
		detalle = fmt.Sprintf(
			"la suite oculta no compila · %s · no juzga nada: borra %s y vuelve a correr la tarea",
			tail(salida), filepath.ToSlash(filepath.Join(".loom", "sealed", t.ID)),
		)
	} else {
		detalle = fmt.Sprintf(
			"suite oculta falló · brecha=%s · %s",
			// ponytail: --reexaminar flag not yet implemented, referenced for future UX
			brechaStr, tail(salida),
		)
	}
	if ruta := guardarSalidaOculta(root, t.ID, pruebas, salida); ruta != "" {
		detalle += " · detalle en " + ruta
	}
	return brechaVal, detalle, false
}

// noCompila reporta si el fallo es del propio examen y no de la
// implementación: el compilador nombra el archivo de la suite.
func noCompila(salida, archivo string) bool {
	base := filepath.Base(archivo)
	return base != "" && strings.Contains(salida, base) &&
		(strings.Contains(salida, "build failed") || strings.Contains(salida, "setup failed") || strings.Contains(salida, "could not compile"))
}

// guardarSalidaOculta deja la salida del examen oculto junto a los
// intentos de la tarea, que es donde el usuario ya busca el detalle de un
// fallo. Devuelve la ruta relativa al repo, o "" si no se pudo escribir:
// no tener el log no cambia el veredicto de la compuerta.
func guardarSalidaOculta(root, id, pruebas, salida string) string {
	dir := filepath.Join(loop.RunsDir(root), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	rel := filepath.Join(".loom", "runs", id, "suite-oculta.log")
	cuerpo := fmt.Sprintf("$ %s\n\n%s", pruebas, salida)
	if err := os.WriteFile(filepath.Join(root, rel), []byte(cuerpo), 0o644); err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// calcularBrecha computes visible_pct - hidden_pct.
// visible_pct comes from the last attempts.jsonl entry.
// Returns nil when either side has no data.
func calcularBrecha(root, id string, hiddenPasaron, hiddenFallaron *int) *float64 {
	if hiddenPasaron == nil && hiddenFallaron == nil {
		return nil
	}
	hiddenTotal := 0
	if hiddenPasaron != nil {
		hiddenTotal += *hiddenPasaron
	}
	if hiddenFallaron != nil {
		hiddenTotal += *hiddenFallaron
	}
	if hiddenTotal == 0 {
		return nil
	}
	hiddenPct := 0.0
	if hiddenPasaron != nil {
		hiddenPct = float64(*hiddenPasaron) / float64(hiddenTotal) * 100
	}

	as, err := loop.ReadAttempts(root, id)
	if err != nil || len(as) == 0 {
		return nil
	}
	last := as[len(as)-1]
	if last.TestsPasaron == nil {
		return nil
	}
	visibleTotal := 0
	if last.TestsPasaron != nil {
		visibleTotal += *last.TestsPasaron
	}
	if last.TestsFallaron != nil {
		visibleTotal += *last.TestsFallaron
	}
	if visibleTotal == 0 {
		return nil
	}
	visiblePct := float64(*last.TestsPasaron) / float64(visibleTotal) * 100
	b := visiblePct - hiddenPct
	return &b
}
