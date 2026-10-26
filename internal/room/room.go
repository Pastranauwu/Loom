// Package room manages the isolated workrooms: one git
// worktree per active task under Dir(root)/<id>/ (outside the project tree), on branch
// loom/<id>, with its own dependencies and port. Rooms are
// destroyed when the task ends.
package room

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/Pastranauwu/loom/internal/state"
)

// Room is an isolated worktree for one task.
type Room struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Rama   string `json:"rama"`
	Puerto int    `json:"puerto"`
	// Commit es el commit desde el que arrancó el cuarto, ya resuelto a
	// hash. Marca dónde empieza el trabajo propio de la tarea.
	Commit string `json:"commit,omitempty"`
}

// Dir es la carpeta de cuartos del repositorio en root. Vive FUERA del
// árbol del proyecto (~/.loom/rooms/<nombre>-<hash>): un cuarto es
// una copia entera del repo, y adentro cualquier herramienta que recorra
// el directorio (vitest, jest, linters, buscadores) la toma por código
// del proyecto. En una corrida real, `npx vitest run` en la raíz corría 305
// archivos de 30 cuartos, tardaba 10 veces más y fallaba con pruebas
// viejas. LOOM_ROOMS elige otra carpeta. Un repo que ya tiene
// cuartos en .loom/rooms los sigue usando hasta que se vacíen.
func Dir(root string) string {
	viejo := filepath.Join(root, ".loom", "rooms")
	if es, _ := os.ReadDir(viejo); len(es) > 0 {
		return viejo
	}
	base := os.Getenv("LOOM_ROOMS")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return viejo
		}
		base = filepath.Join(home, ".loom", "rooms")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	hash := sha1.Sum([]byte(abs))
	return filepath.Join(base, fmt.Sprintf("%s-%x", filepath.Base(abs), hash[:4]))
}

// Branch returns the branch name of a task's room.
func Branch(id string) string { return "loom/" + id }

// RamaExiste reporta si la rama de un cuarto sigue en el repo. El
// trabajo verde de una corrida anterior vive ahí hasta que `ship` lo
// entrega, así que una corrida nueva necesita saber si puede apoyarse
// en él.
func RamaExiste(ctx context.Context, root, id string) bool {
	_, err := git(ctx, root, "rev-parse", "--verify", "--quiet", Branch(id)+"^{commit}")
	return err == nil
}

// VerificarBase comprueba que la rama base sirva para crear cuartos y
// distingue los dos motivos por los que no: un repo sin commits se
// arregla con un commit inicial, pero una base que no existe (un
// `base: master` heredado en un repo cuya rama es `main`) no — y ese
// consejo equivocado dejaba la corrida entera detenida sin salida.
func VerificarBase(ctx context.Context, root, base string) error {
	if _, err := git(ctx, root, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err == nil {
		return nil
	}
	if _, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		return fmt.Errorf("la rama base %s no existe en este repo · corrige `base:` en .loom/config.yml", base)
	}
	return fmt.Errorf("no hay commits en %s · haz un commit inicial y reintenta", base)
}

// IntegrationBranch es la rama temporal donde se encadenan las oleadas:
// el trabajo verde de una oleada se mergea aquí y la siguiente oleada
// crea sus cuartos desde esta rama.
const IntegrationBranch = "loom/_integra"

// ResetIntegration borra la rama y el worktree de integración previos y
// los recrea desde base. Corre al inicio de una corrida por oleadas.
func ResetIntegration(ctx context.Context, root, base string) error {
	_ = Destroy(ctx, root, "_integra")
	if base == "" {
		base = "HEAD"
	}
	if err := VerificarBase(ctx, root, base); err != nil {
		return err
	}
	if err := ExcluirArtefactos(ctx, root); err != nil {
		return err
	}
	path := filepath.Join(Dir(root), "_integra")
	if out, err := git(ctx, root, "worktree", "add", path, "-b", IntegrationBranch, base); err != nil {
		return fmt.Errorf("no se pudo crear la rama de integración · %s", strings.TrimSpace(out))
	}
	return nil
}

