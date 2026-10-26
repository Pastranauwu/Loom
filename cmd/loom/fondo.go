package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// FlagFondo es la bandera que pide desprender la corrida de la terminal.
const FlagFondo = "--background"

// CorridasDir es donde viven los registros de las corridas desprendidas.
// Aparte de runs/, que es por tarea: esto es la salida del comando entero.
func CorridasDir(root string) string {
	return filepath.Join(root, ".loom", "corridas")
}

// lanzarEnFondo vuelve a arrancar este mismo comando desprendido de la
// terminal y devuelve sin esperarlo.
//
// Existe porque una tarea de agente dura lo que dura: dejarla corriendo y
// cerrar la terminal mataba el proceso y con él la corrida. El trabajo ya
// hecho no se perdía —los `wip:` del cuarto quedan commiteados— pero la
// tarea se quedaba en `en_curso` y había que retomarla a mano.
//
// El entorno ya se preparó en el proceso padre, donde el humano todavía
// podía contestar preguntas; el hijo hereda un config.yml completo y por
// eso no necesita terminal. Su stdin es nil y su salida va al registro,
// así que esTUI() da falso solo y el hijo escribe en texto plano.
func lanzarEnFondo(root string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("no se pudo ubicar el binario para desprenderlo · %s", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(CorridasDir(root), 0o755); err != nil {
		return err
	}
	ruta := filepath.Join(CorridasDir(root), time.Now().Format("2006-01-02T15-04-05")+".log")
	registro, err := os.Create(ruta)
	if err != nil {
		return err
	}
	defer registro.Close() // el hijo se queda con su propio descriptor

	cmd := exec.Command(exe, sinFondo(os.Args[1:])...)
	cmd.Dir = cwd
	cmd.Stdin = nil
	cmd.Stdout = registro
	cmd.Stderr = registro
	cmd.SysProcAttr = desprender()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("no se pudo arrancar en segundo plano · %s", err)
	}
	pid := cmd.Process.Pid
	// soltarlo: sin esto el padre deja un zombi al salir
	_ = cmd.Process.Release()

	rel, err := filepath.Rel(cwd, ruta)
	if err != nil {
		rel = ruta
	}
	out.Line("corriendo en segundo plano · pid %d", pid)
	out.Line("registro · %s", rel)
	out.Line("avance · loom board · en vivo · tail -f %s", rel)
	out.Line("parar · loom stop · retomar después · loom run --retry")
	return nil
}

// sinFondo quita la bandera de la línea de argumentos con que se relanza
// el comando. Sin esto el hijo se desprendería otra vez, y otra, para
// siempre. Cubre las dos formas que acepta cobra: `--background` suelto y
// `--background=true`.
//
// ponytail: filtro textual, no un parseo real de la línea. El caso que
// se le escapa es `--titulo --background`, donde la bandera es en realidad el
// VALOR de otra opción y no debería quitarse. Titular un PR "--background" es
// bastante absurdo como para no pagar un parser completo; si algún día
// hay una opción que reciba valores así, esto pasa a leer la definición
// de banderas de cobra en vez de comparar texto.
func sinFondo(args []string) []string {
	fuera := make([]string, 0, len(args))
	for _, a := range args {
		if a == FlagFondo || strings.HasPrefix(a, FlagFondo+"=") || a == "--fondo" || strings.HasPrefix(a, "--fondo=") {
			continue
		}
		fuera = append(fuera, a)
	}
	return fuera
}
