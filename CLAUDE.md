# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

AWS Account Operator is a Kubernetes operator that manages AWS accounts for OpenShift Dedicated clusters. It creates and maintains pools of AWS accounts, assigns them to AccountClaims, and handles AWS resource configuration including IAM, networking, and federation.

## Development Commands

### Building and Testing
- `make test-all` - Runs all test suites (includes lint, unit tests, and integration tests)
- `make test` - Run unit tests only
- `make test-apis` - Run API tests only
- `make test-integration` - Run integration tests
- `make lint` - Run linting
- `make build` - Build the operator binary
- `go test ./...` - Run unit tests from any directory

### Running Tests by Category
- `make test-account-creation` - Test account creation flows
- `make test-ccs` - Test Customer Cloud Subscription (BYOC) flows
- `make test-reuse` - Test account reuse functionality

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