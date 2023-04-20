#!/bin/bash

# Load Environment vars
source test/integration/test_envs 

if [ -z "$OSD_STAGING_1_ACCOUNT_CR_NAME_OSD" ]; then
    "OSD_STAGING_1_ACCOUNT_CR_NAME_OSD not set"
    exit 1
fi

for secret in $(oc get secrets -n "${NAMESPACE}" | awk "/${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}/"'{ print $1 }')
do
    oc delete secret $secret -n ${NAMESPACE} || true
done
	