// Integrar mergea la rama de una tarea verde en la rama de integración.
// Devuelve la salida del conflicto (vacía si el merge fue limpio) y un
// error si hubo conflicto u otro fallo.
func Integrar(ctx context.Context, root, id string) (string, error) {
	path := filepath.Join(Dir(root), "_integra")
	if _, err := os.Stat(path); err != nil {
		return "", errors.New("no hay rama de integración · vuelve a correr loom run")
	}
	msg := "loom: integra " + id
	cmd := exec.CommandContext(ctx, "git", "-c", "user.name=loom", "-c", "user.email=loom@local", "merge", "--no-ff", "-m", msg, Branch(id))
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	if err == nil {
		return "", nil
	}
	_, _ = git(ctx, path, "merge", "--abort")
	return strings.TrimSpace(string(out)), err
}

// Create sets up the room for a task: a worktree on a new branch from
// base, dependencies installed per manifest, and a free port assigned.
// On any failure the half-created room is destroyed.
func Create(ctx context.Context, root, id, base string) (Room, error) {
	r := Room{ID: id, Path: filepath.Join(Dir(root), id), Rama: Branch(id)}

	if _, err := os.Stat(r.Path); err == nil {
		return Room{}, fmt.Errorf("ya existe el cuarto %s · destrúyelo antes de reintentar", id)
	}
	// la carpeta del cuarto no está: limpia cualquier worktree o rama
	// que dejó una corrida anterior con rooms/ borrado, para que el
	// `worktree add` de abajo no choque. Create solo se llama sobre
	// tareas pendientes, así que una rama loom/<id> aquí es basura.
	if err := Destroy(ctx, root, id); err != nil {
		return Room{}, err
	}
	if base == "" {
		base = "HEAD"
	}
	// verificar la base antes: el mensaje de git varía con el idioma
	if err := VerificarBase(ctx, root, base); err != nil {
		return Room{}, err
	}
	if err := ExcluirArtefactos(ctx, root); err != nil {
		return Room{}, err
	}
	if out, err := git(ctx, root, "worktree", "add", r.Path, "-b", r.Rama, base); err != nil {
		return Room{}, fmt.Errorf("no se pudo crear el cuarto %s · %s", id, strings.TrimSpace(out))
	}
	if hash, err := git(ctx, root, "rev-parse", base+"^{commit}"); err == nil {
		r.Commit = strings.TrimSpace(hash)
	}

	fail := func(err error) (Room, error) {
		_ = Destroy(context.Background(), root, id)
		return Room{}, err
	}

	if err := InstalarDependencias(ctx, r.Path); err != nil {
		// el cuarto hereda un entorno que ya se verificó en el cuarto del
		// que parte: que pip no resuelva el manifiesto desde cero no
		// quiere decir que falte algo. Decide listo_cuando; queda rastro
		if !heredaDeCuarto(ctx, r.Path) {
			return fail(err)
		}
		rastro := filepath.Join(root, ".loom", "runs", id)
		if os.MkdirAll(rastro, 0o755) == nil {
			_ = os.WriteFile(filepath.Join(rastro, "entorno.log"), []byte(err.Error()+"\n"), 0o644)
		}
		if Aviso != nil {
			Aviso("las dependencias no instalan de cero en " + id + " · sigue con el entorno heredado · " + tail(err.Error()))
		}
	}
	puerto, err := freePort()
	if err != nil {
		return fail(err)
	}
	r.Puerto = puerto
	return r, nil
}

