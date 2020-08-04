#!/bin/bash

EXIT_PASS=0
EXIT_FAIL_INVALID_KEYS=1
EXIT_FAIL_INVALID_CREDS=2

TEST_ACCOUNT_CR_NAME="osd-staging-1"
TEST_NAMESPACE="aws-account-operator"
EXIT_STATUS=$EXIT_PASS

# Define Expected Secrets and their keys
# FORMAT: expectedPosftix:VARIABLE_WITH_KEYS
EXPECTED_SECRETS=(
  "osdmanagedadminsre-secret:OSDMASRE_SECRET_KEYS"
  "secret:SECRET_KEYS"
)

OSDMASRE_SECRET_KEYS="aws_access_key_id aws_secret_access_key aws_user_name"
SECRET_KEYS="aws_access_key_id aws_secret_access_key aws_user_name"

for secret_map in "${EXPECTED_SECRETS[@]}"; do
  test_secret_validity=false
  has_session_token=false
  secret=${secret_map%%:*}
  expected_keys=${secret_map#*:}
  test_secret="$(oc get secret osd-creds-mgmt-$TEST_ACCOUNT_CR_NAME-$secret -n $TEST_NAMESPACE -o json | jq '.data')"
  
  if [ "$test_secret" == "" ]; then
    EXIT_STATUS="FAIL"
    continue
  fi

  unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN

  # Lookup the expected keys
  for key in ${!expected_keys}; do
    val=$(jq -r ".$key" <<< "$test_secret")
    if [ "$val" == "null" ]; then
      echo "key: '$key' not found in $TEST_ACCOUNT_CR_NAME-$secret"
      EXIT_STATUS=$EXIT_FAIL_INVALID_KEYS
      continue
    fi

    # Prepare variables for validity check
    if [ $key == "aws_access_key_id" ]; then
      export AWS_ACCESS_KEY_ID=$(echo -n $val | base64 -d)
    fi
    if [ $key == "aws_secret_access_key" ]; then
      export AWS_SECRET_ACCESS_KEY=$(echo -n $val | base64 -d)
    fi
    if [ $key == "aws_session_token" ]; then
      export AWS_SESSION_TOKEN=$(echo -n $val | base64 -d)
    fi
  done

  # if the aws access key id is set, we should check the credential too.
  if [ -n "$AWS_ACCESS_KEY_ID" ]; then
    if ! aws sts get-caller-identity > /dev/null 2>&1; then
      echo "Credentials for $TEST_ACCOUNT_CR_NAME-$secret are invalid."
      # We only want to override the status if it's not already failing
      if [ $EXIT_STATUS -eq $EXIT_PASS ]; then
        EXIT_STATUS=$EXIT_FAIL_INVALID_CREDS
      fi
    fi
  fi
done

if [ $EXIT_STATUS -eq $EXIT_PASS ]; then
  echo "Tested Secrets have valid structure and credentials are valid."
fi

exit $EXIT_STATUS
