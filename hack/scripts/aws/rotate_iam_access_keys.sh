#!/bin/bash

usage() {
    cat <<EOF
    usage: $0 [ OPTION ]
    Options
    -a         AWS Account ID
    -u         AWS IAM user name (Required)
    -p         AWS Profile, leave blank for none
    -r         AWS Region leave blank for default us-east-1
    -s         Print secrets
    -o         Output path for secret yaml
    -v         Verbose
EOF
}

if ( ! getopts ":a:u:p:r:o:svh" opt); then
    echo ""
    echo "    $0 requries an argument!"
    usage
    exit 1
fi

# Set delete action to false by default
PRINT_SECRETS=false

VERBOSE=false

while getopts ":a:u:p:r:o:svh" opt; do
    case $opt in
        a)
            AWS_ACCOUNT_ID="$OPTARG"
            ;;
        u)
            AWS_IAM_USER="$OPTARG"
            ;;
        p)
            AWS_DEFAULT_PROFILE="$OPTARG"
            ;;
        r)
            AWS_DEFAULT_REGION="$OPTARG"
            ;;
        s)
            PRINT_SECRETS=true
            ;;
        o)
            SECRET_OUTPUT_PATH="$OPTARG"
            ;;
        v)
            VERBOSE=true
            ;;
        h)
            echo "Invalid option: -$OPTARG" >&2
            usage
            exit 1
            ;;
        \?)
            echo "Invalid option: -$OPTARG" >&2
            usage
            exit 1
            ;;
        :)
            echo "$0 Requires an argument" >&2
            usage
            exit 1
            ;;
        esac
    done


if [ -z "$AWS_IAM_USER" ]; then
	usage
fi

if [ -z "$AWS_DEFAULT_REGION" ]; then
	export AWS_DEFAULT_REGION="us-east-1"
else
	export AWS_DEFAULT_REGION=$AWS_DEFAULT_REGION
fi

if ! [ -z "$AWS_DEFAULT_REGION" ]; then
	export AWS_PROFILE=$AWS_DEFAULT_PROFILE
fi

AWS_STS_SESSION_NAME="SREAdminCreateUser"

# Assume role
if ! [ -z "$AWS_ACCOUNT_ID" ]; then

  AWS_ASSUME_ROLE=$(aws sts assume-role --role-arn arn:aws:iam::"${AWS_ACCOUNT_ID}":role/OrganizationAccountAccessRole --role-session-name "${AWS_STS_SESSION_NAME}")

  AWS_ACCESS_KEY_ID=$(echo "$AWS_ASSUME_ROLE" | jq -r '.Credentials.AccessKeyId')
  AWS_SECRET_ACCESS_KEY=$(echo "$AWS_ASSUME_ROLE" | jq -r '.Credentials.SecretAccessKey')
  AWS_SESSION_TOKEN=$(echo "$AWS_ASSUME_ROLE" | jq -r '.Credentials.SessionToken')

  export AWS_ACCESS_KEY_ID
  export AWS_SECRET_ACCESS_KEY
  export AWS_SESSION_TOKEN
fi

STS_CALLER_IDENTITY=$(aws sts get-caller-identity | jq -r '.Account')

if ! [ "${AWS_ACCOUNT_ID}" -eq "${STS_CALLER_IDENTITY}" ]; then
    echo "Error account id $ASSUMED_ROLE_ACCOUNT_ID does not match $AWS_ACCOUNT_ID"
    exit 1
fi

IAM_USER=$(aws iam list-users | jq --arg AWS_IAM_USER "$AWS_IAM_USER" '.Users[] | select(.UserName==$AWS_IAM_USER) | .UserName' | tr -d '"' )

if [ "$IAM_USER" == "" ]; then
    $VERBOSE && echo "Creating IAM user $AWS_IAM_USER"
    IAM_USER_OUPUT=$(aws iam create-user --user-name "$AWS_IAM_USER")
    $VERBOSE && echo "$IAM_USER_OUPUT" | jq '.'
    aws iam attach-user-policy --user-name "$AWS_IAM_USER" --policy-arn "arn:aws:iam::aws:policy/AdministratorAccess"
    # Set IAM_USER after creation
    IAM_USER="$AWS_IAM_USER"
    # Wait for AWS to create user and attach policy
    sleep 5
fi

if [ "$IAM_USER" = "$AWS_IAM_USER" ]; then
    $VERBOSE && echo "User $IAM_USER exists deleting access keys"
    for access_key in $(aws iam list-access-keys --user-name "$IAM_USER" | jq -r '.AccessKeyMetadata[].AccessKeyId')
    do
        aws iam delete-access-key --user-name "$IAM_USER" --access-key "$access_key"
    done
    CREDENTIALS=$(aws iam create-access-key --user-name "$IAM_USER")
	$VERBOSE && echo "Rotated access keys for $IAM_USER"
    KEY=$(echo "$CREDENTIALS" | jq -j '.AccessKey.AccessKeyId')
    B64_KEY=$(echo -n $CREDENTIALS | jq -j '.AccessKey.AccessKeyId' | base64)
    if $PRINT_SECRETS; then
      echo "Access key: $KEY"
      echo "Base64 access key: $B64_KEY"
    fi

    SECRET=$(echo -n "$CREDENTIALS" | jq -j '.AccessKey.SecretAccessKey')
    B64_SECRET=$(echo -n $CREDENTIALS | jq -j '.AccessKey.SecretAccessKey' | base64)
    if $PRINT_SECRETS; then
      echo "Secret access key: $SECRET"
      echo "Base64 secret key: $B64_SECRET"
    fi
else
    echo "Can't find IAM user: $AWS_IAM_USER"
    exit 1
fi

if ! [ -z "$SECRET_OUTPUT_PATH" ]; then
  cat <<EOF > $SECRET_OUTPUT_PATH
apiVersion: v1
data:
  aws_access_key_id: $B64_KEY
  aws_secret_access_key: $B64_SECRET
kind: Secret
metadata:
  name: byoc
  namespace: test-ccs-namespace
type: Opaque

EOF
fi