#!/bin/bash

AWS_ACCOUNT_NAME=$1

if [ -z "$AWS_ACCOUNT_NAME" ]; then
    echo "No AWS account name specified"
    echo -e "$0 <aws-account-name>\n"
    exit 1
fi

echo "Assuming role in account ${AWS_ACCOUNT_NAME}"
AWS_ACCOUNT_ID=$(oc get account "${AWS_ACCOUNT_NAME}" -n aws-account-operator | jq '.spec.awsAccountID')
AWS_ACCOUNT_CREDENTIALS=$(./scripts/aws_assume_role_cli.sh "${AWS_ACCOUNT_ID}")

echo "Done"

AWS_ACCESS_KEY_ID=$(echo "${AWS_ACCOUNT_CREDENTIALS}" | jq -r '.Credentials.AccessKeyId')
AWS_SECRET_ACCESS_KEY=$(echo "${AWS_ACCOUNT_CREDENTIALS}" | jq -r '.Credentials.SecretAccessKey')
AWS_SESSION_TOKEN=$(echo "${AWS_ACCOUNT_CREDENTIALS}" | jq -r '.Credentials.SessionToken')

export AWS_ACCESS_KEY_ID
export AWS_SECRET_ACCESS_KEY
export AWS_SESSION_TOKEN

echo "Cleaning AWS account ID: $AWS_ACCOUNT_ID"

for IAM_USER in $(echo "osdManagedAdmin osdManagedAdminSRE"); do
    echo "Cleaning up IAM user: $IAM_USER"
    aws iam delete-login-profile --user-name "$IAM_USER" 2> /dev/null
    if [ $? -eq 0 ]; then
        echo "Deleted login profile for user $IAM_USER"
    fi
    # Delete policy attached to IAM user
    aws iam detach-user-policy --user-name "$IAM_USER" --policy-arn "arn:aws:iam::aws:policy/AdministratorAccess"
    
    # List access keys created for user
    ADMIN_ACCESS_KEY_IDS=$(aws iam list-access-keys --user-name "$IAM_USER" | jq -r '.AccessKeyMetadata[].AccessKeyId')
    
    # Delete access keys created for user
    for ID in ${ADMIN_ACCESS_KEY_IDS}; do
      echo "Deleting ACCESS KEY $ID"
      aws iam delete-access-key --user-name "$IAM_USER" --access-key-id "${ID}"
    done

    # Delete IAM user
    aws iam delete-user --user-name "$IAM_USER" 
done
