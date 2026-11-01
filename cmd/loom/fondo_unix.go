//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// desprender pone al hijo en su propia sesión. Sin esto hereda la
// terminal de control del padre y el SIGHUP de cerrarla lo mata, que es
// justo lo que la bandera viene a evitar.
func desprender() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// avisarTamanio manda a c cada cambio de tamaño de la terminal.
func avisarTamanio(c chan os.Signal) { signal.Notify(c, syscall.SIGWINCH) }
