package sealed

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteYReadDanLaVuelta(t *testing.T) {
	root := t.TempDir()
	s := SuiteOculta{
		Content:        "func TestOculto(t *testing.T) {}",
		Archivo:        "internal/export/oculto_test.go",
		Visible:        "func TestVisible(t *testing.T) {}",
		ArchivoVisible: "internal/export/visible_test.go",
	}
	if err := Write(root, "T-001", s); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !Exists(root, "T-001") {
		t.Error("Exists dijo que no después de Write")
	}
	got, err := Read(root, "T-001")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Content != s.Content || got.Archivo != s.Archivo {
		t.Errorf("Read = %+v", got)
	}
	// visible y archivo_visible viajan igual aunque queden fuera del hash
	if got.Visible != s.Visible || got.ArchivoVisible != s.ArchivoVisible {
		t.Errorf("la suite visible no dio la vuelta: %+v", got)
	}
	// Write calcula el hash; el llamador no lo pone
	if got.Hash == "" {
		t.Error("Write no calculó el hash")
	}
}

// La suite sellada vive en el repo principal, NUNCA en el cuarto: el
// cuarto es dominio del implementador y ahí la vería.
func TestElDirectorioSelladoNoEstaEnElCuarto(t *testing.T) {
	d := Dir("/repo", "T-001")
	if d != filepath.Join("/repo", ".loom", "sealed", "T-001") {
		t.Errorf("Dir = %q", d)
	}
	if strings.Contains(d, "rooms") {
		t.Errorf("el directorio sellado cayó dentro del cuarto: %q", d)
	}
}

func TestReadSinSellarDiceQueNoExiste(t *testing.T) {
	if _, err := Read(t.TempDir(), "T-404"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read sin sellar = %v, quiero os.ErrNotExist", err)
	}
}

// El hash detecta corrupción accidental, que es para lo único que está
// (ver el comentario de Read). Si alguien edita el contenido sin tocar el
// hash, Read no devuelve una suite a medias.
func TestContenidoCambiadoSinTocarElHashNoPasa(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, "T-001", SuiteOculta{Content: "original"}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(Dir(root, "T-001"), fileName)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var s SuiteOculta
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	s.Content = "otra cosa" // el hash se queda con el del original
	nuevo, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nuevo, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(root, "T-001"); err == nil {
		t.Error("Read aceptó una suite con el hash desalineado")
	}
}

func TestBurnBorraElDirectorioYEsIdempotente(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, "T-001", SuiteOculta{Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := Burn(root, "T-001"); err != nil {
		t.Fatalf("Burn: %v", err)
	}
	if Exists(root, "T-001") {
		t.Error("Exists dijo que sí después de Burn")
	}
	// ship puede llamarlo dos veces; la segunda no debe fallar
	if err := Burn(root, "T-001"); err != nil {
		t.Errorf("Burn dos veces: %v", err)
	}
}
