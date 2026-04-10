# Integration Tests - Go Implementation

This directory contains Go-based integration tests for the AWS Account Operator. These tests are being migrated from bash scripts to Go for better maintainability, error handling, and IDE integration.

## Status

**Migration Progress:**
- ✅ `test_fake_accountclaim.sh` → `fake_accountclaim_test.go`
- ⏳ `test_accountpool_size.sh` (TODO)
- ⏳ `test_sts_accountclaim.sh` (TODO)
- ⏳ `test_nonccs_account_creation.sh` (TODO)
- ⏳ `test_aws_ou_logic.sh` (TODO)
- ⏳ `test_kms_accountclaim.sh` (TODO)
- ⏳ `test_nonccs_account_reuse.sh` (TODO)
- ⏳ `test_finalizer_cleanup.sh` (TODO)

## Running Tests

### Prerequisites
1. Access to a Kubernetes/OpenShift cluster
2. AWS credentials configured (for tests that create real AWS resources)
3. Operator deployed and running

### Run All Go Integration Tests
```bash
make test-integration-go
```

### Run Individual Tests
```bash
# FAKE AccountClaim test (fastest, no AWS interaction)
make test-integration-go-fake

# Or run directly with go test
go test -v -run TestFakeAccountClaim ./test/integration/tests/
```

### Run with Custom Timeouts
```bash
go test -v -timeout 30m ./test/integration/tests/
```

## Test Structure

Each test follows a consistent pattern:

1. **Setup Phase** - Create resources (namespaces, AccountClaims, etc.)
2. **Validate Phase** - Verify expected behavior
3. **Cleanup Phase** - Remove test resources (using `t.Cleanup()`)

### Example Test Structure
```go
func TestExample(t *testing.T) {
    // Setup
    t.Run("Setup", func(t *testing.T) {
        // Create resources
    })

    // Validate
    t.Run("Validate", func(t *testing.T) {
        // Check expectations
    })

    // Cleanup (automatic via t.Cleanup)
    t.Cleanup(func() {
        // Remove resources
    })
}
```

## Helper Functions

Common test utilities are located in `test/integration/helpers/`:

- **client.go** - Kubernetes client creation and management
- **accountclaim.go** - AccountClaim-specific operations

### Key Helper Functions

```go
// Get Kubernetes client
kubeClient, err := helpers.GetKubeClient()

// Create FAKE AccountClaim
claim, err := helpers.CreateFakeAccountClaim(ctx, kubeClient, name, namespace)

// Wait for AccountClaim to be ready
err := helpers.WaitForAccountClaimReady(ctx, kubeClient, name, namespace, timeout)

// Verify secret exists
err := helpers.VerifySecretExists(ctx, kubeClient, secretName, namespace)

// Delete with finalizer removal (fast cleanup)
err := helpers.DeleteAccountClaim(ctx, kubeClient, name, namespace, timeout, true)
```

## Writing New Tests

When adding new integration tests:

1. **Create test file** in `test/integration/tests/` following the pattern `<feature>_test.go`
2. **Use table-driven tests** where appropriate for multiple scenarios
3. **Add helper functions** to `test/integration/helpers/` for reusable operations
4. **Document the test** with comments explaining what it validates
5. **Use t.Cleanup()** for resource cleanup instead of defer
6. **Add Makefile target** in `test/integration/int-testing.mk`

### Example: Adding a New Test

```go
package tests

import (
    "context"
    "testing"
    "time"

    "github.com/openshift/aws-account-operator/test/integration/helpers"
)

func TestMyFeature(t *testing.T) {
    ctx := context.Background()
    kubeClient, err := helpers.GetKubeClient()
    if err != nil {
        t.Fatalf("Failed to get kube client: %v", err)
    }

    t.Run("Setup", func(t *testing.T) {
        // Setup code
    })

    t.Run("Validate", func(t *testing.T) {
        // Validation code
    })

    t.Cleanup(func() {
        // Cleanup code
    })
}
```

## Comparison: Bash vs Go

### Bash Implementation
- Harder to debug
- No IDE support
- String-based error handling
- Sequential execution
- Difficult to share code

### Go Implementation
- ✅ Type-safe
- ✅ IDE integration (autocomplete, jump-to-definition, etc.)
- ✅ Better error handling
- ✅ Concurrent test execution
- ✅ Shared helper libraries
- ✅ Built-in test framework features

## Troubleshooting

### Test Hangs or Timeouts
- Check operator logs: `oc logs -n aws-account-operator deployment/aws-account-operator`
- Verify cluster connectivity: `oc whoami`
- Check resource status: `oc get accountclaims -A`

### Authentication Errors
- Verify AWS credentials are configured
- Check operator has necessary secrets deployed
- Ensure RBAC permissions are correct

### Cleanup Issues
- Tests use `t.Cleanup()` which runs even if test fails
- Finalizer removal is enabled by default for faster cleanup
- Manual cleanup: `oc delete accountclaim <name> -n <namespace>`
