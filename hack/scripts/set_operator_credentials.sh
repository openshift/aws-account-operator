#!/bin/bash

credentialsFile=$HOME/.aws/credentials

profile=$1

if [ -z $OPERATOR_ACCESS_KEY_ID -o -z $OPERATOR_SECRET_ACCESS_KEY ];
  then
    if [ -z $profile ]; then
        echo "Must pass in a profile name as first parameter"
        exit 1
    fi

    rawCredentials="$(grep -A3 "\[$profile\]" < $credentialsFile | grep -v "\[$profile\]")"

    if [ -z "$rawCredentials" ]; then
      echo "No AWS Profile found for $profile"
      exit 2
    fi

    ID="$(awk -F " " '($1=="aws_access_key_id") {printf "%s", $3}' <<< "$rawCredentials" | base64)"
    SECRET="$(awk -F " " '($1=="aws_secret_access_key") {printf "%s", $3}' <<< "$rawCredentials" | base64)"
    SESSION_TOKEN="$(awk -F " " '($1=="aws_session_token") {printf "%s", $3}' <<< "$rawCredentials" | base64)"
    echo "Deploying AWS Account Operator Credentials using AWS Profile $profile"

  else
    ID="$(echo -n $OPERATOR_ACCESS_KEY_ID | base64)"
    SECRET="$(echo -n $OPERATOR_SECRET_ACCESS_KEY | base64)"
    if [ -n "$OPERATOR_SESSION_TOKEN" ]; then
      SESSION_TOKEN="$(echo -n $OPERATOR_SESSION_TOKEN | base64)"
    else
      SESSION_TOKEN=""
    fi
    echo "Deploying AWS Account Operator Credentials using custom keys"
fi

if [ -n "$SESSION_TOKEN" ]; then
  oc process -p "OPERATOR_ACCESS_KEY_ID=${ID}" -p "OPERATOR_SECRET_ACCESS_KEY=${SECRET}" -p "OPERATOR_SESSION_TOKEN=${SESSION_TOKEN}" -p OPERATOR_NAMESPACE=aws-account-operator -f hack/templates/aws.managed.openshift.io_v1alpha1_aws_account_operator_credentials.tmpl --local -o yaml | oc apply -f -
else
  oc process -p "OPERATOR_ACCESS_KEY_ID=${ID}" -p "OPERATOR_SECRET_ACCESS_KEY=${SECRET}" -p OPERATOR_NAMESPACE=aws-account-operator -f hack/templates/aws.managed.openshift.io_v1alpha1_aws_account_operator_credentials.tmpl --local -o yaml | oc apply -f -
fi
