#!/usr/bin/env bash

# Test Description:
#   When an AccountClaim is created, AAO finds an available Account CR in the account pool to satisfy
#   the claim. AAO then moves the AWS Account from the root OU ($OSD_STAGING_1_OU_ROOT_ID) to an OU
#   under the base OU ($OSD_STAGING_1_OU_BASE_ID) based on the legal entity that owns the cluster.
#   
#   In otherwords the AWS OU structure for an unclaimed account looks like this:
#     ou: $OSD_STAGING_1_OU_ROOT_ID -> aws account: $OSD_STAGING_1_AWS_ACCOUNT_ID 
#
#   After the account is claimed, the OU structure looks like this: 
#     ou: $OSD_STAGING_1_OU_ROOT_ID -> ou: $OSD_STAGING_1_OU_BASE_ID -> ou: legal entity id from account claim -> aws account: $OSD_STAGING_1_AWS_ACCOUNT_ID
#
#   So, this test validates that the AWS account is moved from the root OU to the base OU.

source test/integration/integration-test-lib.sh

# Run pre-flight checks
if [ "${SKIP_PREFLIGHT_CHECKS:-false}" != "true" ]; then
    if ! preflightChecks; then
        echo "Pre-flight checks failed. Set SKIP_PREFLIGHT_CHECKS=true to bypass."
        exit $EXIT_FAIL_UNEXPECTED_ERROR
    fi
fi

EXIT_TEST_FAILED_MOVE_ACCOUNT_ROOT=1

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAILED_MOVE_ACCOUNT_ROOT]="Failed to move account out of root. Check AAO logs for more details."

# isolate global env variable use to prevent them spreading too deep into the tests
awsAccountId="${OSD_STAGING_1_AWS_ACCOUNT_ID}"
accountCrNamespace="${NAMESPACE}"
awsProfile="osd-staging-1"
testName="test-aws-ou-logic-${TEST_START_TIME_SECONDS}"
accountCrName="$testName"
accountClaimCrName="$testName"
accountClaimCrNamespace="${testName}-cluster"
timeout="5m"

function setupTestPhase {
    # move OSD Staging 1 account to root ou to avoid ChildNotFoundInOU errors
    echo "Ensuring AWS Account ${awsAccountId} is in the root OU"
    hack/scripts/aws/verify-organization.sh "${awsAccountId}" --profile osd-staging-1 --move

    createAccountCR "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" || exit "$?"
    timeout="${ACCOUNT_READY_TIMEOUT}"
    waitForAccountCRReadyOrFailed "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" "${timeout}" || exit "$?"

    # AccountClaims live in the cluster's namespace, not the AAO namespace
    createNamespace "${accountClaimCrNamespace}" || exit "$?"
    createAccountClaimCR "${accountClaimCrName}" "${accountClaimCrNamespace}" || exit "$?"
    timeout="${ACCOUNT_CLAIM_READY_TIMEOUT}"
    waitForAccountClaimCRReadyOrFailed "${accountClaimCrName}" "${accountClaimCrNamespace}" "${timeout}" || exit "$?"

    exit "$EXIT_PASS"
}

function cleanupTestPhase {
    local cleanupExitCode="${EXIT_PASS}"
    local removeFinalizers=true
    timeout="${RESOURCE_DELETE_TIMEOUT}"

    if ! deleteAccountClaimCR "${accountClaimCrName}" "${accountClaimCrNamespace}" "${timeout}" $removeFinalizers; then
        echo "Failed to delete AccountClaim CR"
        cleanupExitCode="${EXIT_FAIL_UNEXPECTED_ERROR}"
    fi

    if ! deleteNamespace "${accountClaimCrNamespace}" "${timeout}" $removeFinalizers; then
        echo "Failed to delete AccountClaim namespace"
        cleanupExitCode="${EXIT_FAIL_UNEXPECTED_ERROR}"
    fi

    #note: dont delete the accountCrNamespace because AAO is running there, but we should cleanup the Account CR
    if ! deleteAccountCR "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" "${timeout}" $removeFinalizers; then
        echo "Failed to delete Account CR"
        cleanupExitCode="${EXIT_FAIL_UNEXPECTED_ERROR}"
    fi


    exit "$cleanupExitCode"
}

function testPhase {
    TYPE=$(aws organizations list-parents --child-id "${awsAccountId}" --profile "${awsProfile}" | jq -r ".Parents[0].Type")
    if [ "$TYPE" == "ORGANIZATIONAL_UNIT" ]; then
        echo "Account move successfully"
        exit "$EXIT_PASS"
    else
        echo "Failed to move account out of root"
        exit $EXIT_TEST_FAILED_MOVE_ACCOUNT_ROOT
    fi
}

function explainExitCode {
    local exitCode=$1
    local message=${exitCodeMessages[$exitCode]}
    echo "$message"
}

# The phase are specific keys passed in by the test framework. You can change function names if you want
# but do not change the phase names used as keys in the switch statement.
PHASE=$1
case $PHASE in
setup)
    setupTestPhase
    ;;
cleanup)
    cleanupTestPhase
    ;;
test)
    testPhase
    ;;
explain)
    explainExitCode "$2"
    ;;
*)
    echo "Unknown test phase: '$PHASE'"
    exit 1
    ;;
esac
