package main

import (
	"context"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/executor"
)

type cliFalso struct{ catalogo []string }

func (cliFalso) Name() string     { return "claude" }
func (cliFalso) Available() error { return nil }
func (cliFalso) Run(context.Context, executor.Request) (executor.Result, error) {
	return executor.Result{}, nil
}
func (c cliFalso) Models(context.Context) ([]string, error) { return c.catalogo, nil }

// un modelo desconocido reasignaba los tres y la pesada pasaba
// de sonnet a opus. Solo se toca el que no existe.
func TestRevisarModelosSoloReemplazaElInvalido(t *testing.T) {
	cat := []string{"claude-opus-5-5", "claude-sonnet-5", "claude-haiku-4-5", "opus", "sonnet", "haiku"}
	cfg := config.Config{Modelos: map[string]string{"liviana": "haiku", "media": "no-existe", "pesada": "sonnet"}}
	cambios := revisarModelos(&cfg, cliFalso{cat}, cat)
	if len(cambios) != 1 {
		t.Fatalf("cambios = %v", cambios)
	}
	if cfg.Modelos["liviana"] != "haiku" || cfg.Modelos["pesada"] != "sonnet" || cfg.Modelos["media"] == "no-existe" {
		t.Fatalf("modelos = %v", cfg.Modelos)
	}
}
