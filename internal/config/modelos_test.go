package config

import "testing"

func TestElegirModelos(t *testing.T) {
	// catálogo real de opencode: ids provider/model
	catalogo := []string{
		"opencode/big-pickle",
		"opencode/nemotron-3.5-lightning-free",
		"opencode-go/glm-5.2",
		"opencode-go/qwen3.7-max",
	}
	m := ElegirModelos(catalogo)
	if m["liviana"] != "opencode/nemotron-3.5-lightning-free" {
		t.Errorf("liviana = %q", m["liviana"])
	}
	if m["pesada"] != "opencode-go/qwen3.7-max" {
		t.Errorf("pesada = %q", m["pesada"])
	}
	if m["media"] == m["liviana"] || m["media"] == m["pesada"] {
		t.Errorf("media = %q, debe diferir de liviana y pesada", m["media"])
	}

	// alias de claude
	c := ElegirModelos([]string{"opus", "sonnet", "haiku"})
	if c["liviana"] != "haiku" || c["pesada"] != "opus" || c["media"] != "sonnet" {
		t.Errorf("claude = %+v", c)
	}

	// catálogo sin pistas: todo cae en el mismo, nunca vacío
	u := ElegirModelos([]string{"unico"})
	for _, peso := range Pesos {
		if u[peso] != "unico" {
			t.Errorf("%s = %q, quiero unico", peso, u[peso])
		}
	}

	// los preferidos ganan a las pistas por nombre
	og := ElegirModelos([]string{
		"opencode/muse-spark-1.3-contributor-free",
		"opencode-go/deepseek-v4-flash-vision-exp",
		"opencode-go/deepseek-v4-flash",
		"opencode-go/deepseek-v4-pro",
		"opencode-go/grok-4.7",
		"opencode-go/kimi-k3",
		"opencode-go/muse-spark-1.3-contributor",
	})
	if og["pesada"] != "opencode-go/kimi-k3" || og["media"] != "opencode-go/deepseek-v4-flash" || og["liviana"] != "opencode-go/muse-spark-1.3-contributor" {
		t.Errorf("preferidos = %+v", og)
	}

	// google propio gana en opencode si está configurado
	g := ElegirModelos([]string{
		"google/gemini-2.5-pro",
		"google/gemini-3.8-flash",
		"google/gemini-2.5-flash-lite",
		"opencode-go/kimi-k3",
	})
	if g["pesada"] != "google/gemini-2.5-pro" || g["media"] != "google/gemini-3.8-flash" || g["liviana"] != "google/gemini-2.5-flash-lite" {
		t.Errorf("google = %+v", g)
	}

	if ElegirModelos(nil) != nil {
		t.Error("sin catálogo no se inventa nada")
	}
}

func TestModelosValidos(t *testing.T) {
	catalogo := []string{"opencode-go/glm-5.2", "sonnet"}

	// el id inventado que rompía cada corrida
	malos := ModelosValidos([]string{"glm-5.2", "sonnet", "", "glm-5.2"}, catalogo)
	if len(malos) != 1 || malos[0] != "glm-5.2" {
		t.Errorf("malos = %v, quiero [glm-5.2] sin repetir ni contar el vacío", malos)
	}

	if malos := ModelosValidos([]string{"sonnet"}, catalogo); len(malos) != 0 {
		t.Errorf("malos = %v, quiero ninguno", malos)
	}

	// sin catálogo no se puede afirmar nada: nunca se acusa en falso
	if malos := ModelosValidos([]string{"loquesea"}, nil); len(malos) != 0 {
		t.Errorf("malos = %v, sin catálogo no se juzga", malos)
	}
}

func TestPlanificadorUsaModeloGrande(t *testing.T) {
	c := Config{Modelos: map[string]string{"liviana": "small", "pesada": "large"}}
	if got := ModeloRol(c, "planificador"); got != "large" {
		t.Fatalf("modelo = %q", got)
	}
	c.Proveedores = map[string]Proveedor{"planificador": {Modelo: "explicit"}}
	if got := ModeloRol(c, "planificador"); got != "explicit" {
		t.Fatalf("perdió proveedor explícito: %q", got)
	}
	c.Agentes = map[string]Agente{"planificador": {Modelo: "agent"}}
	if got := ModeloRol(c, "planificador"); got != "agent" {
		t.Fatalf("perdió agente explícito: %q", got)
	}
}

func TestRolRepetitivoNuncaCaeAlPesado(t *testing.T) {
	c := Config{Modelos: map[string]string{"liviana": "small", "media": "mid", "pesada": "large"}}
	if got := ModeloRol(c, "examinador"); got != "mid" {
		t.Fatalf("examinador = %q, quiere media", got)
	}
	if got := ModeloRol(c, "revisor"); got != "mid" {
		t.Fatalf("revisor = %q, quiere media", got)
	}
	// el planificador sigue siendo el único que por defecto cae al pesado
	if got := ModeloRol(c, "planificador"); got != "large" {
		t.Fatalf("planificador = %q, quiere pesada", got)
	}
}

func TestRolRepetitivoSinModelosQuedaVacio(t *testing.T) {
	c := Config{}
	if got := ModeloRol(c, "examinador"); got != "" {
		t.Fatalf("examinador sin modelos = %q, quiere vacío", got)
	}
	if got := ModeloRol(c, "revisor"); got != "" {
		t.Fatalf("revisor sin modelos = %q, quiere vacío", got)
	}
}
