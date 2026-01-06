#!/usr/bin/env bash

# Test Description:
#  This test validates KMS (Key Management Service) key usage in CCS AccountClaims.
#  When a KMS key ID is specified, the operator should use that key to encrypt
#  EBS volumes in the account.
#
#  The test:
#  1. Creates a namespace for the KMS test
#  2. Creates a CCS secret with AWS credentials
#  3. Creates a CCS AccountClaim with a KMS key ID
#  4. Waits for the claim to become Ready
#  5. Validates the kmsKeyId is set on the claim
#  6. Assumes role into the AWS account
#  7. Checks for encrypted EBS volumes using the specified KMS key
#  8. Waits for region initialization to complete
#  9. Cleans up resources
#
#  This validates:
#  - KMS key configuration is propagated correctly
#  - EBS volumes are encrypted with the specified KMS key
#  - CCS workflow works with custom KMS keys

source test/integration/integration-test-lib.sh
source test/integration/test_envs

# Run pre-flight checks
if [ "${SKIP_PREFLIGHT_CHECKS:-false}" != "true" ]; then
    if ! preflightChecks; then
        echo "Pre-flight checks failed. Set SKIP_PREFLIGHT_CHECKS=true to bypass."
        exit $EXIT_FAIL_UNEXPECTED_ERROR
    fi
fi

EXIT_TEST_FAIL_NO_KMS_KEY_ID=1
EXIT_TEST_FAIL_WRONG_ACCOUNT=2
EXIT_TEST_FAIL_ASSUME_ROLE_FAILED=3
EXIT_TEST_FAIL_NO_ENCRYPTED_VOLUME=4
EXIT_TEST_FAIL_SECRET_CREATION_FAILED=5

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAIL_NO_KMS_KEY_ID]="KMS Key ID is not set on AccountClaim."
exitCodeMessages[$EXIT_TEST_FAIL_WRONG_ACCOUNT]="Must use OSD_STAGING_2 account for KMS testing."
exitCodeMessages[$EXIT_TEST_FAIL_ASSUME_ROLE_FAILED]="Failed to assume role into AWS account."
exitCodeMessages[$EXIT_TEST_FAIL_NO_ENCRYPTED_VOLUME]="No encrypted volume found with the specified KMS key."
exitCodeMessages[$EXIT_TEST_FAIL_SECRET_CREATION_FAILED]="Failed to create CCS secret."

kmsClaimName="${KMS_CLAIM_NAME}"
kmsNamespace="${KMS_NAMESPACE_NAME}"
kmsAccountId="${OSD_STAGING_2_AWS_ACCOUNT_ID}"
kmsKeyId="${KMS_KEY_ID}"
accountCrNamespace="${NAMESPACE}"
awsAccountProfile="osd-staging-2"
awsRegion="us-east-1"
sleepInterval="${SLEEP_INTERVAL:-10}"

function explain {
    exitCode=$1
    echo "${exitCodeMessages[$exitCode]}"
}

function assumeAccount {
    echo "Assuming role in AWS account: ${kmsAccountId}"
    local assumeRole
    assumeRole=$(aws sts assume-role --profile "${awsAccountProfile}" --role-arn "arn:aws:iam::${kmsAccountId}:role/OrganizationAccountAccessRole" --role-session-name aao-kms-test --output json)

    if [ -z "$assumeRole" ]; then
        echo "ERROR: Could not assume role in ${kmsAccountId}"
        return $EXIT_TEST_FAIL_ASSUME_ROLE_FAILED
    fi

    export AWS_ACCESS_KEY_ID=$(echo "${assumeRole}" | jq -r '.Credentials.AccessKeyId')
    export AWS_SECRET_ACCESS_KEY=$(echo "${assumeRole}" | jq -r '.Credentials.SecretAccessKey')
    export AWS_SESSION_TOKEN=$(echo "${assumeRole}" | jq -r '.Credentials.SessionToken')

    echo "✓ Successfully assumed role"
    return 0
}

function setup {
    echo "=============================================================="
    echo "SETUP: Creating namespace, CCS secret, and KMS AccountClaim"
    echo "=============================================================="

    echo "Creating namespace: ${kmsNamespace}"
    createNamespace "${kmsNamespace}" || return $?

    echo "Creating CCS secret using rotate_iam_access_keys.sh..."
    echo "  Account: ${kmsAccountId}, Namespace: ${kmsNamespace}"

    if ! ./hack/scripts/aws/rotate_iam_access_keys.sh -p "${awsAccountProfile}" -u osdCcsAdmin -a "${kmsAccountId}" -n "${kmsNamespace}" -o /dev/stdout | oc apply -f -; then
        echo "ERROR: Failed to create CCS secret"
        return $EXIT_TEST_FAIL_SECRET_CREATION_FAILED
    fi

    echo "Waiting ${sleepInterval}s for AWS to propagate IAM credentials..."
    sleep "${sleepInterval}"
    echo "✓ CCS secret created"

    echo "Creating KMS AccountClaim: ${kmsClaimName}"
    local claimYaml
    claimYaml=$(oc process --local -p NAME="${kmsClaimName}" -p NAMESPACE="${kmsNamespace}" -p CCS_ACCOUNT_ID="${kmsAccountId}" -p KMS_KEY_ID="${kmsKeyId}" -f hack/templates/aws.managed.openshift.io_v1alpha1_kms_accountclaim_cr.tmpl)
    ocCreateResourceIfNotExists "${claimYaml}" || return $?

    echo "Waiting for KMS AccountClaim to become Ready..."
    timeout="${ACCOUNT_CLAIM_READY_TIMEOUT}"
    waitForAccountClaimCRReadyOrFailed "${kmsClaimName}" "${kmsNamespace}" "${timeout}" || return $?

    echo "✓ Setup complete"
    return 0
}

