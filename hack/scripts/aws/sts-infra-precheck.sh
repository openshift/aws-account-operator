#!/bin/bash -e

set -o pipefail

unset AWS_PROFILE
unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN

export AWS_ACCESS_KEY_ID=$(oc get secret -n aws-account-operator aws-account-operator-credentials -o json | jq -r '.data.aws_access_key_id' | base64 -d)
export AWS_SECRET_ACCESS_KEY=$(oc get secrets -n aws-account-operator aws-account-operator-credentials -o json | jq -r .data.aws_secret_access_key | base64 -d)

export AWS_PAGER=""
aws sts get-caller-identity

jumpRoleCreds=$(aws sts assume-role --role-arn ${STS_JUMP_ROLE} --role-session-name "STSCredsCheck")
export AWS_ACCESS_KEY_ID=$(jq -r '.Credentials.AccessKeyId' <<< $jumpRoleCreds)
export AWS_SECRET_ACCESS_KEY=$(jq -r '.Credentials.SecretAccessKey' <<< $jumpRoleCreds)
export AWS_SESSION_TOKEN=$(jq -r '.Credentials.SessionToken' <<< $jumpRoleCreds)

aws sts get-caller-identity

accessRoleCreds=$(aws sts assume-role --role-arn ${STS_ROLE_ARN} --role-session-name "STSCredsCheck")
export AWS_ACCESS_KEY_ID=$(jq -r '.Credentials.AccessKeyId' <<< $accessRoleCreds)
export AWS_SECRET_ACCESS_KEY=$(jq -r '.Credentials.SecretAccessKey' <<< $accessRoleCreds)
export AWS_SESSION_TOKEN=$(jq -r '.Credentials.SessionToken' <<< $accessRoleCreds)

aws sts get-caller-identity
