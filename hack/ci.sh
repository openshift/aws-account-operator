#!/usr/bin/env bash
#
# CI Hook: Prek Validation
#
# Runs prek validation in CI/CD pipelines (Tekton, GitHub Actions, etc.)
# Uses hack/prek.ci.toml to skip network-dependent hooks
#
set -euo pipefail

# Ensure we're in the repo root (handle subdirectory invocation)
if ! REPO_ROOT=$(git rev-parse --show-toplevel 2>&1); then
  echo "Error: Not in a git repository. Cannot determine repo root." >&2
  echo "git rev-parse failed with: $REPO_ROOT" >&2
  exit 1
fi
cd "$REPO_ROOT" || exit 1

# Check for prek dependency
if ! command -v prek &>/dev/null; then
  echo "Error: prek is not installed. See CONTRIBUTING.md for setup instructions." >&2
  exit 1
fi

# Run prek with CI-specific config (skips network-dependent hooks)
prek run --config hack/prek.ci.toml
