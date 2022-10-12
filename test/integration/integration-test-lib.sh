#!/usr/bin/env bash

source test/integration/test_envs

export ACCOUNT_READY_TIMEOUT="3m"
export ACCOUNT_CLAIM_READY_TIMEOUT="1m"
export RESOURCE_DELETE_TIMEOUT="30s"


export EXIT_PASS=0
export EXIT_TEST_FAIL_ACCOUNT_CLAIM_UNEXPECTED_STATUS=93
export EXIT_TEST_FAIL_ACCOUNT_CLAIM_PROVISIONING_FAILED=94
export EXIT_TEST_FAIL_ACCOUNT_UNEXPECTED_STATUS=95
export EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED=96
export EXIT_TIMEOUT=97
export EXIT_SKIP=98
export EXIT_FAIL_UNEXPECTED_ERROR=99

declare -A COMMON_EXIT_CODE_MESSAGES
export COMMON_EXIT_CODE_MESSAGES
COMMON_EXIT_CODE_MESSAGES[$EXIT_PASS]="PASS"
COMMON_EXIT_CODE_MESSAGES[$EXIT_FAIL_UNEXPECTED_ERROR]="Unexpected error. Check test logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_TIMEOUT]="Timeout waiting for some condition to be met. Check test logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_SKIP]="Test/phase execution was skipped. Check test logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_TEST_FAIL_ACCOUNT_UNEXPECTED_STATUS]="Account CR has an unexpected status. Consider increasing test timeouts. Check AAO logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED]="Account CR has a status of failed. Check AAO logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_TEST_FAIL_ACCOUNT_CLAIM_UNEXPECTED_STATUS]="AccountClaim CR has an unexpected status. Consider increasing test timeouts. Check AAO logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_TEST_FAIL_ACCOUNT_CLAIM_PROVISIONING_FAILED]="AccountClaim CR has a status of failed. Check AAO logs for more details."

function ocCreateResourceIfNotExists {
    local crYaml=$1
    echo -e "\nCREATE RESOURCE:\n${crYaml}" 1>&2
    if ! echo "${crYaml}" | oc get -f - &>/dev/null; then
        if ! echo "${crYaml}" | oc apply -f -; then
            echo "Failed to create cluster resource"
            return $EXIT_FAIL_UNEXPECTED_ERROR
        fi
    else
        echo "Resource already exists on cluster and *will not* be re-created using provided yaml."
    fi
    return 0
}


# timeout uses oc's timeout syntax (e.g. 30s, 1m, 2h) 
function ocDeleteResourceIfExists {
    local crYaml=$1
    local timeout=$2
    local removeFinalizers=${3:-false}
    echo -e "\nDELETE RESOURCE:\n${crYaml}" 1>&2

    if echo "${crYaml}" | oc get -f - &>/dev/null; then
        if $removeFinalizers; then
            echo "${crYaml}" | oc patch -p '{"metadata":{"finalizers":null}}' --type=merge -f -
        fi
        if ! echo "${crYaml}" | oc delete --now --ignore-not-found --timeout="${timeout}" -f -; then
            echo "Failed to delete cluster resource"
            return $EXIT_FAIL_UNEXPECTED_ERROR
        fi
    fi

    if echo "${crYaml}" | oc get -f - &>/dev/null; then
        echo "Cluster resource still exists after delete attempt." 
        return "$EXIT_FAIL_UNEXPECTED_ERROR"
    else
        return 0
    fi
}

# see `oc wait --help` for details on the --for flag
# timeout uses oc's timeout syntax (e.g. 30s, 1m, 2h) 
function ocWaitForResourceCondition {
    local crYaml=$1
    local timeout=$2
    local forCondition=$3

    # oc wait doesnt seem to like when the resource doesnt exist at all
    if echo "${crYaml}" | oc get -f - &>/dev/null; then 
        echo "${crYaml}" | oc wait --for="${forCondition}" --timeout="${timeout}" -f -
        return $?
    else
        echo "Cluster resource does not exist. Cannot wait for condition."
        return $EXIT_FAIL_UNEXPECTED_ERROR
    fi    
}

# Note: fetching resources this way returns results wrapped in a list:
# {
#    "apiVersion": "v1",
#    "kind": "List",
#    "items": [
#        {
#            "apiVersion": "aws.managed.openshift.io/v1alpha1",
#            "kind": "Account",
#            ...
#        } 
#    ]
# }
function ocGetResourceAsJson {
    local crYaml=$1
    echo "${crYaml}" | oc get -f - -o json
}

function getNamespaceYaml {
    local namespace=$1
    local template='hack/templates/namespace.tmpl'
    oc process --local -p NAME="${namespace}" -f ${template}
}

function createNamespace {
    local namespace=$1
    local crYaml=$(getNamespaceYaml "${namespace}")
    ocCreateResourceIfNotExists "${crYaml}"
    return $?
}

function deleteNamespace {
    local namespace=$1
    local timeout=$2
    local removeFinalizers=${3:-false}
    local crYaml=$(getNamespaceYaml "${namespace}")
    ocDeleteResourceIfExists "${crYaml}" "${timeout}"
    deleteSuccess=$?
    if [ $deleteSuccess -ne 0 ] && [ "$removeFinalizers" = true ]; then
        echo "Failed to delete resource, retrying with finalizers removed."
        ocDeleteResourceIfExists "${crYaml}" "${timeout}" true
        deleteSuccess=$?
    fi
    return $deleteSuccess
    return $?
}

