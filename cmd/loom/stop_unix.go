//go:build !windows

package main

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// pararProceso manda SIGTERM al grupo de la corrida: se lleva también a
// los CLIs de agente que cuelgan de ella. Solo se apunta al grupo cuando
// la corrida lo encabeza (--background o un comando lanzado desde la shell);
// si no, se pararía también a quien la lanzó.
func pararProceso(pid int) error {
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
		return syscall.Kill(-pid, syscall.SIGTERM)
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

// esLoom confirma que el pid sigue vivo y es loom: un pid viejo
// puede haberlo reusado otro proceso.
//
// ponytail: mira /proc, que solo hay en Linux; en macOS basta con que el
// pid esté vivo. Leer el nombre con sysctl si alguna vez se cruza.
func esLoom(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return true
	}
	return strings.Contains(string(cmdline), "loom")
}
