package capturas

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCredencialesPorDefectoYUsaSesion(t *testing.T) {
	if c := (Pantallas{}).Credenciales(); !slices.Contains(c, "SCREENS_USER="+UsuarioCapturas) || !slices.Contains(c, "SCREENS_PASSWORD="+ClaveCapturas) {
		t.Fatalf("credenciales = %v", c)
	}
	if c := (Pantallas{Usuario: "a@b.c"}).Credenciales(); !slices.Contains(c, "SCREENS_USER=a@b.c") {
		t.Fatalf("la config no manda: %v", c)
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "capturas.mjs"), []byte("await page.fill('#email', process.env.SCREENS_USER)"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "sin.mjs"), []byte("await page.goto(base)"), 0o644)
	if !UsaSesion(dir, "node capturas.mjs") || UsaSesion(dir, "node sin.mjs") {
		t.Fatal("UsaSesion no lee el archivo que corre el script")
	}
}
