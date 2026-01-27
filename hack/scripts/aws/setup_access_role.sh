#!/usr/bin/env bash

# Script to setup AccessRole in assigned osd-staging-2 account for local AAO development
# This script assumes you have already authenticated using rh-aws-saml-login

set -e

command -v aws >/dev/null 2>&1 || { echo >&2 "Script requires aws but it's not installed.  Aborting."; exit 1; }
command -v jq >/dev/null 2>&1 || { echo >&2 "Script requires jq but it's not installed.  Aborting."; exit 1; }

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
cd "$DIR" || exit

usage() {
    cat <<EOF
    usage: $0 [ OPTION ]
    Options
    -a      Assigned AWS Account ID (your personal assigned account)
    -p      AWS Profile name for your assigned account (from ~/.aws/credentials)
    -n      Append optional ID to AWS resources created (useful if encountering errors)
    -x      Set debug output for bash

    Example:
    ./setup_access_role.sh -a 123456789012 -p my-assigned-account

    Prerequisites:
    - You must have a profile configured for your assigned account in ~/.aws/credentials
    - The profile should have credentials obtained via rh-aws-saml-login
    - Run: ./update_aws_credentials.sh before running this script (if using standard profiles)
EOF
}

if ( ! getopts ":a:p:n:x:" opt); then
    echo ""
    echo "    $0 requires arguments!"
    usage
    exit 1
fi

while getopts ":a:p:n:x:" opt; do
    case $opt in
        a)
            AWS_ACCOUNT_ID="$OPTARG"
            ;;
        p)
            AWS_PROFILE="$OPTARG"
            ;;
        n)
            ID="$OPTARG"
            ;;
        x)
            set -x
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

if [ -z "$AWS_ACCOUNT_ID" ]; then
    echo "Error: Assigned AWS Account ID is required (-a)"
    usage
    exit 1
fi

if [ -z "$AWS_PROFILE" ]; then
    echo "Error: AWS Profile is required (-p)"
    usage
    exit 1
fi

check_var() {
    if [[ -z "$2" ]]; then
        echo "$1 is not defined. Quitting ..."
        exit 1
    else
        echo "$1=$2"
    fi
}

OSD_STAGING_1_ACCOUNT="277304166082"
JUMP_ROLE_NAME="JumpRole"
JUMP_ROLE_ARN="arn:aws:iam::${OSD_STAGING_1_ACCOUNT}:role/${JUMP_ROLE_NAME}"

echo "========================================="
echo "Setting up AccessRole in assigned account"
echo "========================================="
echo "Target Account: $AWS_ACCOUNT_ID"
echo "AWS Profile: $AWS_PROFILE"
echo "Jump Role ARN: $JUMP_ROLE_ARN"
echo ""

# Verify we're authenticated to the correct account
CURRENT_ACCOUNT=$(aws sts get-caller-identity --profile "$AWS_PROFILE" --query Account --output text 2>/dev/null || echo "")
if [ "$CURRENT_ACCOUNT" != "$AWS_ACCOUNT_ID" ]; then
    echo "Error: Not authenticated to account $AWS_ACCOUNT_ID using profile $AWS_PROFILE"
    echo "Current account: $CURRENT_ACCOUNT"
    echo "Please ensure the profile '$AWS_PROFILE' is configured in ~/.aws/credentials"
    echo "You may need to run: rh-aws-saml-login $AWS_PROFILE"
    exit 1
fi

echo "✓ Authenticated to account $AWS_ACCOUNT_ID using profile $AWS_PROFILE"
echo ""

#########################
# Create Access Role
#########################

ACCESS_ROLE_NAME="AccessRole${ID}"
POLICY_NAME="minimum-permissions-access-role${ID}"

