#!/bin/bash

# Load Environment vars
source test/integration/test_envs

# Delete Account CR
oc process -p AWS_ACCOUNT_ID=${OSD_STAGING_1_AWS_ACCOUNT_ID} -p ACCOUNT_CR_NAME=${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws.managed.openshift.io_v1alpha1_account.tmpl | oc delete --ignore-not-found -f -
