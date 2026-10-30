// Package capturas levanta la interfaz web de un proyecto y la
// fotografía en tamaño celular. Las pruebas de UI solo pueden comprobar
// que una clase existe; si el efecto se ve o no lo juzga alguien que
// mira la pantalla: el arquitecto antes de planear, el revisor visual
// después de cada tarea de interfaz y el humano en el PR.
package capturas

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Pantallas dice cómo levantar la app y qué mirar. La declara el
// arquitecto y vive en config.yml.
type Pantallas struct {
	// Levantar sirve la app completa en $PORT (con build si hace falta).
	Levantar string `json:"levantar"`
	// URL es la raíz, con $PORT: "http://localhost:$PORT".
	URL string `json:"url"`
	// Rutas son las pantallas principales: "/", "/#/agregar".
	Rutas []string `json:"rutas"`
	// Semilla carga datos de ejemplo con la app ya levantada ($PORT):
	// sin ella solo se ven estados vacíos. Opcional.
	Semilla string `json:"semilla,omitempty"`
	// Script recorre flujos con un navegador (elegir una foto, llenar un
	// formulario) y deja PNG en $CAPTURAS; recibe también $BASE_URL. Las
	// pantallas que solo aparecen tras interactuar no tienen ruta. Opcional.
	Script string `json:"script,omitempty"`
	// Muestra es la ruta de una hoja de componentes: la identidad visual
	// (paleta, tipografía, botones, campos, tarjetas, tabla, alertas) y un
	// maquetado básico con todo junto. Se captura primero y es lo que el
	// revisor visual mira antes que cualquier pantalla. Opcional.
	Muestra string `json:"muestra,omitempty"`
	// Max es cuántas capturas ve el revisor visual por intento: cada
	// imagen se paga en tokens. 0 = DefaultMax.
	Max int `json:"max,omitempty"`
}

// DefaultMax es el tope de capturas por revisión visual. En una corrida
// real, 38 capturas en dos rondas fueron un tercio del gasto de la tarea.
const DefaultMax = 5

// Tope devuelve Max, o DefaultMax si no se declaró.
func (p Pantallas) Tope() int {
	if p.Max > 0 {
		return p.Max
	}
	return DefaultMax
}

// prefijoMuestra nombra las capturas de la hoja de componentes, para que
// Elegir las ponga primero.
const prefijoMuestra = "00-muestra-"

// Elegir deja como mucho n capturas: primero las de la hoja de
// componentes y, con los lugares que sobran, una muestra repartida del
// resto para cubrir pantallas distintas en vez de las primeras n.
func Elegir(fotos []string, n int) []string {
	if n <= 0 || len(fotos) <= n {
		return fotos
	}
	var muestra, resto []string
	for _, f := range fotos {
		if strings.HasPrefix(filepath.Base(f), prefijoMuestra) {
			muestra = append(muestra, f)
		} else {
			resto = append(resto, f)
		}
	}
	if len(muestra) >= n {
		return muestra[:n]
	}
	libres := n - len(muestra)
	out := muestra
	for i := 0; i < libres; i++ {
		out = append(out, resto[i*len(resto)/libres])
	}
	return out
}

// Completar llena lo que p no declara con lo de otra: la config manda,
// pero una semilla o un script nuevos del arquitecto se suman.
func (p Pantallas) Completar(o Pantallas) (Pantallas, bool) {
	cambio := false
	llenar := func(dst *string, v string) {
		if strings.TrimSpace(*dst) == "" && strings.TrimSpace(v) != "" {
			*dst, cambio = v, true
		}
	}
	llenar(&p.Levantar, o.Levantar)
	llenar(&p.URL, o.URL)
	llenar(&p.Semilla, o.Semilla)
	llenar(&p.Script, o.Script)
	llenar(&p.Muestra, o.Muestra)
	if len(p.Rutas) == 0 && len(o.Rutas) > 0 {
		p.Rutas, cambio = o.Rutas, true
	}
	return p, cambio
}

func (p Pantallas) Vacia() bool {
	return strings.TrimSpace(p.Levantar) == "" || strings.TrimSpace(p.URL) == "" || len(p.Rutas) == 0
}

// Tamanios son las pantallas en que se captura cada ruta: un celular
// (iPhone 14/15) y un escritorio. Una interfaz mobile-first también se
// abre en la compu, y ahí se ve distinto.
var Tamanios = []struct {
	Nombre      string
	Ancho, Alto int
}{{"celular", 390, 844}, {"escritorio", 1280, 800}}

// Espera es cuánto se le da a la app para responder: puede incluir un
// build de producción.
var Espera = 3 * time.Minute

