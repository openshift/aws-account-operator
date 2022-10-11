#!/usr/bin/env bash

source test/integration/test_envs
source test/integration/integration-test-lib.sh

EXIT_TEST_FAILED_MOVE_ACCOUNT_ROOT=1

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAILED_MOVE_ACCOUNT_ROOT]="Failed to move account out of root. Check AAO logs for more details."


function setupTestPhase {
    local awsAccountId="${OSD_STAGING_1_AWS_ACCOUNT_ID}"
    local accountCrName="${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
    local accountCrNamespace="${NAMESPACE}"
    local accountClaimCrName="${ACCOUNT_CLAIM_NAME}"
    local accountClaimCrNamespace="${ACCOUNT_CLAIM_NAME}"
    local timeoutSeconds="${STATUS_CHANGE_TIMEOUT}"

    # move OSD Staging 1 account to root ou to avoid ChildNotFoundInOU errors
    hack/scripts/aws/verify-organization.sh "${awsAccountId}" --profile osd-staging-1 --move

    createAccountCR "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" || exit "$?"
    waitForAccountCRReadyOrFailed "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" "${timeoutSeconds}" || exit "$?"

    createNamespace "${accountClaimCrNamespace}" || exit "$?"
    createAccountClaimCR "${accountClaimCrName}" "${accountClaimCrNamespace}" || exit "$?"
    waitForAccountClaimCRReadyOrFailed "${accountClaimCrName}" "${accountClaimCrNamespace}" "${timeoutSeconds}" || exit "$?"

    exit "$EXIT_PASS"
}

function cleanupTestPhase {
    local awsAccountId="${OSD_STAGING_1_AWS_ACCOUNT_ID}"
    local accountCrName="${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
    local accountCrNamespace="${NAMESPACE}"
    local accountClaimCrName="${ACCOUNT_CLAIM_NAME}"
    local accountClaimCrNamespace="${ACCOUNT_CLAIM_NAME}"
    local timeoutSeconds="${STATUS_CHANGE_TIMEOUT}"

    local cleanupExitCode="${EXIT_PASS}"

    if ! deleteAccountClaimCR "${accountClaimCrName}" "${accountClaimCrNamespace}" "${timeoutSeconds}"; then
        echo "Failed to delete AccountClaim CR"
        cleanupExitCode="${EXIT_FAIL_UNEXPECTED_ERROR}"
    fi

    if ! deleteNamespace "${accountClaimCrNamespace}" "${timeoutSeconds}"; then
        echo "Failed to delete AccountClaim namespace"
        cleanupExitCode="${EXIT_FAIL_UNEXPECTED_ERROR}"
    fi

    #note: dont delete the accountCrNamespace because AAO is running there, but we should cleanup the Account CR
    if ! deleteAccountCR "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" "${timeoutSeconds}"; then
        echo "Failed to delete Account CR"
        cleanupExitCode="${EXIT_FAIL_UNEXPECTED_ERROR}"
    fi


    exit "$cleanupExitCode"
}

function testPhase {
    TYPE=$(aws organizations list-parents --child-id "${OSD_STAGING_1_AWS_ACCOUNT_ID}" --profile osd-staging-1 | jq -r ".Parents[0].Type")
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