# Check if role already exists
if aws iam get-role --role-name "$ACCESS_ROLE_NAME" --profile "$AWS_PROFILE" >/dev/null 2>&1; then
    echo "Warning: Role $ACCESS_ROLE_NAME already exists"
    read -p "Do you want to delete and recreate it? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Detaching policies from $ACCESS_ROLE_NAME..."
        ATTACHED_POLICIES=$(aws iam list-attached-role-policies --role-name "$ACCESS_ROLE_NAME" --profile "$AWS_PROFILE" --query 'AttachedPolicies[*].PolicyArn' --output text)
        for policy in $ATTACHED_POLICIES; do
            echo "  Detaching policy: $policy"
            aws iam detach-role-policy --role-name "$ACCESS_ROLE_NAME" --policy-arn "$policy" --profile "$AWS_PROFILE"
        done

        echo "Deleting role $ACCESS_ROLE_NAME..."
        aws iam delete-role --role-name "$ACCESS_ROLE_NAME" --profile "$AWS_PROFILE"
        echo "✓ Deleted existing role"
    else
        echo "Aborting. Please use a different ID with -n flag"
        exit 1
    fi
fi

# Create trust relationship document
echo "Creating AccessRole trust relationship..."
TRUST_POLICY=$(cat <<EOF
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Principal": {
                "AWS": "${JUMP_ROLE_ARN}"
            },
            "Action": "sts:AssumeRole",
            "Condition": {}
        }
    ]
}
EOF
)

# Create the Access Role with retry logic
# AWS IAM has eventual consistency, so the JumpRole ARN might not be immediately recognized
max_retries=5
i=1
echo "Creating AccessRole..."
while ! STS_ROLE_OUTPUT=$(echo "$TRUST_POLICY" | aws iam create-role --role-name "$ACCESS_ROLE_NAME" --assume-role-policy-document file:///dev/stdin --profile "$AWS_PROFILE" --output json 2>&1)
do
    if [[ $i > $max_retries ]]; then
        echo "Error: Could not create access role after $max_retries attempts"
        echo "$STS_ROLE_OUTPUT"
        exit 1
    fi
    ((j=2**i))
    echo "Access role creation failed (attempt $i/$max_retries). Retrying in $j seconds..."
    sleep "$j"
    ((i=i+1))
done

STS_ROLE_ARN=$(echo "${STS_ROLE_OUTPUT}" | jq -r '.Role.Arn')
check_var "STS_ROLE_ARN" "$STS_ROLE_ARN"
echo "✓ Created AccessRole: $STS_ROLE_ARN"

# Create and attach the access policy
echo "Creating and attaching permissions policy..."
POLICY_ARN=$(aws iam create-policy \
    --policy-name "$POLICY_NAME" \
    --policy-document "file://${PWD}/setup-aws-policies/AccessRolePolicy.json" \
    --profile "$AWS_PROFILE" \
    --output json | jq -r ".Policy.Arn")

if [ -z "$POLICY_ARN" ] || [ "$POLICY_ARN" == "null" ]; then
    echo "Error: Failed to create policy"
    exit 1
fi

aws iam attach-role-policy \
    --policy-arn "${POLICY_ARN}" \
    --role-name "$ACCESS_ROLE_NAME" \
    --profile "$AWS_PROFILE"

echo "✓ Attached policy: $POLICY_ARN"
echo ""

echo "========================================="
echo "Setup Complete!"
echo "========================================="
echo ""
echo "Add the following to your .envrc file:"
echo ""
echo "export STS_ROLE_ARN=${STS_ROLE_ARN}"
echo "export STS_JUMP_ARN=${JUMP_ROLE_ARN}"
echo "export STS_JUMP_ROLE=${JUMP_ROLE_ARN}"
echo "export OSD_STAGING_2_AWS_ACCOUNT_ID=${AWS_ACCOUNT_ID}"
echo ""
echo "Note: The JumpRole is managed centrally in the shared osd-staging-1 account (${OSD_STAGING_1_ACCOUNT})"
echo "      You do not need to create or manage the JumpRole yourself."
echo ""