// Ensure devuelve el cuarto de una tarea, reusándolo si ya existe. Es lo
// que permite reintentar una tarea detenida sin tirar el trabajo parcial
// que quedó en su rama: el agente arranca viendo lo que ya escribió, en
// vez de rehacerlo desde cero y volver a pagar los mismos tokens.
func Ensure(ctx context.Context, root, id, base string) (Room, error) {
	r := Room{ID: id, Path: filepath.Join(Dir(root), id), Rama: Branch(id)}
	if _, err := os.Stat(r.Path); err != nil {
		return Create(ctx, root, id, base)
	}
	// la carpeta está, pero solo sirve si git la reconoce como worktree
	if _, err := git(ctx, r.Path, "rev-parse", "--git-dir"); err != nil {
		if err := Destroy(ctx, root, id); err != nil {
			return Room{}, err
		}
		return Create(ctx, root, id, base)
	}
	// un cuarto de antes de que se copiaran los .env tampoco los tiene
	traerEntorno(ctx, r.Path)
	// el cuarto se reusa, así que su punto de partida es el que ya
	// tenía: recalcularlo desde `base` contaría como propio el trabajo
	// que hereda de una oleada anterior
	if st, err := state.Get(root, id); err == nil && st.Commit != "" {
		r.Commit = st.Commit
	} else {
		// sin estado (el esqueleto lo guarda al terminar, o se borró
		// .loom/state): el punto de partida es donde la rama se
		// separó de la base. Vacío, `git diff "" HEAD` salía con 128.
		if base == "" {
			base = "HEAD"
		}
		if h, err := git(ctx, r.Path, "merge-base", base, "HEAD"); err == nil {
			r.Commit = strings.TrimSpace(h)
		}
	}
	// la base cambió desde que el cuarto arrancó (un arreglo fuera del
	// alcance de la tarea, verdes nuevos): el trabajo propio se reaplica
	// encima y el punto de partida pasa a ser la base nueva. Sin esto un
	// cuarto reusado nunca ve el arreglo y reintenta contra lo mismo. Si
	// no aplica limpio ni aplanado, el cuarto sigue como estaba y se avisa.
	if r.Commit != "" && base != "" {
		if _, err := git(ctx, r.Path, "diff", "--quiet", r.Commit, base); err != nil {
			_, err := git(ctx, r.Path, "-c", "user.name=loom", "-c", "user.email=loom@local", "rebase", "--onto", base, r.Commit)
			if err != nil {
				_, _ = git(ctx, r.Path, "rebase", "--abort")
				err = aplanarSobre(ctx, r.Path, id, base, r.Commit)
			}
			if err != nil {
				if Aviso != nil {
					Aviso("cuarto " + id + " choca con la base nueva y sigue sobre la anterior · no ve lo que cambió en " + base)
				}
			} else if h, err := git(ctx, r.Path, "rev-parse", base+"^{commit}"); err == nil {
				r.Commit = strings.TrimSpace(h)
			}
		}
	}
	puerto, err := freePort()
	if err != nil {
		return Room{}, err
	}
	r.Puerto = puerto
	return r, nil
}

// aplanarSobre pone sobre base el trabajo neto del cuarto (desde..HEAD)
// en un solo commit. Es la salida cuando el rebase choca en commits
// intermedios que el neto ya no trae: el agente toca un archivo fuera de
// alcance, la reversión lo deshace, y el arreglo de ese mismo archivo en
// la base choca con los dos. El árbol se calcula sin tocar el cuarto;
// solo se mueve si el neto aplica limpio y no hay cambios sin commitear.
func aplanarSobre(ctx context.Context, path, id, base, desde string) error {
	if _, err := git(ctx, path, "diff", "--quiet", "HEAD"); err != nil {
		return err
	}
	arbol, err := git(ctx, path, "merge-tree", "--write-tree", "--merge-base="+desde, base, "HEAD")
	if err != nil {
		return err
	}
	arbol, _, _ = strings.Cut(strings.TrimSpace(arbol), "\n")
	commit, err := git(ctx, path, "-c", "user.name=loom", "-c", "user.email=loom@local",
		"commit-tree", arbol, "-p", base, "-m", "wip: "+id+" sobre la base nueva")
	if err != nil {
		return err
	}
	_, err = git(ctx, path, "reset", "--hard", strings.TrimSpace(commit))
	return err
}

// Destroy removes the worktree and its branch. Missing pieces are
// skipped: Destroy always leaves a clean state, even when the room
// directory was wiped out of band. rooms/ is gitignored, so a `git
// clean -fdx` o un rm a mano dejan la registración del worktree y su
// rama huérfanas; sin limpiarlas, el próximo `worktree add` choca con
// "fatal: una rama llamada 'loom/<id>' ya existe".
func Destroy(ctx context.Context, root, id string) error {
	path := filepath.Join(Dir(root), id)
	// quita el worktree si su carpeta sigue ahí, luego poda las
	// registraciones que quedaron cuando la carpeta ya no está.
	_, _ = git(ctx, root, "worktree", "remove", "--force", path)
	_, _ = git(ctx, root, "worktree", "prune")
	// rev-parse no depende del idioma de git; `branch -D` sí.
	if _, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "refs/heads/"+Branch(id)); err != nil {
		return nil
	}
	if out, err := git(ctx, root, "branch", "-D", Branch(id)); err != nil {
		return fmt.Errorf("no se pudo borrar la rama %s · %s", Branch(id), strings.TrimSpace(out))
	}
	return nil
}

