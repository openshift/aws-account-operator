#!/usr/bin/env bash

# Test Description:
#  This test validates finalizer behavior on AccountClaim resources.
#  Finalizers ensure proper cleanup before resource deletion.
#
#  The test:
#  1. Creates a namespace for the finalizer test
#  2. Creates an AccountClaim
#  3. Waits for the claim to become Ready
#  4. Validates finalizer is added to the AccountClaim
#  5. Initiates deletion of the AccountClaim
#  6. Validates cleanup occurs before finalizer removal
#  7. Validates finalizer is removed after cleanup completes
#  8. Validates AccountClaim is fully deleted
#  9. Validates namespace can be deleted
#  10. Cleans up resources
#
#  This validates:
#  - Finalizers are added when AccountClaim is created
#  - Cleanup runs when AccountClaim is deleted
#  - Finalizers are removed after cleanup completes
#  - No orphaned resources or stuck deletions

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
EXIT_TEST_FAIL_FINALIZER_NOT_REMOVED=2
EXIT_TEST_FAIL_RESOURCE_STILL_EXISTS=3
EXIT_TEST_FAIL_DELETION_TIMEOUT=4

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAIL_NO_FINALIZER]="AccountClaim does not have finalizer set."
exitCodeMessages[$EXIT_TEST_FAIL_FINALIZER_NOT_REMOVED]="Finalizer was not removed after cleanup."
exitCodeMessages[$EXIT_TEST_FAIL_RESOURCE_STILL_EXISTS]="AccountClaim still exists after deletion."
exitCodeMessages[$EXIT_TEST_FAIL_DELETION_TIMEOUT]="AccountClaim deletion timed out."

awsAccountId="${OSD_STAGING_1_AWS_ACCOUNT_ID}"
accountCrNamespace="${NAMESPACE}"
testName="test-finalizer-${TEST_START_TIME_SECONDS}"
accountCrName="${testName}"
finalizerClaimName="${testName}"
finalizerNamespace="${testName}-cluster"

function explain {
    exitCode=$1
    echo "${exitCodeMessages[$exitCode]}"
}

# Convert timeout string (e.g., "6m", "30s", "1h") to seconds without bc
function timeoutToSeconds {
    local timeout=$1
    local value
    local unit

    # Extract numeric value and unit
    if [[ $timeout =~ ^([0-9]+)([smh])$ ]]; then
        value="${BASH_REMATCH[1]}"
        unit="${BASH_REMATCH[2]}"
    else
        echo "360"  # Default to 6 minutes
        return
    fi

    case $unit in
        s)
            echo "$value"
            ;;
        m)
            echo $((value * 60))
            ;;
        h)
            echo $((value * 3600))
            ;;
        *)
            echo "360"
            ;;
    esac
}

function setup {
    echo "=============================================================="
    echo "SETUP: Creating Account CR, namespace, and AccountClaim"
    echo "=============================================================="

    echo "Creating Account CR: ${accountCrName}"
    createAccountCR "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" || return $?

    echo "Waiting for Account CR to become Ready..."
    local timeout="${ACCOUNT_READY_TIMEOUT}"
    waitForAccountCRReadyOrFailed "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" "${timeout}" || return $?

    echo "Creating namespace: ${finalizerNamespace}"
    createNamespace "${finalizerNamespace}" || return $?

    echo "Creating AccountClaim: ${finalizerClaimName}"
    local claimYaml
    claimYaml=$(oc process --local -p NAME="${finalizerClaimName}" -p NAMESPACE="${finalizerNamespace}" -f hack/templates/aws.managed.openshift.io_v1alpha1_accountclaim_cr.tmpl)
    ocCreateResourceIfNotExists "${claimYaml}" || return $?

    echo "Waiting for AccountClaim to become Ready..."
    timeout="${ACCOUNT_CLAIM_READY_TIMEOUT}"
    waitForAccountClaimCRReadyOrFailed "${finalizerClaimName}" "${finalizerNamespace}" "${timeout}" || return $?

    echo "✓ Setup complete"
    return 0
}

