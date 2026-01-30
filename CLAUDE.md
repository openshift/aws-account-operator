# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

AWS Account Operator is a Kubernetes operator that manages AWS accounts for OpenShift Dedicated clusters. It creates and maintains pools of AWS accounts, assigns them to AccountClaims, and handles AWS resource configuration including IAM, networking, and federation.

## Development Commands

### Building and Testing
- `make test-all` - Runs all test suites (includes lint, unit tests, and integration tests)
- `make test` - Run unit tests only
- `make test-apis` - Run API tests only
- `make test-integration` - Run full integration test suite (for CI/PROW)
- `make test-integration-local` - **EASY BUTTON**: Automated local integration testing with setup
- `make lint` - Run linting
- `make build` - Build the operator binary
- `go test ./...` - Run unit tests from any directory

### Integration Testing

**Quick Start** (Local testing with 5/8 tests):
```bash
make test-integration-local
```

This command automatically:
1. Validates AWS credentials and cluster connectivity
2. Configures operator and starts it locally
3. Runs 5 integration tests (~20-25 minutes total)
4. Skips 3 tests (see below)

**What Runs Where:**

*Local Profile* (5/8 tests, ~20-25 minutes):
- ✅ Fast tests: FAKE AccountClaim, AccountPool (~3 min)
- ✅ AWS integration: STS, Account creation, OU logic (~15-20 min)
- ❌ Skipped: KMS (requires special IAM permissions), Account reuse, Finalizer cleanup (too slow locally)

*CI/PROW Profile* (8/8 tests, ~15-20 minutes):
- All 8 tests with real AWS integration
- Faster infrastructure (2-4x faster than local)
- Required for PR merge

**Manual Setup** (if you want more control):
```bash
# Run setup only
./hack/scripts/setup_local_integration_testing.sh

# Then run tests manually
./test/integration/integration-test-bootstrap.sh -p local
```

**Integration Tests (Local Profile)** - 5 tests (~20-25 minutes total):
- test_fake_accountclaim.sh - FAKE claim workflow (~30s)
- test_accountpool_size.sh - AccountPool management (~1-2m)
- test_sts_accountclaim.sh - STS account workflow (~5m)
- test_nonccs_account_creation.sh - Account creation workflow (~5-7m)
- test_aws_ou_logic.sh - Organizational Unit assignment (~7-10m)

**Skipped Locally** (3 tests):
- test_kms_accountclaim.sh - Requires cross-account IAM role assumption permissions not available locally
- test_nonccs_account_reuse.sh - Account reuse with finalizer cleanup (~10-15m, often times out)
- test_finalizer_cleanup.sh - Extended finalizer operations (~20-25m, designed for slow cleanup)

**Integration Tests (CI/PROW Profile)** - All 8 tests (~15-20 minutes):
- Runs all 5 local tests PLUS the 3 skipped tests
- Faster infrastructure (2-4x faster than local)
- Required for PR merge

**Why Skip 3 Tests Locally?**
- **KMS test**: Requires cross-account IAM role assumption permissions (ManagedOpenShift-Support role) not available in local dev environment
- **Account reuse and finalizer tests**: Local infrastructure is 2-4x slower than CI (consumer internet, external operator)
  - These tests involve extensive AWS cleanup (10-25 minutes)
  - Often timeout even with 15m limits locally
- 5/8 tests still provide excellent coverage for local validation
- All PRs must pass full 8/8 test suite in CI before merge

**Performance Notes**:
- **Local development**: Account CR operations take 5-15 minutes due to:
  - Consumer internet connection (vs datacenter networking in CI)
  - External operator-sdk process (vs in-cluster operator in CI)
  - IAM eventual consistency (AWS can take 2+ minutes to propagate changes)
  - Sequential AWS API calls for resource cleanup
- **PROW/CI environment**: 2-4x faster due to datacenter infrastructure
- **Timeouts are extended for local profile**: 7m Account ready, 7m AccountClaim ready, 5m resource delete

**Optimization Strategies**:
- Local profile runs 5/8 tests for good coverage without excessive wait times
- All PRs must pass full 8/8 test suite in CI before merge
- Extended timeouts account for slower local infrastructure
- Operator reuse: When running tests multiple times locally, the operator process is reused for faster iteration
  - ConfigMap and CRDs are automatically recreated even when operator is reused
  - Finalizers are removed immediately during cleanup (no double-timeout wait)
- Parallel execution: Not currently supported for single AWS account (resource conflicts)
  - Future: Could parallelize with multiple AWS accounts assigned to different tests

**Local Development Notes**:
- **Credential Expiration**: STS tokens expire after a few hours. If tests timeout with "null" status:
  - Run `./hack/scripts/update_aws_credentials.sh` to refresh credentials
  - Timeout messages include credential expiration hints for local development
