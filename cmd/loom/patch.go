package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/task"
)

// topeParche es el tamaño máximo (old + new de todas las ediciones) y
// topeArchivosParche los archivos de un loom_patch: más que eso ya es una
// tarea y va por spec.
const (
	topeParche         = 6000
	topeArchivosParche = 5
)

type edicionParche struct{ Path, Old, New string }

// parchear es la vía rápida del conductor: aplica ediciones exactas en la
// rama actual, corre check y hace commit solo si sale verde; en rojo deja
// el árbol como estaba. Sin planificador, cuartos ni agentes.
func parchear(root string, a map[string]any) (string, error) {
	mensaje, check := str(a, "message"), str(a, "check")
	if mensaje == "" || check == "" {
		return "", errors.New("faltan message o check · sin comando que verifique no hay parche")
	}
	crudas, _ := a["edits"].([]any)
	var eds []edicionParche
	total := 0
	for _, c := range crudas {
		m, _ := c.(map[string]any)
		e := edicionParche{Path: str(m, "path")}
		e.Old, _ = m["old"].(string)
		e.New, _ = m["new"].(string)
		limpio := filepath.ToSlash(filepath.Clean(e.Path))
		if !filepath.IsLocal(e.Path) || limpio == ".git" || strings.HasPrefix(limpio, ".git/") || strings.HasPrefix(limpio, ".loom/") {
			return "", fmt.Errorf("ruta no permitida: %q · usa una ruta relativa al repo, fuera de .git y .loom", e.Path)
		}
		if e.Old == e.New {
			return "", fmt.Errorf("%s: old y new son iguales · no hay nada que cambiar", e.Path)
		}
		total += len(e.Old) + len(e.New)
		eds = append(eds, e)
	}
	if len(eds) == 0 {
		return "", errors.New("faltan edits · cada uno lleva path, old y new")
	}
	if total > topeParche || len(eds) > topeArchivosParche {
		return "", fmt.Errorf("parche de %d bytes en %d ediciones (tope %d y %d) · es una tarea: escríbela en el spec y usa loom_apply", total, len(eds), topeParche, topeArchivosParche)
	}
	// un commit en la base con verdes sin entregar choca en ship, con el
	// trabajo ya pagado; lo pendiente no cuenta, Ensure lo rebasea solo
	if e, _ := leerEstadoRepo(root); len(e.SinEntregar) > 0 {
		return "", fmt.Errorf("hay tareas verdes sin entregar (%s) · entrega con loom_ship y vuelve a parchear", strings.Join(e.SinEntregar, ", "))
	}
	rutas := make([]string, len(eds))
	for i, e := range eds {
		rutas[i] = e.Path
	}
	// sin cambios previos en esos archivos, deshacer es exacto
	if s, err := gitEn(root, append([]string{"status", "--porcelain", "--"}, rutas...)...); err != nil || strings.TrimSpace(s) != "" {
		return "", fmt.Errorf("hay cambios sin commit en los archivos del parche · haz commit o descártalos\n%s", strings.TrimSpace(s))
	}

	var nuevos []string
	deshacer := func() {
		for _, n := range nuevos {
			os.Remove(filepath.Join(root, n))
		}
		_, _ = gitEn(root, append([]string{"checkout", "--"}, rutas...)...)
	}
	for _, e := range eds {
		abs := filepath.Join(root, e.Path)
		previo, err := os.ReadFile(abs)
		var contenido string
		switch {
		case e.Old == "" && os.IsNotExist(err):
			nuevos = append(nuevos, e.Path)
			contenido = e.New
			err = os.MkdirAll(filepath.Dir(abs), 0o755)
		case e.Old == "":
			err = fmt.Errorf("%s ya existe · pasa en old el texto exacto a reemplazar", e.Path)
		case err == nil:
			if n := strings.Count(string(previo), e.Old); n != 1 {
				err = fmt.Errorf("%s: old aparece %d veces, debe aparecer una · copia más contexto", e.Path, n)
			}
			contenido = strings.Replace(string(previo), e.Old, e.New, 1)
		}
		if err == nil {
			err = os.WriteFile(abs, []byte(contenido), 0o644)
		}
		if err != nil {
			deshacer()
			return "", err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	salida, err := task.Shell(ctx, root, check, false).CombinedOutput()
	cola := strings.TrimSpace(string(salida))
	if len(cola) > 3000 {
		cola = cola[len(cola)-3000:]
	}
	if err != nil || loop.SinPruebas(string(salida)) {
		deshacer()
		if err == nil {
			err = errors.New("el comando salió con 0 sin correr ninguna prueba")
		}
		return "", fmt.Errorf("check en rojo, parche deshecho · %v\n%s", err, cola)
	}
	if s, err := gitEn(root, append([]string{"add", "--"}, rutas...)...); err != nil {
		deshacer()
		return "", fmt.Errorf("git add: %s", strings.TrimSpace(s))
	}
	if s, err := gitEn(root, append([]string{"commit", "--quiet", "-m", mensaje, "-m", "Agent: conductor", "--"}, rutas...)...); err != nil {
		_, _ = gitEn(root, append([]string{"reset", "--quiet", "--"}, rutas...)...)
		deshacer()
		return "", fmt.Errorf("git commit: %s", strings.TrimSpace(s))
	}
	sha, _ := gitEn(root, "rev-parse", "--short", "HEAD")
	return fmt.Sprintf("commit %s · %s · check verde\n%s", strings.TrimSpace(sha), mensaje, cola), nil
}
