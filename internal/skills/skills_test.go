package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, nombre, contenido string) {
	t.Helper()
	dir := filepath.Join(Dir(root), nombre)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(contenido), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadParseaFrontmatterYCuerpo(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "caveman", "---\nname: caveman\ndescription: >\n  habla breve\n---\n\nRespond terse.\n")

	sk, err := Read(root, "caveman")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Descripcion != "habla breve" {
		t.Errorf("Descripcion = %q, quiero %q", sk.Descripcion, "habla breve")
	}
	if sk.Cuerpo != "Respond terse." {
		t.Errorf("Cuerpo = %q, quiero %q", sk.Cuerpo, "Respond terse.")
	}
}

func TestInstalado(t *testing.T) {
	root := t.TempDir()
	if Instalado(root, "caveman") {
		t.Error("Instalado debió ser false sin fetch")
	}
	writeSkill(t, root, "caveman", "cuerpo")
	if !Instalado(root, "caveman") {
		t.Error("Instalado debió ser true tras escribir el SKILL.md")
	}
}

func TestContentSaltaSkillsFaltantesSinBloquear(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "clean-code", "---\nname: clean-code\n---\n\nfunciones chicas.")

	got := Content(root, []string{"clean-code", "no-existe"})
	if got == "" {
		t.Fatal("Content vacío, esperaba el cuerpo de clean-code")
	}
	if !strings.Contains(got, "funciones chicas.") {
		t.Errorf("Content sin el cuerpo instalado: %q", got)
	}
}

func TestContentVacioSinSkillsInstaladas(t *testing.T) {
	root := t.TempDir()
	if got := Content(root, []string{"no-existe"}); got != "" {
		t.Errorf("Content = %q, quiero vacío", got)
	}
}

// El catálogo es el diccionario de constructores (instalado o no) más lo
// instalado aparte, menos lo que la Base reemplaza y lo del orquestador.
func TestCatalogoSinLoQueReemplazaLaBase(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"implement", "caveman", "frontend-design", "clean-code"} {
		dir := filepath.Join(Dir(root), n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+n+"\ndescription: d "+n+"\n---\ncuerpo"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var nombres []string
	for _, sk := range Catalogo(root) {
		nombres = append(nombres, sk.Nombre)
	}
	if strings.Join(nombres, ",") != "ponytail,codebase-design,impeccable,tdd,agent-browser,clean-code,frontend-design" {
		t.Errorf("catálogo = %v", nombres)
	}
}

func TestTocaUI(t *testing.T) {
	if !TocaUI([]string{"frontend/src/pages/HomePage.tsx", "frontend/src/pages/HomePage.test.tsx"}) {
		t.Error("una página .tsx es interfaz")
	}
	if TocaUI([]string{"backend/app/rules/palette.py", "frontend/src/api/httpClient.ts"}) {
		t.Error("python y un cliente .ts no son interfaz")
	}
}

func TestContentQuitaSetupYComandosYTraeElPiso(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "diseno", "---\nname: diseno\n---\n\nPrincipios.\n\n## Setup\n\nCorre scripts/lanzador.\n\n## How to design\n\nLa brief manda.\n\n## Commands\n\n| cmd | ref |\n|---|---|\n| polish | reference/polish.md |\n")
	_ = os.MkdirAll(filepath.Join(Dir(root), "diseno", "reference"), 0o755)
	_ = os.WriteFile(filepath.Join(Dir(root), "diseno", "reference", "craft-floor.md"), []byte("# Craft floor\nContraste 4.5:1."), 0o644)

	got := Content(root, []string{"diseno"})
	for _, quiero := range []string{"Principios.", "La brief manda.", "Contraste 4.5:1."} {
		if !strings.Contains(got, quiero) {
			t.Errorf("falta %q: %q", quiero, got)
		}
	}
	for _, no := range []string{"lanzador", "polish", "## Commands"} {
		if strings.Contains(got, no) {
			t.Errorf("sobra %q: %q", no, got)
		}
	}
}

func TestDiccionarioUnaSkillPorNecesidad(t *testing.T) {
	if got := strings.Join(DeRol("backend"), ","); got != "ponytail,codebase-design" {
		t.Errorf("backend = %s", got)
	}
	if got := strings.Join(DeRol("tester"), ","); got != "tdd" {
		t.Errorf("tester = %s", got)
	}
	for _, s := range DefaultSources() {
		if s.Para == "" || s.Repo == "" {
			t.Errorf("%s sin Para o Repo: el planificador no sabría cuándo ponerla", s.Nombre)
		}
		if s.Orquestador && len(s.Roles) > 0 {
			t.Errorf("%s es del orquestador y no se inyecta a constructores", s.Nombre)
		}
	}
}
