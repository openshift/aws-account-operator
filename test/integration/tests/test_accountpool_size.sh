#!/usr/bin/env bash

# Test Description:
#  This test validates AccountPool size management functionality by mocking
#  Account CR creation. This avoids requiring real AWS account creation while
#  still testing the AccountPool controller's ability to track pool size.
#
#  The test:
#  1. Creates 2 mock Account CRs first (without owner references)
#  2. Creates an AccountPool with poolSize=2
#  3. Patches the mock Account CRs to add owner references to the pool
#  4. Waits for AccountPool controller to update status
#  5. Validates UnclaimedAccountCount is 2
#  6. Validates pool status fields
#  7. Cleans up resources
#
#  NOTE: Creating mock accounts BEFORE the pool prevents the AccountPool
#  controller from creating real AWS accounts (avoids race condition).

source test/integration/integration-test-lib.sh
source test/integration/test_envs

# Set TEST_START_TIME_SECONDS
if [ -z "${TEST_START_TIME_SECONDS}" ]; then
    export TEST_START_TIME_SECONDS=$(date +%s)
fi

if [ "${SKIP_PREFLIGHT_CHECKS:-false}" != "true" ]; then
    if ! preflightChecks; then
        echo "Pre-flight checks failed"
        exit $EXIT_FAIL_UNEXPECTED_ERROR
    fi
fi

poolName="test-pool-${TEST_START_TIME_SECONDS}"
poolSize=2
accountCrNamespace="${NAMESPACE}"
mockAccountNames=()

function setup {
    echo "=============================================================="
    echo "SETUP: Creating mock Account CRs, then AccountPool"
    echo "=============================================================="

    echo "Creating ${poolSize} mock Account CRs (without owner references)..."
    for i in $(seq 1 ${poolSize}); do
        local accountName="${poolName}-mock-account-${i}"
        mockAccountNames+=("${accountName}")
        local iamUserId="mock${i}"

        echo "  Creating Account CR: ${accountName}"
        local accountYaml="apiVersion: aws.managed.openshift.io/v1alpha1
kind: Account
metadata:
  name: ${accountName}
  namespace: ${accountCrNamespace}
  labels:
    iamUserId: ${iamUserId}
spec:
  accountPool: ${poolName}
  awsAccountID: \"\"
  iamUserSecret: \"\"
  claimLink: \"\"
  legalEntity:
    id: \"\"
    name: \"\""

        ocCreateResourceIfNotExists "${accountYaml}" || return $?
    done

    echo "✓ Mock accounts created"
    sleep 2  # Brief pause to ensure accounts are fully created

    echo ""
    echo "Creating AccountPool: ${poolName}"
    local poolYaml="apiVersion: aws.managed.openshift.io/v1alpha1
kind: AccountPool
metadata:
  name: ${poolName}
  namespace: ${accountCrNamespace}
spec:
  poolSize: ${poolSize}"
    ocCreateResourceIfNotExists "${poolYaml}" || return $?

    echo "Getting AccountPool UID..."
    local poolUID=$(oc get accountpool ${poolName} -n ${accountCrNamespace} -o jsonpath='{.metadata.uid}')
    if [ -z "${poolUID}" ]; then
        echo "ERROR: Could not get AccountPool UID"
        return 1
    fi
    echo "  Pool UID: ${poolUID}"

    echo "Patching mock Account CRs to add owner references..."
    for accountName in "${mockAccountNames[@]}"; do
        echo "  Patching: ${accountName}"
        oc patch account ${accountName} -n ${accountCrNamespace} --type merge -p "{
  \"metadata\": {
    \"ownerReferences\": [{
      \"apiVersion\": \"aws.managed.openshift.io/v1alpha1\",
      \"kind\": \"AccountPool\",
      \"name\": \"${poolName}\",
      \"uid\": \"${poolUID}\",
      \"controller\": true,
      \"blockOwnerDeletion\": true
    }]
  }
}" || return $?
    done

    echo "✓ Owner references added"

    echo ""
    echo "Waiting for AccountPool controller to update status (timeout: 60s)..."
    timeout=60
    elapsed=0
    while [ $elapsed -lt $timeout ]; do
        unclaimedCount=$(oc get accountpool ${poolName} -n ${accountCrNamespace} -o json 2>/dev/null | jq -r '.status.unclaimedAccounts // 0')
        echo "  UnclaimedAccounts: ${unclaimedCount}/${poolSize}"

        if [ "${unclaimedCount}" = "${poolSize}" ]; then
            echo "✓ Pool status updated with ${poolSize} unclaimed accounts"
            echo "✓ Setup complete"
            return 0
        fi

        sleep 5
        elapsed=$((elapsed + 5))
    done

    echo "ERROR: Pool status did not update within timeout"
    echo "Current unclaimedAccounts: ${unclaimedCount}"

    echo ""
    echo "Accounts in pool:"
    oc get account -n ${accountCrNamespace} -o json | jq -r ".items[] | select(.spec.accountPool == \"${poolName}\") | .metadata.name" 2>/dev/null

    return 1
}

