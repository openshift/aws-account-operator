#!/usr/bin/env bash

# Test Description:
#  This test validates the FAKE AccountClaim workflow which creates an AccountClaim
#  that does NOT create an actual AWS Account, but instead creates a secret with
#  fake credentials for testing purposes.
#
#  The test:
#  1. Creates a namespace for the FAKE AccountClaim
#  2. Creates a FAKE AccountClaim (accountOU: "fake")
#  3. Waits for the claim to become Ready
#  4. Verifies the claim has finalizers
#  5. Verifies the claim has NO accountLink (no Account CR created)
#  6. Verifies a secret was created in the claim namespace
#  7. Cleans up the claim and namespace
#
#  This validates:
#  - FAKE AccountClaims don't create Account CRs
#  - FAKE AccountClaims create AWS credential secrets
#  - FAKE mode works for testing without real AWS resources

source test/integration/integration-test-lib.sh
source test/integration/test_envs

# Run pre-flight checks
if [ "${SKIP_PREFLIGHT_CHECKS:-false}" != "true" ]; then
    if ! preflightChecks; then
        echo "Pre-flight checks failed. Set SKIP_PREFLIGHT_CHECKS=true to bypass."
        exit $EXIT_FAIL_UNEXPECTED_ERROR
    fi
fi

EXIT_TEST_FAIL_NO_FINALIZER=1
EXIT_TEST_FAIL_HAS_ACCOUNT_LINK=2
EXIT_TEST_FAIL_NO_SECRET=3

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAIL_NO_FINALIZER]="FAKE AccountClaim does not have finalizers set."
exitCodeMessages[$EXIT_TEST_FAIL_HAS_ACCOUNT_LINK]="FAKE AccountClaim should NOT have an accountLink."
exitCodeMessages[$EXIT_TEST_FAIL_NO_SECRET]="Expected secret not found in claim namespace."

fakeClaimName="${FAKE_CLAIM_NAME}"
fakeNamespace="${FAKE_NAMESPACE_NAME}"

function explain {
    exitCode=$1
    echo "${exitCodeMessages[$exitCode]}"
}

function setup {
    echo "=============================================================="
    echo "SETUP: Creating namespace and FAKE AccountClaim"
    echo "=============================================================="

    echo "Creating namespace: ${fakeNamespace}"
    createNamespace "${fakeNamespace}" || return $?

    echo "Creating FAKE AccountClaim: ${fakeClaimName}"
    local claimYaml
    claimYaml=$(oc process --local -p NAME="${fakeClaimName}" -p NAMESPACE="${fakeNamespace}" -f hack/templates/aws.managed.openshift.io_v1alpha1_fake_accountclaim_cr.tmpl)
    ocCreateResourceIfNotExists "${claimYaml}" || return $?

    echo "Waiting for FAKE AccountClaim to become Ready..."
    timeout="${ACCOUNT_CLAIM_READY_TIMEOUT}"
    waitForAccountClaimCRReadyOrFailed "${fakeClaimName}" "${fakeNamespace}" "${timeout}" || return $?

    echo "✓ Setup complete"
    return 0
}

function test {
    echo "=============================================================="
    echo "TEST: Validating FAKE AccountClaim"
    echo "=============================================================="

    echo "Getting AccountClaim: ${fakeClaimName}"
    local claimYaml
    claimYaml=$(generateAccountClaimCRYaml "${fakeClaimName}" "${fakeNamespace}")
    local accClaim
    accClaim=$(ocGetResourceAsJson "${claimYaml}" | jq -r '.items[0]')

    echo "Validating AccountClaim has finalizers..."
    local finalizerCount
    finalizerCount=$(echo "$accClaim" | jq '.metadata.finalizers | length')
    if [ "$finalizerCount" -lt 1 ]; then
        echo "ERROR: No finalizers set on FAKE accountclaim"
        return $EXIT_TEST_FAIL_NO_FINALIZER
    fi
    echo "✓ Finalizers present: $finalizerCount"

    echo "Validating there is NO accountLink..."
    local accountLink
    accountLink=$(echo "$accClaim" | jq -r '.spec.accountLink // ""')
    if [ -n "$accountLink" ]; then
        echo "ERROR: AccountLink should be empty but is: ${accountLink}"
        echo "  FAKE AccountClaims should NOT create Account CRs"
        return $EXIT_TEST_FAIL_HAS_ACCOUNT_LINK
    fi
    echo "✓ No accountLink (as expected for FAKE claims)"

    echo "Validating secret exists in namespace..."
    if ! retryWithBackoff "$MAX_RETRIES" oc get secret aws -n "${fakeNamespace}" &>/dev/null; then
        echo "ERROR: Secret ${fakeNamespace}/aws does not exist"
        return $EXIT_TEST_FAIL_NO_SECRET
    fi
    echo "✓ Secret 'aws' exists in namespace ${fakeNamespace}"

    echo ""
    echo "========================================"
    echo "FAKE ACCOUNTCLAIM TEST PASSED!"
    echo "========================================"
    echo "✓ FAKE AccountClaim has finalizers"
    echo "✓ FAKE AccountClaim has no accountLink (no Account CR created)"
    echo "✓ FAKE AccountClaim created AWS credentials secret"

    return 0
}

function cleanup {
    echo "=============================================================="
    echo "CLEANUP: Removing test resources"
    echo "=============================================================="

    local cleanupExitCode=0

    echo "Deleting FAKE AccountClaim..."
    deleteAccountClaimCR "${fakeClaimName}" "${fakeNamespace}" "${RESOURCE_DELETE_TIMEOUT}" true 2>/dev/null || {
        echo "WARNING: Failed to delete FAKE AccountClaim"
        cleanupExitCode=$EXIT_FAIL_UNEXPECTED_ERROR
    }

    echo "Deleting namespace..."
    deleteNamespace "${fakeNamespace}" "${RESOURCE_DELETE_TIMEOUT}" true 2>/dev/null || {
        echo "WARNING: Failed to delete namespace"
        cleanupExitCode=$EXIT_FAIL_UNEXPECTED_ERROR
    }

    echo "✓ Cleanup complete"
    return $cleanupExitCode
}

# Handle the explain command
if [ "${1:-}" == "explain" ]; then
    explain "$2"
    exit 0
fi

# Main test execution
case "${1:-}" in
    setup)
        setup
        ;;
    test)
        test
        ;;
    cleanup)
        cleanup
        ;;
    *)
        echo "Usage: $0 {setup|test|cleanup|explain <exit_code>}"
        exit 1
        ;;
esac
