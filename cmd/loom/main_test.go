package main

import (
	"os"
	"testing"
)

// Los cuartos viven fuera del repo (room.Dir): en pruebas van a una
// carpeta temporal, no al ~/.loom/rooms de quien las corre.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "loom-rooms-")
	if err != nil {
		panic(err)
	}
	os.Setenv("LOOM_ROOMS", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
