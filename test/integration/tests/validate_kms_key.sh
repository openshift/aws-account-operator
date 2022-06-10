#!/usr/bin/env bash

AWS_ACCOUNT_ID="${OSD_STAGING_2_AWS_ACCOUNT_ID}"
AWS_ACCOUNT_PROFILE="osd-staging-2"
AWS_REGION="us-east-1"

source hack/scripts/test_envs

assume_account() {
    echo "Logging in to $AWS_ACCOUNT_ID"
    AWS_ASSUME_ROLE=$(aws sts assume-role --profile "$AWS_ACCOUNT_PROFILE" --role-arn arn:aws:iam::"$AWS_ACCOUNT_ID":role/OrganizationAccountAccessRole --role-session-name aao-kms-setup --output json)
    if [ -z "$AWS_ASSUME_ROLE" ]; then
        echo "Could not login to $AWS_ACCOUNT_ID using assume-role. Quitting"
        exit 1
    fi
    AWS_ACCESS_KEY_ID=$(echo "${AWS_ASSUME_ROLE}" | jq -r '.Credentials.AccessKeyId')
    AWS_SECRET_ACCESS_KEY=$(echo "${AWS_ASSUME_ROLE}" | jq -r '.Credentials.SecretAccessKey')
    AWS_SESSION_TOKEN=$(echo "${AWS_ASSUME_ROLE}" | jq -r '.Credentials.SessionToken')
    export AWS_ACCESS_KEY_ID
    export AWS_SECRET_ACCESS_KEY
    export AWS_SESSION_TOKEN
}

accClaim=$(oc get accountclaim "$KMS_CLAIM_NAME" -n "$KMS_NAMESPACE_NAME" -o json)

if [[ $(jq -r '.spec.kmsKeyId | length' <<< "$accClaim") -lt 1 ]]; then
    echo $(jq -r '.spec.kmsKeyId' <<< "$accClaim")
    echo "KmsKeyID is empty"
    exit 1
fi

accountName=$(jq -r '.spec.accountLink' <<< "$accClaim")
account=$(oc get account $accountName -n aws-account-operator -o json)

if [[ $(jq -r '.spec.awsAccountID' <<< "$account") != "${OSD_STAGING_2_AWS_ACCOUNT_ID}" ]]; then
    echo "Must use the OSD_STAGING_2_AWS_ACCOUNT_ID to run this test, otherwise the key is not available."
    exit 1
fi

assume_account

TARGET_KEY_ID=$(aws kms list-aliases --region "${AWS_REGION}" | jq -r '.Aliases[] | select(.AliasName == "alias/aao-test-key").TargetKeyId')

# Check for 60 seconds if an encrypted volume with the correct tag occurs in the region we are checking.
# If this happens the tests is a success, otherwise something likely went wrong.
echo "Waiting 60 seconds for encrypted volumes to appear using key ${TARGET_KEY_ID}"
RETRIES=10
VOLUME_FOUND="false"
while :
do
    RETRIES=$(($RETRIES - 1))
    if [[ "$RETRIES" == "0" ]]; then
        echo "Did not find an encrypted volume in time, kms test failed"
        VOLUME_FOUND="false"
        break
    fi
    ENCRYPTION_KEY=$(aws ec2 describe-volumes --filters 'Name=encrypted,Values=true' --region="${AWS_REGION}" | jq -r '.Volumes[] | select(.Tags[].Key == "kms-test").KmsKeyId')
    if [[ "${ENCRYPTION_KEY}" =~ .*${TARGET_KEY_ID} ]]; then
        echo "Volume encrypted with correct key found."
        VOLUME_FOUND="true"
        break
    else
        echo -n "."
        sleep 5
    fi
done

RETRIES=30
echo "Waiting for the account region initialization to finish, before cleaning up (or a maximum of 150 seconds)."
while :
do
    RETRIES=$(($RETRIES - 1))
    if [[ "$RETRIES" == "0" ]]; then
        echo "Region initialization is taking a lot longer than expected, cancelling wait."
        break
    fi
    ACCOUNT_STATUS=$(oc get account -n "$NAMESPACE" "$OSD_STAGING_1_ACCOUNT_CR_NAME_OSD" -o json | jq -r ".status.state")
    if [[ "$ACCOUNT_STATUS" != "InitializingRegions" ]]; then
        break
    else
        echo -n "."
        sleep 5
    fi
done

if [[ "$VOLUME_FOUND" = "true" ]]; then
    exit 0
else
    echo "Failing test - volume was not found."
    exit 1
fi
