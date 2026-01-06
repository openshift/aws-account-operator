#!/usr/bin/env bash

# Test Description:
#   When an AccountClaim is deleted, the AAO performs some AWS side work to clean up the account
#   before putting it back into the pool for reuse (e.g. deleting s3 buckets that may contain customer
#   data).
#
#   This test verifies that the account is cleaned up properly and then put back into the pool for
#   reuse.

source test/integration/integration-test-lib.sh

# Run pre-flight checks
if [ "${SKIP_PREFLIGHT_CHECKS:-false}" != "true" ]; then
    if ! preflightChecks; then
        echo "Pre-flight checks failed. Set SKIP_PREFLIGHT_CHECKS=true to bypass."
        exit $EXIT_FAIL_UNEXPECTED_ERROR
    fi
fi

EXIT_TEST_FAIL_REUSED_ACCOUNT_NOT_READY=1
EXIT_TEST_FAIL_ACCOUNT_NOT_REUSED=2
EXIT_TEST_FAIL_SECRET_INVALID_CREDS=3
EXIT_TEST_FAIL_S3_BUCKET_CREATION=4
EXIT_TEST_FAIL_S3_BUCKET_STILL_EXISTS=5
EXIT_TEST_FAIL_ACCOUNT_CLAIM_NOT_DELETED=6
EXIT_TEST_FAIL_ACCOUNT_CLAIM_NAMESPACE_NOT_DELETED=7

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAIL_REUSED_ACCOUNT_NOT_READY]="Test Account CR is not in a ready state. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_ACCOUNT_NOT_REUSED]="Test Account CR was not reused. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_SECRET_INVALID_CREDS]="Test Account secret credentials are invalid. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_S3_BUCKET_CREATION]="Failed to create AWS S3 bucket. Check logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_S3_BUCKET_STILL_EXISTS]="AWS S3 bucket still exists after AccountClaim CR deletion. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_ACCOUNT_CLAIM_NOT_DELETED]="Test AccountClaim CR failed to delete. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_ACCOUNT_CLAIM_NAMESPACE_NOT_DELETED]="Test AccountClaim Namespace failed to delete. Most likely the AccountClaim CR still exists. Check AAO logs for more details."

# isolate global env variable use to prevent them spreading too deep into the tests
awsAccountId="${OSD_STAGING_1_AWS_ACCOUNT_ID}"
accountCrNamespace="${NAMESPACE}"

testName="test-nonccs-account-reuse-${TEST_START_TIME_SECONDS}"
accountCrName="${testName}"
accountClaimCrName="${testName}"
accountClaimCrNamespace="${testName}-cluster"
awsAccountSecretCrName="${testName}-secret"
timeout="5m"

function setupTestPhase {
    # move OSD Staging 1 account to root ou to avoid ChildNotFoundInOU errors
    echo "Ensuring AWS Account ${awsAccountId} is in the root OU"
    hack/scripts/aws/verify-organization.sh "${awsAccountId}" --profile osd-staging-1 --move

    echo "Creating Account CR."
    createAccountCR "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" || exit "$?"
    timeout="${ACCOUNT_READY_TIMEOUT}"
    waitForAccountCRReadyOrFailed "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" "${timeout}" || exit "$?"

    # AccountClaims live in the cluster's namespace, not the AAO namespace
    echo "Creating customer cluster namespace and AccountClaim CR."
    createNamespace "${accountClaimCrNamespace}" || exit "$?"
    createAccountClaimCR "${accountClaimCrName}" "${accountClaimCrNamespace}" || exit "$?"
    timeout="${ACCOUNT_CLAIM_READY_TIMEOUT}"
    waitForAccountClaimCRReadyOrFailed "${accountClaimCrName}" "${accountClaimCrNamespace}" "${timeout}" || exit "$?"

    # Create S3 Bucket
    echo "Getting AWS account credentials."
    AWS_ACCESS_KEY_ID=$(oc get secret "${awsAccountSecretCrName}" -n "${accountCrNamespace}" -o json | jq -r '.data.aws_access_key_id' | base64 -d)
    export AWS_ACCESS_KEY_ID

    AWS_SECRET_ACCESS_KEY=$(oc get secret "${awsAccountSecretCrName}" -n "${accountCrNamespace}" -o json | jq -r '.data.aws_secret_access_key' | base64 -d)
    export AWS_SECRET_ACCESS_KEY

    if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ]; then
        echo "AWS credentials not found in secret"
        exit $EXIT_TEST_FAIL_SECRET_INVALID_CREDS
    fi


    echo "Simulating \"customer resource\" by creating an S3 bucket."
    reuseBucketName="${testName}-bucket"

    echo "Creating S3 bucket with retry logic..."
    if ! retryWithBackoff "$MAX_RETRIES" aws s3api create-bucket --bucket "${reuseBucketName}" --region=us-east-1; then
        echo "Failed to create s3 bucket ${reuseBucketName} after $MAX_RETRIES attempts."
        exit $EXIT_TEST_FAIL_S3_BUCKET_CREATION
    fi

    exit "$EXIT_PASS"
}

