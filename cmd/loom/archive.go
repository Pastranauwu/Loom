package main

import (
	"context"
	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/historial"
	"github.com/Pastranauwu/loom/internal/spec"
)

func newArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive",
		Short: "archiva el spec probado en el historial y lo vacía",
		Long: `Guarda el spec actual en .loom/historial/NNNN-<feature>/ junto a su
resultado, agrega una línea a .loom/index.md, vacía el spec y hace un
commit. Solo archiva si la última entrega conjunta (ship --todas) pasó
todos los criterios de aceptación.

Se corre en la rama del PR, antes del merge.`,
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
			e, err := historial.Archivar(root, path)
			if err != nil {
				return err
			}
			if err := out.Data(map[string]string{"entrada": e.Nombre, "dir": e.Dir}); err != nil {
				return err
			}
			if len(e.Gitignore) > 0 {
				out.Line("· .gitignore: `.loom/` pasa a ignorar su contenido menos el historial:")
				for _, l := range e.Gitignore {
					out.Line("    %s", l)
				}
			}
			// el feature quedó entregado y archivado: sus cuartos (una
			// copia del repo con dependencias por tarea) ya no sirven
			if n := liberarCuartos(context.Background(), root, e.Tareas); n > 0 {
				out.Line("· %d cuartos liberados", n)
			}
			out.Line("✓ archivado %s · spec vacío para el siguiente feature · commit hecho", e.Nombre)
			return nil
		},
	}
}
