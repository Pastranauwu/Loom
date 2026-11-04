package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/tui"
)

const urlSimbolosNerd = "https://github.com/ryanoasis/nerd-fonts/releases/latest/download/NerdFontsSymbolsOnly.zip"

// asegurarFuente instala la fuente de símbolos Nerd para los iconos de
// loom ide si la terminal no tiene una. Es solo de símbolos: en Linux la
// terminal la toma como respaldo sin cambiar tu fuente. Se intenta una vez
// (marca en ~/.loom); si falla, avisa y la TUI sigue con símbolos Unicode.
func asegurarFuente() {
	casa, err := os.UserHomeDir()
	if err != nil || tui.HayNerdFont() {
		return
	}
	marca := filepath.Join(casa, ".loom", "fuente-nerd")
	if _, err := os.Stat(marca); err == nil {
		return
	}
	var dir string
	switch runtime.GOOS {
	case "linux":
		dir = filepath.Join(casa, ".local", "share", "fonts", "loom")
	case "darwin":
		dir = filepath.Join(casa, "Library", "Fonts")
	default:
		return
	}
	_ = os.MkdirAll(filepath.Dir(marca), 0o755)
	_ = os.WriteFile(marca, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644)
	out.Line("%s", idioma.T("· instalando Symbols Nerd Font para los iconos de loom ide…"))
	if err := instalarSimbolosNerd(urlSimbolosNerd, dir); err != nil {
		out.Line(idioma.T("· sin fuente de iconos (%v) · loom ide sigue con símbolos simples; LOOM_ICONS=nerd si ya tienes una Nerd Font"), err)
		return
	}
	if runtime.GOOS == "linux" {
		_ = exec.Command("fc-cache", "-f", dir).Run()
		out.Line("%s", idioma.T("· fuente de iconos instalada · tu terminal la usa como respaldo, sin cambiar tu fuente"))
		return
	}
	out.Line("%s", idioma.T("· fuente de iconos instalada · elígela como fuente de símbolos (non-ASCII) en tu terminal"))
}

func instalarSimbolosNerd(url, dir string) error {
	c := http.Client{Timeout: 60 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("descarga %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return err
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range z.File {
		// la Mono mide una celda por glifo: es la que no desarma los bordes
		if f.Name != "SymbolsNerdFontMono-Regular.ttf" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, f.Name), b, 0o644)
	}
	return fmt.Errorf("el zip no trae SymbolsNerdFontMono-Regular.ttf")
}
