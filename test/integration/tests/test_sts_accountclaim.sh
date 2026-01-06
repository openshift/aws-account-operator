#!/usr/bin/env bash

# Test Description:
#  This test validates the STS (Security Token Service) AccountClaim workflow.
#  STS AccountClaims use customer-provided AWS accounts with temporary credentials
#  via AWS STS assume-role.
#
#  The test:
#  1. Creates a namespace for the STS AccountClaim
#  2. Creates an STS AccountClaim (with byoc=true, manualSTSMode=true)
#  3. Waits for the claim to become Ready
#  4. Validates AccountClaim fields (byoc, manualSTSMode, stsRoleARN, etc.)
#  5. Validates that an Account CR was created and linked
#  6. Validates Account CR fields match the claim
#  7. Validates legal entities match between claim and account
#  8. Cleans up resources
#
#  This validates:
#  - STS AccountClaims create linked Account CRs
#  - STS mode configuration is properly propagated
#  - BYOC (Bring Your Own Cloud) fields are set correctly

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
EXIT_TEST_FAIL_BYOC_NOT_SET=2
EXIT_TEST_FAIL_WRONG_ACCOUNT_ID=3
EXIT_TEST_FAIL_NO_ACCOUNT_LINK=4
EXIT_TEST_FAIL_MANUAL_STS_NOT_SET=5
EXIT_TEST_FAIL_WRONG_STS_ROLE=6
EXIT_TEST_FAIL_ACCOUNT_FIELD_MISMATCH=7
EXIT_TEST_FAIL_LEGAL_ENTITY_MISMATCH=8

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAIL_NO_FINALIZER]="STS AccountClaim does not have finalizers set."
exitCodeMessages[$EXIT_TEST_FAIL_BYOC_NOT_SET]="STS AccountClaim should have .spec.byoc set to true."
exitCodeMessages[$EXIT_TEST_FAIL_WRONG_ACCOUNT_ID]="AWS Account ID mismatch."
exitCodeMessages[$EXIT_TEST_FAIL_NO_ACCOUNT_LINK]="STS AccountClaim should create an Account CR to link."
exitCodeMessages[$EXIT_TEST_FAIL_MANUAL_STS_NOT_SET]="manualSTSMode should be set to true."
exitCodeMessages[$EXIT_TEST_FAIL_WRONG_STS_ROLE]="STS Role ARN mismatch."
exitCodeMessages[$EXIT_TEST_FAIL_ACCOUNT_FIELD_MISMATCH]="Account CR fields do not match AccountClaim."
exitCodeMessages[$EXIT_TEST_FAIL_LEGAL_ENTITY_MISMATCH]="Legal entities differ between Account and AccountClaim."

stsClaimName="${STS_CLAIM_NAME}"
stsNamespace="${STS_NAMESPACE_NAME}"
stsAccountId="${OSD_STAGING_2_AWS_ACCOUNT_ID}"
stsRoleArn="${STS_ROLE_ARN}"
accountCrNamespace="${NAMESPACE}"

function explain {
    exitCode=$1
    echo "${exitCodeMessages[$exitCode]}"
}

function setup {
    echo "=============================================================="
    echo "SETUP: Creating namespace and STS AccountClaim"
    echo "=============================================================="

    echo "Creating namespace: ${stsNamespace}"
    createNamespace "${stsNamespace}" || return $?

    echo "Creating STS AccountClaim: ${stsClaimName}"
    local claimYaml
    claimYaml=$(oc process --local -p NAME="${stsClaimName}" -p NAMESPACE="${stsNamespace}" -p STS_ACCOUNT_ID="${stsAccountId}" -p STS_ROLE_ARN="${stsRoleArn}" -f hack/templates/aws.managed.openshift.io_v1alpha1_sts_accountclaim_cr.tmpl)
    ocCreateResourceIfNotExists "${claimYaml}" || return $?

    echo "Waiting for STS AccountClaim to become Ready..."
    # STS claims take longer to process than regular claims
    timeout="${STS_CLAIM_READY_TIMEOUT:-3m}"
    waitForAccountClaimCRReadyOrFailed "${stsClaimName}" "${stsNamespace}" "${timeout}" || return $?

    echo "✓ Setup complete"
    return 0
}

