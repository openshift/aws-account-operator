#!/bin/bash

AWS_ACCOUNT_NAME=$1

if [ -z "$AWS_ACCOUNT_NAME" ]; then
    echo "No AWS account name specified"
    echo -e "$0 <aws-account-name>\n"
    exit 1
fi


for secret in $(oc get secrets -n aws-account-operator --no-headers | grep "${AWS_ACCOUNT_NAME}" | awk '{print $1}'); do
    echo "Deleting secret $secret"
    oc delete secret "$secret" -n aws-account-operator
done
