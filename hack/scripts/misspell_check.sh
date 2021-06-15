MISSPELL_VERSION="0.3.4"
GOOS=$(go env GOOS)
REPO_ROOT=$(git rev-parse --show-toplevel)

cd ${REPO_ROOT}
mkdir -p .misspell/bin
cd .misspell/bin

# mapping from https://github.com/client9/misspell/blob/master/goreleaser.yml
[[ "$GOOS" == "darwin" ]] && os=osx || os="$GOOS"
misspell="misspell-$MISSPELL_VERSION-$os-64bit"
misspell_download_url="https://github.com/client9/misspell/releases/download/v$MISSPELL_VERSION/misspell_${MISSPELL_VERSION}_${os}_64bit.tar.gz"
curl -sfL "$misspell_download_url" | tar -xzf - -O misspell > "$misspell"
chmod +x "$misspell"
ln -fs "$misspell" misspell

./misspell ~/docs
./misspell ~/pkg
