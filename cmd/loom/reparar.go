package main

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/ship"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

func newRepararCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reparar [id]",
		Short: "reabre la tarea que rompió la última entrega conjunta",
		Long: `Cuando ship --todas frena al probar el conjunto (integradas o aceptación),
reparar reabre la tarea responsable con el fallo en su contrato y la corre
de nuevo en su cuarto. Sin id elige la tarea que toca el código que cubre
la prueba que falló; con id reabre esa.

No se crea un agente nuevo: quien repara es la tarea que hizo el cambio,
con su contrato delante, y con modelo medio como mínimo. Decidir si está
mal el código o la prueba vieja no es trabajo para el modelo liviano.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			a, hay := ship.LeerAceptacion(root)
			if !hay || a.Fallo == nil {
				return errors.New("la última entrega no frenó al probar el conjunto · nada que reparar (corre loom ship --todas)")
			}
			dir := config.TasksDir(root)
			var entregadas []task.Task
			for _, id := range a.Tareas {
				if t, err := task.Load(dir, id); err == nil {
					entregadas = append(entregadas, t)
				}
			}
			var id string
			if len(args) == 1 {
				id = args[0]
				if err := validTaskID(id); err != nil {
					return err
				}
			} else {
				ids := responsables(entregadas, a.Fallo.Pruebas)
				switch len(ids) {
				case 1:
					id = ids[0]
				case 0:
					// no hay tarea que reabrir cuando lo que falta es la
					// prueba misma: nadie la escribió (se borró
					// la tarea final y la aceptación la seguía pidiendo)
					if f := task.PruebaSinEscribir(root, a.Fallo.Comando); f != "" {
						return fmt.Errorf("%s no existe y ninguna tarea la escribe · no hay nada que reabrir · crea la tarea que la escriba (loom task add) o quita `%s` de la aceptación en .loom/feature.json", f, a.Fallo.Comando)
					}
					return fmt.Errorf("no sé qué tarea rompió %s · %s · di cuál con loom reparar T-00N", a.Fallo.Comando, a.Fallo.Salida)
				default:
					return fmt.Errorf("varias tareas tocan ese código: %s · di cuál con loom reparar T-00N", strings.Join(ids, ", "))
				}
			}
			t, err := task.Load(dir, id)
			if err != nil {
				return err
			}
			st, err := state.Get(root, id)
			if err != nil {
				return err
			}
			if st.Estado != state.Lista {
				return fmt.Errorf("%s no está lista (estado %s) · reparar reabre tareas que ya habían salido verdes", id, st.Estado)
			}
			if err := task.Save(dir, reabierta(t, *a.Fallo)); err != nil {
				return err
			}
			if err := state.Save(root, state.State{ID: id, Estado: state.Pendiente, Rama: st.Rama, Puerto: st.Puerto, Commit: st.Commit}); err != nil {
				return err
			}
			out.Line("· %s reabierta con el fallo de %s en su contrato · se reusa su cuarto", id, a.Fallo.Paso)
			return runCmd(1, "", "", true, false)
		},
	}
}

// responsables son las tareas que tocan el código que cubre una prueba
// fallida: su archivo de código se llama como la prueba.
//
// ponytail: empareja por nombre de archivo (tutoriales.test.ts ↔
// tutoriales.ts). Una prueba que cubre varios módulos o que no se llama
// como ninguno no encuentra a nadie y hay que dar el id a mano. Seguir
// los imports de la prueba si esto se queda corto.
func responsables(tareas []task.Task, pruebas []string) []string {
	var ids []string
	for _, t := range tareas {
		for _, f := range t.TocarSolo {
			if task.EsArchivoDePrueba(f) {
				continue
			}
			for _, p := range pruebas {
				if raiz(f) == raiz(p) && !slices.Contains(ids, t.ID) {
					ids = append(ids, t.ID)
				}
			}
		}
	}
	if len(ids) > 0 {
		return ids
	}
	// ningún módulo se llama como la prueba (una de integración, de punta
	// a punta): responde la tarea que la escribió
	for _, t := range tareas {
		for _, p := range pruebas {
			if slices.Contains(t.TocarSolo, p) && !slices.Contains(ids, t.ID) {
				ids = append(ids, t.ID)
			}
		}
	}
	return ids
}

// raiz es el nombre de un archivo sin extensión ni marcas de prueba.
func raiz(f string) string {
	b := path.Base(f)
	b = strings.TrimSuffix(b, path.Ext(b))
	for _, s := range []string{".test", ".spec", "_test"} {
		b = strings.TrimSuffix(b, s)
	}
	return strings.TrimPrefix(b, "test_")
}

// reabierta es el contrato de una tarea que vuelve a correr para reparar
// lo que rompió al integrarse: el fallo entra a su listo_cuando y a sus
// notas, y las pruebas que fallaron entran a su alcance.
func reabierta(t task.Task, f ship.Fallo) task.Task {
	comando := f.Comando
	// la suite completa en el cuarto de una tarea puede fallar por lo que
	// todavía no tiene de sus hermanas: se corre solo lo que falló
	// en la forma que entiende su runner (Selectores); un fallo guardado
	// antes de que existieran solo trae los archivos
	solo := f.Selectores
	if len(solo) == 0 {
		solo = f.Pruebas
	}
	if len(solo) > 0 && len(task.ArchivosDePrueba(comando)) == 0 {
		comando += " " + strings.Join(solo, " ")
	}
	if !strings.Contains(t.ListoCuando, comando) {
		t.ListoCuando += " && (" + comando + ")"
	}
	for _, p := range f.Pruebas {
		if !slices.Contains(t.TocarSolo, p) {
			t.TocarSolo = append(t.TocarSolo, p)
		}
	}
	// decidir entre "mi código está mal" y "la prueba vieja quedó
	// obsoleta" pide leer el contrato con cuidado: no es del liviano
	if t.Peso == "" || t.Peso == "liviana" {
		t.Peso = "media"
	}
	t.Notas = strings.TrimSpace(t.Notas) + "\n\nREPARACIÓN. Tu trabajo salió verde solo, pero al integrarlo con las demás tareas falló `" + f.Comando + "`:\n" + fallaron(f) + f.Salida +
		"\nObsoleto: de las pruebas que ya existían (" + strings.Join(f.Pruebas, ", ") + ") solo lo que este contrato pidió cambiar de forma explícita. Si el contrato pidió ese cambio, actualiza únicamente esa aserción; si no lo pidió, la prueba tiene razón y lo que corriges es tu código. No borres ni relajes ningún otro caso."
	return t
}

// fallaron es la línea de las notas que nombra las pruebas que fallaron.
func fallaron(f ship.Fallo) string {
	if len(f.Nombres) == 0 {
		return ""
	}
	return "Pruebas que fallaron: " + strings.Join(f.Nombres, ", ") + "\n"
}
