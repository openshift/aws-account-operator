#!/bin/bash

AWS_ACCOUNT_NAME=$1

for acc in $(oc get accounts -n aws-account-operator --no-headers | grep "$AWS_ACCOUNT_NAME" | awk '{print $1}')
do
    echo "$acc"
    oc patch account "$acc" -n aws-account-operator -p '{"metadata":{"finalizers":[]}}' --type=merge
    oc delete account "$acc" -n aws-account-operator
done
