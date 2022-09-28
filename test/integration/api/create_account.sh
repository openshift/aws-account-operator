#!/bin/bash

# Load Environment vars
source hack/scripts/test_envs

ACCOUNT_CR_EXISTS=$(oc get account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -n ${NAMESPACE})
ACCOUNT_CR_EXISTS=$?
spin='-\|/'

if [[ ${ACCOUNT_CR_EXISTS} = 0 ]]; then
    echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} already exists, skipping creation."
    exit 0
else
    echo "Creating Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" 
fi

# Create Account CR 
oc process -p AWS_ACCOUNT_ID=${OSD_STAGING_1_AWS_ACCOUNT_ID} -p ACCOUNT_CR_NAME=${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws.managed.openshift.io_v1alpha1_account.tmpl | oc apply -f -

# Wait for account to become ready
echo "Waiting for account to become ready, this may take 5+ minutes."
i=0
t=0
timeout=600

while true
do STATUS=$(oc get account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -n ${NAMESPACE} -o json | jq -r '.status.state');
    if [ "$STATUS" == "Ready" ]; then
        break
    elif [ "$STATUS" == "Failed" ]; then
        echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} failed to create"
        exit 1
    elif [ "$t" -gt "$timeout" ]; then
        echo "Timed out waiting for account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} to become ready"
        exit 1
    fi
    i=$(( (i+1) %4 ))
    t=$((t+1))
    printf "\r${spin:$i:1}"
    sleep 1
done
