#!/bin/bash -e

set -o pipefail

unset AWS_PROFILE
unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN

echo "Getting Operator User credentials from secret..."
export AWS_ACCESS_KEY_ID=$(oc get secret -n aws-account-operator aws-account-operator-credentials -o json | jq -r '.data.aws_access_key_id' | base64 -d)
export AWS_SECRET_ACCESS_KEY=$(oc get secrets -n aws-account-operator aws-account-operator-credentials -o json | jq -r .data.aws_secret_access_key | base64 -d)
SESSION_TOKEN=$(oc get secrets -n aws-account-operator aws-account-operator-credentials -o json | jq -r '.data.aws_session_token // empty' | base64 -d)
if [ -n "$SESSION_TOKEN" ]; then
  export AWS_SESSION_TOKEN=$SESSION_TOKEN
fi

export AWS_PAGER=""
operatorUser=$(aws sts get-caller-identity --output json | jq -r .Arn)
echo "Validating Operator User: $operatorUser"

echo "Assuming Jump Role..."
jumpRoleCreds=$(aws sts assume-role --role-arn ${STS_JUMP_ROLE} --role-session-name "STSCredsCheck" --output json )
export AWS_ACCESS_KEY_ID=$(jq -r '.Credentials.AccessKeyId' <<< $jumpRoleCreds)
export AWS_SECRET_ACCESS_KEY=$(jq -r '.Credentials.SecretAccessKey' <<< $jumpRoleCreds)
export AWS_SESSION_TOKEN=$(jq -r '.Credentials.SessionToken' <<< $jumpRoleCreds)

jumpRole=$(aws sts get-caller-identity --output json  | jq -r .Arn)
echo "Validated Jump Role: $jumpRole"

echo "Assuming Installer Role..."
accessRoleCreds=$(aws sts assume-role --role-arn ${STS_ROLE_ARN} --role-session-name "STSCredsCheck" --output json )
export AWS_ACCESS_KEY_ID=$(jq -r '.Credentials.AccessKeyId' <<< $accessRoleCreds)
export AWS_SECRET_ACCESS_KEY=$(jq -r '.Credentials.SecretAccessKey' <<< $accessRoleCreds)
export AWS_SESSION_TOKEN=$(jq -r '.Credentials.SessionToken' <<< $accessRoleCreds)

installerRole=$(aws sts get-caller-identity --output json  | jq -r .Arn)
echo "Validated Installer Role: $installerRole"

echo "All roles validated."
