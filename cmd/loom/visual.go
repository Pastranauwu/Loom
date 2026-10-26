package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/capturas"
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/skills"
	"github.com/Pastranauwu/loom/internal/task"
)

// revisorVisualEnBucle levanta la interfaz del cuarto, la captura en
// tamaño celular y le pide a un modelo que ve imágenes que juzgue si se
// nota lo que la tarea pide. Las pruebas de UI solo saben que una clase
// existe: en una corrida real, 9 tareas "liquid glass" salieron verdes y en la
// pantalla no había vidrio. Degrada en abierto: sin capturas o sin
// respuesta, aprueba.
type revisorVisualEnBucle struct {
	ex        executor.Executor
	modelo    string
	root      string
	pantallas capturas.Pantallas
}

// revisorVisualPara arma el revisor visual con el rol `revisor_visual`, o
// el modelo pesado: el gusto lo juzga el mejor modelo, y el tope de
// críticas por tarea (loop.TopeVisual) acota el gasto. nil si el
// proyecto no declara cómo levantar su interfaz.
func revisorVisualPara(root string, cfg config.Config, ex executor.Executor) loop.RevisorVisual {
	if cfg.Pantallas.Vacia() || ex == nil {
		return nil
	}
	modelo := config.ModeloRol(cfg, "revisor_visual")
	if modelo == "" {
		modelo = cfg.ModeloPeso("pesada")
	}
	return revisorVisualEnBucle{ex: ejecutorPara(ex, modelo), modelo: modelo, root: root, pantallas: cfg.Pantallas}
}

func (r revisorVisualEnBucle) Revisar(ctx context.Context, cuarto room.Room, t task.Task, intento int) (bool, string, loop.Tokens) {
	dir := filepath.Join(loop.RunsDir(r.root), t.ID, fmt.Sprintf("visual-%d", intento))
	fotos := tomarCapturas(ctx, cuarto.Path, r.pantallas, cuarto.Puerto, dir)
	pide := t.Titulo
	if t.Porque != "" {
		pide += "\nPor qué: " + t.Porque
	}
	if n := plan.SepararNotas(t.Notas).Tarea; n != "" {
		pide += "\n" + n
	}
	return r.juzgar(ctx, cuarto.Path, fotos, pide)
}

// juzgar le muestra las capturas a un modelo que ve imágenes y le pide
// un veredicto contra lo pedido. Sin capturas o sin respuesta, aprueba.
func (r revisorVisualEnBucle) juzgar(ctx context.Context, dir string, fotos []string, pide string) (bool, string, loop.Tokens) {
	if len(fotos) == 0 {
		return true, "", loop.Tokens{}
	}
	res, err := r.ex.Run(ctx, executor.Request{
		Rol:      executor.RolVisual,
		RoomPath: dir,
		Prompt:   promptVisual(pide, fotos),
		Model:    r.modelo,
		Timeout:  5 * time.Minute,
	})
	tk := tokensDe(res.Tokens)
	if err != nil {
		return true, "", tk
	}
	cumple, cambios, ok := parseVisual(res.Text)
	if !ok || cumple {
		return true, "", tk
	}
	return false, strings.Join(cambios, "\n") + "\n(capturas en " + filepath.Dir(fotos[0]) + ")", tk
}

func promptVisual(pide string, fotos []string) string {
	var b strings.Builder
	b.WriteString("Eres el REVISOR VISUAL de loom. Abre estas capturas de la app en tamaño celular con tu herramienta para leer archivos. Son de ahora, del código actual:\n")
	for _, f := range fotos {
		b.WriteString("- " + f + "\n")
	}
	fmt.Fprintf(&b, "\nLo que se pidió:\n%s\n", pide)
	b.WriteString("\nCriterios de la interfaz:\n" + skills.UI + "\n")
	b.WriteString(`
Juzga SOLO lo que se ve en las capturas, no el código:
- ¿Se nota a simple vista lo que la tarea pide? Un efecto que casi no se ve cuenta como no hecho.
- ¿Algo se ve roto o sin terminar? Texto encimado o cortado, controles sin estilo, contraste ilegible, espacios descuadrados.
No pidas nada fuera de esta tarea ni cambios de gusto menores.

Responde al final solo con este JSON:
{"cumple": true, "cambios": []}
o, si no cumple, con cambios concretos y visuales (qué elemento, qué se ve hoy, cómo debe verse):
{"cumple": false, "cambios": ["..."]}
`)
	return b.String()
}

// parseVisual lee el último JSON con "cumple" de la respuesta.
func parseVisual(texto string) (cumple bool, cambios []string, ok bool) {
	i := strings.LastIndex(texto, `"cumple"`)
	if i < 0 {
		return false, nil, false
	}
	j := strings.LastIndex(texto[:i], "{")
	if j < 0 {
		return false, nil, false
	}
	var v struct {
		Cumple  bool     `json:"cumple"`
		Cambios []string `json:"cambios"`
	}
	if err := json.NewDecoder(strings.NewReader(texto[j:])).Decode(&v); err != nil {
		return false, nil, false
	}
	return v.Cumple, v.Cambios, true
}
