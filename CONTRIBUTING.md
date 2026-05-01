# Contributing

## Prerequisites

- **Go 1.22+** — required for building and testing
- **golangci-lint** — installed automatically via `make lint`
- **[prek](https://prek.j178.dev/)** — git hook manager that runs validation automatically on commit

## Setup

```bash
# Install prek (macOS)
brew install prek

# Install prek (Linux)
curl -fsSL https://prek.j178.dev/install.sh | bash

# Install pre-commit hooks
prek install
```

`prek install` sets up pre-commit hooks that automatically run file hygiene checks and golangci-lint before each commit.

## prek Version

The `.prek-version` file in the repo root pins the prek version used in CI. Periodically check [prek releases](https://github.com/j178/prek/releases) and update `.prek-version` when a new version is available.

## Validation

The validation runs automatically via pre-commit hooks, or you can run it manually:

```bash
# Run all validations (file hygiene + golangci-lint)
prek run --all-files

# Run linting (olm-deploy-yaml-validate + golangci-lint)
make lint

# Run only golangci-lint
make go-check
```

For CI pipelines, use `hack/ci.sh`.

## Linting

This project uses golangci-lint configured via the boilerplate framework:

- **Full config**: `boilerplate/openshift/golang-osd-operator/golangci.yml`
- **Project override**: `.golangci.yml` (minimal - just concurrency: 10)

Enabled linters: errcheck, gosec, govet, ineffassign, misspell, staticcheck, unused

```bash
# Run linting (includes olm-deploy-yaml-validate + golangci-lint)
make lint

# Run only golangci-lint
make go-check
```

## Testing

```bash
# Run unit tests
make test

# Run API tests
make test-apis

# Run all tests (lint + unit + API + integration)
make test-all

# Run integration tests locally (automated setup, 5/8 tests)
make test-integration-local

# Run integration tests for CI/PROW (all 8 tests)
make test-integration
```

See [CLAUDE.md](CLAUDE.md) for detailed integration testing documentation.

## Stop Hook: prek Validation on Every Turn

A Claude Code [stop hook](https://docs.anthropic.com/en/docs/claude-code/hooks) in `.claude/settings.json` runs `prek run --all-files` every time Claude finishes a turn and is about to stop. If prek finds violations (trailing whitespace, invalid JSON/YAML, linting errors, etc.), the hook **blocks Claude from stopping** and feeds the errors back so Claude can fix them automatically.

Without this hook, prek violations would only surface at `git commit` time via the pre-commit hook. The stop hook shortens the feedback loop by catching issues between prompts, allowing longer stretches of autonomous work without human intervention.

The hook script (`.claude/hooks/stop-prek-validation.sh`) includes a guard against infinite loops: if it has already blocked once and Claude retries, it allows the stop to proceed.

## Boilerplate Framework

This repository uses the [openshift/golang-osd-operator](https://github.com/openshift/boilerplate/tree/master/boilerplate/openshift/golang-osd-operator) convention from [boilerplate](https://github.com/openshift/boilerplate/).

Key boilerplate targets:
- `make validate` — Check code generation and boilerplate
- `make lint` — Static analysis (olm-deploy-yaml-validate + golangci-lint)
- `make test` — Unit tests
- `make coverage` — Code coverage analysis

See [boilerplate/openshift/golang-osd-operator/README.md](boilerplate/openshift/golang-osd-operator/README.md) for details.

## PROW CI

CI is configured via [openshift/release](https://github.com/openshift/release) repository. PROW runs:
- `make validate` — Code generation checks
- `hack/ci.sh` — prek validation (file hygiene + golangci-lint)
- `make lint` — Static analysis
- `make test` — Unit tests
- `make coverage` — Code coverage (postsubmit)
- `make test-integration` — Integration tests (optional, requires AWS)

**Note:** `hack/ci.sh` runs prek validation independently. Future boilerplate versions may integrate prek into `make validate`.

Config: `openshift/release/ci-operator/config/openshift/aws-account-operator/openshift-aws-account-operator-master.yaml`

## Development Workflow

1. Make code changes
2. Run `make lint` to check locally
3. Commit (pre-commit hooks run automatically via prek)
4. If pre-commit hooks fail, fix issues and re-commit
5. Push to GitHub
6. PROW CI runs validation, linting, and tests
