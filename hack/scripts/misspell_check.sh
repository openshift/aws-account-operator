MISSPELL_VERSION="0.3.4"
GOOS=$(go env GOOS)
REPO_ROOT=$(git rev-parse --show-toplevel)
MISSPELL_DIR="$REPO_ROOT/.misspell/bin"

cd "${REPO_ROOT}" || exit 1
mkdir -p "$MISSPELL_DIR"

# Download misspell
if ! "$MISSPELL_DIR/misspell" -v | grep "$MISSPELL_VERSION" &>/dev/null; then
  [[ "$GOOS" == "darwin" ]] && os=mac || os="$GOOS"
  misspell="misspell-$MISSPELL_VERSION-$os-64bit"
  misspell_download_url="https://github.com/client9/misspell/releases/download/v${MISSPELL_VERSION}/misspell_${MISSPELL_VERSION}_${os}_64bit.tar.gz"
  if ! curl -s --head --request GET "${misspell_download_url}" | grep "404 Not Found" >/dev/null; then
    curl -sfL "$misspell_download_url" | tar -xzf - -O misspell >"$misspell"
    chmod +x "$misspell"
    ln -fs "$misspell" misspell
  else
    echo "404 Page Not Found: ${misspell_download_url}"
    exit 1
  fi
fi

# Run misspell tests
echo "Checking for typos ..."
"$MISSPELL_DIR/misspell" --error build cmd config deploy docs hack pkg test version Makefile README.md