function test {
    echo "=============================================================="
    echo "TEST: Validating KMS key usage in encrypted volumes"
    echo "=============================================================="

    echo "Getting KMS AccountClaim..."
    local claimYaml
    claimYaml=$(generateAccountClaimCRYaml "${kmsClaimName}" "${kmsNamespace}")
    local accClaim
    accClaim=$(ocGetResourceAsJson "${claimYaml}" | jq -r '.items[0]')

    echo "Validating kmsKeyId is set..."
    local claimKmsKeyId
    claimKmsKeyId=$(echo "$accClaim" | jq -r '.spec.kmsKeyId // ""')
    if [ -z "$claimKmsKeyId" ]; then
        echo "ERROR: kmsKeyId is empty on AccountClaim"
        return $EXIT_TEST_FAIL_NO_KMS_KEY_ID
    fi
    echo "✓ kmsKeyId is set: ${claimKmsKeyId}"

    echo "Getting linked Account CR..."
    local accountLink
    accountLink=$(echo "$accClaim" | jq -r '.spec.accountLink')
    local account
    account=$(oc get account "${accountLink}" -n "${accountCrNamespace}" -o json)

    echo "Validating Account uses OSD_STAGING_2..."
    local accountAwsId
    accountAwsId=$(echo "$account" | jq -r '.spec.awsAccountID')
    if [ "$accountAwsId" != "${kmsAccountId}" ]; then
        echo "ERROR: Must use OSD_STAGING_2_AWS_ACCOUNT_ID (${kmsAccountId}) for this test"
        echo "  Got: ${accountAwsId}"
        return $EXIT_TEST_FAIL_WRONG_ACCOUNT
    fi
    echo "✓ Using correct AWS account: ${accountAwsId}"

    assumeAccount || return $?

    echo "Getting target KMS key ID from AWS..."
    local targetKeyId
    targetKeyId=$(aws kms list-aliases --region "${awsRegion}" | jq -r '.Aliases[] | select(.AliasName == "alias/aao-test-key").TargetKeyId')
    echo "  Target KMS Key ID: ${targetKeyId}"

    echo "Checking for encrypted volumes with KMS key tag..."
    echo "  Waiting up to 60 seconds for encrypted volumes to appear..."

    local retries=10
    local volumeFound="false"

    while [ "$retries" -gt 0 ]; do
        retries=$((retries - 1))

        local encryptionKey
        encryptionKey=$(aws ec2 describe-volumes --filters 'Name=encrypted,Values=true' --region="${awsRegion}" | jq -r '.Volumes[] | select(.Tags[]?.Key == "kms-test").KmsKeyId')

        if [[ "${encryptionKey}" =~ .*${targetKeyId} ]]; then
            echo "✓ Volume encrypted with correct KMS key found!"
            volumeFound="true"
            break
        else
            if [ "$retries" -gt 0 ]; then
                echo -n "."
                sleep 5
            fi
        fi
    done
    echo ""

    if [ "$volumeFound" = "false" ]; then
        echo "ERROR: No encrypted volume found using KMS key ${targetKeyId}"
        return $EXIT_TEST_FAIL_NO_ENCRYPTED_VOLUME
    fi

    echo "Waiting for account region initialization to finish (max 150s)..."
    retries=30
    while [ "$retries" -gt 0 ]; do
        retries=$((retries - 1))

        local accountStatus
        accountStatus=$(oc get account -n "${accountCrNamespace}" "${accountLink}" -o json | jq -r ".status.state")

        if [ "$accountStatus" != "InitializingRegions" ]; then
            echo "✓ Region initialization complete (status: ${accountStatus})"
            break
        fi

        if [ "$retries" -gt 0 ]; then
            echo -n "."
            sleep 5
        else
            echo ""
            echo "WARNING: Region initialization taking longer than expected"
        fi
    done
    echo ""

    echo "========================================"
    echo "KMS ACCOUNTCLAIM TEST PASSED!"
    echo "========================================"
    echo "✓ KMS key ID set on AccountClaim"
    echo "✓ Encrypted volume created with specified KMS key"
    echo "✓ KMS test completed successfully"

    return 0
}

function cleanup {
    echo "=============================================================="
    echo "CLEANUP: Removing test resources"
    echo "=============================================================="

    local cleanupExitCode=0

    echo "Deleting KMS AccountClaim..."
    deleteAccountClaimCR "${kmsClaimName}" "${kmsNamespace}" "${RESOURCE_DELETE_TIMEOUT}" true 2>/dev/null || {
        echo "WARNING: Failed to delete KMS AccountClaim"
        cleanupExitCode=$EXIT_FAIL_UNEXPECTED_ERROR
    }

    echo "Deleting CCS secret..."
    oc delete secret byoc -n "${kmsNamespace}" 2>/dev/null || {
        echo "WARNING: Failed to delete CCS secret"
    }

    echo "Deleting namespace..."
    deleteNamespace "${kmsNamespace}" "${RESOURCE_DELETE_TIMEOUT}" true 2>/dev/null || {
        echo "WARNING: Failed to delete namespace"
        cleanupExitCode=$EXIT_FAIL_UNEXPECTED_ERROR
    }

    echo "✓ Cleanup complete"
    return $cleanupExitCode
}

# Handle the explain command
if [ "${1:-}" == "explain" ]; then
    explain "$2"
    exit 0
fi

# Main test execution
case "${1:-}" in
    setup)
        setup
        ;;
    test)
        test
        ;;
    cleanup)
        cleanup
        ;;
    *)
        echo "Usage: $0 {setup|test|cleanup|explain <exit_code>}"
        exit 1
        ;;
esac