function test {
    echo "=============================================================="
    echo "TEST: Validating STS AccountClaim and Account"
    echo "=============================================================="

    echo "Getting STS AccountClaim..."
    local claimYaml
    claimYaml=$(generateAccountClaimCRYaml "${stsClaimName}" "${stsNamespace}")
    local accClaim
    accClaim=$(ocGetResourceAsJson "${claimYaml}" | jq -r '.items[0]')

    echo ""
    echo "--- Validating AccountClaim fields ---"

    echo "Checking finalizers..."
    local finalizerCount
    finalizerCount=$(echo "$accClaim" | jq '.metadata.finalizers | length')
    if [ "$finalizerCount" -lt 1 ]; then
        echo "ERROR: No finalizers set on STS accountclaim"
        return $EXIT_TEST_FAIL_NO_FINALIZER
    fi
    echo "✓ Finalizers present: $finalizerCount"

    echo "Checking spec.byoc..."
    local byoc
    byoc=$(echo "$accClaim" | jq -r '.spec.byoc')
    if [ "$byoc" != "true" ]; then
        echo "ERROR: STS AccountClaim should have .spec.byoc=true, got: ${byoc}"
        return $EXIT_TEST_FAIL_BYOC_NOT_SET
    fi
    echo "✓ spec.byoc is true"

    echo "Checking spec.byocAWSAccountID..."
    local claimAccountId
    claimAccountId=$(echo "$accClaim" | jq -r '.spec.byocAWSAccountID')
    if [ "$claimAccountId" != "${stsAccountId}" ]; then
        echo "ERROR: Expected byocAWSAccountID=${stsAccountId}, got: ${claimAccountId}"
        return $EXIT_TEST_FAIL_WRONG_ACCOUNT_ID
    fi
    echo "✓ spec.byocAWSAccountID is ${stsAccountId}"

    echo "Checking spec.accountLink..."
    local accountLink
    accountLink=$(echo "$accClaim" | jq -r '.spec.accountLink // ""')
    if [ -z "$accountLink" ]; then
        echo "ERROR: STS AccountClaim should create an Account CR to link"
        return $EXIT_TEST_FAIL_NO_ACCOUNT_LINK
    fi
    echo "✓ spec.accountLink is set: ${accountLink}"

    echo "Checking spec.manualSTSMode..."
    local claimManualSts
    claimManualSts=$(echo "$accClaim" | jq -r '.spec.manualSTSMode')
    if [ "$claimManualSts" != "true" ]; then
        echo "ERROR: STS AccountClaim should have .spec.manualSTSMode=true, got: ${claimManualSts}"
        return $EXIT_TEST_FAIL_MANUAL_STS_NOT_SET
    fi
    echo "✓ spec.manualSTSMode is true"

    echo "Checking spec.stsRoleARN..."
    local claimStsRole
    claimStsRole=$(echo "$accClaim" | jq -r '.spec.stsRoleARN')
    if [ "$claimStsRole" != "${stsRoleArn}" ]; then
        echo "ERROR: Expected stsRoleARN=${stsRoleArn}, got: ${claimStsRole}"
        return $EXIT_TEST_FAIL_WRONG_STS_ROLE
    fi
    echo "✓ spec.stsRoleARN is ${stsRoleArn}"

    local claimLegalEntity
    claimLegalEntity=$(echo "$accClaim" | jq -c '.spec.legalEntity')
    echo "  AccountClaim legal entity: ${claimLegalEntity}"

    echo ""
    echo "--- Validating Account CR ---"

    echo "Getting Account CR: ${accountLink}"
    local account
    account=$(oc get account "${accountLink}" -n "${accountCrNamespace}" -o json)

    echo "Checking Account spec.manualSTSMode..."
    local accountManualSts
    accountManualSts=$(echo "$account" | jq -r '.spec.manualSTSMode')
    if [ "$accountManualSts" != "true" ]; then
        echo "ERROR: Account should have .spec.manualSTSMode=true, got: ${accountManualSts}"
        return $EXIT_TEST_FAIL_MANUAL_STS_NOT_SET
    fi
    echo "✓ Account spec.manualSTSMode is true"

    echo "Checking Account spec.awsAccountID..."
    local accountAwsId
    accountAwsId=$(echo "$account" | jq -r '.spec.awsAccountID')
    if [ "$accountAwsId" != "${stsAccountId}" ]; then
        echo "ERROR: Account .spec.awsAccountID should be ${stsAccountId}, got: ${accountAwsId}"
        return $EXIT_TEST_FAIL_WRONG_ACCOUNT_ID
    fi
    echo "✓ Account spec.awsAccountID is ${stsAccountId}"

    echo "Checking Account spec.byoc..."
    local accountByoc
    accountByoc=$(echo "$account" | jq -r '.spec.byoc')
    if [ "$accountByoc" != "true" ]; then
        echo "ERROR: Account should have .spec.byoc=true, got: ${accountByoc}"
        return $EXIT_TEST_FAIL_BYOC_NOT_SET
    fi
    echo "✓ Account spec.byoc is true"

    echo "Checking Account spec.claimLink..."
    local accountClaimLink
    accountClaimLink=$(echo "$account" | jq -r '.spec.claimLink')
    if [ "$accountClaimLink" != "${stsClaimName}" ]; then
        echo "ERROR: Account .spec.claimLink should be ${stsClaimName}, got: ${accountClaimLink}"
        return $EXIT_TEST_FAIL_ACCOUNT_FIELD_MISMATCH
    fi
    echo "✓ Account spec.claimLink is ${stsClaimName}"

    echo "Checking Account spec.claimLinkNamespace..."
    local accountClaimNs
    accountClaimNs=$(echo "$account" | jq -r '.spec.claimLinkNamespace')
    if [ "$accountClaimNs" != "${stsNamespace}" ]; then
        echo "ERROR: Account .spec.claimLinkNamespace should be ${stsNamespace}, got: ${accountClaimNs}"
        return $EXIT_TEST_FAIL_ACCOUNT_FIELD_MISMATCH
    fi
    echo "✓ Account spec.claimLinkNamespace is ${stsNamespace}"

    echo "Verifying legal entities match..."
    local accountLegalEntity
    accountLegalEntity=$(echo "$account" | jq -c '.spec.legalEntity')
    echo "  Account legal entity: ${accountLegalEntity}"

    local equalLegalEntities
    equalLegalEntities=$(diff <(jq -S <<< "$accountLegalEntity") <(jq -S <<< "$claimLegalEntity") || echo "differ")
    if [ "${#equalLegalEntities}" -gt 0 ]; then
        echo "ERROR: Legal entities differ between Account and AccountClaim"
        return $EXIT_TEST_FAIL_LEGAL_ENTITY_MISMATCH
    fi
    echo "✓ Legal entities match"

    echo ""
    echo "========================================"
    echo "STS ACCOUNTCLAIM TEST PASSED!"
    echo "========================================"
    echo "✓ STS AccountClaim has correct BYOC and STS configuration"
    echo "✓ Account CR was created and linked correctly"
    echo "✓ All fields match between AccountClaim and Account"
    echo "✓ Legal entities are consistent"

    return 0
}

function cleanup {
    echo "=============================================================="
    echo "CLEANUP: Removing test resources"
    echo "=============================================================="

    local cleanupExitCode=0

    echo "Deleting STS AccountClaim..."
    deleteAccountClaimCR "${stsClaimName}" "${stsNamespace}" "${RESOURCE_DELETE_TIMEOUT}" true 2>/dev/null || {
        echo "WARNING: Failed to delete STS AccountClaim"
        cleanupExitCode=$EXIT_FAIL_UNEXPECTED_ERROR
    }

    echo "Deleting namespace..."
    deleteNamespace "${stsNamespace}" "${RESOURCE_DELETE_TIMEOUT}" true 2>/dev/null || {
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