// Soltar quita la carpeta de un cuarto y deja su rama: es lo que se hace
// con la rama de entrega, que es el PR y tiene que seguir existiendo.
func Soltar(ctx context.Context, root, id string) {
	_, _ = git(ctx, root, "worktree", "remove", "--force", filepath.Join(Dir(root), id))
	_, _ = git(ctx, root, "worktree", "prune")
}

// freePort returns a port that was free a moment ago. Ports are
// assigned, never fixed.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// InstalarDependencias instala dependencias por cada manifiesto hasta
// dos niveles bajo path: npm install por package.json, go mod download
// por go.mod y un .venv por pyproject.toml o requirements.txt. Lo usa
// Create y también el nivel funcional de overlap, que monta un árbol
// fusionado en un worktree suelto y necesita las mismas dependencias.
//
// Mirar solo la raíz dejaba sin instalar un monorepo (frontend/ +
// backend/): las suites fallaban por dependencias o pasaban porque un
// agente había commiteado node_modules. Un package.json instalado no
// se vuelve a buscar debajo: los workspaces de npm los resuelve la raíz.
//
// heredarDe son ids de cuartos cuyo .venv se hereda aunque su rama no sea
// ancestro de path: la rama de entrega trae su trabajo por cherry-pick.
func InstalarDependencias(ctx context.Context, path string, heredarDe ...string) error {
	traerEntorno(ctx, path)
	for _, dir := range dirsManifiesto(path) {
		rel, _ := filepath.Rel(path, dir)
		if exists(filepath.Join(dir, "package.json")) && !bajoNode(path, dir) {
			if err := instalar(ctx, dir, rel, gestorNode(dir), "install"); err != nil {
				return err
			}
		}
		for _, m := range manifiestos {
			if exists(filepath.Join(dir, m.archivo)) {
				if err := instalar(ctx, dir, rel, m.args[0], m.args[1:]...); err != nil {
					return err
				}
			}
		}
		if exists(filepath.Join(dir, "pyproject.toml")) || exists(filepath.Join(dir, "requirements.txt")) {
			avisar("python", rel)
			// en fila: pip desempaca en /tmp (3 GB con torch) y seis cuartos
			// a la vez llenan un tmpfs. Lo que el cuarto hereda no se
			// reinstala (heredar); sin .venv en el repo sí va entero
			pipEnFila.Lock()
			err := venv(ctx, dir, heredarDe...)
			pipEnFila.Unlock()
			if err != nil {
				return fmt.Errorf("dependencias de python fallaron en %s · %s", rel, err)
			}
		}
	}
	return nil
}

// traerEntorno copia al worktree path los .env* que el repo principal
// tiene ignorados, hasta dos niveles: un worktree no los hereda y sin
// ellos la suite muere en la configuración (settings que lee os.environ,
// la base de datos) sin llegar a probar el código. Copia y no enlace: lo
// que el agente cambie se queda en el cuarto. Solo los ignorados, para
// que `git add -A` no los suba; lo que ya está en el cuarto no se pisa.
// Los de producción (deProduccion) se quedan fuera: el agente lee todo lo
// que hay en el cuarto y eso viaja al proveedor del modelo.
func traerEntorno(ctx context.Context, path string) {
	comun, err := git(ctx, path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return
	}
	principal := filepath.Dir(strings.TrimSpace(comun))
	for _, dir := range dirsManifiesto(principal) {
		archivos, _ := filepath.Glob(filepath.Join(dir, ".env*"))
		for _, origen := range archivos {
			rel, _ := filepath.Rel(principal, origen)
			destino := filepath.Join(path, rel)
			if exists(destino) {
				continue
			}
			if _, err := git(ctx, principal, "check-ignore", "-q", rel); err != nil {
				continue
			}
			if deProduccion(filepath.Base(rel)) {
				if Aviso != nil {
					Aviso("no copiado al cuarto · " + rel + " parece de producción · el agente lo podría leer")
				}
				continue
			}
			b, err := os.ReadFile(origen) // una carpeta .env falla aquí y se salta
			if err != nil || os.WriteFile(destino, b, 0o600) != nil {
				continue
			}
			if Aviso != nil {
				Aviso("variables de entorno copiadas al cuarto · " + rel)
			}
		}
	}
}

