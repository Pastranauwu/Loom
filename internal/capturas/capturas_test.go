package capturas

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUnirYNombre(t *testing.T) {
	if got := unir("http://localhost:8000/", "/#/agregar"); got != "http://localhost:8000/#/agregar" {
		t.Errorf("unir = %q", got)
	}
	if nombre("/") != "inicio" || nombre("/#/agregar") != "agregar" {
		t.Errorf("nombres: %q %q", nombre("/"), nombre("/#/agregar"))
	}
}

func puertoLibre(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// levanta una app de verdad, la fotografía y la apaga
func TestTomarLevantaCapturaYApaga(t *testing.T) {
	if Navegador() == "" {
		t.Skip("sin navegador headless")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("sin python3")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1 style='background:#4FB3A2'>hola</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	puerto := puertoLibre(t)
	p := Pantallas{
		Levantar: "exec python3 -m http.server $PORT --bind 127.0.0.1", URL: "http://127.0.0.1:$PORT", Rutas: []string{"/"},
		Semilla: `echo "$BASE_URL" > semilla.txt`,
		Script:  `printf png > "$CAPTURAS/10-flujo.png"`,
	}
	fotos, err := Tomar(context.Background(), dir, p, puerto, nil, filepath.Join(dir, "fotos"))
	if err != nil {
		t.Fatal(err)
	}
	// una ruta en celular y escritorio, más lo que dejó el script
	if len(fotos) != 3 || filepath.Base(fotos[2]) != "10-flujo.png" {
		t.Fatalf("fotos = %v", fotos)
	}
	for _, f := range fotos[:2] {
		if info, err := os.Stat(f); err != nil || info.Size() == 0 {
			t.Fatalf("captura vacía %s: %v", f, err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "semilla.txt")); err != nil || len(b) == 0 {
		t.Fatalf("la semilla no corrió con la app levantada: %v", err)
	}
	// la app quedó apagada: el puerto se puede volver a tomar
	l, err := net.Listen("tcp", "127.0.0.1:"+itoa(puerto))
	if err != nil {
		t.Fatalf("la app sigue viva en %d: %v", puerto, err)
	}
	l.Close()
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestCompletarSumaLoQueFalta(t *testing.T) {
	cfg := Pantallas{Levantar: "a", URL: "u", Rutas: []string{"/"}}
	got, cambio := cfg.Completar(Pantallas{Levantar: "b", Script: "s"})
	if !cambio || got.Levantar != "a" || got.Script != "s" {
		t.Fatalf("%+v %v", got, cambio)
	}
}

// el script abría /agregar y caía en un 404. Sus capturas no se
// le muestran al revisor, y el error sirve para que el arquitecto lo arregle
func TestScriptQueFallaNoEntregaCapturasYSeReporta(t *testing.T) {
	if Navegador() == "" {
		t.Skip("sin navegador headless")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("sin python3")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hola</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Pantallas{
		Levantar: "exec python3 -m http.server $PORT --bind 127.0.0.1", URL: "http://127.0.0.1:$PORT", Rutas: []string{"/"},
		Script: `printf png > "$CAPTURAS/01-roto.png"; echo "TimeoutError: no encontré Elegir de la galería"; exit 1`,
	}
	fotos, err := Tomar(context.Background(), dir, p, puertoLibre(t), nil, filepath.Join(dir, "a"))
	if err != nil || len(fotos) != 2 {
		t.Fatalf("quiero solo las 2 de la ruta: %v %v", fotos, err)
	}
	err = ProbarFlujos(context.Background(), dir, p, puertoLibre(t), nil, filepath.Join(dir, "b"))
	if err == nil || !strings.Contains(err.Error(), "Elegir de la galería") {
		t.Fatalf("el error del script no llegó: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b", "flujos")); err == nil {
		t.Fatal("los PNG de la prueba del script quedaron en disco")
	}
}

func TestElegir(t *testing.T) {
	var fotos []string
	for i := 0; i < 20; i++ {
		fotos = append(fotos, fmt.Sprintf("/x/flujos/%02d.png", i))
	}
	fotos = append(fotos, "/x/00-muestra-celular.png", "/x/00-muestra-escritorio.png")
	got := Elegir(fotos, 5)
	if len(got) != 5 || got[0] != "/x/00-muestra-celular.png" || got[1] != "/x/00-muestra-escritorio.png" {
		t.Fatalf("la hoja de componentes va primero y el total es 5: %v", got)
	}
	// el resto se reparte, no son las primeras tres
	if got[2] != "/x/flujos/00.png" || got[4] == "/x/flujos/02.png" {
		t.Errorf("reparto = %v", got[2:])
	}
	if got := Elegir(fotos[:3], 5); len(got) != 3 {
		t.Errorf("con menos fotos que el tope se mandan todas: %v", got)
	}
	if (Pantallas{}).Tope() != DefaultMax || (Pantallas{Max: 9}).Tope() != 9 {
		t.Error("Tope")
	}
}

func TestSinRepetirQuitaLaRedireccionAlLogin(t *testing.T) {
	dir := t.TempDir()
	escribe := func(n, c string) string {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	fotos := []string{escribe("03-login.png", "A"), escribe("04-panel.png", "A"), escribe("05-bancos.png", "B")}
	u, r := SinRepetir(fotos)
	if len(u) != 2 || len(r) != 1 || r[0] != "04-panel.png" {
		t.Fatalf("SinRepetir = %v, %v", u, r)
	}
}
