#!/usr/bin/env bash

export PATH=/tmp:$PATH

# Retry a command with exponential backoff
# Usage: retryWithBackoff MAX_RETRIES command [args...]
# Example: retryWithBackoff 5 aws sts get-caller-identity
function retryWithBackoff {
    local max_attempts=$1
    shift
    local attempt=1
    local delay=2

    # If max_attempts is not set or empty, default to 1 (no retry)
    if [ -z "$max_attempts" ]; then
        max_attempts=1
    fi

    while [ $attempt -le $max_attempts ]; do
        if "$@"; then
            return 0
        fi

        if [ $attempt -lt $max_attempts ]; then
            echo "Command failed (attempt $attempt/$max_attempts). Retrying in ${delay}s..." >&2
            sleep $delay
            delay=$((delay * 2))
        fi

        attempt=$((attempt + 1))
    done

    return 1
}

# Default timeouts (can be overridden by bootstrap script for local profile)
# Use := to only set if not already set (preserves values from bootstrap)
export ACCOUNT_READY_TIMEOUT="${ACCOUNT_READY_TIMEOUT:-3m}"
export ACCOUNT_CLAIM_READY_TIMEOUT="${ACCOUNT_CLAIM_READY_TIMEOUT:-1m}"
export RESOURCE_DELETE_TIMEOUT="${RESOURCE_DELETE_TIMEOUT:-7m}"  # Increased for PROW - EC2 instance cleanup can take 5m + additional cleanup time
export MAX_RETRIES="${MAX_RETRIES:-5}"

export EXIT_PASS=0
export EXIT_FAIL_UNEXPECTED_ERROR=99
export EXIT_SKIP=98
export EXIT_TIMEOUT=97
export EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED=96
export EXIT_TEST_FAIL_ACCOUNT_UNEXPECTED_STATUS_AFTER_TIMEOUT=95
export EXIT_TEST_FAIL_ACCOUNT_CLAIM_PROVISIONING_FAILED=94
export EXIT_TEST_FAIL_ACCOUNT_CLAIM_UNEXPECTED_STATUS_AFTER_TIMEOUT=93
export EXIT_TEST_FAIL_CLUSTER_RESOURCE_NOT_DELETED=92

declare -A COMMON_EXIT_CODE_MESSAGES
export COMMON_EXIT_CODE_MESSAGES
COMMON_EXIT_CODE_MESSAGES[$EXIT_PASS]="PASS"
COMMON_EXIT_CODE_MESSAGES[$EXIT_FAIL_UNEXPECTED_ERROR]="Unexpected error. Check test logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_TIMEOUT]="Timeout waiting for some condition to be met. Check test logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_SKIP]="Test/phase execution was skipped. Check test logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_TEST_FAIL_ACCOUNT_UNEXPECTED_STATUS_AFTER_TIMEOUT]="Condition Timeout - Account CR has an unexpected status (not Ready or Failed). Consider increasing the ACCOUNT_READY_TIMEOUT timeout. Check AAO logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED]="Account CR has a status of failed. Check AAO logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_TEST_FAIL_ACCOUNT_CLAIM_UNEXPECTED_STATUS_AFTER_TIMEOUT]="Condition Timeout - AccountClaim CR has an unexpected status (not Ready or Failed). Consider increasing ACCOUNT_CLAIM_READY_TIMEOUT timeouts. Check AAO logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_TEST_FAIL_ACCOUNT_CLAIM_PROVISIONING_FAILED]="AccountClaim CR has a status of failed. Check AAO logs for more details."
COMMON_EXIT_CODE_MESSAGES[$EXIT_TEST_FAIL_CLUSTER_RESOURCE_NOT_DELETED]="Condition Timeout - Cluster resource not deleted. Consider increasing the RESOURCE_DELETE_TIMEOUT timeout, however this usually means a resource finalizer is unable to complete due to some error. Check AAO logs for more details."


#
# TODO - consider adding retries for flakey oc network errors like:
#   error: An error occurred while waiting for the condition to be satisfied: an error on the server ("unable to decode an event from the watch stream: http2: client connection lost") has prevented the request from succeedingUnable to connect to the server: net/http: TLS handshake timeout

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
# if removeFinalizers is true, it will remove finalizers before deletion and treat timeouts as warnings
# (deletion will complete asynchronously after finalizers are removed)
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
            if $removeFinalizers; then
                echo "Warning: Delete operation timed out, but finalizers were removed. Cleanup will complete asynchronously."
            else
                echo "Failed to delete cluster resource"
                return $EXIT_TEST_FAIL_CLUSTER_RESOURCE_NOT_DELETED
            fi
        fi
    fi

    if echo "${crYaml}" | oc get -f - &>/dev/null; then
        if $removeFinalizers; then
            echo "Warning: Resource still exists but finalizers removed. Deletion will complete asynchronously."
            return 0
        else
            echo "Cluster resource still exists after delete attempt."
            return "$EXIT_TEST_FAIL_CLUSTER_RESOURCE_NOT_DELETED"
        fi
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

