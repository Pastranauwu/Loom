package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallShSugiereDoctorAntesQueInit(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	texto := string(data)
	doctor := strings.Index(texto, "$bin doctor")
	if doctor < 0 {
		t.Fatal("install.sh debe sugerir doctor como siguiente paso")
	}
	init := strings.Index(texto, "$bin init")
	if init < 0 {
		t.Fatal("install.sh debe mencionar init después de doctor")
	}
	if init < doctor {
		t.Fatalf("init aparece antes que doctor (init=%d doctor=%d)", init, doctor)
	}
}