- **Cleanup Improvements**: Finalizers are now removed immediately instead of waiting for timeout
  - Reduced cleanup timeout from 15m to 5m for local profile
  - Tests complete faster when cleaning up resources
- **Operator ConfigMap**: The operator requires `aws-account-operator-configmap` to reconcile Account CRs
  - **CI/PROW**: ConfigMap created from mounted secrets with real OU/STS values
  - **Local Dev**: ConfigMap auto-created with placeholder values (OU vars not needed for BYOC testing)
  - If OU environment variables are set in `.envrc`, `make predeploy` will create proper ConfigMap
  - Otherwise, bootstrap script creates minimal ConfigMap sufficient for integration tests

### Local Development
- `make predeploy` - Deploy prerequisites (CRDs, namespaces, credentials)
- `make deploy-local` - Run operator locally with `FORCE_DEV_MODE=local`
- `make deploy-cluster` - Deploy to cluster with development image
- `make clean-operator` - Clean up operator resources

### Environment Setup

**Initial Setup**:
1. Authenticate to your assigned account: `rh-aws-saml-login <your-profile-name>`
2. Create AccessRole in your assigned account: `./hack/scripts/aws/setup_access_role.sh -a <ACCOUNT_ID> -p <your-profile-name>`

Set these environment variables for testing (in `.envrc`):
- `FORCE_DEV_MODE=local` - Enable local development mode
- `OSD_STAGING_2_AWS_ACCOUNT_ID` - Your assigned osd-staging-2 account ID (not osd-staging-1)
- `OSD_STAGING_1_OU_ROOT_ID` and `OSD_STAGING_1_OU_BASE_ID` - Organizational Unit IDs
- `STS_JUMP_ROLE=arn:aws:iam::<SHARED_ACCOUNT_ID>:role/JumpRole` - Shared jump role (centrally managed)
- `STS_JUMP_ARN=arn:aws:iam::<SHARED_ACCOUNT_ID>:role/JumpRole` - Same as above
- `STS_ROLE_ARN=arn:aws:iam::<YOUR_ACCOUNT_ID>:role/AccessRole` - Your AccessRole ARN

**Note**: Credentials are now managed via temporary STS tokens using `rh-aws-saml-login`, stored in `~/.aws/credentials`. No long-lived IAM user keys are used. Credentials expire after a few hours and need refreshing via `update_aws_credentials.sh`.

## Architecture

### Core Components

**Controllers** (in `controllers/` directory):
- `account/` - Manages AWS Account CR lifecycle, handles account setup, IAM roles, networking
- `accountclaim/` - Processes AccountClaim CRs, assigns accounts from pools
- `accountpool/` - Maintains pools of ready-to-use AWS accounts
- `awsfederatedrole/` - Manages federated IAM roles for cross-account access
- `awsfederatedaccountaccess/` - Handles temporary access grants to federated accounts
- `validation/` - Validates account and pool configurations

**Custom Resources** (in `api/v1alpha1/`):
- `Account` - Represents a single AWS account with configuration state
- `AccountClaim` - Request for an AWS account (regular, CCS/BYOC, STS, or fake)
- `AccountPool` - Defines pools of accounts to maintain
- `AWSFederatedRole` - Cross-account IAM role definitions
- `AWSFederatedAccountAccess` - Temporary access grants

**AWS Integration** (in `pkg/awsclient/`):
- `client.go` - Main AWS SDK wrapper with organization operations
- `iam.go` - IAM roles, policies, and user management
- `tags.go` - AWS resource tagging operations
- `sts/` - STS assume role functionality

### Account Types and Flows

1. **Standard Accounts**: Created and managed entirely by the operator
2. **CCS/BYOC (Customer Cloud Subscription)**: Customer-provided AWS accounts with operator setup
3. **STS Accounts**: Use AWS STS for temporary role-based access
4. **Fake Accounts**: For testing without real AWS resources

### Key Patterns

**Account Lifecycle**: AccountClaim → Account assignment from pool → AWS setup (IAM, networking, etc.) → Ready state

**Reuse Logic**: Accounts can be reused when claims are deleted, returning to the pool after cleanup

**Dev Mode Detection**: Environment variable `FORCE_DEV_MODE` controls testing behavior (skips support cases, etc.)

## Key Configuration

- **Namespace**: All resources operate in `aws-account-operator` namespace
- **ConfigMaps**: Default configuration in `aws-account-operator-configmap`
- **Secrets**: AWS credentials stored in operator namespace
- **CRDs**: Located in `deploy/crds/` directory

## Development Notes

- The operator uses controller-runtime framework with reconciliation patterns
- Local development requires OpenShift cluster (CRC/Minishift supported)
- Integration tests require real AWS environment with proper IAM setup
- FIPS mode supported via build tags and configuration
- Metrics exposed on port 8080 in local mode, within cluster otherwise