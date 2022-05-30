#!/usr/bin/env bash

AWS_ACCOUNT_PROFILE="osd-staging-1"
AWS_REGION="us-east-1"

usage() {
    echo "Usage: $0 -a <AWS_ACCOUNT_ID> -r <AWS_REGION|us-east-1> [-k <KEY_ALIAS|aao-test-key>] [-p <AWS_ACCOUNT_PROFILE|osd-staging-1>]" 1>&2; exit 1
}

if [ "$#" -lt 2 ]; then
    usage
fi

while getopts ":a:k:p:r:" o; do
    case "$o" in
        a)
            AWS_ACCOUNT_ID="${OPTARG}"
            ;;
        k)
            KEY_ALIAS="${OPTARG}"
            ;;
        p)
            AWS_ACCOUNT_PROFILE="${OPTARG}"
            ;;
        r)
            AWS_REGION="${OPTARG}"
            ;;
        *) usage
           ;;
    esac
done

if [ -z "$KEY_ALIAS" ]; then
    KEY_ALIAS="aao-test-key"
fi

# Login to the correct AWS account
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

alias_check() {
    echo "Verifying if the alias already exists"
    ALIASES=$(aws kms list-aliases --query="Aliases[?AliasName=='alias/$KEY_ALIAS']"  --region "$AWS_REGION" --output=json)
    FOUND=$(jq -r 'length' <<< "$ALIASES")
    if [ "$FOUND" -eq 0 ]; then
        echo "Key alias $KEY_ALIAS not yet found for account - must be created."
    elif [ "$FOUND" -eq 1 ]; then
        echo "Key alias $KEY_ALIAS already exists, no further actions required."
        exit 0
    else
        echo "Too many aliases found matching 'alias/$KEY_ALIAS' - please cleanup or choose a different name."
    fi
}

key_create() {
    echo "Creating key and alias"
    KEY_OUTPUT=$(aws kms create-key --region "$AWS_REGION" --output json)
    KEY_ID=$(jq -r '.KeyMetadata.KeyId' <<< "$KEY_OUTPUT")
    aws kms create-alias --region "$AWS_REGION" --alias-name "alias/$KEY_ALIAS" --target-key-id "$KEY_ID"
}

assume_account

alias_check

key_create