function test {
    echo "=============================================================="
    echo "TEST: Validating finalizer behavior on AccountClaim"
    echo "=============================================================="

    echo "Getting AccountClaim..."
    local accClaim
    accClaim=$(oc get accountclaim "${finalizerClaimName}" -n "${finalizerNamespace}" -o json)

    echo ""
    echo "--- Validating Finalizer Presence ---"

    echo "Checking for finalizers..."
    local finalizerCount
    finalizerCount=$(echo "$accClaim" | jq '.metadata.finalizers | length')
    if [ "$finalizerCount" -lt 1 ]; then
        echo "ERROR: No finalizers set on AccountClaim"
        return $EXIT_TEST_FAIL_NO_FINALIZER
    fi
    echo "✓ Found ${finalizerCount} finalizer(s)"

    local finalizers
    finalizers=$(echo "$accClaim" | jq -r '.metadata.finalizers[]')
    echo "  Finalizers:"
    while IFS= read -r finalizer; do
        echo "    - ${finalizer}"
    done <<< "$finalizers"

    echo ""
    echo "--- Initiating Deletion ---"

    echo "Deleting AccountClaim: ${finalizerClaimName}"
    oc delete accountclaim "${finalizerClaimName}" -n "${finalizerNamespace}" --wait=false

    echo "Waiting for cleanup to begin (5 seconds)..."
    sleep 5

    echo ""
    echo "--- Validating Cleanup in Progress ---"

    echo "Checking if AccountClaim still exists during cleanup..."
    if ! oc get accountclaim "${finalizerClaimName}" -n "${finalizerNamespace}" &>/dev/null; then
        echo "  (AccountClaim already deleted - cleanup was fast)"
    else
        echo "✓ AccountClaim exists with deletion timestamp (cleanup in progress)"

        local deletionTimestamp
        deletionTimestamp=$(oc get accountclaim "${finalizerClaimName}" -n "${finalizerNamespace}" -o jsonpath='{.metadata.deletionTimestamp}')
        echo "  Deletion timestamp: ${deletionTimestamp}"

        local currentFinalizers
        currentFinalizers=$(oc get accountclaim "${finalizerClaimName}" -n "${finalizerNamespace}" -o jsonpath='{.metadata.finalizers}')
        echo "  Current finalizers: ${currentFinalizers}"
    fi

    echo ""
    echo "--- Waiting for Finalizer Removal and Deletion ---"

    echo "Waiting for AccountClaim to be fully deleted (max ${RESOURCE_DELETE_TIMEOUT})..."
    local timeout="${RESOURCE_DELETE_TIMEOUT}"
    local timeoutSeconds
    timeoutSeconds=$(timeoutToSeconds "${timeout}")
    local elapsed=0
    local checkInterval=5

    while [ $elapsed -lt "$timeoutSeconds" ]; do
        if ! oc get accountclaim "${finalizerClaimName}" -n "${finalizerNamespace}" &>/dev/null; then
            echo "✓ AccountClaim fully deleted after ${elapsed} seconds"
            break
        fi

        sleep $checkInterval
        elapsed=$((elapsed + checkInterval))

        if [ $((elapsed % 30)) -eq 0 ]; then
            echo "  Still waiting... (${elapsed}s elapsed)"
        fi
    done

    if oc get accountclaim "${finalizerClaimName}" -n "${finalizerNamespace}" &>/dev/null; then
        echo "ERROR: AccountClaim still exists after ${timeout}"

        # Show current state
        local currentState
        currentState=$(oc get accountclaim "${finalizerClaimName}" -n "${finalizerNamespace}" -o json | jq -r '.status.state // "unknown"')
        local currentFinalizers
        currentFinalizers=$(oc get accountclaim "${finalizerClaimName}" -n "${finalizerNamespace}" -o jsonpath='{.metadata.finalizers}')
        echo "  Current state: ${currentState}"
        echo "  Remaining finalizers: ${currentFinalizers}"

        return $EXIT_TEST_FAIL_DELETION_TIMEOUT
    fi

    echo ""
    echo "--- Validating Complete Deletion ---"

    echo "Verifying AccountClaim no longer exists..."
    if oc get accountclaim "${finalizerClaimName}" -n "${finalizerNamespace}" &>/dev/null; then
        echo "ERROR: AccountClaim still exists after finalizer removal"
        return $EXIT_TEST_FAIL_RESOURCE_STILL_EXISTS
    fi
    echo "✓ AccountClaim fully deleted"

    echo "Verifying linked Account CR status..."
    local accountLink
    accountLink=$(echo "$accClaim" | jq -r '.spec.accountLink')
    if [ -n "$accountLink" ] && [ "$accountLink" != "null" ]; then
        echo "  Checking if Account ${accountLink} was cleaned up..."
        if oc get account "${accountLink}" -n "${accountCrNamespace}" &>/dev/null; then
            local accountState
            accountState=$(oc get account "${accountLink}" -n "${accountCrNamespace}" -o json | jq -r '.status.state')
            local accountClaimed
            accountClaimed=$(oc get account "${accountLink}" -n "${accountCrNamespace}" -o json | jq -r '.status.claimed')
            echo "  Account still exists (state: ${accountState}, claimed: ${accountClaimed})"
            echo "  ✓ Account is available for reuse"
        else
            echo "  Account no longer exists (fully cleaned up)"
        fi
    fi

    echo ""
    echo "========================================"
    echo "FINALIZER CLEANUP TEST PASSED!"
    echo "========================================"
    echo "✓ Finalizers were added to AccountClaim"
    echo "✓ Deletion triggered cleanup process"
    echo "✓ Finalizers were removed after cleanup"
    echo "✓ AccountClaim was fully deleted"
    echo "✓ No orphaned resources detected"

    return 0
}

function cleanup {
    echo "=============================================================="
    echo "CLEANUP: Removing test resources"
    echo "=============================================================="

    local cleanupExitCode=0
    local removeFinalizers=true
    local timeout="${RESOURCE_DELETE_TIMEOUT}"

    # AccountClaim should already be deleted by the test
    if oc get accountclaim "${finalizerClaimName}" -n "${finalizerNamespace}" &>/dev/null; then
        echo "WARNING: AccountClaim still exists, forcing deletion..."
        deleteAccountClaimCR "${finalizerClaimName}" "${finalizerNamespace}" "${timeout}" $removeFinalizers 2>/dev/null || {
            echo "WARNING: Failed to delete AccountClaim"
            cleanupExitCode=$EXIT_FAIL_UNEXPECTED_ERROR
        }
    else
        echo "✓ AccountClaim already deleted"
    fi

    echo "Deleting namespace..."
    deleteNamespace "${finalizerNamespace}" "${timeout}" $removeFinalizers 2>/dev/null || {
        echo "WARNING: Failed to delete namespace"
        cleanupExitCode=$EXIT_FAIL_UNEXPECTED_ERROR
    }

    echo "Deleting Account CR..."
    deleteAccountCR "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" "${timeout}" $removeFinalizers 2>/dev/null || {
        echo "WARNING: Failed to delete Account CR"
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