// deProduccion dice si un .env* nombra un ambiente real. Compara
// segmentos enteros: por subcadena, ".env.delivery" casaría con "live".
func deProduccion(nombre string) bool {
	for _, parte := range strings.Split(strings.ToLower(nombre), ".") {
		switch parte {
		case "prod", "production", "staging", "stage", "live":
			return true
		}
	}
	return false
}

var pipEnFila sync.Mutex

// manifiestos son los stacks que se instalan con un solo comando del
// toolchain. Python va aparte (venv) y node también (gestor por lockfile).
var manifiestos = []struct {
	archivo string
	args    []string
}{
	{"go.mod", []string{"go", "mod", "download"}},
	{"Cargo.toml", []string{"cargo", "fetch"}},
	{"Gemfile", []string{"bundle", "install"}},
	{"composer.json", []string{"composer", "install", "--no-interaction"}},
}

// gestorNode elige el gestor por el lockfile: npm install sobre un repo
// de pnpm o yarn ignora su lockfile y resuelve otras versiones.
func gestorNode(dir string) string {
	switch {
	case exists(filepath.Join(dir, "pnpm-lock.yaml")):
		return "pnpm"
	case exists(filepath.Join(dir, "yarn.lock")):
		return "yarn"
	case exists(filepath.Join(dir, "bun.lockb")), exists(filepath.Join(dir, "bun.lock")):
		return "bun"
	}
	return "npm"
}

func instalar(ctx context.Context, dir, rel, prog string, args ...string) error {
	if _, err := exec.LookPath(prog); err != nil {
		return fmt.Errorf("%s no está instalado y %s lo necesita · instálalo o borra el manifiesto", prog, valorRel(rel))
	}
	avisar(prog, rel)
	if out, err := run(ctx, dir, prog, args...); err != nil {
		return fmt.Errorf("%s %s falló en %s · %s", prog, strings.Join(args, " "), valorRel(rel), tail(out))
	}
	return nil
}

// Aviso recibe una línea antes de cada instalación: pip puede tardar
// minutos sin escribir nada y la corrida parece colgada. Nil por defecto;
// lo fija quien imprime en serie (el esqueleto), no la corrida en paralelo.
var Aviso func(string)

func avisar(prog, rel string) {
	if Aviso != nil {
		Aviso(fmt.Sprintf("instalando dependencias de %s en %s", prog, valorRel(rel)))
	}
}

func valorRel(rel string) string {
	if rel == "." || rel == "" {
		return "la raíz"
	}
	return rel
}

// Entorno devuelve el PATH con los bin de cada .venv del cuarto
// delante, para que `pytest` en listo_cuando y en el agente use las
// dependencias instaladas y no las del sistema. Vacío si no hay venv.
func Entorno(path string) []string {
	var bins []string
	for _, dir := range dirsManifiesto(path) {
		bin := filepath.Join(dir, ".venv", binVenv())
		if info, err := os.Stat(bin); err == nil && info.IsDir() {
			bins = append(bins, bin)
		}
	}
	if len(bins) == 0 {
		return nil
	}
	return []string{"PATH=" + strings.Join(append(bins, os.Getenv("PATH")), string(os.PathListSeparator))}
}

