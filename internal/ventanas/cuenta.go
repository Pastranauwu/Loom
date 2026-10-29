package ventanas

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Medidor es una ventana de la cuenta tal como la mide el proveedor.
type Medidor struct {
	Nombre   string    `json:"nombre"`
	Pct      int       `json:"pct"`
	Reinicia time.Time `json:"reinicia,omitempty"`
}

// Cuenta es el uso real de la cuenta de un CLI: lo mismo que muestra su
// /usage, no lo que loom contó. Motivo dice por qué no hay medidores.
type Cuenta struct {
	Proveedor string    `json:"proveedor"`
	Plan      string    `json:"plan,omitempty"`
	Medidores []Medidor `json:"medidores,omitempty"`
	Leido     time.Time `json:"leido"`
	Motivo    string    `json:"motivo,omitempty"`
	// Reintento es cuándo se puede volver a consultar tras un fallo pasajero
	// (429, sin red, 5xx). Antes de esa hora no se toca la red.
	Reintento time.Time `json:"reintento,omitempty"`
}

// usoOAuthURL es el endpoint del /usage de Claude Code. Var para pruebas.
var usoOAuthURL = "https://api.anthropic.com/api/oauth/usage"

// ttlCuenta acota las consultas: el panel redibuja cada 2 s y el endpoint
// responde 429 si se le pregunta seguido.
const ttlCuenta = 5 * time.Minute

