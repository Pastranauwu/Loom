package ventanas

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Uso es la utilización real de la cuenta Claude que devuelve la sonda.
// Los campos son porcentajes (0-100) de las ventanas de la cuenta
// (subscription). nil = el proveedor no devolvió el dato.
type Uso struct {
	CincoH    *int      `json:"5h,omitempty"`
	Semanal   *int      `json:"semanal,omitempty"`
	Mensual   *int      `json:"mensual,omitempty"`
	Fecha     time.Time `json:"fecha"`
	Accesible bool      `json:"accesible"` // false si la cuenta no respondió utilización
}

// sondaModelo es el modelo con el que se hace la llamada mínima. El
// costo real es ~1 token de entrada; se puede forzar otro con
// LOOM_SONDA_MODELO si el default deja de existir.
func sondaModelo() string {
	if m := strings.TrimSpace(firstEnv("LOOM_SONDA_MODELO", "DEVCLEAN_SONDA_MODELO")); m != "" {
		return m
	}
	return "claude-3-5-haiku-latest"
}

// sondaURL es el endpoint de Anthropic. Var (no const) para poder
// apuntarlo a un servidor de prueba.
var sondaURL = "https://api.anthropic.com/v1/messages"

// SondaClaude consulta la utilización real de la cuenta con una llamada
// mínima y lee las cabeceras anthropic-ratelimit-unified-5h/7d-utilization.
// Requiere una API key (subscription OAuth no expone las cabeceras con una
// key API; en ese caso la sonda degrada a Accesible=false).
func SondaClaude(ctx context.Context, key string, timeout time.Duration) (Uso, error) {
	var u Uso
	u.Fecha = time.Now().UTC()
	if strings.TrimSpace(key) == "" {
		u.Accesible = false
		return u, nil
	}
	body, _ := json.Marshal(map[string]any{
		"model":      sondaModelo(),
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sondaURL, bytes.NewReader(body))
	if err != nil {
		return u, err
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		u.Accesible = false
		return u, nil // sin red: no es un error fatal, degrada al ledger
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	u.CincoH = pctHeader(resp.Header.Get("anthropic-ratelimit-unified-5h-utilization"))
	u.Semanal = pctHeader(resp.Header.Get("anthropic-ratelimit-unified-7d-utilization"))
	u.Accesible = u.CincoH != nil || u.Semanal != nil
	if !u.Accesible && resp.StatusCode != http.StatusOK {
		// 401/403 = key inválida, 429 = rate limit de la sonda misma
		u.Accesible = false
	}
	return u, nil
}

// pctHeader convierte el header (porcentaje o número) en int; nil si no
// vino o no es número.
func pctHeader(v string) *int {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if n, err := strconv.Atoi(v); err == nil {
		return &n
	}
	return nil
}

// cachePath es dónde vive la última lectura de la sonda. Var para poder
// apuntarla a un directorio de prueba.
var cachePathFunc = cachePathReal

func cachePath() string { return cachePathFunc() }

func cachePathReal() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return rutaCasa(home, "uso-claude.json")
}

// TTL de la caché: la sonda cuesta una llamada, board/standup no la
// repiten a cada refresco.
const ttlSonda = 5 * time.Minute

// SondaCached devuelve la utilización real, usando la caché si es fresca.
// force=true ignora la caché. Si no hay caché ni red, devuelve Uso vacío.
func SondaCached(ctx context.Context, key string, force bool) Uso {
	if !force {
		if u, ok := leerCache(); ok && time.Since(u.Fecha) < ttlSonda {
			return u
		}
	}
	u, _ := SondaClaude(ctx, key, 15*time.Second)
	_ = escribirCache(u)
	return u
}

func leerCache() (Uso, bool) {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return Uso{}, false
	}
	var u Uso
	if json.Unmarshal(data, &u) != nil {
		return Uso{}, false
	}
	return u, true
}

func escribirCache(u Uso) error {
	if err := os.MkdirAll(filepath.Dir(cachePath()), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath(), data, 0o644)
}

// KeyClaude busca la API key de Claude: primero la env var del rol
// claude en config, luego ANTHROPIC_API_KEY. Vacío si no hay.
func KeyClaude(keyEnv string) string {
	if keyEnv != "" {
		if k := os.Getenv(keyEnv); k != "" {
			return k
		}
	}
	return os.Getenv("ANTHROPIC_API_KEY")
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}
