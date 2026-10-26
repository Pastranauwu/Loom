//go:build windows

package main

import "syscall"

// detachedProcess es DETACHED_PROCESS de la API de Windows: el hijo
// arranca sin heredar la consola del padre, que es el equivalente de
// setsid. No viene en el paquete syscall, así que se declara aquí.
//
// No se combina con CREATE_NEW_CONSOLE: la documentación de Microsoft
// dice que son excluyentes.
const detachedProcess = 0x00000008

// desprender arranca al hijo fuera de la consola del padre y en su
// propio grupo de procesos, para que cerrar la terminal no se lo lleve.
func desprender() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
