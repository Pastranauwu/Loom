#!/bin/sh
# Demo reproducible de loom, sin gastar tokens: un agente falso que
# resuelve las tareas. Para grabar el GIF:
#   vhs docs/demo.tape
# Para una demo con agentes reales, quita FAKE=1 y usa --ejecutor opencode.
set -e

# ruta absoluta del binario: sobrevive al `cd "$repo"` de más abajo
raiz="$(cd "$(dirname "$0")/.." && pwd)"
bin="${LOOM_BIN:-$raiz/loom}"
# siempre recompila: go build es incremental y un binario viejo grababa
# una demo que ya no corresponde al código del repo
if [ -z "$LOOM_BIN" ]; then
  echo "compilando loom..."
  (cd "$raiz" && go build -o loom ./cmd/loom) || exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# HOME aislado: el ledger de gasto vive en $HOME/.loom/ventanas.jsonl y
# es global al usuario, no por repo. Sin esto la demo — agente falso, cero
# tokens reales — escribía tokens inventados en el ledger de quien la corre,
# y sobre ese ledger se calculan `loom usage` y los topes de
# `presupuesto:` que deciden si el trabajo real puede seguir. Mismo aislamiento
# que ya hacía scripts/demo-env.sh.
export HOME="$tmp/home"
mkdir -p "$HOME"
cat > "$HOME/.gitconfig" <<'GITCFG'
[user]
	name = loom demo
	email = demo@loom.local
[init]
	defaultBranch = main
GITCFG

# agente falso: escribe el archivo que pide cada listo_cuando
mkdir -p "$tmp/bin"
cat > "$tmp/bin/opencode" <<'EOF'
#!/bin/sh
if [ "$1" = "--version" ]; then echo "0.1.0"; exit 0; fi
# catalogo de modelos: sin esto init guardaba la salida JSON del agente
# como id de modelo y la corrida lo imprimia como nombre
if [ "$1" = "models" ]; then
  printf '%s\n' anthropic/claude-haiku-4-5 anthropic/claude-sonnet-4-5 anthropic/claude-opus-4-1
  exit 0
fi
dir="."; prev=""
for a in "$@"; do [ "$prev" = "--dir" ] && dir="$a"; prev="$a"; done
case "$2" in
  *"planificador de loom"*)
    printf '%s\n' '{"type":"message","part":{"type":"text","text":"[{\"titulo\":\"exportar clientes a CSV\",\"listo_cuando\":\"test -f src/export.go\",\"tocar_solo\":[\"src/**\"]},{\"titulo\":\"documentar la API\",\"listo_cuando\":\"test -f docs/api.md\",\"tocar_solo\":[\"docs/**\"]}]"}}'
    printf '%s\n' '{"type":"step_finish","tokens":{"input":10,"output":5}}'
    ;;
  *)
    path=$(printf '%s' "$2" | grep -o 'test -f [^ ]*' | awk '{print $3}')
    [ -n "$path" ] && { mkdir -p "$dir/$(dirname "$path")"; echo "demo" > "$dir/$path"; }
    printf '%s\n' '{"type":"step_finish","tokens":{"input":100,"output":20}}'
    ;;
esac
EOF
chmod +x "$tmp/bin/opencode"
export PATH="$tmp/bin:$PATH"

# repo de demo
repo="$tmp/repo"
mkdir "$repo" && cd "$repo"
git init -b main -q
git -c user.email=t@t -c user.name=t commit --allow-empty -m init -q

run() { echo "\$ $*"; "$@"; echo; sleep 1; }

run "$bin" init --pruebas "true" --plain
run "$bin" plan "necesito exportar clientes y documentar la API" --aprobar --plain
run "$bin" board --plain
run "$bin" run --agentes 2 --ejecutor opencode --plain
run "$bin" board --plain
run "$bin" logs T-001 --plain
run "$bin" ship T-001 --dry-run --plain
run "$bin" report --plain