// Navegador devuelve un navegador que sabe capturar en headless, o "".
// chrome-headless-shell (el de Playwright) primero: no abre perfil ni
// ventana. Brave queda fuera: en headless se cuelga con --screenshot.
func Navegador() string {
	casa, _ := os.UserHomeDir()
	for _, patron := range []string{
		filepath.Join(casa, ".cache", "ms-playwright", "chromium_headless_shell-*", "chrome-*", "chrome-headless-shell"),
		filepath.Join(casa, "Library", "Caches", "ms-playwright", "chromium_headless_shell-*", "chrome-*", "chrome-headless-shell"),
	} {
		if m, _ := filepath.Glob(patron); len(m) > 0 {
			return m[len(m)-1]
		}
	}
	for _, b := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "microsoft-edge"} {
		if p, err := exec.LookPath(b); err == nil {
			return p
		}
	}
	return ""
}

// Instalar trae chrome-headless-shell con Playwright (unos 100 MB, una
// vez por máquina). Sin node no hay forma.
func Instalar(ctx context.Context) error {
	if _, err := exec.LookPath("npx"); err != nil {
		return errors.New(idioma.T("sin navegador para capturas y sin npx para instalar uno · instala chromium o node"))
	}
	out, err := exec.CommandContext(ctx, "npx", "--yes", "playwright", "install", "chromium-headless-shell").CombinedOutput()
	if err != nil {
		return fmt.Errorf(idioma.T("no se pudo instalar chrome-headless-shell · %s"), ultimas(string(out)))
	}
	return nil
}

// Tomar levanta la app en dir con PORT=puerto, espera a que responda y
// captura cada ruta en outDir. La app se apaga al terminar, falle o no.
// Si el script de flujos falla, sus capturas se descartan: una página
// rota a mitad de flujo le haría rechazar al revisor algo que no es de
// la tarea (el script abría /agregar y caía en un 404).
func Tomar(ctx context.Context, dir string, p Pantallas, puerto int, env []string, outDir string) ([]string, error) {
	fotos, _, err := tomar(ctx, dir, p, puerto, env, outDir, true)
	return fotos, err
}

// ErrNoLevanta distingue la app que no arranca del script de flujos que
// falla: lo primero puede ser del entorno y no del arquitecto.
var ErrNoLevanta = errors.New(idioma.T("la app no respondió"))

// ProbarFlujos levanta la app y corre solo la semilla y el script de
// flujos: el error del script, con la cola de su salida, es lo que el
// arquitecto necesita para corregirlo antes de repartir tareas. Sus PNG
// se borran: son de la app en stubs (páginas en blanco) y el humano los
// tomaba por las capturas de la revisión. Quedan app.log y flujos.log.
func ProbarFlujos(ctx context.Context, dir string, p Pantallas, puerto int, env []string, outDir string) error {
	_, errScript, err := tomar(ctx, dir, p, puerto, env, outDir, false)
	_ = os.RemoveAll(filepath.Join(outDir, "flujos"))
	if err != nil {
		return err
	}
	return errScript
}

func tomar(ctx context.Context, dir string, p Pantallas, puerto int, env []string, outDir string, rutas bool) ([]string, error, error) {
	if p.Vacia() {
		return nil, nil, errors.New(idioma.T("sin pantallas declaradas"))
	}
	nav := Navegador()
	if nav == "" {
		return nil, nil, errors.New(idioma.T("sin navegador headless · corre npx playwright install chromium-headless-shell"))
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, nil, err
	}
	base := strings.ReplaceAll(p.URL, "$PORT", strconv.Itoa(puerto))
	app := exec.Command("sh", "-c", p.Levantar)
	if runtime.GOOS == "windows" {
		app = exec.Command("cmd", "/c", p.Levantar)
	}
	app.Dir = dir
	app.Env = append(append(os.Environ(), env...), "PORT="+strconv.Itoa(puerto))
	registro, err := os.Create(filepath.Join(outDir, "app.log"))
	if err != nil {
		return nil, nil, err
	}
	defer registro.Close()
	app.Stdout, app.Stderr = registro, registro
	enGrupo(app)
	if err := app.Start(); err != nil {
		return nil, nil, fmt.Errorf(idioma.T("no se pudo levantar la app · %w"), err)
	}
	defer apagar(app)

	if err := esperar(ctx, base, Espera); err != nil {
		return nil, nil, fmt.Errorf(idioma.T("%w en %s · %s · detalle en %s"), ErrNoLevanta, base, err, registro.Name())
	}
	envApp := append(append([]string{}, app.Env...), "BASE_URL="+base)
	// la semilla falla en abierto: sin datos se captura igual, vacío
	if strings.TrimSpace(p.Semilla) != "" {
		_ = auxiliar(ctx, dir, p.Semilla, envApp, registro, 2*time.Minute)
	}
	var fotos []string
	if rutas && strings.TrimSpace(p.Muestra) != "" {
		// la hoja que falla no frena las demás: sin ella, Elegir reparte
		for _, t := range Tamanios {
			foto := filepath.Join(outDir, prefijoMuestra+t.Nombre+".png")
			if err := capturar(ctx, nav, unir(base, p.Muestra), foto, t.Ancho, t.Alto); err != nil {
				fmt.Fprintf(registro, "loom: sin hoja de componentes en %s · %v\n", p.Muestra, err)
				continue
			}
			fotos = append(fotos, foto)
		}
	}
	for i, ruta := range p.Rutas {
		if !rutas {
			break
		}
		for _, t := range Tamanios {
			foto := filepath.Join(outDir, fmt.Sprintf("%02d-%s-%s.png", i+1, nombre(ruta), t.Nombre))
			if err := capturar(ctx, nav, unir(base, ruta), foto, t.Ancho, t.Alto); err != nil {
				return fotos, nil, fmt.Errorf(idioma.T("captura de %s · %w"), ruta, err)
			}
			fotos = append(fotos, foto)
		}
	}
	var errScript error
	if strings.TrimSpace(p.Script) != "" {
		flujos := filepath.Join(outDir, "flujos")
		if err := os.MkdirAll(flujos, 0o755); err != nil {
			return fotos, nil, err
		}
		salida, err := os.Create(filepath.Join(outDir, "flujos.log"))
		if err != nil {
			return fotos, nil, err
		}
		err = auxiliar(ctx, dir, p.Script, append(envApp, "CAPTURAS="+flujos), salida, 5*time.Minute)
		salida.Close()
		if err != nil {
			texto, _ := os.ReadFile(salida.Name())
			errScript = fmt.Errorf(idioma.T("el script de flujos falló (%v): %s"), err, ultimasN(string(texto), 12))
		} else {
			extra, _ := filepath.Glob(filepath.Join(flujos, "*.png"))
			sort.Strings(extra)
			fotos = append(fotos, extra...)
		}
	}
	return fotos, errScript, nil
}

