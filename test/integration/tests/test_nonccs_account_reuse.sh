#!/usr/bin/env bash

# Test Description:
#   When an AccountClaim is deleted, the AAO performs some AWS side work to clean up the account
#   before putting it back into the pool for reuse (e.g. deleting s3 buckets that may contain customer
#   data).
#
#   This test verifies that the account is cleaned up properly and then put back into the pool for
#   reuse.

source test/integration/test_envs
source test/integration/integration-test-lib.sh

EXIT_TEST_FAIL_REUSED_ACCOUNT_NOT_READY=1
EXIT_TEST_FAIL_ACCOUNT_NOT_REUSED=2
EXIT_TEST_FAIL_SECRET_INVALID_CREDS=3
EXIT_TEST_FAIL_S3_BUCKET_CREATION=4
EXIT_TEST_FAIL_S3_BUCKET_STILL_EXISTS=5

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAIL_REUSED_ACCOUNT_NOT_READY]="Test Account CR is not in a ready state. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_ACCOUNT_NOT_REUSED]="Test Account CR was not reused. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_SECRET_INVALID_CREDS]="Test Account secret credentials are invalid. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_S3_BUCKET_CREATION]="Failed to create AWS S3 bucket. Check logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_S3_BUCKET_STILL_EXISTS]="AWS S3 bucket still exists after AccountClaim CR deletion. Check AAO logs for more details."

# isolate global env variable use to prevent them spreading too deep into the tests
awsAccountId="${OSD_STAGING_1_AWS_ACCOUNT_ID}"
accountCrName="${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
accountCrNamespace="${NAMESPACE}"
accountClaimCrName="${ACCOUNT_CLAIM_NAME}"
accountClaimCrNamespace="${ACCOUNT_CLAIM_NAMESPACE}"
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

    # Create S3 Bucket
    AWS_ACCESS_KEY_ID=$(oc get secret "${IAM_USER_SECRET}" -n "${NAMESPACE}" -o json | jq -r '.data.aws_access_key_id' | base64 -d)
    export AWS_ACCESS_KEY_ID

    AWS_SECRET_ACCESS_KEY=$(oc get secret "${IAM_USER_SECRET}" -n "${NAMESPACE}" -o json | jq -r '.data.aws_secret_access_key' | base64 -d)
    export AWS_SECRET_ACCESS_KEY

    if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ]; then
        echo "AWS credentials not found in secret"
        exit $EXIT_TEST_FAIL_SECRET_INVALID_CREDS
    fi

    REUSE_UUID=$(uuidgen)

    # make uuid lowercase for S3 bucket name requirements
    REUSE_BUCKET_NAME="test-reuse-bucket-${REUSE_UUID,,}" 

    if ! aws s3api create-bucket --bucket "${REUSE_BUCKET_NAME}" --region=us-east-1; then
        echo "Failed to create s3 bucket ${REUSE_BUCKET_NAME}."
        exit $EXIT_TEST_FAIL_S3_BUCKET_CREATION
    fi

    timeout="${RESOURCE_DELETE_TIMEOUT}"
    deleteAccountClaimCR "${accountClaimCrName}" "${accountClaimCrNamespace}" "${timeout}" || exit "$?"
    deleteNamespace "${accountClaimCrNamespace}" "${timeout}" || exit "$?"

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
    # Validate re-use
    IS_READY=$(getAccountCRAsJson "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" | jq -r '.status.state')
    if [ "$IS_READY" != "Ready" ]; then
        echo "Reused Account is not Ready"
        exit $EXIT_TEST_FAIL_REUSED_ACCOUNT_NOT_READY
    fi

    IS_REUSED=$(getAccountCRAsJson "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" | jq -r '.status.reused')
    if [ "$IS_REUSED" != true ]; then
        echo "Account is not Reused"
        exit $EXIT_TEST_FAIL_ACCOUNT_NOT_REUSED
    fi

    # List S3 bucket
    BUCKETS=$(
        AWS_ACCESS_KEY_ID=$(oc get secret "${IAM_USER_SECRET}" -n "${NAMESPACE}" -o json | jq -r '.data.aws_access_key_id' | base64 -d)
        export AWS_ACCESS_KEY_ID
        AWS_SECRET_ACCESS_KEY=$(oc get secret "${IAM_USER_SECRET}" -n "${NAMESPACE}" -o json | jq -r '.data.aws_secret_access_key' | base64 -d)
        export AWS_SECRET_ACCESS_KEY

        aws s3api list-buckets | jq '[.Buckets[] | .Name] | length'
    )
    if [ "$BUCKETS" == 0 ]; then
        echo "Reuse successfully complete"
    else
        echo "Reuse failed"
        exit $EXIT_TEST_FAIL_S3_BUCKET_STILL_EXISTS
    fi

    exit "$EXIT_PASS"
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