type credencialClaude struct {
	ClaudeAiOauth struct {
		AccessToken      string `json:"accessToken"`
		ExpiresAt        int64  `json:"expiresAt"`
		SubscriptionType string `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// leerCredencialClaude lee la sesión que dejó `claude` al iniciar sesión.
// loom nunca la renueva: rotar el refresh token cerraría la sesión del CLI.
func leerCredencialClaude() (credencialClaude, error) {
	var c credencialClaude
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	data, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if err != nil && runtime.GOOS == "darwin" {
		data, err = exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
	}
	if err != nil {
		return c, errors.New("sin sesión de claude · inicia sesión con claude")
	}
	if err := json.Unmarshal(data, &c); err != nil || c.ClaudeAiOauth.AccessToken == "" {
		return c, errors.New("sin sesión de claude · inicia sesión con claude")
	}
	return c, nil
}

// CuentaClaude consulta el uso de la suscripción con el token de claude.
func CuentaClaude(ctx context.Context) Cuenta {
	c := Cuenta{Proveedor: "claude", Leido: time.Now()}
	cred, err := leerCredencialClaude()
	if err != nil {
		c.Motivo = err.Error()
		return c
	}
	c.Plan = cred.ClaudeAiOauth.SubscriptionType
	if exp := cred.ClaudeAiOauth.ExpiresAt; exp > 0 && time.UnixMilli(exp).Before(time.Now()) {
		c.Motivo = "sesión de claude vencida · ábrelo para renovarla"
		return c
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usoOAuthURL, nil)
	if err != nil {
		c.Motivo = err.Error()
		return c
	}
	req.Header.Set("Authorization", "Bearer "+cred.ClaudeAiOauth.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		c.Motivo, c.Reintento = "sin red", time.Now().Add(ttlCuenta)
		return c
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		c.Motivo = "sesión de claude vencida · ábrelo para renovarla"
		return c
	case resp.StatusCode == http.StatusTooManyRequests:
		c.Motivo = "claude limita la consulta de uso · reintenta en unos minutos"
		c.Reintento = time.Now().Add(ttlCuenta)
		if s, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && time.Duration(s)*time.Second > ttlCuenta {
			c.Reintento = time.Now().Add(time.Duration(s) * time.Second)
		}
		return c
	case resp.StatusCode >= 500:
		c.Motivo, c.Reintento = fmt.Sprintf("claude respondió %d", resp.StatusCode), time.Now().Add(ttlCuenta)
		return c
	case resp.StatusCode != http.StatusOK:
		c.Motivo = fmt.Sprintf("claude respondió %d", resp.StatusCode)
		return c
	}
	type ventana struct {
		Utilization *float64 `json:"utilization"`
		ResetsAt    string   `json:"resets_at"`
	}
	var r map[string]*ventana
	_ = json.NewDecoder(resp.Body).Decode(&r)
	for _, v := range []struct{ clave, nombre string }{{"five_hour", "5h"}, {"seven_day", "semana"}, {"seven_day_opus", "opus"}, {"seven_day_sonnet", "sonnet"}} {
		w := r[v.clave]
		if w == nil || w.Utilization == nil {
			continue
		}
		m := Medidor{Nombre: v.nombre, Pct: int(math.Round(*w.Utilization))}
		m.Reinicia, _ = time.Parse(time.RFC3339Nano, w.ResetsAt)
		c.Medidores = append(c.Medidores, m)
	}
	if len(c.Medidores) == 0 {
		c.Motivo = "claude no devolvió ventanas"
	}
	return c
}

// CuentaClaudeCached es CuentaClaude con una caché de cinco minutos en
// ~/.loom/cuenta-claude.json, compartida por el panel y loom usage. force
// salta la caché pero no el Reintento: tras un 429 no se insiste. Un fallo
// pasajero conserva la última lectura buena; su Leido dice de cuándo es.
func CuentaClaudeCached(ctx context.Context, force bool) Cuenta {
	ruta := rutaCasa(casa(), "cuenta-claude.json")
	var prev Cuenta
	if data, err := os.ReadFile(ruta); err == nil && json.Unmarshal(data, &prev) == nil {
		ahora := time.Now()
		if ahora.Before(prev.Reintento) || (!force && ahora.Sub(prev.Leido) < ttlCuenta) {
			return prev
		}
	}
	c := CuentaClaude(ctx)
	if !c.Reintento.IsZero() && len(prev.Medidores) > 0 {
		prev.Motivo, prev.Reintento = "", c.Reintento
		c = prev
	}
	if data, err := json.Marshal(c); err == nil && os.MkdirAll(filepath.Dir(ruta), 0o755) == nil {
		_ = os.WriteFile(ruta, data, 0o600)
	}
	return c
}

func casa() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return home
}

// CuentaCodex lee los límites que codex anota en su última sesión. Es una
// foto de cuando corrió, no una consulta: Leido dice de cuándo es.
func CuentaCodex() Cuenta {
	c := Cuenta{Proveedor: "codex"}
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		dir = filepath.Join(casa(), ".codex")
	}
	sesiones, _ := filepath.Glob(filepath.Join(dir, "sessions", "*", "*", "*", "*.jsonl"))
	sort.Strings(sesiones) // año/mes/día/rollout-<fecha>: el orden de nombre es el de tiempo
	for i := len(sesiones) - 1; i >= 0 && i >= len(sesiones)-5; i-- {
		if c2, ok := limitesCodex(sesiones[i]); ok {
			return c2
		}
	}
	c.Motivo = "sin sesiones de codex con límites"
	return c
}

func limitesCodex(ruta string) (Cuenta, bool) {
	f, err := os.Open(ruta)
	if err != nil {
		return Cuenta{}, false
	}
	defer f.Close()
	var ultima string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		if l := sc.Text(); strings.Contains(l, `"rate_limits":{`) {
			ultima = l
		}
	}
	i := strings.Index(ultima, `"rate_limits":`)
	if i < 0 {
		return Cuenta{}, false
	}
	type ventana struct {
		UsedPercent     float64 `json:"used_percent"`
		WindowMinutes   int     `json:"window_minutes"`
		ResetsAt        int64   `json:"resets_at"`
		ResetsInSeconds int64   `json:"resets_in_seconds"`
	}
	var rl struct {
		Primary   *ventana `json:"primary"`
		Secondary *ventana `json:"secondary"`
		PlanType  string   `json:"plan_type"`
	}
	if json.NewDecoder(strings.NewReader(ultima[i+len(`"rate_limits":`):])).Decode(&rl) != nil {
		return Cuenta{}, false
	}
	info, _ := os.Stat(ruta)
	c := Cuenta{Proveedor: "codex", Plan: rl.PlanType, Leido: info.ModTime()}
	for _, v := range []*ventana{rl.Primary, rl.Secondary} {
		if v == nil {
			continue
		}
		m := Medidor{Nombre: nombreVentana(v.WindowMinutes), Pct: int(math.Round(v.UsedPercent))}
		if v.ResetsAt > 0 {
			m.Reinicia = time.Unix(v.ResetsAt, 0)
		} else if v.ResetsInSeconds > 0 {
			m.Reinicia = c.Leido.Add(time.Duration(v.ResetsInSeconds) * time.Second)
		}
		c.Medidores = append(c.Medidores, m)
	}
	return c, len(c.Medidores) > 0
}

func nombreVentana(min int) string {
	switch {
	case min == 0:
		return "?"
	case min == 60*24*7:
		return "semana"
	case min%(60*24) == 0:
		return fmt.Sprintf("%dd", min/(60*24))
	case min%60 == 0:
		return fmt.Sprintf("%dh", min/60)
	}
	return fmt.Sprintf("%dm", min)
}