// auxiliar corre la semilla o el script de flujos con la app levantada.
func auxiliar(ctx context.Context, dir, cmd string, env []string, log *os.File, tope time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, tope)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/c", cmd)
	}
	c.Dir, c.Env, c.Stdout, c.Stderr = dir, env, log, log
	return c.Run()
}

func esperar(ctx context.Context, url string, tope time.Duration) error {
	fin := time.Now().Add(tope)
	cliente := http.Client{Timeout: 3 * time.Second}
	for time.Now().Before(fin) {
		if r, err := cliente.Get(url); err == nil {
			r.Body.Close()
			if r.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return errors.New(idioma.T("se acabó la espera"))
}

func capturar(ctx context.Context, nav, url, foto string, ancho, alto int) error {
	perfil, err := os.MkdirTemp("", "loom-nav-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(perfil)
	// Sin sandbox: Ubuntu 24.04 veda los user namespaces por AppArmor y
	// chrome aborta al arrancar (core dumped). Playwright hace lo mismo;
	// solo abre la app del propio proyecto.
	args := []string{
		"--no-sandbox", "--disable-gpu", "--hide-scrollbars", "--no-first-run", "--no-default-browser-check",
		"--user-data-dir=" + perfil,
		fmt.Sprintf("--window-size=%d,%d", ancho, alto),
		"--virtual-time-budget=6000",
		"--screenshot=" + foto, url,
	}
	if !strings.Contains(filepath.Base(nav), "headless-shell") {
		args = append([]string{"--headless=new"}, args...)
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, nav, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v · %s", err, ultimas(string(out)))
	}
	if _, err := os.Stat(foto); err != nil {
		return errors.New(idioma.T("el navegador no escribió la captura"))
	}
	return nil
}

// unir pega la ruta a la raíz sin duplicar ni perder la barra.
func unir(base, ruta string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(ruta, "/")
}

var noNombre = regexp.MustCompile(`[^a-z0-9]+`)

func nombre(ruta string) string {
	n := strings.Trim(noNombre.ReplaceAllString(strings.ToLower(ruta), "-"), "-")
	if n == "" {
		return "inicio"
	}
	return n
}

func ultimasN(s string, n int) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

func ultimas(s string) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	if len(l) > 3 {
		l = l[len(l)-3:]
	}
	return strings.Join(l, " · ")
}

// SinRepetir quita las capturas idénticas byte a byte a una anterior y
// devuelve sus nombres: una ruta que redirige al login sale igual que el
// login, y juzgarla como si fuera su pantalla engaña al revisor.
func SinRepetir(fotos []string) (unicas, repetidas []string) {
	vistas := map[[32]byte]bool{}
	for _, f := range fotos {
		b, err := os.ReadFile(f)
		if err != nil {
			unicas = append(unicas, f)
			continue
		}
		h := sha256.Sum256(b)
		if vistas[h] {
			repetidas = append(repetidas, filepath.Base(f))
			continue
		}
		vistas[h] = true
		unicas = append(unicas, f)
	}
	return unicas, repetidas
}
