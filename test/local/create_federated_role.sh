#!/bin/bash

source test/integration/test_envs

yq_cmd=$(command -v yq &> /dev/null)
if [[ $? != 0 ]]
then
  echo "yq is not installed. Please install yq to run this command, otherwise you can copy the yaml out of the olm-registry file and apply it manually."
  exit 1
fi

yq '. | del(.parameters) | del( .objects[] | select(.kind != "AWSFederatedRole"))' hack/olm-registry/olm-artifacts-template.yaml | oc process -f - | oc apply -f -
