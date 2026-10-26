package main

import (
	"errors"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Pastranauwu/loom/internal/state"
	"github.com/spf13/cobra"
)

// corridaPath guarda el pid de la corrida viva (run o up), para que
// `loom stop` sepa a quién parar desde otra terminal o con la
// corrida en --background.
func corridaPath(root string) string {
	return filepath.Join(root, ".loom", "corrida.pid")
}

// marcarCorrida anota este proceso como la corrida viva y devuelve con
// qué soltarla. Si ya está anotado (up que llama a run), no hace nada:
// así el run de adentro no borra la marca antes de que up entregue.
func marcarCorrida(root string) func() {
	pid := strconv.Itoa(os.Getpid())
	if b, err := os.ReadFile(corridaPath(root)); err == nil && strings.TrimSpace(string(b)) == pid {
		return func() {}
	}
	if os.WriteFile(corridaPath(root), []byte(pid), 0o644) != nil {
		return func() {}
	}
	return func() {
		if b, err := os.ReadFile(corridaPath(root)); err == nil && strings.TrimSpace(string(b)) == pid {
			_ = os.Remove(corridaPath(root))
		}
	}
}

// otraCorridaViva reporta si hay otra corrida de loom trabajando en
// este repo: corrida.pid apunta a un proceso que existe y no es este.
func otraCorridaViva(root string) bool {
	b, err := os.ReadFile(corridaPath(root))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid == os.Getpid() {
		return false
	}
	return esLoom(pid)
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "para la corrida en curso y a sus agentes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			b, err := os.ReadFile(corridaPath(root))
			if errors.Is(err, os.ErrNotExist) {
				out.Line(idioma.T("no hay corrida en curso"))
				return nil
			}
			if err != nil {
				return err
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				_ = os.Remove(corridaPath(root))
				return errors.New(idioma.T("corrida.pid corrupto · se borró · no había nada que parar"))
			}
			if !esLoom(pid) {
				_ = os.Remove(corridaPath(root))
				out.Line(idioma.T("la corrida %d ya no existe · marca borrada"), pid)
				return nil
			}
			if err := pararProceso(pid); err != nil {
				return errors.New(idioma.T("no se pudo parar la corrida ") + strconv.Itoa(pid) + " · " + err.Error())
			}
			_ = os.Remove(corridaPath(root))

			// lo que estaba en curso queda detenido: sin esto el tablero
			// la pinta corriendo hasta que el latido se pone rancio
			var paradas []string
			estados, _ := state.List(root)
			for _, s := range estados {
				if s.Estado != state.EnCurso {
					continue
				}
				s.Estado, s.UltimoError = state.Detenida, "parada con loom stop"
				if state.Save(root, s) == nil {
					paradas = append(paradas, s.ID)
				}
			}
			out.Line(idioma.T("corrida %d parada"), pid)
			if len(paradas) > 0 {
				out.Line(idioma.T("detenidas · %s · retómalas con loom run --retry"), strings.Join(paradas, ", "))
			}
			return nil
		},
	}
}

// tomarEntrega reserva la entrega del repo para este proceso. Dos `ship`
// a la vez rehacían el mismo cuarto de entrega uno debajo del otro, y el
// segundo corría las pruebas sobre un árbol a medio montar. Una marca de
// un proceso que ya murió no cuenta: se toma encima.
func tomarEntrega(root string) (func(), error) {
	p := filepath.Join(root, ".loom", "entrega.pid")
	pid := strconv.Itoa(os.Getpid())
	if b, err := os.ReadFile(p); err == nil {
		otro := strings.TrimSpace(string(b))
		if otro == pid {
			return func() {}, nil
		}
		if n, err := strconv.Atoi(otro); err == nil && esLoom(n) {
			return nil, errors.New(idioma.T("ya hay una entrega en curso (pid ") + otro + ") · espera a que termine o párala con loom stop")
		}
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, []byte(pid), 0o644); err != nil {
		return nil, err
	}
	return func() {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) == pid {
			_ = os.Remove(p)
		}
	}, nil
}