function generateAccountCRYaml {
    local awsAccountId=$1
    local accountCrName=$2
    local accountCrNamespace=$3
    local template='hack/templates/aws.managed.openshift.io_v1alpha1_account.tmpl'
    oc process --local -p AWS_ACCOUNT_ID="${awsAccountId}" -p ACCOUNT_CR_NAME="${accountCrName}" -p NAMESPACE="${accountCrNamespace}" -f ${template}
}

function createAccountCR {
    local awsAccountId=$1
    local accountCrName=$2
    local accountCrNamespace=$3
    local crYaml=$(generateAccountCRYaml "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}")
    ocCreateResourceIfNotExists "${crYaml}"
    return $?
}

function deleteAccountCR {
    local awsAccountId=$1
    local accountCrName=$2
    local accountCrNamespace=$3
    local timeout=$4
    local removeFinalizers=${5:-false}
    local crYaml=$(generateAccountCRYaml "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}")
    ocDeleteResourceIfExists "${crYaml}" "${timeout}"
    deleteSuccess=$?
    if [ $deleteSuccess -ne 0 ] && [ "$removeFinalizers" = true ]; then
        echo "Failed to delete resource, retrying with finalizers removed."
        ocDeleteResourceIfExists "${crYaml}" "${timeout}" true
        deleteSuccess=$?
    fi
    return $deleteSuccess
}

function generateAccountClaimCRYaml {
    local accountClaimCrName=$1
    local accountClaimCrNamespace=$2
    local template='hack/templates/aws.managed.openshift.io_v1alpha1_accountclaim_cr.tmpl'
    oc process --local -p NAME="${accountClaimCrName}" -p NAMESPACE="${accountClaimCrNamespace}" -f ${template}
}

function createAccountClaimCR {
    local accountClaimCrName=$1
    local accountClaimCrNamespace=$2
    local crYaml=$(generateAccountClaimCRYaml "${accountClaimCrName}" "${accountClaimCrNamespace}")
    ocCreateResourceIfNotExists "${crYaml}"
    return $?
}

function deleteAccountClaimCR {
    local accountClaimCrName=$1
    local accountClaimCrNamespace=$2
    local timeout=$3
    local removeFinalizers=${4:-false}
    local crYaml=$(generateAccountClaimCRYaml "${accountClaimCrName}" "${accountClaimCrNamespace}")
    ocDeleteResourceIfExists "${crYaml}" "${timeout}"
    deleteSuccess=$?
    if [ $deleteSuccess -ne 0 ] && [ "$removeFinalizers" = true ]; then
        echo "Failed to delete resource, retrying with finalizers removed."
        ocDeleteResourceIfExists "${crYaml}" "${timeout}" true
        deleteSuccess=$?
    fi
    return $deleteSuccess
}

function getAccountCRAsJson {
    local awsAccountId=$1
    local accountCrName=$2
    local accountCrNamespace=$3
    local crYaml=$(generateAccountCRYaml "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}")
    ocGetResourceAsJson "${crYaml}" | jq -r '.items[0]'
}

function waitForAccountCRReadyOrFailed {
    local awsAccountId=$1
    local accountCrName=$2
    local accountCrNamespace=$3
    local timeout=$4
    local crYaml=$(generateAccountCRYaml "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}")
    
    echo -e "\nWaiting for Account CR to become ready (timeout: ${timeout})"
    if ! ocWaitForResourceCondition "${crYaml}" "${timeout}" "condition=Ready"; then
        if status=$(ocGetResourceAsJson "${crYaml}" | jq -r '.items[0].status.state'); then
            if [ "${status}" == "Failed" ]; then
                echo "Account CR has a status of failed. Check AAO logs for more details."
                return $EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED
            else
                echo "Unexpected Account CR status: ${status}"
                return $EXIT_TEST_FAIL_ACCOUNT_UNEXPECTED_STATUS
            fi
        else
            return $EXIT_FAIL_UNEXPECTED_ERROR
        fi
    fi
    return 0
}

function waitForAccountClaimCRReadyOrFailed {
    local accountClaimCrName=$1
    local accountClaimCrNamespace=$2
    local timeout=$3
    local crYaml=$(generateAccountClaimCRYaml "${accountClaimCrName}" "${accountClaimCrNamespace}")
    
    echo "Waiting for AccountClaim CR to become ready (timeout: ${timeout})"
    # for some reason condition=Ready doesn't work here, will look into it later
    if ! ocWaitForResourceCondition "${crYaml}" "${timeout}" "jsonpath={.status.state}=Ready"; then
        if status=$(ocGetResourceAsJson "${crYaml}" | jq -r '.items[0].status.state'); then
            if [ "${status}" == "Failed" ]; then
                echo "AccountClaim CR has a status of failed. Check AAO logs for more details."
                return $EXIT_TEST_FAIL_ACCOUNT_CLAIM_PROVISIONING_FAILED
            else
                echo "Unexpected AccountClaim CR status: ${status}"
                return $EXIT_TEST_FAIL_ACCOUNT_CLAIM_UNEXPECTED_STATUS
            fi
        else
            return $EXIT_FAIL_UNEXPECTED_ERROR
        fi
    fi
    return 0
}