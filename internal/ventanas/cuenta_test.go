package ventanas

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCuentaClaudeLeeElUsoDeLaSuscripcion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	exp := time.Now().Add(time.Hour).UnixMilli()
	cred := `{"claudeAiOauth":{"accessToken":"tok-1","expiresAt":` + itoa(exp) + `,"subscriptionType":"pro"}}`
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(cred), 0o600); err != nil {
		t.Fatal(err)
	}
	var auth, beta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, beta = r.Header.Get("Authorization"), r.Header.Get("anthropic-beta")
		w.Write([]byte(`{"five_hour":{"utilization":23.4,"resets_at":"2026-10-28T21:40:00.5+00:00"},"seven_day":{"utilization":3.0,"resets_at":"2026-11-04T13:00:00+00:00"},"seven_day_opus":null}`))
	}))
	defer srv.Close()
	viejo := usoOAuthURL
	usoOAuthURL = srv.URL
	defer func() { usoOAuthURL = viejo }()

	c := CuentaClaude(context.Background())
	if c.Motivo != "" || c.Plan != "pro" || len(c.Medidores) != 2 {
		t.Fatalf("cuenta = %+v", c)
	}
	if auth != "Bearer tok-1" || beta == "" {
		t.Errorf("headers: %q %q", auth, beta)
	}
	if m := c.Medidores[0]; m.Nombre != "5h" || m.Pct != 23 || m.Reinicia.IsZero() {
		t.Errorf("5h = %+v", m)
	}

	vencida := `{"claudeAiOauth":{"accessToken":"tok-1","expiresAt":1}}`
	_ = os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(vencida), 0o600)
	if c := CuentaClaude(context.Background()); c.Motivo == "" {
		t.Error("un token vencido no se usa ni se renueva")
	}
}

func TestCuentaCodexLeeLaUltimaSesion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	if c := CuentaCodex(); c.Motivo == "" {
		t.Error("sin sesiones: motivo")
	}
	ses := filepath.Join(dir, "sessions", "2026", "10", "08")
	_ = os.MkdirAll(ses, 0o755)
	lineas := `{"type":"session_meta"}
{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":5.0,"window_minutes":300,"resets_at":1794113144},"secondary":null,"plan_type":"plus"}}}
{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":9.0,"window_minutes":300,"resets_at":1794113144},"secondary":{"used_percent":41.6,"window_minutes":10080,"resets_at":1794113144},"plan_type":"plus"}}}
`
	_ = os.WriteFile(filepath.Join(ses, "rollout-2026-10-08T22-43-30-x.jsonl"), []byte(lineas), 0o644)
	c := CuentaCodex()
	if c.Motivo != "" || c.Plan != "plus" || len(c.Medidores) != 2 {
		t.Fatalf("cuenta = %+v", c)
	}
	if c.Medidores[0].Nombre != "5h" || c.Medidores[0].Pct != 9 || c.Medidores[1].Nombre != "semana" || c.Medidores[1].Pct != 42 {
		t.Errorf("medidores = %+v · manda la última línea", c.Medidores)
	}
}

func itoa(n int64) string {
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
