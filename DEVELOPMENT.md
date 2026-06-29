# Development Guide

Quick reference for developing AWS Account Operator.

## Prerequisites

- **Go**: 1.22 or later
- **operator-sdk**: v1.21.0 or later
- **kubectl/oc**: For cluster interaction
- **prek**: Git hook manager v0.4.1 (`uv tool install prek==0.4.1` - pinned version in `.prek-version`)
- **AWS credentials**: Configured via `rh-aws-saml-login`

## Initial Setup

```bash
# Clone repository
git clone https://github.com/openshift/aws-account-operator.git
cd aws-account-operator

# Install pre-commit hooks
prek install
```

## Common Commands

### Build
```bash
make build                    # Build operator binary
make docker-build             # Build container image
```

### Test
```bash
make test                     # Run unit tests
make test-all                 # Run all tests (lint + unit + integration)
make test-integration-local   # Run integration tests locally
go test ./controllers/...     # Test specific package
```

### Lint
```bash
make go-check                 # Full linting (golangci-lint)
prek run --all-files          # Run all prek hooks
```

### Code Generation
```bash
# After modifying API types (api/v1alpha1/*.go)
# or interfaces requiring mocks
boilerplate/_lib/container-make generate

# What this generates:
# - Deepcopy methods (zz_generated.deepcopy.go)
# - OpenAPI schemas
# - Mock interfaces for testing
```

### Run Locally
```bash
# Prerequisites and deploy CRDs/secrets
make predeploy

# Run operator locally against cluster
make deploy-local

# Or with debugging enabled
make deploy-local-debug
```

### Container-based Build
```bash
# Run make targets inside boilerplate container
# (ensures consistent environment with CI)
boilerplate/_lib/container-make go-test
boilerplate/_lib/container-make generate
```

## Fast Local Iteration

**Minimal validation loop:**
```bash
# After code changes
go build ./...                # Fast compile check (~5s)
go test ./pkg/mypackage       # Run affected tests
prek run                      # Lint staged files
```

**Full validation (pre-PR):**
```bash
prek run --all-files          # All hooks (~15-30s)
make test-all                 # Full test suite (lint + unit + integration)
```

## Targeted Testing

```bash
# Run specific test package
go test -v ./controllers/account/

# Run API tests
make test-apis

# Integration tests (local profile, ~20-25 minutes)
make test-integration-local
```

## Debugging

```bash
# Print specific package logs
go test -v ./pkg/... 2>&1 | grep "MyFunction"

# Ginkgo verbose output
ginkgo -v ./...
```

## Dependency Management

```bash
# Add new dependency
go get github.com/some/package@v1.2.3

# Update dependency
go get -u github.com/some/package

# Tidy (removes unused, adds missing)
go mod tidy

# Verify checksums
go mod verify
```

**Note**: `go.sum` changes automatically trigger validation in prek hooks.

## Architecture Pointers

- **API Types**: `api/v1alpha1/` - CRD definitions (Account, AccountClaim, AccountPool, AWSFederatedRole, etc.)
- **Controllers**: `controllers/` - Reconciliation logic for each CR type
- **AWS Integration**: `pkg/awsclient/` - AWS SDK wrapper (IAM, STS, Organizations, etc.)
- **Tests**: `*_test.go` alongside source
- **Integration Tests**: `test/integration/` - Full workflow tests
- **Config**: `deploy/` - CRDs and operator deployment manifests

## CI Parity

Local prek hooks mirror Tekton CI checks:
- **go-check** ↔ Tekton lint job
- **go-build** ↔ Compilation in CI
- **go-test** ↔ Unit test job
- **gitleaks** ↔ Security scanning
- **go-mod-tidy** ↔ Dependency consistency checks
- **rbac-wildcard-check** ↔ RBAC policy checks

Run `prek run --all-files` before pushing to catch CI failures early.

## Boilerplate Integration

This repo uses OpenShift boilerplate:
- Centralized Makefiles: `boilerplate/openshift/golang-osd-operator/`
- Standard targets: `go-build`, `go-check`, `go-test`
- Container builds: `boilerplate/_lib/container-make`
- Update boilerplate: `make boilerplate-update`

## Troubleshooting

**Mock generation fails:**
```bash
# Use container-make for consistency with CI
boilerplate/_lib/container-make generate
```

**Prek hook timeout:**
```bash
# macOS: Install GNU timeout
brew install coreutils

# Linux: timeout is built-in
```

**go.sum checksum mismatch:**
```bash
export GOPROXY="https://proxy.golang.org"
go mod tidy
```

**Tests fail locally but pass in CI:**
```bash
# Use container environment
boilerplate/_lib/container-make go-test
```

## Further Reading

- [Testing Guide](./TESTING.md)
- [Contributing Guide](./CONTRIBUTING.md)
- [Integration Testing](./test/integration/README.md)
- [Operator SDK Docs](https://sdk.operatorframework.io/)
