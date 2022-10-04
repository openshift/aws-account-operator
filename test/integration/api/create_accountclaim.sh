# Load Environment vars
source test/integration/test_envs

#!/bin/bash
spin='-\|/'

# Create AccountClaim CR 
oc process --local -p NAME=${ACCOUNT_CLAIM_NAME} -p NAMESPACE=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/aws.managed.openshift.io_v1alpha1_accountclaim_cr.tmpl | oc apply -f -

# Wait for accountclaim to become ready
echo "Waiting for accountclaim to become ready, this may take 5+ minutes."
i=0
timeout=$INT_TEST_ACCOUNT_READY_TIMEOUT

while true
do STATUS=$(oc get accountclaim ${ACCOUNT_CLAIM_NAME} -n ${ACCOUNT_CLAIM_NAMESPACE} -o json | jq -r '.status.state');
    if [ "$STATUS" == "Ready" ]; then
        break
    elif [ "$STATUS" == "Failed" ]; then
        echo "Account claim ${ACCOUNT_CLAIM_NAME} failed to create"
        exit 1
    elif [ $timeout -le 0 ]; then
        echo "Timed out waiting for account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} to become ready"
        exit 1
    fi
    i=$(( (i+1) %4 ))
    timeout=$((timeout-1))
    printf "\r${spin:$i:1} (timeout in: $timeout seconds)"
    sleep 1
done
