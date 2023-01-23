#!/bin/bash -e

# get name/namespace
source test/integration/test_envs

# get accountclaim
accClaim=$(oc get accountclaim "$CCS_CLAIM_NAME" -n "$CCS_NAMESPACE_NAME" -o json)

################################
# CCS Account Claim Validation
################################

# validate accountclaim has finalizer
if [[ $(jq '.metadata.finalizers | length' <<< "$accClaim") -lt 1 ]]; then
  echo "No finalizers set on accountclaim."
  exit 1
fi

# validate byoc fields
if [[ $(jq -r '.spec.byoc' <<< "$accClaim") != true ]]; then
  echo "CCS Accountclaim should have .spec.byoc set to true."
  exit 1
fi

# validate byoc fields
if [[ $(jq -r '.spec.byocAWSAccountID' <<< "$accClaim") != ${OSD_STAGING_2_AWS_ACCOUNT_ID} ]]; then
  echo "CCS Accountclaim should have .spec.byocAWSAccountID set to ${OSD_STAGING_2_AWS_ACCOUNT_ID}."
  exit 1
fi

# validate byoc fields
if [[ $(jq -r '.spec.byocSecretRef.name' <<< "$accClaim") != "byoc" ]]; then
  echo "CCS Accountclaim should have .spec.byocSecretRef.name set to byoc."
  exit 1
fi

# validate byoc fields
if [[ $(jq -r '.spec.byocSecretRef.namespace' <<< "$accClaim") != ${CCS_NAMESPACE_NAME} ]]; then
  echo "CCS Accountclaim should have .spec.byocSecretRef.namespace set to ${CCS_NAMESPACE_NAME}"
  exit 1
fi

# validate there is an accountlink (ccs accountclaims should create an account to claim)
if [[ $(jq -r '.spec.accountLink' <<< "$accClaim") == "" ]]; then
  echo "CCS Accountclaim didn't create an account to link."
  exit 1
fi

AccountClaimLegalEntity=$(jq -r '.spec.legalEntity' <<< "$accClaim")

################################
# CCS Account Validation
################################

# get account
accountName=$(jq -r '.spec.accountLink' <<< "$accClaim")
account=$(oc get account $accountName -n aws-account-operator -o json)

# validate there is an accountlink (ccs accountclaims should create an account to claim)
if [[ $(jq -r '.spec.accountLink' <<< "$account") == "" ]]; then
  echo "CCS Account should have .spec.accountLink set."
  exit 1
fi

# validate awsAccountID matches that of claim
if [[ $(jq -r '.spec.awsAccountID' <<< "$account") != ${OSD_STAGING_2_AWS_ACCOUNT_ID} ]]; then
  echo "CCS Account .spec.awsAccountID should be set to ${OSD_STAGING_2_AWS_ACCOUNT_ID}."
  exit 1
fi

# validate byoc fields
if [[ $(jq -r '.spec.byoc' <<< "$account") != true ]]; then
  echo "CCS Account should have .spec.byoc set to true."
  exit 1
fi

# validate claimLink
if [[ $(jq -r '.spec.claimLink' <<< "$account") != ${CCS_CLAIM_NAME} ]]; then
  echo "CCS Account should have .spec.claimLink set to ${CCS_CLAIM_NAME}."
  exit 1
fi

# validate claimLinkNamespace
if [[ $(jq -r '.spec.claimLinkNamespace' <<< "$account") !=  ${CCS_NAMESPACE_NAME} ]]; then
  echo "CCS Account should have .spec.claimLinkNamespace set to ${CCS_NAMESPACE_NAME}."
  exit 1
fi


AccountLegalEntity=$(jq -r '.spec.legalEntity' <<< "$account")
# -S sorts the keys to ensure ordering isn't an issue
equalLegalEntities=$(diff <(jq -S <<< $AccountLegalEntity) <(jq  -S <<< $AccountClaimLegalEntity))
# ensure the legal entites found in the account and accountclaim are equal
if [ ${#equalLegalEntities} -gt 0 ]; then
  echo "Legal Entities found in the Account and AccountClaim differ."
  exit 1
fi

################################
# CCS Secret Validation
################################

if [[ 1 -ne $(oc get secrets -n "$CCS_NAMESPACE_NAME" aws -o name | wc -l) ]]; then
  echo "Secret ${CCS_NAMESPACE_NAME}/secret/aws does not exist."
  exit 1
fi
