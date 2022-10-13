#!/usr/bin/env bash

# Test Description:
#  When an Account CR is created, AAO has to do some AWS side setup before the account can
#  be added to the account pool (region initialization is the big one). 
#
#  This test creates an Account CR then verifies the account becomes ready and generates 
#  valid AWS credentials to access the "new" account.
#
#  normally this Account CR creation process is handled automatically by the AccountPool 
#  controller which actually creates a new AWS account as well, but for testing purposes 
#  we create an Account CR manually for the AWS account we have already created to be 
#  reused for all the tests

# Load Environment vars
source test/integration/integration-test-lib.sh

EXIT_TEST_FAIL_NO_ACCOUNT_SECRET=1
EXIT_TEST_FAIL_SECRET_INVALID_KEYS=2
EXIT_TEST_FAIL_SECRET_INVALID_CREDS=3

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAIL_NO_ACCOUNT_SECRET]="Test Account CR is ready, but no secret found. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_SECRET_INVALID_KEYS]="Test Account secret contains invalid keys. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_SECRET_INVALID_CREDS]="Test Account secret credentials are invalid. Check AAO logs for more details."

# isolate global env variable use to prevent them spreading too deep into the tests
awsAccountId="${OSD_STAGING_1_AWS_ACCOUNT_ID}"
accountCrNamespace="${NAMESPACE}"
testName="test-nonccs-account-creation-${TEST_START_TIME_SECONDS}"
accountCrName="${testName}"
awsAccountSecretCrName="${testName}-secret"
timeout="5m"

function setupTestPhase {
    echo "Creating Account CR."
    createAccountCR "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" || exit "$?"
    
    exit "$EXIT_PASS"
}

function cleanupTestPhase {
    timeout="${RESOURCE_DELETE_TIMEOUT}"
    local removeFinalizers=true
    local cleanupExitCode="${EXIT_PASS}"
    
    #note: dont delete the accountCrNamespace because AAO is running there, but we should cleanup the Account CR
    if ! deleteAccountCR "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" "${timeout}" $removeFinalizers; then
        echo "Failed to delete Account CR"
        cleanupExitCode="${EXIT_FAIL_UNEXPECTED_ERROR}"
    fi

    exit "$cleanupExitCode"
}

function testPhase {
    timeout="${ACCOUNT_READY_TIMEOUT}"
    waitForAccountCRReadyOrFailed "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}" "${timeout}"
    local testStatus=$?

    if [ $testStatus -eq 0 ]; then
        echo "Account ${accountCrName} is ready."
        verifyAccountSecrets
        testStatus=$?
    fi

    exit $testStatus
}

function explainExitCode {
    local exitCode=$1
    local message=${exitCodeMessages[$exitCode]}
    echo "$message"
}

function verifyAccountSecrets {

    echo "Verifying account secret exists."
    if ! test_secret="$(oc get secret "$awsAccountSecretCrName" -n "$accountCrNamespace" -o json | jq '.data')"; then
        return "$EXIT_FAIL_UNEXPECTED_ERROR"
    elif [ "$test_secret" == "" ]; then
        return $EXIT_TEST_FAIL_NO_ACCOUNT_SECRET
    fi

    unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN

    echo "Extracting aws_access_key_id from secret."
    AWS_ACCESS_KEY_ID=$(echo "$test_secret" | jq -r ".aws_access_key_id" | base64 -d)
    export AWS_ACCESS_KEY_ID
    if [ -z "$AWS_ACCESS_KEY_ID" ]; then
      echo "AWS Access Key not found in secret"
      return $EXIT_TEST_FAIL_SECRET_INVALID_KEYS
    fi

    echo "Extracting aws_secret_access_key from secret."
    AWS_SECRET_ACCESS_KEY=$(echo "$test_secret" | jq -r ".aws_secret_access_key" | base64 -d)
    export AWS_SECRET_ACCESS_KEY
    if [ -z "$AWS_SECRET_ACCESS_KEY" ]; then
      echo "AWS Secret Access Key not found in secret"
      return $EXIT_TEST_FAIL_SECRET_INVALID_KEYS
    fi

    echo "Extracting aws_user_name from secret."
    AWS_USER_NAME=$(echo "$test_secret" | jq -r ".aws_user_name" | base64 -d)
    export AWS_USER_NAME
    if [ -z "$AWS_USER_NAME" ]; then
      echo "AWS User Name not found in secret"
      return $EXIT_TEST_FAIL_SECRET_INVALID_KEYS
    fi

    # if the aws access key id is set, we should check the credential too.
    echo "Verifying AWS credentials work."
    if [ -n "$AWS_ACCESS_KEY_ID" ]; then
        if ! aws sts get-caller-identity > /dev/null 2>&1; then
            echo "Credentials for $accountCrName are invalid."
            return $EXIT_TEST_FAIL_SECRET_INVALID_CREDS
        fi
    fi

    return "$EXIT_PASS"
}

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