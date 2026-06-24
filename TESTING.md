# Testing Guide

Testing guidelines for AWS Account Operator.

## Framework

- **Go testing**: Standard go test framework
- **testify**: Assertions and mocking
- **AWS SDK mocks**: For AWS API interaction testing

## Quick Commands

```bash
# Run unit tests
make test

# Run API tests
make test-apis

# Run all tests (lint + unit + integration)
make test-all

# Integration tests - local profile (~20-25 minutes)
make test-integration-local

# Integration tests - full CI profile (~15-20 minutes)
make test-integration

# Run specific package
go test -v ./controllers/account/

# Container-based (CI parity)
boilerplate/_lib/container-make test
```

## Writing Tests

### Test Structure

Each package with tests includes:
- `*_suite_test.go`: Ginkgo test suite setup
- `*_test.go`: Actual test cases

**Example:**
```go
package mypackage_test

import (
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

var _ = Describe("MyFeature", func() {
    Context("when condition X", func() {
        It("should do Y", func() {
            result := MyFunction()
            Expect(result).To(Equal(expected))
        })
    })
})
```

### Creating New Tests

```bash
# Create test file alongside source
touch pkg/newpackage/myfile_test.go

# Follow existing test patterns in the package
```

### Mocking Interfaces

Use GoMock for external dependencies:

```go
//go:generate mockgen -destination=mocks/mock_client.go -package=mocks sigs.k8s.io/controller-runtime/pkg/client Client
```

**Regenerate all mocks:**
```bash
boilerplate/_lib/container-make generate
```

**Why container-make?**
- Ensures same mockgen version as CI
- Prevents version drift in generated code

## Test Organization

### Unit Tests
- Test individual functions and methods
- Mock AWS SDK clients (IAM, STS, Organizations)
- Fast execution (<1s per package)
- Located alongside source code (`*_test.go`)

### Controller Tests
- Test reconciliation logic
- Mock Kubernetes client and AWS clients
- Test custom resource lifecycle
- Located in `controllers/*/`

### Integration Tests
- Full operator workflow with real AWS
- Test account creation, claim assignment, IAM setup
- Run locally (`make test-integration-local`) or in CI
- Located in `test/integration/`

## Agent-Driven Validation

When AI agents modify code:

**Minimal validation:**
```bash
# After changing controllers/account/
go test ./controllers/account/
```

**Full validation before commit:**
```bash
make test-all
```

**If tests fail:**
1. Read test output carefully
2. Fix the underlying issue (don't skip tests)
3. Rerun to confirm fix
4. Check AWS mock setup if AWS-related tests fail

## Common Patterns

### Testing Controllers

```go
func TestAccountReconcile(t *testing.T) {
    // Setup mock AWS client
    mockCtrl := gomock.NewController(t)
    defer mockCtrl.Finish()
    mockAWSClient := mock.NewMockClient(mockCtrl)

    // Setup expectations
    mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return("123456789012", nil)

    // Test reconcile
    result, err := reconciler.Reconcile(ctx, req)
    assert.NoError(t, err)
    assert.Equal(t, reconcile.Result{}, result)
}
```

### Testing Error Conditions

```go
func TestAccountReconcile_Error(t *testing.T) {
    // Setup mock to return error
    mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return("", errors.New("API error"))

    // Verify error handling
    _, err := reconciler.Reconcile(ctx, req)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "API error")
}
```

### Using testify Assertions

```go
// Equality
assert.Equal(t, expected, actual)
assert.NotEqual(t, unexpected, actual)

// Nil checks
assert.NoError(t, err)
assert.Nil(t, obj)
assert.NotNil(t, obj)

// Collections
assert.Contains(t, slice, "item")
assert.Len(t, slice, 3)
assert.Empty(t, slice)

// Booleans
assert.True(t, condition)
assert.False(t, condition)

// Error messages
assert.EqualError(t, err, "expected message")
```

## Coverage

Generate coverage report:
```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

**Note**: Aim for meaningful coverage, not arbitrary percentages.
- Test critical paths and error handling
- Don't test generated code or trivial getters/setters

## Debugging Tests

```bash
# Verbose output
go test -v ./...

# Run single test
go test -v -run TestSpecificTest ./pkg/mypackage/

# Print debug info in tests
t.Logf("Debug: %v", value)

# Show all output
go test -v -count=1 ./...
```

## CI Expectations

Tests run in Tekton pipeline with:
- Fresh environment
- No cached dependencies
- Strict timeout limits

**Local CI parity:**
```bash
boilerplate/_lib/container-make go-test
```

## Test Performance

**Target timings:**
- Unit tests: <5s per package
- Controller tests: <15s per controller
- Full suite: <2min

**If tests are slow:**
- Check for unnecessary sleeps
- Use `Eventually` with shorter intervals
- Mock external calls
- Avoid creating unnecessary Kubernetes resources

## Common Issues

**Mock not found:**
```bash
# Regenerate mocks
boilerplate/_lib/container-make generate
```

**envtest not installed:**
```bash
make setup-envtest
```

**Test passes locally, fails in CI:**
```bash
# Run in container environment
boilerplate/_lib/container-make go-test

# Check for:
# - Time-dependent tests
# - Environment-specific assumptions
# - File path dependencies
```

**Flaky tests:**
- Use `Eventually` instead of `Expect` for async operations
- Avoid hardcoded delays
- Ensure test isolation (clean up resources)

## Prek Integration

Tests are **not** included in the prek pre-commit hooks because `make test` is
too slow for a commit hook. Run tests manually before pushing:

```bash
make test-all
```

## Integration Testing Details

See [test/integration/README.md](test/integration/README.md) for detailed integration testing documentation, including:
- Local vs CI test profiles
- AWS credential setup
- Test timeouts and performance
- Troubleshooting guide

## Further Reading

- [Development Guide](./DEVELOPMENT.md)
- [Contributing Guide](./CONTRIBUTING.md)
- [Integration Testing](./test/integration/README.md)
- [testify Documentation](https://github.com/stretchr/testify)
