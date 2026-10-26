package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/ui"
)

type generadorGuion struct {
	respuestas []string
	prompts    []string
}

func (g *generadorGuion) Generar(_ context.Context, prompt string) (string, error) {
	g.prompts = append(g.prompts, prompt)
	r := g.respuestas[0]
	g.respuestas = g.respuestas[1:]
	return r, nil
}

const preguntasFalsas = `{"questions":[{"question":"¿qué pasa al fallar?","why":"pierde datos"},{"question":"¿quién puede encolar?"}]}`
const specFalso = `{"feature":"cola","why":"segundo plano","requirements":["encola un trabajo","reintenta al fallar"],"rules":["Assumption: FIFO"],"acceptance":["x"]}`

func TestSketchEscribeElSpec(t *testing.T) {
	out = ui.New(io.Discard, false)
	root := t.TempDir()
	gen := &generadorGuion{respuestas: []string{preguntasFalsas, `{"feature":"cola","why":"segundo plano","requirements":["encola un trabajo","reintenta al fallar"],"rules":["Assumption: FIFO"],"acceptance":[{"criterion":"flujo","command":""}]}`}}
	entrada := strings.NewReader("reintenta 3 veces\n\ns\n")
	if err := runSketch(root, config.Config{}, gen, entrada, "una cola", "", false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "loom.spec.yml"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := spec.Parse(b)
	if err != nil || s.Feature != "cola" || len(s.Requirements) != 2 {
		t.Fatalf("spec escrito inválido: %v\n%s", err, b)
	}
	if !strings.Contains(gen.prompts[1], "A: reintenta 3 veces") || !strings.Contains(gen.prompts[1], "no answer") {
		t.Errorf("el segundo prompt debe llevar las respuestas y marcar la vacía:\n%s", gen.prompts[1])
	}
}

func TestSketchNoPisaYCancela(t *testing.T) {
	out = ui.New(io.Discard, false)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "loom.spec.yml"), []byte("feature: viejo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runSketch(root, config.Config{}, &generadorGuion{}, strings.NewReader(""), "x", "", false); err == nil {
		t.Fatal("debe negarse a pisar un spec existente")
	}
	gen := &generadorGuion{respuestas: []string{preguntasFalsas, specFalso}}
	if err := runSketch(root, config.Config{}, gen, strings.NewReader("a\nb\nn\n"), "x", "", true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "loom.spec.yml")); !strings.Contains(string(b), "viejo") {
		t.Error("con n no se escribe nada")
	}
}