function cleanupTestPhase {
    local cleanupExitCode="${EXIT_PASS}"
    local removeFinalizers=true
    timeout="${RESOURCE_DELETE_TIMEOUT}"

    if ! deleteAccountClaimCR "${accountClaimCrName}" "${accountClaimCrNamespace}" "${timeout}" $removeFinalizers; then
        echo "Failed to delete AccountClaim CR - $accountClaimCrName"
        cleanupExitCode="${EXIT_FAIL_UNEXPECTED_ERROR}"
    fi

    if ! deleteNamespace "${accountClaimCrNamespace}" "${timeout}" $removeFinalizers; then
        echo "Failed to delete AccountClaim namespace - ${accountClaimCrNamespace}"
        cleanupExitCode="${EXIT_FAIL_UNEXPECTED_ERROR}"
    fi

    #note: dont delete the accountCrNamespace because AAO is running there, but we should cleanup the Account CR
    if ! deleteAccountCR "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" "${timeout}" $removeFinalizers; then
        echo "Failed to delete Account CR - ${accountCrName}"
        cleanupExitCode="${EXIT_FAIL_UNEXPECTED_ERROR}"
    fi

    exit "$cleanupExitCode"
}

function testPhase {
    timeout="${RESOURCE_DELETE_TIMEOUT}"
    if ! deleteAccountClaimCR "${accountClaimCrName}" "${accountClaimCrNamespace}" "${timeout}"; then
        echo "AccountClaim CR $accountClaimCrName failed to delete."
        exit $EXIT_TEST_FAIL_ACCOUNT_CLAIM_NOT_DELETED
    fi
    
    # do we really need to delete the namespace?
    if ! deleteNamespace "${accountClaimCrNamespace}" "${timeout}"; then
        echo "AccountClaim Namespace $accountClaimCrNamespace failed to delete."
        exit $EXIT_TEST_FAIL_ACCOUNT_CLAIM_NAMESPACE_NOT_DELETED
    fi

    # Validate re-use
    echo "Validating Account CR is ready."
    IS_READY=$(getAccountCRAsJson "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" | jq -r '.status.state')
    if [ "$IS_READY" != "Ready" ]; then
        echo "Account CR $accountCrName is not Ready"
        exit $EXIT_TEST_FAIL_REUSED_ACCOUNT_NOT_READY
    fi

    echo "Validating reuse status set on Account CR."
    IS_REUSED=$(getAccountCRAsJson "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" | jq -r '.status.reused')
    if [ "$IS_REUSED" != true ]; then
        echo "Account CR $accountCrName is missing the .status.reused field"
        exit $EXIT_TEST_FAIL_ACCOUNT_NOT_REUSED
    fi

    # List S3 bucket
    echo "Validating customer resources (s3 bucket) were removed."
    BUCKETS=$(
        AWS_ACCESS_KEY_ID=$(oc get secret "${awsAccountSecretCrName}" -n "${accountCrNamespace}" -o json | jq -r '.data.aws_access_key_id' | base64 -d)
        export AWS_ACCESS_KEY_ID
        AWS_SECRET_ACCESS_KEY=$(oc get secret "${awsAccountSecretCrName}" -n "${accountCrNamespace}" -o json | jq -r '.data.aws_secret_access_key' | base64 -d)
        export AWS_SECRET_ACCESS_KEY

        retryWithBackoff "$MAX_RETRIES" aws s3api list-buckets | jq '[.Buckets[] | .Name] | length'
    )
    if [ "$BUCKETS" -ne 0 ]; then
        echo "Customer resources (s3 bucket) still exists after account deletion."
        exit $EXIT_TEST_FAIL_S3_BUCKET_STILL_EXISTS
    fi

    echo ""
    echo "========================================"
    echo "NON-CCS ACCOUNT REUSE TEST PASSED!"
    echo "========================================"
    echo "✓ AccountClaim deleted successfully"
    echo "✓ Account returned to pool (unclaimed)"
    echo "✓ S3 buckets cleaned up"
    echo "✓ Account ready for reuse"

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
