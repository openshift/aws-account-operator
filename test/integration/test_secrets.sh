#!/bin/bash

TEST_ACCOUNT="osd-staging-1"
TEST_NAMESPACE="aws-account-operator"
EXIT_STATUS="PASS"

# Define Expected Secrets and their keys
# FORMAT: expectedPosftix:VARIABLE_WITH_KEYS
EXPECTED_SECRETS=(
  "osdmanagedadminsre-secret:OSDMASRE_SECRET_KEYS"
  "secret:SECRET_KEYS"
  "sre-cli-credentials:SRE_CLI_KEYS"
  "sre-console-url:CONSOLE_KEYS"
)

OSDMASRE_SECRET_KEYS="aws_access_key_id aws_secret_access_key aws_user_name"
SECRET_KEYS="aws_access_key_id aws_secret_access_key aws_user_name"
SRE_CLI_KEYS="aws_access_key_id aws_secret_access_key aws_session_token"
CONSOLE_KEYS="aws_console_login_url"

for secret_map in "${EXPECTED_SECRETS[@]}"; do
  secret=${secret_map%%:*}
  expected_keys=${secret_map#*:}
  test_secret="$(oc get secret osd-creds-mgmt-$TEST_ACCOUNT-$secret -n $TEST_NAMESPACE -o json | jq '.data')"
  
  if [ "$test_secret" == "" ]; then
    EXIT_STATUS="FAIL"
    continue
  fi

  # Lookup the expected keys
  for key in ${!expected_keys}; do
    val=$(jq -r ".$key" <<< "$test_secret")
    if [ "$val" == "null" ]; then
      echo "key: '$key' not found in $TEST_ACCOUNT-$secret"
      EXIT_STATUS="FAIL"
    fi
  done
done

if [ $EXIT_STATUS == "FAIL" ]; then
  exit 1
fi

echo "Tested Secrets have valid structure."
