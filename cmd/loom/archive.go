package main

import (
	"context"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/spf13/cobra"
	"os/exec"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/historial"
	"github.com/Pastranauwu/loom/internal/ship"
	"github.com/Pastranauwu/loom/internal/spec"
)

func newArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive",
		Short: "archiva el spec probado en el historial y lo vacía",
		Long: `Guarda el spec actual en .loom/historial/NNNN-<feature>/ junto a su
resultado, agrega una línea a .loom/index.md, vacía el spec y hace un
commit. Solo archiva si la última entrega conjunta (ship --all) pasó
todos los criterios de aceptación.

Se corre en la rama del PR, antes del merge. Sin remoto (entrega local),
primero avanza la base hasta loom/_entrega en línea recta, sin merge.

Con release: <rama> en .loom/config.yml (p. ej. base: dev, release: main),
deja además en esa rama un solo commit con el árbol de la base, el tag de
versión (minor si hubo feat, patch si solo fix) y un resumen de mejoras y
reparaciones sacado de los Conventional Commits. La base guarda todo el
historial; la de releases, una línea por versión. No sube nada.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			path, err := spec.Find(root)
			if err != nil {
				return err
			}
			cfg, err := config.Load(root)
			if err != nil {
				return err
			}
			if cfg.Release != "" {
				if err := historial.PuedePublicar(root, cfg.Release); err != nil {
					return err
				}
			}
			// sin remoto la entrega es una rama local: la base la alcanza en
			// línea recta antes del commit del archivo, sin merge a mano
			if a, hay := ship.LeerAceptacion(root); hay && entregaLocal(root, cfg) {
				if ok, err := historial.Avanzar(root, a.Rama); err != nil {
					return err
				} else if ok {
					out.Line(idioma.T("· base avanzada hasta %s"), a.Rama)
				}
			}
			s, _ := spec.Load(path)
			e, err := historial.Archivar(root, path)
			if err != nil {
				return err
			}
			if err := out.Data(map[string]string{"entrada": e.Nombre, "dir": e.Dir}); err != nil {
				return err
			}
			if len(e.Gitignore) > 0 {
				out.Line(idioma.T("· .gitignore: `.loom/` pasa a ignorar su contenido menos el historial:"))
				for _, l := range e.Gitignore {
					out.Line("    %s", l)
				}
			}
			// el feature quedó entregado y archivado: sus cuartos (una
			// copia del repo con dependencias por tarea) ya no sirven
			if n := liberarCuartos(context.Background(), root, e.Tareas); n > 0 {
				out.Line(idioma.T("· %d cuartos liberados"), n)
			}
			// el tablero del siguiente feature empieza limpio: las tareas
			// entregadas se apartan junto a su historial
			if n, err := historial.Apartar(root, e.Nombre, e.Tareas); err != nil {
				out.Line(idioma.T("· no se pudieron apartar las tareas entregadas · %v · muévelas con loom task rm"), err)
			} else if n > 0 {
				out.Line(idioma.T("· %d tareas apartadas en %s"), n, historial.Apartados(root, e.Nombre))
			}
			out.Line(idioma.T("✓ archivado %s · spec vacío para el siguiente feature · commit hecho"), e.Nombre)
			if cfg.Release == "" {
				return nil
			}
			r, err := historial.Publicar(root, cfg.Release, s.Feature, s.Motivo)
			if err != nil {
				return fmt.Errorf(idioma.T("archivado, pero sin release · %w"), err)
			}
			out.Line(idioma.T("✓ release %s en %s · %d mejoras · %d reparaciones"), r.Tag, r.Rama, len(r.Mejoras), len(r.Reparaciones))
			if !entregaLocal(root, cfg) {
				out.Line(idioma.T("· publícala con git push origin %s %s"), r.Rama, r.Tag)
			}
			return nil
		},
	}
}

// entregaLocal: sin origin o con `entrega: local`, la entrega es una rama
// del repo y nadie la mergea por un PR.
func entregaLocal(root string, cfg config.Config) bool {
	return cfg.Entrega == config.EntregaLocal || exec.Command("git", "-C", root, "remote", "get-url", "origin").Run() != nil
}