// venv crea dir/.venv con acceso a los paquetes del sistema (pytest
// global sigue a mano aunque el proyecto no lo declare) e instala las
// dependencias declaradas. Del pyproject se instalan solo las
// dependencias, no el proyecto: un layout plano con app/ y tests/ hace
// fallar `pip install -e .` por "multiple top-level packages".
func venv(ctx context.Context, dir string, heredarDe ...string) error {
	py := filepath.Join(dir, ".venv", binVenv(), "python")
	base := pythonDe(ctx, dir)
	// un cuarto reusado puede traer un .venv hecho con otro python que el
	// del repo: se rehace, o sus dependencias siguen sin instalar
	rehacer := exists(py) && base != pythonSistema() && versionPython(ctx, dir, py) != versionPython(ctx, dir, base)
	if rehacer || (!exists(py) && !exists(py+".exe")) {
		if out, err := run(ctx, dir, base, "-m", "venv", "--clear", "--system-site-packages", ".venv"); err != nil {
			return errors.New(tail(out))
		}
	}
	heredar(ctx, dir, py, heredarDe...)
	if exists(filepath.Join(dir, "requirements.txt")) {
		if err := pipInstall(ctx, dir, py, "-r", "requirements.txt"); err != nil {
			return err
		}
	}
	if !exists(filepath.Join(dir, "pyproject.toml")) {
		return nil
	}
	out, err := run(ctx, dir, py, "-c", depsPyproject)
	if err != nil {
		return errors.New(tail(out))
	}
	// primera línea: dependencias, obligatorias. Cada línea siguiente es
	// un extra: opcional por definición, se intenta y si no instala se
	// salta. En una corrida real, el arquitecto declaró un extra `jev` con un SDK que
	// no está en PyPI (y lo dejó comentado): exigirlo tiraba el esqueleto.
	grupos := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if deps := strings.Fields(grupos[0]); len(deps) > 0 {
		if err := pipInstall(ctx, dir, py, deps...); err != nil {
			return err
		}
	}
	for _, g := range grupos[1:] {
		if extra := strings.Fields(g); len(extra) > 0 {
			_ = pipInstall(ctx, dir, py, extra...)
		}
	}
	return nil
}

// pipInstall instala en el venv de py. Un .venv creado con uv (lo hacen
// los agentes) no trae pip: ahí se usa `uv pip`, y sin uv, ensurepip.
// Asumir pip tiró un esqueleto después de 39 minutos pagados.
func pipInstall(ctx context.Context, dir, py string, args ...string) error {
	if _, err := run(ctx, dir, py, "-m", "pip", "--version"); err != nil {
		if _, errUV := exec.LookPath("uv"); errUV == nil {
			if out, err := run(ctx, dir, "uv", append([]string{"pip", "install", "-q", "--python", py}, args...)...); err != nil {
				return errors.New(tail(out))
			}
			return nil
		}
		if out, err := run(ctx, dir, py, "-m", "ensurepip", "-q"); err != nil {
			return errors.New(tail(out))
		}
	}
	if out, err := run(ctx, dir, py, append([]string{"-m", "pip", "install", "-q", "--disable-pip-version-check"}, args...)...); err != nil {
		return errors.New(tail(out))
	}
	return nil
}

// depsPyproject imprime las dependencias de pyproject.toml en la primera
// línea y cada extra en una línea propia, separadas por espacio y sin
// espacios dentro (pip acepta "fastapi>=0.110").
const depsPyproject = `import tomllib
p = tomllib.load(open("pyproject.toml", "rb")).get("project", {})
grupos = [p.get("dependencies", [])] + list(p.get("optional-dependencies", {}).values())
print("\n".join(" ".join(x.replace(" ", "") for x in g) for g in grupos))`

func binVenv() string {
	if runtime.GOOS == "windows" {
		return "Scripts"
	}
	return "bin"
}

