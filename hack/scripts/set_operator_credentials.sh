#!/bin/bash

credentialsFile=$HOME/.aws/credentials

profile=$1

if [ -z $profile ]; then
  echo "Must pass in a profile name as first parameter"
  exit 1
fi

rawCredentials="$(grep -A2 $profile < $credentialsFile | grep -v $profile)"

if [ -z "$rawCredentials" ]; then
  echo "No AWS Profile found for $profile"
  exit 2
fi

export OPERATOR_ACCESS_KEY_ID="$(awk -F " " '($1=="aws_access_key_id") {print $3}' <<< "$rawCredentials")"
export OPERATOR_SECRET_ACCESS_KEY="$(awk -F " " '($1=="aws_secret_access_key") {print $3}' <<< "$rawCredentials")"

echo "Deploying AWS Account Operator Credentials using AWS Profile $profile"
make deploy-aws-account-operator-credentials
