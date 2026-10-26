package main

import (
	"os"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/idioma"
)

// idiomaDelProyecto reads language from the project's config.yml, if there
// is one, before anything is printed; LOOM_LANGUAGE still wins.
func idiomaDelProyecto() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	if root, err := config.RepoRoot(cwd); err == nil && config.Exists(root) {
		_, _ = config.Load(root) // Load sets the language
		return
	}
	idioma.Set("")
}
