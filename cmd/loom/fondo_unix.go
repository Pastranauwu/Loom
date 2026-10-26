//go:build !windows

package main

import "syscall"

// desprender pone al hijo en su propia sesión. Sin esto hereda la
// terminal de control del padre y el SIGHUP de cerrarla lo mata, que es
// justo lo que la bandera viene a evitar.
func desprender() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
