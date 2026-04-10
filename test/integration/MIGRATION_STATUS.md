# Integration Test Migration: Bash → Go

## Overview
This document tracks the migration of integration tests from bash scripts to Go tests for better maintainability, type safety, and IDE integration.

## Completed ✅

### 1. Infrastructure & Helpers
- **`test/integration/helpers/client.go`** - Kubernetes client management
  - `GetKubeClient()` - Get controller-runtime client with AWS Account Operator types
  - `GetKubeConfig()` - Load kubeconfig (in-cluster or local)
  - `CreateNamespace()` - Create namespace if not exists
  - `DeleteNamespace()` - Delete namespace with timeout
  - `RemoveFinalizers()` - Strip finalizers from any resource

- **`test/integration/helpers/accountclaim.go`** - AccountClaim operations
  - `CreateFakeAccountClaim()` - Create FAKE AccountClaim for testing
  - `WaitForAccountClaimReady()` - Wait for Ready/Claimed status
  - `GetAccountClaim()` - Retrieve AccountClaim CR
  - `DeleteAccountClaim()` - Delete with optional finalizer removal
  - `VerifySecretExists()` - Check for secret existence

### 2. Tests Migrated

#### ✅ test_fake_accountclaim.sh → fake_accountclaim_test.go
**Status:** Complete
**Runtime:** ~30 seconds
**Coverage:**
- Creates namespace for FAKE AccountClaim
- Creates FAKE AccountClaim (no real AWS resources)
- Waits for claim to become Ready
- Verifies finalizers present
- Verifies NO accountLink (no Account CR created)
- Verifies secret created in namespace
- Automatic cleanup via t.Cleanup()

**Improvements over bash:**
- Type-safe API interactions
- Better error messages
- Automatic cleanup even on test failure
- IDE integration (autocomplete, jump-to-definition)
- Structured test phases (Setup/Validate/Cleanup)

### 3. Build System
- **`test/integration/int-testing.mk`** - Added Go test targets
  - `make test-integration-go` - Run all Go integration tests
  - `make test-integration-go-fake` - Run FAKE AccountClaim test only

### 4. Documentation
- **`test/integration/tests/README.md`** - Comprehensive guide
  - How to run tests
  - Helper function documentation
  - Writing new tests guide
  - Troubleshooting tips
  - Bash vs Go comparison

## In Progress 🚧

None currently - ready for next test migration!

## TODO 📋

### High Priority (Local Profile Tests)
These tests run in the local development profile (~20-25 minutes total):

1. **test_accountpool_size.sh** → `accountpool_size_test.go`
   - Runtime: ~1-2 minutes
   - Tests AccountPool management and sizing
   - No AWS Account creation (uses pool logic)

2. **test_sts_accountclaim.sh** → `sts_accountclaim_test.go`
   - Runtime: ~5 minutes
   - Tests STS account workflow with role assumption
   - Validates STS credentials and external ID

3. **test_nonccs_account_creation.sh** → `nonccs_account_creation_test.go`
   - Runtime: ~5-7 minutes
   - Tests standard account creation workflow
   - Verifies IAM roles, secrets, Account CR state

4. **test_aws_ou_logic.sh** → `aws_ou_logic_test.go`
   - Runtime: ~7-10 minutes
   - Tests Organizational Unit assignment logic
   - Validates Account CR in correct OU

### Lower Priority (CI/PROW Only Tests)
These tests are skipped locally due to special requirements or long runtimes:

5. **test_kms_accountclaim.sh** → `kms_accountclaim_test.go`
   - Requires cross-account IAM role assumption (ManagedOpenShift-Support)
   - Tests KMS key encryption for volumes

6. **test_nonccs_account_reuse.sh** → `nonccs_account_reuse_test.go`
   - Runtime: ~10-15 minutes (often times out locally)
   - Tests account reuse after claim deletion
   - Extensive AWS cleanup operations

7. **test_finalizer_cleanup.sh** → `finalizer_cleanup_test.go`
   - Runtime: ~20-25 minutes
   - Tests extended finalizer operations
   - Designed for slow cleanup scenarios

### Additional Helper Functions Needed

As we migrate more tests, we'll need helpers for:

- **Account CR operations**
  - `CreateAccount()` - Create Account CR
  - `WaitForAccountReady()` - Wait for Account ready state
  - `GetAccount()` - Retrieve Account CR
  - `DeleteAccount()` - Delete Account CR

- **AWS operations**
  - `GetAWSCredentialsFromSecret()` - Extract AWS creds from secret
  - `AssumeSTSRole()` - Assume STS role and return credentials
  - `VerifyIAMRole()` - Check IAM role exists in AWS
  - `VerifyOrganizationalUnit()` - Check account in correct OU

- **AccountPool operations**
  - `CreateAccountPool()` - Create AccountPool CR
  - `WaitForAccountPoolReady()` - Wait for pool to fill
  - `GetAccountPool()` - Retrieve AccountPool CR
  - `VerifyPoolSize()` - Check pool has expected number of accounts

## Migration Strategy

### Phase 1: Foundation (COMPLETE ✅)
- ✅ Set up helper infrastructure
- ✅ Migrate simplest test (FAKE AccountClaim)
- ✅ Establish patterns and documentation

### Phase 2: Core Tests (NEXT)
- Migrate high-priority local profile tests
- Build out Account and AWS helper functions
- Test with real AWS integration

### Phase 3: Advanced Tests
- Migrate CI/PROW-only tests
- Handle special cases (KMS, long-running cleanup)
- Performance optimization

### Phase 4: Cleanup
- Remove bash test scripts (keep as reference initially)
- Update CI/CD pipelines
- Full integration test suite in Go

## Testing the Migration

### Before Removing Bash Tests
Run both bash and Go versions to ensure parity:

```bash
# Bash version
./test/integration/tests/test_fake_accountclaim.sh setup
./test/integration/tests/test_fake_accountclaim.sh test
./test/integration/tests/test_fake_accountclaim.sh cleanup

# Go version
make test-integration-go-fake
```

### Validation Checklist
- [ ] Same resources created
- [ ] Same validation checks performed
- [ ] Same cleanup behavior
- [ ] Similar or better error messages
- [ ] Comparable or better runtime

## Benefits of Go Implementation

1. **Type Safety** - Catch errors at compile time
2. **IDE Integration** - Autocomplete, jump-to-definition, refactoring
3. **Better Error Handling** - Structured error types vs string parsing
4. **Code Reuse** - Shared helper libraries
5. **Concurrent Execution** - Run independent tests in parallel
6. **Built-in Test Framework** - Subtests, table-driven tests, benchmarks
7. **Easier Debugging** - Step through code with debugger
8. **Maintainability** - Refactoring tools, static analysis

## Notes

- Bash tests will remain until Go equivalents are validated
- Helper functions designed for reuse across all tests
- Timeouts configurable via test flags
- Cleanup automatic via `t.Cleanup()` - runs even on failure
- Finalizer removal enabled by default for faster local testing
