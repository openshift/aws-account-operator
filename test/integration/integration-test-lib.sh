# Int Testing Framework Constants
STATUS_CHANGE_TIMEOUT=300
EXIT_PASS=0
EXIT_TEST_FAIL_ACCOUNT_CLAIM_PROVISIONING_FAILED=95
EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED=96
EXIT_TIMEOUT=97
EXIT_SKIP=98
EXIT_FAIL_UNEXPECTED_ERROR=99

declare -A COMMON_EXIT_CODE_MESSAGES
GENERAL_EXIT_CODE_MESSAGES[$EXIT_PASS]="PASS"
GENERAL_EXIT_CODE_MESSAGES[$EXIT_FAIL_UNEXPECTED_ERROR]="Unexpected error. Check test logs for more details."
GENERAL_EXIT_CODE_MESSAGES[$EXIT_TIMEOUT]="Timeout waiting for some condition to be met. Check test logs for more details."
GENERAL_EXIT_CODE_MESSAGES[$EXIT_SKIP]="Test/phase execution was skipped. Check test logs for more details."
GENERAL_EXIT_CODE_MESSAGES[$EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED]="Account CR has a status of failed. Check AAO logs for more details."
GENERAL_EXIT_CODE_MESSAGES[$EXIT_TEST_FAIL_ACCOUNT_CLAIM_PROVISIONING_FAILED]="AccountClaim CR has a status of failed. Check AAO logs for more details."

function ocCreateResourceIfNotExists {
    local crYaml=$1
    echo -e "CREATE RESOURCE:\n${crYaml}"
    if ! echo "${crYaml}" | oc get -f - 2>/dev/null; then
        if ! echo "${crYaml}" | oc apply -f -; then
            echo "Failed to create cluster resource"
            return $EXIT_FAIL_UNEXPECTED_ERROR
        fi
    else
        echo "Resource already exists on cluster and *will not* be re-created using provided yaml."
    fi
    return 0
}

function ocDeleteResourceIfExists {
    local crYaml=$1
    local timeoutSeconds=$2
    echo -e "DELETE RESOURCE:\n${crYaml}"

    if echo "${crYaml}" | oc get -f - 2>/dev/null; then
        if ! echo "${crYaml}" | oc delete --now --ignore-not-found --timeout="${timeoutSeconds}s" -f -; then
            echo "Failed to delete cluster resource"
            return $EXIT_FAIL_UNEXPECTED_ERROR
        fi
    fi

    if echo "${crYaml}" | oc get -f - 2>/dev/null; then
        echo "Cluster resource still exists after delete attempt." 
        return "$EXIT_FAIL_UNEXPECTED_ERROR"
    else
        echo "Cluster resource deleted."
        return 0
    fi
}

function ocWaitForResourceCondition {
    local crYaml=$1
    local timeoutSeconds=$2
    local forCondition=$3
    echo "${crYaml}" | oc wait --for="${forCondition}" --timeout="${timeoutSeconds}s" -f -
    return $? 
}

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
    local timeoutSeconds=$2
    local crYaml=$(getNamespaceYaml "${namespace}")
    ocDeleteResourceIfExists "${crYaml}" "${timeoutSeconds}"
    return $?
}

function getAccountCRYaml {
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
    local crYaml=$(getAccountCRYaml "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}")
    ocCreateResourceIfNotExists "${crYaml}"
    return $?
}

function deleteAccountCR {
    local awsAccountId=$1
    local accountCrName=$2
    local accountCrNamespace=$3
    local timeoutSeconds=$4
    local crYaml=$(getAccountCRYaml "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}")
    ocDeleteResourceIfExists "${crYaml}" "${timeoutSeconds}"
    return $?
}

function getAccountClaimCRYaml {
    local accountClaimCrName=$1
    local accountClaimCrNamespace=$2
    local template='hack/templates/aws.managed.openshift.io_v1alpha1_accountclaim_cr.tmpl'
    oc process --local -p NAME="${accountClaimCrName}" -p NAMESPACE="${accountClaimCrNamespace}" -f ${template}
}

function createAccountClaimCR {
    local accountClaimCrName=$1
    local accountClaimCrNamespace=$2
    local crYaml=$(getAccountClaimCRYaml "${accountClaimCrName}" "${accountClaimCrNamespace}")
    ocCreateResourceIfNotExists "${crYaml}"
    return $?
}

function deleteAccountClaimCR {
    local accountClaimCrName=$1
    local accountClaimCrNamespace=$2
    local timeoutSeconds=$3
    local crYaml=$(getAccountClaimCRYaml "${accountClaimCrName}" "${accountClaimCrNamespace}")
    ocDeleteResourceIfExists "${crYaml}" "${timeoutSeconds}"
}

function waitForAccountCRReadyOrFailed {
    local awsAccountId=$1
    local accountCrName=$2
    local accountCrNamespace=$3
    local timeoutSeconds=$4
    local crYaml=$(getAccountCRYaml "${awsAccountId}" "${accountCrName}" "${accountCrNamespace}")
    
    echo "Waiting for Account CR to become ready"
    if ! ocWaitForResourceCondition "${crYaml}" "${timeoutSeconds}" "jsonpath='{.status.state}'=Ready"; then
        if status=$(ocGetResourceAsJson "${crYaml}" | jq -r '.status.state'); then
            if [ "${status}" == "Failed" ]; then
                echo "Account CR has a status of failed. Check AAO logs for more details."
                return $EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED
            else
                echo "Unexpected Account CR status: ${status}"
                return $EXIT_FAIL_UNEXPECTED_ERROR
            fi
        fi
    fi
    return 0
}

function waitForAccountClaimCRReadyOrFailed {
    local accountClaimCrName=$1
    local accountClaimCrNamespace=$2
    local timeoutSeconds=$3
    local crYaml=$(getAccountClaimCRYaml "${accountClaimCrName}" "${accountClaimCrNamespace}")
    
    echo "Waiting for AccountClaim CR to become ready"
    if ! ocWaitForResourceCondition "${crYaml}" "${timeoutSeconds}" "jsonpath='{.status.state}'=Ready"; then
        if status=$(ocGetResourceAsJson "${crYaml}" | jq -r '.status.state'); then
            if [ "${status}" == "Failed" ]; then
                echo "AccountClaim CR has a status of failed. Check AAO logs for more details."
                return $EXIT_TEST_FAIL_ACCOUNT_CLAIM_PROVISIONING_FAILED
            else
                echo "Unexpected Account CR status: ${status}"
                return $EXIT_FAIL_UNEXPECTED_ERROR
            fi
        fi
    fi
    return 0
}