// pythonDe elige el intérprete con que se crea el .venv de dir: el del
// .venv que el repo principal ya tiene en esa misma carpeta, y si no hay,
// el del sistema. El cuarto es un worktree y no hereda el .venv (está
// ignorado); con el python del sistema, un requirements.txt con versiones
// fijadas para 3.12 no encuentra wheels en 3.14, compila y falla antes de
// llamar a ningún agente.
func pythonDe(ctx context.Context, dir string) string {
	comun, err := git(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	rel, err2 := git(ctx, dir, "rev-parse", "--show-prefix")
	if err == nil && err2 == nil {
		py := filepath.Join(filepath.Dir(strings.TrimSpace(comun)), strings.TrimSpace(rel), ".venv", binVenv(), "python")
		if exists(py) {
			return py
		}
	}
	return pythonSistema()
}

// pthHeredado es el archivo del .venv de un cuarto que suma los paquetes
// de otros entornos. site lo lee al arrancar el intérprete.
const pthHeredado = "_loom_heredado.pth"

// entornosBase lista los .venv de los que hereda el cuarto dir, del más
// cercano al más lejano: los de los cuartos cuyo trabajo ya está en este
// (el esqueleto, los verdes de oleadas anteriores) y el del repo principal.
// Son entornos donde ese código ya corrió. Los cuartos de heredarDe (ids)
// cuentan sin mirar la ascendencia.
func entornosBase(ctx context.Context, dir string, heredarDe ...string) (cuartos []string, principal string) {
	lista, err := git(ctx, dir, "worktree", "list", "--porcelain")
	propio, err2 := git(ctx, dir, "rev-parse", "--show-toplevel")
	rel, err3 := git(ctx, dir, "rev-parse", "--show-prefix")
	if err != nil || err2 != nil || err3 != nil {
		return nil, ""
	}
	propio, rel = strings.TrimSpace(propio), strings.TrimSpace(rel)
	primero := true
	for _, l := range strings.Split(lista, "\n") {
		wt, ok := strings.CutPrefix(l, "worktree ")
		if !ok {
			continue
		}
		py := filepath.Join(wt, rel, ".venv", binVenv(), "python")
		// el primero de la lista es siempre el repo principal
		if primero {
			primero = false
			if wt != propio && exists(py) {
				principal = py
			}
			continue
		}
		if wt == propio || !exists(py) {
			continue
		}
		if slices.Contains(heredarDe, filepath.Base(wt)) {
			cuartos = append(cuartos, py)
		} else if cabeza, err := git(ctx, wt, "rev-parse", "HEAD"); err == nil {
			if _, err := git(ctx, dir, "merge-base", "--is-ancestor", strings.TrimSpace(cabeza), "HEAD"); err == nil {
				cuartos = append(cuartos, py)
			}
		}
	}
	return cuartos, principal
}

// heredar deja en el .venv de dir un .pth con los paquetes de sus
// entornos base. Instalar de cero en cada cuarto repite minutos y gigas en
// un repo grande, y resuelve distinto que el entorno donde el código ya
// corre: un requirements.txt sin versiones fijas que instala bien encima
// del entorno del esqueleto puede retroceder, de cero, a una versión que
// no compila, y las tareas se detienen sin llamar a un agente. pip da por instalado
// lo heredado y solo trae lo que falta; lo que tenga que cambiar de versión
// lo instala en el cuarto, que va antes en sys.path, y no toca el entorno
// ajeno (lo trata como paquetes del sistema).
//
// ponytail: si se libera el cuarto del que se hereda antes que este, sus
// paquetes desaparecen; hoy los cuartos de un feature se liberan juntos.
func heredar(ctx context.Context, dir, py string, heredarDe ...string) {
	cuartos, principal := entornosBase(ctx, dir, heredarDe...)
	if principal != "" {
		cuartos = append(cuartos, principal)
	}
	const purelib = "import sysconfig; print(sysconfig.get_path('purelib'))"
	destino, err := run(ctx, dir, py, "-c", purelib)
	if err != nil {
		return
	}
	version := versionPython(ctx, dir, py)
	var rutas []string
	for _, base := range cuartos {
		// otra versión de python: sus extensiones compiladas no cargan
		if versionPython(ctx, dir, base) != version {
			continue
		}
		if sp, err := run(ctx, dir, base, "-c", purelib); err == nil {
			rutas = append(rutas, strings.TrimSpace(sp))
		}
	}
	pth := filepath.Join(strings.TrimSpace(destino), pthHeredado)
	if len(rutas) == 0 {
		_ = os.Remove(pth)
		return
	}
	_ = os.WriteFile(pth, []byte(strings.Join(rutas, "\n")+"\n"), 0o644)
}

// heredaDeCuarto reporta si algún .venv del cuarto hereda de otro cuarto,
// es decir, de un entorno que loom ya verificó.
func heredaDeCuarto(ctx context.Context, path string) bool {
	for _, dir := range dirsManifiesto(path) {
		if cuartos, _ := entornosBase(ctx, dir); len(cuartos) > 0 && exists(filepath.Join(dir, ".venv", binVenv(), "python")) {
			return true
		}
	}
	return false
}

// versionPython devuelve "3.12" para el intérprete py; vacío si no corre.
func versionPython(ctx context.Context, dir, py string) string {
	out, _ := run(ctx, dir, py, "-c", "import sys; print('%d.%d' % sys.version_info[:2])")
	return strings.TrimSpace(out)
}

func pythonSistema() string {
	if _, err := exec.LookPath("python3"); err == nil {
		return "python3"
	}
	return "python"
}

// dirsManifiesto lista path y sus subdirectorios hasta dos niveles,
// sin dependencias, artefactos ni carpetas ocultas.
func dirsManifiesto(path string) []string {
	dirs := []string{path}
	nivel := []string{path}
	for i := 0; i < 2; i++ {
		var siguiente []string
		for _, d := range nivel {
			entradas, err := os.ReadDir(d)
			if err != nil {
				continue
			}
			for _, e := range entradas {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || Artefacto(e.Name()) {
					continue
				}
				siguiente = append(siguiente, filepath.Join(d, e.Name()))
			}
		}
		dirs = append(dirs, siguiente...)
		nivel = siguiente
	}
	return dirs
}

// bajoNode reporta si algún ancestro de dir dentro de path ya tiene
// package.json: esos los instala la raíz (workspaces).
func bajoNode(path, dir string) bool {
	for d := filepath.Dir(dir); len(d) >= len(path) && d != dir; d = filepath.Dir(d) {
		if exists(filepath.Join(d, "package.json")) {
			return true
		}
		if d == path {
			break
		}
	}
	return false
}

// artefactos son carpetas que se generan o se instalan, nunca código:
// ni se versionan ni se buscan manifiestos adentro.
var artefactos = []string{"node_modules", ".venv", "venv", "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", "dist", "build", "target", ".next", ".nuxt", ".svelte-kit", "coverage", ".turbo", ".gradle", "vendor",
	// lo de loom que es de la máquina y no del plan: también para
	// repos que se iniciaron antes de que init lo ignorara
	".loom/rooms", ".loom/runs", ".loom/corridas"}

// Artefacto reporta si name es una carpeta de dependencias o de build.
func Artefacto(name string) bool {
	for _, a := range artefactos {
		if a == name {
			return true
		}
	}
	return false
}

// ExcluirArtefactos agrega las carpetas de dependencias y de build al
// info/exclude del repo, que comparten todos sus worktrees. El bucle
// indexa con `git add -A`: en un repo nuevo sin .gitignore, la tarea
// que corrió `npm install` commiteó 1.3M líneas de node_modules y esa
// basura viajó a cada cuarto, diff y revisor siguiente. info/exclude no
// se versiona, así que no ensucia el repo del usuario.
func ExcluirArtefactos(ctx context.Context, root string) error {
	dir, err := git(ctx, root, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("no se pudo ubicar el repo git · %s", strings.TrimSpace(dir))
	}
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	p := filepath.Join(dir, "info", "exclude")
	previo, _ := os.ReadFile(p)
	tiene := map[string]bool{}
	for _, l := range strings.Split(string(previo), "\n") {
		tiene[strings.TrimSpace(l)] = true
	}
	var faltan []string
	for _, a := range artefactos {
		if !tiene[a+"/"] {
			faltan = append(faltan, a+"/")
		}
	}
	if len(faltan) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	sep := ""
	if len(previo) > 0 && !strings.HasSuffix(string(previo), "\n") {
		sep = "\n"
	}
	_, err = f.WriteString(sep + "# loom: dependencias y artefactos de build\n" + strings.Join(faltan, "\n") + "\n")
	return err
}

func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	return run(ctx, dir, "git", args...)
}

func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// tail keeps the last lines of a command output for error messages.
func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " · ")
}
