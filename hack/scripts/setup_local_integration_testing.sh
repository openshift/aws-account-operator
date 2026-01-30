#!/usr/bin/env bash

# Setup script for local AWS Account Operator integration testing
# This script:
# - Validates and refreshes AWS credentials
# - Creates required AWS infrastructure (AccessRole)
# - Validates cluster connectivity
# - Sets up operator credentials
# - Validates environment is ready for integration tests

set -eo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_ROOT="$( cd "${SCRIPT_DIR}/../.." && pwd )"

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

function log_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

function log_success() {
    echo -e "${GREEN}✓${NC} $1"
}

function log_warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

function log_error() {
    echo -e "${RED}✗${NC} $1"
}

function print_header() {
    echo ""
    echo "========================================================================"
    echo "$1"
    echo "========================================================================"
}

function check_prerequisites() {
    print_header "Checking Prerequisites"

    local missing_tools=()

    for tool in oc jq aws; do
        if ! command -v $tool &> /dev/null; then
            missing_tools+=($tool)
        else
            log_success "$tool is installed"
        fi
    done

    # direnv is optional but recommended
    if command -v direnv &> /dev/null; then
        log_success "direnv is installed (optional)"
    else
        log_warn "direnv not installed (optional - can still proceed)"
    fi

    if [ ${#missing_tools[@]} -ne 0 ]; then
        log_error "Missing required tools: ${missing_tools[*]}"
        echo ""
        echo "Install missing tools:"
        echo "  - oc: https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html"
        echo "  - jq: sudo dnf install jq"
        echo "  - aws: https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"
        echo "  - direnv: sudo dnf install direnv"
        return 1
    fi

    if [ ! -f "${REPO_ROOT}/.envrc" ]; then
        log_error ".envrc file not found in repository root"
        echo ""
        echo "Create .envrc with required environment variables."
        echo "See .envrc.example or documentation for details."
        return 1
    fi

    log_success "All prerequisites met"
    return 0
}

function load_environment() {
    print_header "Loading Environment Configuration"

    cd "${REPO_ROOT}"

    # Source .envrc
    if [ -f .envrc ]; then
        source .envrc
        log_success "Loaded .envrc"
    else
        log_error ".envrc not found"
        return 1
    fi

    # Source test_envs for additional test configuration
    if [ -f test/integration/test_envs ]; then
        source test/integration/test_envs
        log_success "Loaded test/integration/test_envs"
    fi

    # Set default NAMESPACE if not already set
    if [ -z "${NAMESPACE}" ]; then
        export NAMESPACE="aws-account-operator"
        log_info "Using default NAMESPACE: ${NAMESPACE}"
    fi

    # Validate required environment variables
    local required_vars=(
        "OSD_STAGING_1_AWS_ACCOUNT_ID"
        "OSD_STAGING_2_AWS_ACCOUNT_ID"
        "NAMESPACE"
    )

    local missing_vars=()
    for var in "${required_vars[@]}"; do
        if [ -z "${!var}" ]; then
            missing_vars+=($var)
        else
            log_success "$var is set"
        fi
    done

    if [ ${#missing_vars[@]} -ne 0 ]; then
        log_error "Missing required environment variables: ${missing_vars[*]}"
        return 1
    fi

    return 0
}

function check_aws_credentials() {
    print_header "Checking AWS Credentials"

    # Check osd-staging-1 credentials
    log_info "Checking osd-staging-1 profile..."
    if ! aws sts get-caller-identity --profile osd-staging-1 &> /dev/null; then
        log_warn "osd-staging-1 credentials expired or invalid"
        log_info "Refreshing AWS credentials..."

        if [ -f "${REPO_ROOT}/hack/scripts/update_aws_credentials.sh" ]; then
            "${REPO_ROOT}/hack/scripts/update_aws_credentials.sh" || {
                log_error "Failed to refresh credentials"
                echo ""
                echo "Manually refresh credentials with:"
                echo "  ./hack/scripts/update_aws_credentials.sh"
                return 1
            }
            log_success "AWS credentials refreshed"
        else
            log_error "update_aws_credentials.sh script not found"
            return 1
        fi
    else
        local account=$(aws sts get-caller-identity --profile osd-staging-1 --query 'Account' --output text)
        log_success "osd-staging-1 credentials valid (account: $account)"
    fi

    # Check devaccount credentials
    log_info "Checking devaccount profile..."
    if ! aws sts get-caller-identity --profile devaccount &> /dev/null; then
        log_warn "devaccount credentials invalid - some tests may fail"
        log_info "You may need to authenticate to your dev account"
    else
        local dev_account=$(aws sts get-caller-identity --profile devaccount --query 'Account' --output text)
        log_success "devaccount credentials valid (account: $dev_account)"
    fi

    return 0
}

function check_cluster_connectivity() {
    print_header "Checking Cluster Connectivity"

    log_info "Testing cluster connection..."
    if ! oc whoami &> /dev/null; then
        log_error "Not logged into OpenShift cluster"
        echo ""
        echo "Login to your cluster with:"
        echo "  ocm backplane login <cluster-id>"
        return 1
    fi

    local user=$(oc whoami)
    local cluster=$(oc whoami --show-server)
    log_success "Connected as: $user"
    log_success "Cluster: $cluster"

    # Check if namespace exists or is terminating
    log_info "Checking operator namespace: ${NAMESPACE}"
    local ns_status=$(oc get namespace "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

    if [ "$ns_status" = "Terminating" ]; then
        log_warn "Namespace ${NAMESPACE} is terminating"

        # Check for stuck resources with finalizers
        log_info "Checking for stuck Account CRs with finalizers..."
        local stuck_accounts=$(oc get accounts.aws.managed.openshift.io -n "${NAMESPACE}" -o name 2>/dev/null || echo "")

        if [ -n "$stuck_accounts" ]; then
            log_warn "Found stuck Account CRs - removing finalizers to unblock deletion"
            for account in $stuck_accounts; do
                oc patch "$account" -n "${NAMESPACE}" -p '{"metadata":{"finalizers":null}}' --type=merge &> /dev/null || true
            done
            log_info "Finalizers removed, waiting for cleanup to complete..."
            sleep 5
        fi

        log_info "Waiting for namespace deletion to complete (max 2 minutes)..."
        local wait_count=0
        while [ $wait_count -lt 24 ]; do
            if ! oc get namespace "${NAMESPACE}" &> /dev/null; then
                log_success "Namespace deletion completed"
                ns_status="NotFound"
                break
            fi
            sleep 5
            wait_count=$((wait_count + 1))
        done

        if [ "$ns_status" = "Terminating" ]; then
            log_error "Namespace still terminating after 2 minutes"
            log_info "This usually resolves itself. Try running the script again in a few minutes."
            return 1
        fi
    fi

    if [ "$ns_status" = "NotFound" ]; then
        log_info "Creating namespace ${NAMESPACE}..."
        oc create namespace "${NAMESPACE}" || {
            log_error "Failed to create namespace"
            return 1
        }

        log_info "Waiting for namespace to become active..."
        local wait_count=0
        while [ $wait_count -lt 12 ]; do
            local phase=$(oc get namespace "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
            if [ "$phase" = "Active" ]; then
                log_success "Namespace is active"
                break
            fi
            sleep 1
            wait_count=$((wait_count + 1))
        done

        if [ "$phase" != "Active" ]; then
            log_warn "Namespace not fully active yet, but proceeding"
        fi
    elif [ "$ns_status" = "Active" ]; then
        log_success "Namespace ${NAMESPACE} exists and is active"
    fi

    # Check cluster-admin permissions
    log_info "Checking permissions..."
    if ! oc auth can-i create processedtemplates.template.openshift.io &> /dev/null; then
        log_warn "Insufficient permissions for integration tests"
        log_info "Granting cluster-admin permissions..."

        oc --as backplane-cluster-admin adm policy add-cluster-role-to-user cluster-admin "$(oc whoami)" || {
            log_error "Failed to grant cluster-admin permissions"
            echo ""
            echo "Manually grant permissions with:"
            echo "  oc --as backplane-cluster-admin adm policy add-cluster-role-to-user cluster-admin \$(oc whoami)"
            return 1
        }
        log_success "Cluster-admin permissions granted"
    else
        log_success "Sufficient permissions confirmed"
    fi

    return 0
}

function setup_aws_infrastructure() {
    print_header "Setting Up AWS Infrastructure"

    local dev_account="${OSD_STAGING_2_AWS_ACCOUNT_ID}"

    log_info "Checking for AccessRole in account ${dev_account}..."

    if aws iam get-role --role-name AccessRole --profile devaccount &> /dev/null; then
        log_success "AccessRole already exists"
    else
        log_warn "AccessRole does not exist"
        log_info "Creating AccessRole..."

        if [ -f "${REPO_ROOT}/hack/scripts/aws/setup_access_role.sh" ]; then
            "${REPO_ROOT}/hack/scripts/aws/setup_access_role.sh" -a "${dev_account}" -p devaccount || {
                log_error "Failed to create AccessRole"
                echo ""
                echo "The AccessRole is required for STS integration tests."
                echo "You can create it manually or skip STS tests."
                return 1
            }
            log_success "AccessRole created successfully"
        else
            log_error "setup_access_role.sh script not found"
            return 1
        fi
    fi

    # Validate STS_ROLE_ARN is set correctly
    local expected_sts_role="arn:aws:iam::${dev_account}:role/AccessRole"
    if [ "${STS_ROLE_ARN}" != "${expected_sts_role}" ]; then
        log_warn "STS_ROLE_ARN mismatch in .envrc"
        log_info "Expected: ${expected_sts_role}"
        log_info "Current:  ${STS_ROLE_ARN}"
        log_warn "STS tests may fail - update .envrc if needed"
    else
        log_success "STS_ROLE_ARN correctly configured"
    fi

    return 0
}

function setup_operator_credentials() {
    print_header "Setting Up Operator Credentials"

    log_info "Configuring operator credentials secret..."

    if [ -f "${REPO_ROOT}/hack/scripts/set_operator_credentials.sh" ]; then
        "${REPO_ROOT}/hack/scripts/set_operator_credentials.sh" osd-staging-1 || {
            log_error "Failed to set operator credentials"
            return 1
        }
        log_success "Operator credentials configured"
    else
        log_error "set_operator_credentials.sh script not found"
        return 1
    fi

    return 0
}

function check_operator_status() {
    print_header "Checking Operator Status"

    # Check if operator is running on port 8080
    if lsof -i :8080 &> /dev/null; then
        local pid=$(lsof -ti :8080)
        log_success "Operator is running (PID: $pid)"
        log_info "Operator is ready for integration tests"
    else
        log_warn "No operator running on port 8080"
        echo ""
        echo "Start the operator with:"
        echo "  make deploy-local"
        echo ""
        echo "Or let the integration test bootstrap start it automatically"
    fi

    return 0
}

function print_summary() {
    print_header "Setup Complete - Ready for Integration Testing"

    echo ""
    echo "Environment configured for local integration testing:"
    echo ""
    echo "  AWS Account (OSD-1):    ${OSD_STAGING_1_AWS_ACCOUNT_ID}"
    echo "  AWS Account (Dev):      ${OSD_STAGING_2_AWS_ACCOUNT_ID}"
    echo "  Operator Namespace:     ${NAMESPACE}"
    echo "  Cluster:                $(oc whoami --show-server | sed 's|https://||')"
    echo ""
    echo "Infrastructure ready:"
    echo "  ✓ AWS credentials valid"
    echo "  ✓ Cluster connectivity confirmed"
    echo "  ✓ AccessRole exists in dev account"
    echo "  ✓ Operator credentials configured"
    echo ""
    echo "Run integration tests with:"
    echo "  ${GREEN}make test-integration-local${NC}"
    echo ""
    echo "Or run tests manually:"
    echo "  ${BLUE}./test/integration/integration-test-bootstrap.sh -p local${NC}"
    echo ""
}

# Main execution
main() {
    cd "${REPO_ROOT}"

    log_info "AWS Account Operator - Local Integration Testing Setup"
    echo ""

    check_prerequisites || exit 1
    load_environment || exit 1
    check_aws_credentials || exit 1
    check_cluster_connectivity || exit 1
    setup_aws_infrastructure || exit 1
    setup_operator_credentials || exit 1
    check_operator_status || exit 1

    print_summary

    return 0
}

main "$@"
