#!/usr/bin/env bash

source test/integration/test_envs

EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED=2
EXIT_TEST_FAILED_MOVE_ACCOUNT_ROOT=3

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED]="Test Account CR has a status of failed. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAILED_MOVE_ACCOUNT_ROOT]="Failed to move account out of root. Check AAO logs for more details."

function setupTestPhase {
    # move OSD Staging 1 account to root ou to avoid ChildNotFoundInOU errors
    hack/scripts/aws/verify-organization.sh "${OSD_STAGING_1_AWS_ACCOUNT_ID}" --profile osd-staging-1 --move

    oc process --local -p NAME="${ACCOUNT_CLAIM_NAMESPACE}" -f hack/templates/namespace.tmpl | oc apply -f -

    if ! oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" 2>/dev/null; then
        echo "Creating Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
        if ! oc process -p AWS_ACCOUNT_ID="${OSD_STAGING_1_AWS_ACCOUNT_ID}" -p ACCOUNT_CR_NAME="${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -p NAMESPACE="${NAMESPACE}" -f hack/templates/aws.managed.openshift.io_v1alpha1_account.tmpl | oc apply -f -; then
            echo "Failed to create account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
            exit "$EXIT_FAIL_UNEXPECTED_ERROR"
        fi
    fi

    if ! STATUS=$(oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" -o json | jq -r '.status.state'); then
        echo "Failed to get status of account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
        exit "$EXIT_FAIL_UNEXPECTED_ERROR"
    fi

    if [ "$STATUS" == "Ready" ]; then
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} is ready."
    elif [ "$STATUS" == "Failed" ]; then
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} failed to create"
        exit $EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED
    else
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} status is ${STATUS}, waiting for it to become ready or fail."
        exit "$EXIT_RETRY"
    fi

    if ! oc get accountclaim "${ACCOUNT_CLAIM_NAME}" -n "${ACCOUNT_CLAIM_NAMESPACE}" 2>/dev/null; then
        echo "Creating Account Claim ${ACCOUNT_CLAIM_NAME}"
        if ! oc process --local -p NAME="${ACCOUNT_CLAIM_NAME}" -p NAMESPACE="${ACCOUNT_CLAIM_NAMESPACE}" -f hack/templates/aws.managed.openshift.io_v1alpha1_accountclaim_cr.tmpl | oc apply -f -; then
            echo "Failed to create account claim ${ACCOUNT_CLAIM_NAME}"
            exit "$EXIT_FAIL_UNEXPECTED_ERROR"
        fi
    fi

    if ! STATUS=$(oc get accountclaim "${ACCOUNT_CLAIM_NAME}" -n "${ACCOUNT_CLAIM_NAMESPACE}" -o json | jq -r '.status.state'); then
        echo "Failed to get status of account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
        exit "$EXIT_FAIL_UNEXPECTED_ERROR"
    fi

    if [ "$STATUS" == "Ready" ]; then
        echo "AccountClaim ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} is ready."
    elif [ "$STATUS" == "Failed" ]; then
        echo "AccountClaim ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} failed to create"
        exit $EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED
    else
        echo "AccountClaim ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} status is ${STATUS}, waiting for it to become ready or fail."
        exit "$EXIT_RETRY"
    fi

    exit "$EXIT_PASS"
}

function cleanupTestPhase {
    oc delete namespace "${ACCOUNT_CLAIM_NAMESPACE}"

    if oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" 2>/dev/null; then
        oc patch account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" -p '{"metadata":{"finalizers":null}}' --type=merge
        oc process -p AWS_ACCOUNT_ID="${OSD_STAGING_1_AWS_ACCOUNT_ID}" -p ACCOUNT_CR_NAME="${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -p NAMESPACE="${NAMESPACE}" -f hack/templates/aws.managed.openshift.io_v1alpha1_account.tmpl | oc delete --now --ignore-not-found -f -

        if oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" 2>/dev/null; then
            echo "Failed to delete account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
            exit "$EXIT_FAIL_UNEXPECTED_ERROR"
        else
            echo "Successfully cleaned up account"
        fi
    fi

    exit "$EXIT_PASS"
}

function testPhase {
    PARENTS=$(aws organizations list-parents --child-id "${OSD_STAGING_1_AWS_ACCOUNT_ID}" --profile osd-staging-1 | jq -r ".Parents | length")

    if ((PARENTS > 1)); then
        echo "Account move successful"
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
