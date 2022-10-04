#!/bin/bash

# Load Environment vars
source hack/scripts/test_envs

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
#
# The before test phase is used to do setup needed by the test before it runs.
# For example creating CRs which the operator you are testing acts on.
#
# If setup was successful, or encounters failures that dont matter, your implementation 
# should return a 0 exit code so the test phase will
#
# input: None
# return: 
#   -1 - setup is waiting for some condition to be met before proceeding
#    0 - setup succeeded and the test should proceed
#    1 - setup failed in a unrecoverable way and the test should not proceed
#
function setupTestPhase {
    oc get account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -n ${NAMESPACE} 2>/dev/null
    ACCOUNT_CR_EXISTS=$?

    if [ $ACCOUNT_CR_EXISTS -ne 0 ]; then
        echo "Creating Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" 
        oc process -p AWS_ACCOUNT_ID=${OSD_STAGING_1_AWS_ACCOUNT_ID} -p ACCOUNT_CR_NAME=${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws.managed.openshift.io_v1alpha1_account.tmpl | oc apply -f -
        if [ $? -ne 0 ]; then
            echo "Failed to create account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
            exit $EXIT_FAIL_UNEXPECTED_ERROR
        fi
    fi

    echo "Account CR ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} created, test can proceed."
    exit $EXIT_PASS
}

function cleanupTestPhase {
    oc get account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -n ${NAMESPACE} 2>/dev/null

    if [ $? -eq 0 ]; then
        oc patch account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -n ${NAMESPACE} -p '{"metadata":{"finalizers":null}}' --type=merge
        oc process -p AWS_ACCOUNT_ID=${OSD_STAGING_1_AWS_ACCOUNT_ID} -p ACCOUNT_CR_NAME=${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws.managed.openshift.io_v1alpha1_account.tmpl | oc delete --now --ignore-not-found -f -
    
        oc get account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -n ${NAMESPACE} 2>/dev/null
        if [ $? -eq 0 ]; then
            echo "Failed to delete account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
            exit $EXIT_FAIL_UNEXPECTED_ERROR
        fi
    fi

    exit $EXIT_PASS
}

function testPhase {
    oc get account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -n ${NAMESPACE} 2>/dev/null

    if [ $? -ne 0 ]; then
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} doesnt seem to exist."
        exit $EXIT_TEST_FAIL_NO_ACCOUNT_CR
    fi

    STATUS=$(oc get account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -n ${NAMESPACE} -o json | jq -r '.status.state');
    if [ $? -ne 0 ]; then
        echo "Failed to get status of account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
        exit $EXIT_FAIL_UNEXPECTED_ERROR
    fi

    if [ "$STATUS" == "Ready" ]; then
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} is ready."
        verifyAccountSecrets
    elif [ "$STATUS" == "Failed" ]; then
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} failed to create"
        exit $EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED
    else
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} status is ${STATUS}, waiting for it to become ready or fail."
        exit $EXIT_RETRY
    fi
}

function explainCode {
    local phase=$1
    local exitCode=$2
    local message=${exitCodeMessages[$exitCode]}
    echo $message
}

function verifyAccountSecrets {

    TEST_ACCOUNT_CR_NAME=${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}
    TEST_NAMESPACE=${NAMESPACE}
    EXIT_STATUS=$EXIT_PASS

    # Define Expected Secrets and their keys
    # FORMAT: expectedPosftix:VARIABLE_WITH_KEYS
    EXPECTED_SECRETS=(
    "secret:SECRET_KEYS"
    )

    SECRET_KEYS="aws_access_key_id aws_secret_access_key aws_user_name"

    for secret_map in "${EXPECTED_SECRETS[@]}"; do
        secret=${secret_map%%:*}
        expected_keys=${secret_map#*:}
        test_secret="$(oc get secret $TEST_ACCOUNT_CR_NAME-$secret -n $TEST_NAMESPACE -o json | jq '.data')"
    
        if [ $? -ne 0 ]; then
            exit $EXIT_FAIL_UNEXPECTED_ERROR
        elif [ "$test_secret" == "" ]; then
            exit $EXIT_TEST_FAIL_NO_ACCOUNT_SECRET
        fi

        unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN

        # Lookup the expected keys
        for key in ${!expected_keys}; do
            val=$(jq -r ".$key" <<< "$test_secret")
            if [ "$val" == "null" ]; then
                echo "key: '$key' not found in $TEST_ACCOUNT_CR_NAME-$secret"
                exit $EXIT_TEST_FAIL_SECRET_INVALID_KEYS
            fi

            # Prepare variables for validity check
            if [ $key == "aws_access_key_id" ]; then
                export AWS_ACCESS_KEY_ID=$(echo -n $val | base64 -d)
            fi
            if [ $key == "aws_secret_access_key" ]; then
                export AWS_SECRET_ACCESS_KEY=$(echo -n $val | base64 -d)
            fi
            if [ $key == "aws_session_token" ]; then
                export AWS_SESSION_TOKEN=$(echo -n $val | base64 -d)
            fi
        done

        # if the aws access key id is set, we should check the credential too.
        if [ -n "$AWS_ACCESS_KEY_ID" ]; then
            if ! aws sts get-caller-identity > /dev/null 2>&1; then
                echo "Credentials for $TEST_ACCOUNT_CR_NAME-$secret are invalid."
                exit $EXIT_TEST_FAIL_SECRET_INVALID_CREDS
            fi
        fi
    done

    exit $EXIT_PASS
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
        explainCode $PHASE $2
        ;;
    *)
        echo "Unknown test phase: '$PHASE'"
        exit 1
        ;;
esac