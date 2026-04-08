# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AWS Account Operator is a Kubernetes operator that creates and manages a pool of AWS accounts for OpenShift Dedicated (OSD) provisioning. It handles AWS account creation via Organizations, IAM resource setup, credential rotation, and account reuse. Deployed to the `aws-account-operator` namespace.

## Build & Development Commands

```bash
# Build
make go-build                    # Binary -> build/_output/bin/aws-account-operator

# Lint
make lint                        # go-check + YAML validation + spell check
make go-check                    # golangci-lint only

# Unit tests
make test                        # All unit tests (uses envtest)
make test-apis                   # Tests in api/ subdirectory only

# Run a single test package
go test ./controllers/account    # Run one package
go test ./controllers/account -run "TestSomething"  # Single test by name
# Note: tests use Ginkgo v2 -- use -ginkgo.focus="description" to filter by Describe/It text

# Code generation (run after modifying api/v1alpha1/*_types.go)
make generate                    # CRDs + deepcopy + mocks + OpenAPI (all three below)
make op-generate                 # CRDs + deepcopy only
make go-generate                 # Mocks only (mockgen)
make openapi-generate            # OpenAPI specs only
make generate-check              # Verify generated code is up-to-date

# Validate (CI check -- ensures generation is current + boilerplate unchanged)
make validate

# Local development (requires cluster access + AWS credentials)
make predeploy                   # Setup namespace, CRDs, credentials, configmap
make deploy-local                # Run operator locally (FORCE_DEV_MODE=local)

# Integration tests (require running cluster + AWS staging accounts)
make test-account-creation
make test-ccs
make test-sts
make test-reuse
make test-fake-accountclaim
make test-integration            # All integration tests
```

## Architecture

### API Group: `aws.managed.openshift.io/v1alpha1`

Five CRDs, defined in `api/v1alpha1/`:

| CRD | Purpose |
|-----|---------|
| **Account** | A provisioned AWS account. Lifecycle: Pending -> Creating -> InitializingRegions -> PendingVerification -> Ready. |
| **AccountClaim** | A request for an AWS account. Links to an Account CR when fulfilled. |
| **AccountPool** | Maintains a pool of pre-created, unclaimed Account CRs at a target size. |
| **AWSFederatedRole** | Defines an IAM role template for federated access. |
| **AWSFederatedAccountAccess** | Grants a user federated access to an account using an AWSFederatedRole. |

The API types live in a **separate Go module** (`api/go.mod`), so `make test-apis` runs separately.

### Controllers (in `controllers/`)

Each controller reconciles its corresponding CRD. The **Account controller** (`controllers/account/`) is by far the largest and most complex -- it orchestrates AWS account creation, IAM user/role setup, EC2 region initialization, credential management, and support case creation. Supporting logic is split into:
- `byoc.go` -- BYOC (Bring Your Own Cloud / CCS) account handling
- `iam.go` -- IAM user and policy management
- `ec2.go` -- EC2 instance launch/terminate for region initialization
- `secrets.go` -- Kubernetes secret management for AWS credentials

The **AccountClaim controller** handles account assignment, reuse logic (`reuse.go`), and organizational unit placement (`organizational_units.go`).

### AWS Client (`pkg/awsclient/`)

All AWS API calls go through the `Client` interface in `pkg/awsclient/client.go`. This wraps EC2, IAM, Organizations, STS, Support, S3, Route53, and ServiceQuotas SDK clients. The `IBuilder` interface allows injecting mock clients in tests.

Mocks are generated via `mockgen` (`pkg/awsclient/mock/zz_generated.mock_client.go`). A second mock for the controller-runtime `Client` is at `controllers/accountclaim/mock/cr-client.go`.

### Dev Mode (`FORCE_DEV_MODE`)

Set via environment variable. Three modes:
- `local` -- skips leader election, uses dev logging, registers Prometheus metrics locally
- `cluster` -- deployed to cluster with dev image
- unset -- production mode

### FedRAMP Support

Controlled by `fedramp` key in the operator ConfigMap. Changes default AWS region to `us-gov-east-1` and adjusts ARN partition to `aws-us-gov`. See `config/config.go`.

## Testing Patterns

- **Framework**: Ginkgo v2 + Gomega. Test suites use `RegisterFailHandler(Fail)` / `RunSpecs()`.
- **AWS mocking**: Tests use `mockgen`-generated mocks of the `awsclient.Client` interface. Controller tests create mock AWS clients and inject them into reconcilers.
- **Kubernetes mocking**: Tests use `envtest` (kubebuilder assets v1.23) for a real API server, plus `mockgen`-generated mocks of the controller-runtime `client.Client` where needed.
- **FIPS**: Build flag `FIPS_ENABLED=true` is set in the Makefile. The `fips.go` file enables FIPS-compliant crypto.

## Boilerplate

This repo uses [openshift/boilerplate](https://github.com/openshift/boilerplate) (`golang-osd-operator` convention). Most build/test/lint targets come from `boilerplate/openshift/golang-osd-operator/standard.mk`. Update boilerplate with `make boilerplate-update`.

## Key Constants and Config

- Operator namespace: `aws-account-operator`
- ConfigMap name: `aws-account-operator-configmap`
- AWS credentials secret: `aws-account-operator-credentials`
- Default region: `us-east-1` (or `us-gov-east-1` for FedRAMP)
- IAM user name: `osdManagedAdmin`
- Finalizer: `finalizer.aws.managed.openshift.io`

## PR Conventions

- Reference OSD ticket: "Ref OSD-0000"
- Checklist: tested locally, unit tests included, docs updated
