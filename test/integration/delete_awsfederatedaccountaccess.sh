#!/bin/bash

source hack/scripts/test_envs

while [ "$1" != "" ]; do
  case $1 in
    -r | --role )  shift;
                   AWS_FEDERATED_ROLE_NAME=$1
                   ;;
    -n | --name )  shift;
                   FED_USER=$1
                   ;;

    * ) echo "Unexpected parameter $1"
        usage
        exit 1
  esac
  shift
done

echo "# Delete federatedaccountaccess with secret"
oc process -p AWS_IAM_ARN="${AWS_IAM_ARN}" -p IAM_USER_SECRET="${IAM_USER_SECRET}" -p AWS_FEDERATED_ROLE_NAME="${AWS_FEDERATED_ROLE_NAME}" -p NAMESPACE="${NAMESPACE}" -p FED_USER="${FED_USER}" -f hack/templates/aws_v1alpha1_awsfederatedaccountaccess_cr.tmpl | oc delete -f -
