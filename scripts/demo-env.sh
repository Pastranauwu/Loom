#!/bin/sh
# Prepara el entorno para grabar la TUI de loom: agente falso con pausa
# (para que se vea la animación) y un repo de demo en /tmp/loom-demo.
# No corre ningún comando de loom; eso lo hace el tape con la TUI.
set -e

raiz="$(cd "$(dirname "$0")/.." && pwd)"
bin="${LOOM_BIN:-$raiz/loom}"
# siempre recompila: go build es incremental y un binario viejo grababa
# una demo que ya no corresponde al código del repo
if [ -z "$LOOM_BIN" ]; then
  echo "compilando loom..."
  (cd "$raiz" && go build -o loom ./cmd/loom) || exit 1
fi

# agente falso
mkdir -p /tmp/fakebin
cat > /tmp/fakebin/opencode <<'EOF'
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
  *"loom's planner"*)
    printf '%s\n' '{"type":"message","part":{"type":"text","text":"[{\"title\":\"exportador CSV\",\"done_when\":\"test -f src/csv/export.go\",\"only_touch\":[\"src/csv/**\"]},{\"title\":\"exportador JSON\",\"done_when\":\"test -f src/json/export.go\",\"only_touch\":[\"src/json/**\"]}]"}}'
    printf '%s\n' '{"type":"step_finish","tokens":{"input":10,"output":5}}'
    ;;
  *)
    for path in $(printf '%s' "$2" | grep -o 'test -f [^ ]*' | awk '{print $3}'); do
      mkdir -p "$dir/$(dirname "$path")"
      echo "demo" > "$dir/$path"
      printf '%s\n' "{\"type\":\"message\",\"part\":{\"type\":\"text\",\"text\":\"escribiendo $path\"}}"
    done
    sleep 1
    printf '%s\n' '{"type":"step_finish","tokens":{"input":100,"output":20}}'
    ;;
esac
EOF
chmod +x /tmp/fakebin/opencode
cp "$bin" /tmp/fakebin/loom

# HOME aislado: el ledger de consumo vive en $HOME/.loom, y sin esto
# el GIF grabaria el gasto real de quien graba. El tape exporta este HOME.
casa=/tmp/loom-demo-home
rm -rf "$casa"
mkdir -p "$casa"
cat > "$casa/.gitconfig" <<'GITCFG'
[user]
	name = loom demo
	email = demo@loom.local
[init]
	defaultBranch = main
GITCFG

# repo de demo en ruta fija
rm -rf /tmp/loom-demo
mkdir /tmp/loom-demo
cd /tmp/loom-demo
git init -b main -q
git -c user.email=t@t -c user.name=t commit --allow-empty -m init -q
HOME="$casa" PATH="/tmp/fakebin:$PATH" /tmp/fakebin/loom init --pruebas true --plain >/dev/null
echo "recursion_max: 1" >> .loom/config.yml

cat > .loom/tasks/T-001.md <<'EOF'
---
version: 1
id: T-001
title: crear el módulo de exportación
done_when: test -f src/export.go
only_touch: ["src/**"]
max_retries: 3
max_lines: 200
---
EOF
cat > .loom/tasks/T-002.md <<'EOF'
---
version: 1
id: T-002
title: documentar la API
done_when: test -f docs/api.md
only_touch: ["docs/**"]
max_retries: 3
max_lines: 200
---
EOF

# T-003 recursiva: llega como .pendiente, el tape la activa (mv) después
# de que T-001/T-002 ya corrieron — si entrara pendiente desde el arranque,
# la esclusa de entrada la rechaza por cruce de tocar_solo con T-001/T-002
# en la misma oleada, que es un chequeo real y correcto, no un bug.
cat > .loom/tasks/T-003.md.pendiente <<'EOF'
---
version: 1
id: T-003
title: exportar en CSV y en JSON
done_when: test -f src/csv/export.go && test -f src/json/export.go
only_touch: ["src/**"]
max_retries: 3
max_lines: 200
recursivo: true
limite_subtareas: 2
---
EOF
echo "demo lista en /tmp/loom-demo"
