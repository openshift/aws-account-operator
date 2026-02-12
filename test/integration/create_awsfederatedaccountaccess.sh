#!/bin/bash

source test/integration/test_envs

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

AWS_IAM_ARN=$(aws sts get-caller-identity --profile=osd-staging-2 | jq -r '.Arn')

echo "# Create awsFederatedAccountAccess CR"
oc process --local -p AWS_IAM_ARN="${AWS_IAM_ARN}" -p IAM_USER_SECRET="${IAM_USER_SECRET}" -p AWS_FEDERATED_ROLE_NAME="${AWS_FEDERATED_ROLE_NAME}" -p NAMESPACE="${NAMESPACE}" -p FED_USER="${FED_USER}" -f hack/templates/aws.managed.openshift.io_v1alpha1_awsfederatedaccountaccess_cr.tmpl | oc apply -f -

echo "# Wait for awsFederatedAccountAccess CR to become ready"
while true; do
  STATUS="$(oc get awsfederatedaccountaccess -n "${NAMESPACE}" "${FED_USER}" -o json | jq -r '.status.state')"
  if [ "$STATUS" == "Ready" ]; then
    break
  elif [ "$STATUS" == "Failed" ]; then
    echo "awsFederatedAccountAccess CR ${FED_USER} failed to create"
    exit 1
  fi
  sleep 1
done

echo "# Print out AWS Console URL"
oc get awsfederatedaccountaccess -n "${NAMESPACE}" "${FED_USER}" -o json | jq -r '.status.consoleURL'

echo "# Wait ${SLEEP_INTERVAL} seconds for AWS to register role"
sleep "${SLEEP_INTERVAL}"
