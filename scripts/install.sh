#!/bin/sh
# Instalador de una línea para loom.
#   curl -fsSL https://github.com/Pastranauwu/loom/releases/latest/download/install.sh | sh
set -e

repo="Pastranauwu/loom"
bin="loom"

# detectar SO y arquitectura
os=$(uname -s)
arch=$(uname -m)
case "$os" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "sistema no soportado: $os" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "arquitectura no soportada: $arch" >&2; exit 1 ;;
esac

version="${LOOM_VERSION:-latest}"
if [ "$version" = "latest" ]; then
  url="https://github.com/$repo/releases/latest/download/${bin}_${os}_${arch}.tar.gz"
else
  url="https://github.com/$repo/releases/download/${version}/${bin}_${os}_${arch}.tar.gz"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "descargando $url"
curl -fsSL "$url" -o "$tmp/$bin.tar.gz"
tar -xzf "$tmp/$bin.tar.gz" -C "$tmp"

dest="${LOOM_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$dest"
install -m 0755 "$tmp/$bin" "$dest/$bin"

echo "instalado en $dest/$bin"

case ":$PATH:" in
  *":$dest:"*) ;;
  *)
    linea="export PATH=\"$dest:\$PATH\""
    case "$(basename "${SHELL:-}")" in
      fish) rc="$HOME/.config/fish/config.fish"; linea="fish_add_path $dest" ;;
      zsh)  rc="$HOME/.zshrc" ;;
      bash) rc="$HOME/.bashrc" ;;
      *)    rc="$HOME/.profile" ;;
    esac
    if [ -f "$rc" ] && grep -qF "$dest" "$rc" 2>/dev/null; then
      echo "$dest ya está en $rc, abre una terminal nueva"
    else
      mkdir -p "$(dirname "$rc")"
      printf '\n# agregado por el instalador de loom\n%s\n' "$linea" >> "$rc"
      echo "agregado $dest a tu PATH en $rc · abre una terminal nueva (o corre: source $rc)"
    fi
    ;;
esac

echo
echo "siguiente paso: $bin doctor"
echo "si todo está verde: $bin init"
