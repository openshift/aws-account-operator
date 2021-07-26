MISSPELL_VERSION="0.3.4"
GOOS=$(go env GOOS)
REPO_ROOT=$(git rev-parse --show-toplevel)
MISSPELL_DIR="$REPO_ROOT/.misspell/bin"

cd "${REPO_ROOT}" || exit 1
mkdir -p "$MISSPELL_DIR"

download_misspell() {
  echo "Downloading misspell binary"
  [[ "$GOOS" == "darwin" ]] && os=mac || os="$GOOS"
  misspell="misspell-$MISSPELL_VERSION-$os-64bit"
  misspell_download_url="https://github.com/client9/misspell/releases/download/v${MISSPELL_VERSION}/misspell_${MISSPELL_VERSION}_${os}_64bit.tar.gz"
  if ! curl -s --head --request GET "${misspell_download_url}" | grep "404 Not Found" >/dev/null; then
    curl -sfL "$misspell_download_url" | tar -xzf - -O misspell >"$MISSPELL_DIR/$misspell"
    chmod +x "$MISSPELL_DIR/$misspell"
    ln -fs "$MISSPELL_DIR/$misspell" "$MISSPELL_DIR/misspell"
  else
    echo "404 Page Not Found: ${misspell_download_url}"
    exit 1
  fi
}

# Download misspell binary if not present
if [ ! -f "$MISSPELL_DIR/misspell" ]; then
  download_misspell
fi

# Update misspell binary (if present) if not correct version
if ! "$MISSPELL_DIR/misspell" -v | grep "$MISSPELL_VERSION" &>/dev/null; then
  echo "Updating misspell binary"
  download_misspell
fi

# Run misspell tests
echo "Checking for typos ..."
"$MISSPELL_DIR/misspell" --error build cmd config deploy docs hack pkg test version Makefile README.md
