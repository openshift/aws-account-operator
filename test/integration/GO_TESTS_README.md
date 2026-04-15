# Go Integration Tests for PROW

## Overview

This directory contains a simplified Go-based integration test approach for PROW that avoids the overhead and fragility of the bash bootstrap process.

## Key Differences from Bash Tests

### Old Approach (Bash)
1. PROW builds operator image ✓
2. Test script **rebuilds** operator image in-cluster using OpenShift Build API
3. Deploy operator
4. Run bash tests
5. **Problem**: Requires Build API to be available on test cluster (intermittent failures)

### New Approach (Go)
1. PROW builds operator image ✓
2. Deploy operator **using PROW's pre-built image** (no rebuild)
3. Run Go tests
4. **Benefit**: No Build API dependency, faster, more reliable

## Files

- `run-go-tests.sh` - Simplified test runner that deploys operator and runs Go tests
- `tests/fake_accountclaim_test.go` - Example Go integration test
- `helpers/client.go` - Kubernetes client helpers
- `helpers/accountclaim.go` - AccountClaim test helpers

## Usage

### For PROW

Run the simplified Go tests:
```bash
make test-integration-go-prow
```

Run the traditional bash tests (fallback):
```bash
make test-integration
```

### For Local Development

Run Go tests locally (requires operator already deployed):
```bash
make test-integration-go-fake
```

## Testing the New Approach

To test this in your PR:

1. The new `make test-integration-go-prow` target is available
2. PROW config would need to be updated to use it (external config repo)
3. For now, this can be tested manually by:
   - Commenting out the Build step in bootstrap
   - OR running `make test-integration-go-prow` directly in PROW pod

## Migration Status

- ✅ FAKE AccountClaim test migrated to Go
- ⏳ 7 more tests to migrate (accountpool, STS, KMS, etc.)

## Benefits

1. **Reliability**: No dependency on Build API availability
2. **Speed**: Skips unnecessary rebuild (saves ~2-3 minutes)
3. **Simplicity**: ~100 lines vs ~600 lines of bash
4. **Maintainability**: Go tests easier to debug and extend
5. **Type Safety**: Compile-time checks vs runtime bash errors

## Next Steps

1. Validate this approach works in PROW
2. Migrate remaining bash tests to Go
3. Update central PROW config to use `make test-integration-go-prow`
4. Deprecate bash bootstrap for PROW (keep for local dev if needed)
