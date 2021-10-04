#!/bin/bash -e

# get name/namespace
source hack/scripts/test_envs

# get accountclaim
accClaim=$(oc get accountclaim "$STS_CLAIM_NAME" -n "$STS_NAMESPACE_NAME" -o json)

################################
# STS Account Claim Validation
################################

# validate accountclaim has finalizer
if [[ $(jq '.metadata.finalizers | length' <<< "$accClaim") -lt 1 ]]; then
  echo "No finalizers set on accountclaim."
  exit 1
fi

# validate byoc fields
if [[ $(jq -r '.spec.byoc' <<< "$accClaim") != true ]]; then
  echo "STS Accountclaim should have .spec.byoc set to true."
  exit 1
fi

# validate byoc fields
if [[ $(jq -r '.spec.byocAWSAccountID' <<< "$accClaim") != ${OSD_STAGING_2_AWS_ACCOUNT_ID} ]]; then
  echo "STS Accountclaim should have .spec.byocAWSAccountID set to ${OSD_STAGING_2_AWS_ACCOUNT_ID}."
  exit 1
fi

# validate there is an accountlink (STS accountclaims should create an account to claim)
if [[ $(jq -r '.spec.accountLink' <<< "$accClaim") == "" ]]; then
  echo "STS Accountclaim didn't create an account to link."
  exit 1
fi

# validate manualSTSMode fields
if [[ $(jq -r '.spec.manualSTSMode' <<< "$accClaim") != true ]]; then
  echo "STS Accountclaim should have .spec.manualSTSMode set to true."
  exit 1
fi

# validate stsRoleARN fields
if [[ $(jq -r '.spec.stsRoleARN' <<< "$accClaim") != ${STS_ROLE_ARN} ]]; then
  echo "STS Accountclaim should have .spec.stsRoleARN set to STS_ROLE_ARN."
  exit 1
fi

AccountClaimLegalEntity=$(jq -r '.spec.legalEntity' <<< "$accClaim")

################################
# STS Account Validation
################################

# get account
accountName=$(jq -r '.spec.accountLink' <<< "$accClaim")
account=$(oc get account $accountName -n aws-account-operator -o json)

# validate manualSTSMode field
if [[ $(jq -r '.spec.manualSTSMode' <<< "$account") != true ]]; then
  echo "STS Account should have .spec.manualSTSMode set to true."
  exit 1
fi

# validate there is an accountlink (STS accountclaims should create an account to claim)
if [[ $(jq -r '.spec.accountLink' <<< "$account") == "" ]]; then
  echo "STS Account should have .spec.accountLink set."
  exit 1
fi

# validate awsAccountID matches that of claim
if [[ $(jq -r '.spec.awsAccountID' <<< "$account") != ${OSD_STAGING_2_AWS_ACCOUNT_ID} ]]; then
  echo "STS Account .spec.awsAccountID should be set to ${OSD_STAGING_2_AWS_ACCOUNT_ID}."
  exit 1
fi

# validate byoc fields
if [[ $(jq -r '.spec.byoc' <<< "$account") != true ]]; then
  echo "STS Account should have .spec.byoc set to true."
  exit 1
fi

# validate claimLink
if [[ $(jq -r '.spec.claimLink' <<< "$account") != ${STS_CLAIM_NAME} ]]; then
  echo "STS Account should have .spec.claimLink set to ${STS_CLAIM_NAME}."
  exit 1
fi

# validate claimLinkNamespace
if [[ $(jq -r '.spec.claimLinkNamespace' <<< "$account") !=  ${STS_NAMESPACE_NAME} ]]; then
  echo "STS Account should have .spec.claimLinkNamespace set to ${STS_NAMESPACE_NAME}."
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
