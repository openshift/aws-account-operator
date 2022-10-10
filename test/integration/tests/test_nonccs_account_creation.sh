#!/usr/bin/env bash

# Load Environment vars
source test/integration/test_envs

EXIT_TEST_FAIL_NO_ACCOUNT_CR=2
EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED=3
EXIT_TEST_FAIL_NO_ACCOUNT_SECRET=4
EXIT_TEST_FAIL_SECRET_INVALID_KEYS=5
EXIT_TEST_FAIL_SECRET_INVALID_CREDS=6

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAIL_NO_ACCOUNT_CR]="Test Account CR not found on cluster. It should have been created by test setup."
exitCodeMessages[$EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED]="Test Account CR has a status of failed. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_NO_ACCOUNT_SECRET]="Test Account CR is ready, but no secret found. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_SECRET_INVALID_KEYS]="Test Account secret contains invalid keys. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_SECRET_INVALID_CREDS]="Test Account secret credentials are invalid. Check AAO logs for more details."

function setupTestPhase {
    oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" 2>/dev/null
    ACCOUNT_CR_EXISTS=$?

    if [ $ACCOUNT_CR_EXISTS -ne 0 ]; then
        echo "Creating Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
        if ! oc process -p AWS_ACCOUNT_ID="${OSD_STAGING_1_AWS_ACCOUNT_ID}" -p ACCOUNT_CR_NAME="${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -p NAMESPACE="${NAMESPACE}" -f hack/templates/aws.managed.openshift.io_v1alpha1_account.tmpl | oc apply -f -; then
            echo "Failed to create account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
            exit "$EXIT_FAIL_UNEXPECTED_ERROR"
        fi
    fi

    echo "Account CR ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} created, test can proceed."
    exit "$EXIT_PASS"
}

function cleanupTestPhase {
    if ! oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" 2>/dev/null; then
        oc patch account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" -p '{"metadata":{"finalizers":null}}' --type=merge
        oc process -p AWS_ACCOUNT_ID="${OSD_STAGING_1_AWS_ACCOUNT_ID}" -p ACCOUNT_CR_NAME="${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -p NAMESPACE="${NAMESPACE}" -f hack/templates/aws.managed.openshift.io_v1alpha1_account.tmpl | oc delete --now --ignore-not-found -f -

        if ! oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" 2>/dev/null; then
            echo "Failed to delete account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
            exit "$EXIT_FAIL_UNEXPECTED_ERROR"
        fi
    fi

    exit "$EXIT_PASS"
}

function testPhase {
    oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" 2>/dev/null
    if ! oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" 2>/dev/null; then
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} doesnt seem to exist."
        exit $EXIT_TEST_FAIL_NO_ACCOUNT_CR
    fi

    if ! STATUS=$(oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" -o json | jq -r '.status.state'); then
        echo "Failed to get status of account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
        exit "$EXIT_FAIL_UNEXPECTED_ERROR"
    fi

    if [ "$STATUS" == "Ready" ]; then
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} is ready."
        verifyAccountSecrets
    elif [ "$STATUS" == "Failed" ]; then
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} failed to create"
        exit $EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED
    else
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} status is ${STATUS}, waiting for it to become ready or fail."
        exit "$EXIT_RETRY"
    fi
}

function explainExitCode {
    local exitCode=$1
    local message=${exitCodeMessages[$exitCode]}
    echo "$message"
}

function verifyAccountSecrets {
    TEST_ACCOUNT_CR_NAME=${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}
    TEST_NAMESPACE=${NAMESPACE}

    if ! test_secret="$(oc get secret "$TEST_ACCOUNT_CR_NAME"-secret -n "$TEST_NAMESPACE" -o json | jq '.data')"; then
        exit "$EXIT_FAIL_UNEXPECTED_ERROR"
    elif [ "$test_secret" == "" ]; then
        exit $EXIT_TEST_FAIL_NO_ACCOUNT_SECRET
    fi

    unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN

    AWS_ACCESS_KEY_ID=$(echo "$test_secret" | jq -r ".aws_access_key_id" | base64 -d)
    export AWS_ACCESS_KEY_ID
    if [ -z "$AWS_ACCESS_KEY_ID" ]; then
      echo "AWS Access Key not found in secret"
      exit $EXIT_TEST_FAIL_SECRET_INVALID_KEYS
    fi

    AWS_SECRET_ACCESS_KEY=$(echo "$test_secret" | jq -r ".aws_secret_access_key" | base64 -d)
    export AWS_SECRET_ACCESS_KEY
    if [ -z "$AWS_SECRET_ACCESS_KEY" ]; then
      echo "AWS Secret Access Key not found in secret"
      exit $EXIT_TEST_FAIL_SECRET_INVALID_KEYS
    fi

    AWS_USER_NAME=$(echo "$test_secret" | jq -r ".aws_user_name" | base64 -d)
    export AWS_USER_NAME
    if [ -z "$AWS_USER_NAME" ]; then
      echo "AWS User Name not found in secret"
      exit $EXIT_TEST_FAIL_SECRET_INVALID_KEYS
    fi

    # if the aws access key id is set, we should check the credential too.
    if [ -n "$AWS_ACCESS_KEY_ID" ]; then
        if ! aws sts get-caller-identity > /dev/null 2>&1; then
            echo "Credentials for $TEST_ACCOUNT_CR_NAME-secret are invalid."
            exit $EXIT_TEST_FAIL_SECRET_INVALID_CREDS
        fi
    fi

    exit "$EXIT_PASS"
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