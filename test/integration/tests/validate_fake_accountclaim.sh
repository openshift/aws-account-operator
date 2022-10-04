#!/bin/bash -e

# get name/namespace
source test/integration/test_envs

# get accountclaim
ac=$(oc get accountclaim "$FAKE_CLAIM_NAME" -n "$FAKE_NAMESPACE_NAME" -o json)

# validate accountclaim has finalizer
if [[ $(jq '.metadata.finalizers | length' <<< "$ac") -lt 1 ]]; then
  echo "No finalizers set on fake accountclaim."
  exit 1
fi

# validate there is no accountlink (we want the accountclaim to have not created an account)
if [[ $(jq -r '.spec.accountLink' <<< "$ac") != "" ]]; then
  echo "AccountLink is not empty."
  exit 1
fi

# get secrets
if [[ 1 -ne $(oc get secrets -n "$FAKE_NAMESPACE_NAME" aws -o name | wc -l) ]]; then
  echo "Secret ${FAKE_NAMESPACE_NAME}/secret/aws does not exist."
  exit 1
fi
