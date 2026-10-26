//go:build windows

package main

import "os"

// pararProceso mata la corrida.
//
// ponytail: Windows no tiene grupos de señal; los CLIs de agente que ya
// estaban corriendo quedan huérfanos hasta que terminen su intento. Usar
// job objects si hace falta cortarlos al instante.
func pararProceso(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

func esLoom(pid int) bool {
	_, err := os.FindProcess(pid)
	return err == nil
}