# if removeFinalizers is true, it will remove finalizers immediately before attempting deletion
function deleteNamespace {
    local namespace=$1
    local timeout=$2
    local removeFinalizers=${3:-false}
    local crYaml=$(getNamespaceYaml "${namespace}")

    # If removeFinalizers is true, remove them immediately before attempting deletion
    # Deletion timeouts are treated as warnings since cleanup will complete asynchronously
    ocDeleteResourceIfExists "${crYaml}" "${timeout}" "$removeFinalizers"
    deleteSuccess=$?
    return $deleteSuccess
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

    # If removeFinalizers is true, remove them immediately before attempting deletion
    # Deletion timeouts are treated as warnings since cleanup will complete asynchronously
    ocDeleteResourceIfExists "${crYaml}" "${timeout}" "$removeFinalizers"
    deleteSuccess=$?
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

    # If removeFinalizers is true, remove them immediately before attempting deletion
    # Deletion timeouts are treated as warnings since cleanup will complete asynchronously
    ocDeleteResourceIfExists "${crYaml}" "${timeout}" "$removeFinalizers"
    deleteSuccess=$?
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
                echo "Unexpected Account CR status after timeout: ${status}"
                if [ "${FORCE_DEV_MODE:-}" == "local" ]; then
                    echo "Note: If running tests locally with operator reuse, AWS credentials may have"
                    echo "      expired during multiple test runs. STS tokens have limited lifetime."
                    echo "      Run ./hack/scripts/update_aws_credentials.sh to refresh and retry."
                fi
                return $EXIT_TEST_FAIL_ACCOUNT_UNEXPECTED_STATUS_AFTER_TIMEOUT
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

    # oc wait --for condition=Ready looks for an entry in the status.conditions array with a type of Ready and a status of True
    # this works for Account CRs, however, even though we set .status.state=Ready on AccountClaim CRs, we dont actually add a 
    # "Ready" condition entry to the .status.conditions array. We can use --for=jsonpath={.status.state}=Ready instead, however, 
    # prow infra has an old version of oc that doesnt support the jsonpath queries and we get an error.
    if ! ocWaitForResourceCondition "${crYaml}" "${timeout}" "condition=Claimed"; then
        if status=$(ocGetResourceAsJson "${crYaml}" | jq -r '.items[0].status.state'); then
            if [ "${status}" == "Failed" ]; then
                echo "AccountClaim CR has a status of failed. Check AAO logs for more details."
                return $EXIT_TEST_FAIL_ACCOUNT_CLAIM_PROVISIONING_FAILED
            else
                echo "Unexpected AccountClaim CR status after timeout: ${status}"
                if [ "${FORCE_DEV_MODE:-}" == "local" ]; then
                    echo "Note: If running tests locally with operator reuse, AWS credentials may have"
                    echo "      expired during multiple test runs. STS tokens have limited lifetime."
                    echo "      Run ./hack/scripts/update_aws_credentials.sh to refresh and retry."
                fi
                return $EXIT_TEST_FAIL_ACCOUNT_CLAIM_UNEXPECTED_STATUS_AFTER_TIMEOUT
            fi
        else
            return $EXIT_FAIL_UNEXPECTED_ERROR
        fi
    fi
    return 0
}
function preflightChecks {
    local failedChecks=0

    echo "Running pre-flight checks..."

    echo "Checking required tools..."
    for tool in oc jq aws; do
        if ! command -v "$tool" &>/dev/null; then
            echo "ERROR: Required tool '$tool' not found in PATH" 1>&2
            failedChecks=$((failedChecks + 1))
        else
            echo "  ✓ $tool found"
        fi
    done

    echo "Checking cluster connectivity..."
    if ! oc version &>/dev/null; then
        echo "ERROR: Cannot connect to OpenShift cluster. Check your oc login status." 1>&2
        failedChecks=$((failedChecks + 1))
    else
        echo "  ✓ Connected to cluster"
    fi

    if [ -n "${NAMESPACE:-}" ]; then
        echo "Checking access to operator namespace: ${NAMESPACE}..."
        if ! oc get namespace "${NAMESPACE}" &>/dev/null; then
            echo "WARNING: Cannot access namespace '${NAMESPACE}'. It may need to be created." 1>&2
        else
            echo "  ✓ Namespace '${NAMESPACE}' accessible"
        fi
    fi

    echo "Checking AWS CLI configuration..."
    if ! aws sts get-caller-identity &>/dev/null; then
        echo "WARNING: AWS CLI not configured or credentials invalid. Some tests may fail." 1>&2
    else
        echo "  ✓ AWS CLI configured"
    fi

    echo "Checking recommended environment variables..."
    local envVars=("OSD_STAGING_1_AWS_ACCOUNT_ID" "OSD_STAGING_2_AWS_ACCOUNT_ID" "NAMESPACE")
    for var in "${envVars[@]}"; do
        if [ -z "${!var:-}" ]; then
            echo "WARNING: Environment variable '$var' not set. Some tests may fail." 1>&2
        else
            echo "  ✓ $var is set"
        fi
    done

    if [ $failedChecks -gt 0 ]; then
        echo "Pre-flight checks failed with $failedChecks critical errors." 1>&2
        return 1
    fi

    echo "Pre-flight checks passed."
    return 0
}