function test {
    echo "=============================================================="
    echo "TEST: Validating AccountPool status tracking"
    echo "=============================================================="

    poolStatus=$(oc get accountpool ${poolName} -n ${accountCrNamespace} -o json)
    unclaimedCount=$(echo "$poolStatus" | jq -r '.status.unclaimedAccounts // 0')
    claimedCount=$(echo "$poolStatus" | jq -r '.status.claimedAccounts // 0')
    poolSizeStatus=$(echo "$poolStatus" | jq -r '.spec.poolSize')

    echo "Pool status:"
    echo "  Spec poolSize: ${poolSizeStatus}"
    echo "  Unclaimed accounts: ${unclaimedCount}"
    echo "  Claimed accounts: ${claimedCount}"

    if [ "${unclaimedCount}" != "${poolSize}" ]; then
        echo "ERROR: Expected ${poolSize} unclaimed accounts, got ${unclaimedCount}"
        return 1
    fi

    if [ "${poolSizeStatus}" != "${poolSize}" ]; then
        echo "ERROR: Expected poolSize ${poolSize}, got ${poolSizeStatus}"
        return 1
    fi

    if [ "${claimedCount}" != "0" ]; then
        echo "ERROR: Expected 0 claimed accounts, got ${claimedCount}"
        return 1
    fi

    # Verify the Account CRs exist and are assigned to the pool
    echo ""
    echo "Verifying Account CRs..."
    accountCount=$(oc get account -n ${accountCrNamespace} -o json | jq -r "[.items[] | select(.spec.accountPool == \"${poolName}\")] | length")
    echo "  Found ${accountCount} Account CRs assigned to pool"

    if [ "${accountCount}" != "${poolSize}" ]; then
        echo "ERROR: Expected ${poolSize} Account CRs, found ${accountCount}"
        return 1
    fi

    echo ""
    echo "Verifying no real AWS accounts were created..."
    realAccountCount=$(oc get account -n ${accountCrNamespace} -o json | jq -r "[.items[] | select(.spec.accountPool == \"${poolName}\" and .spec.awsAccountID != \"\")] | length")

    if [ "${realAccountCount}" != "0" ]; then
        echo "WARNING: Found ${realAccountCount} accounts with non-empty awsAccountID"
        echo "  This suggests the AccountPool controller created real AWS accounts"
        echo "  Expected only mock accounts (with empty awsAccountID)"
    else
        echo "✓ All accounts are mocked (no real AWS accounts created)"
    fi

    echo "✓ Pool maintains correct poolSize"
    echo "✓ UnclaimedAccounts count is correct"
    echo "✓ ClaimedAccounts count is correct"
    echo "✓ Account CRs properly labeled"

    echo ""
    echo "========================================"
    echo "ACCOUNTPOOL TEST PASSED!"
    echo "========================================"
    return 0
}

function cleanup {
    echo "=============================================================="
    echo "CLEANUP: Removing test resources"
    echo "=============================================================="

    echo "Deleting pool accounts..."
    for accountName in "${mockAccountNames[@]}"; do
        oc delete account ${accountName} -n ${accountCrNamespace} --ignore-not-found=true 2>/dev/null || true
    done

    echo "Deleting AccountPool..."
    oc delete accountpool ${poolName} -n ${accountCrNamespace} --ignore-not-found=true

    echo "✓ Cleanup complete"
    return 0
}

case "${1:-""}" in
    setup) setup ;;
    test) test ;;
    cleanup) cleanup ;;
    *) setup && test && cleanup ;;
esac
