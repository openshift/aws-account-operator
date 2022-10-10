#!/usr/bin/env bash

source test/integration/test_envs

EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED=2
EXIT_TEST_FAIL_REUSED_ACCOUNT_NOT_READY=3
EXIT_TEST_FAIL_ACCOUNT_NOT_REUSED=4

declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED]="Test Account CR has a status of failed. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_REUSED_ACCOUNT_NOT_READY]="Test Account CR is not in a ready state. Check AAO logs for more details."
exitCodeMessages[$EXIT_TEST_FAIL_ACCOUNT_NOT_REUSED]="Test Account CR was not reused. Check AAO logs for more details."

function setupTestPhase {
  # move OSD Staging 1 account to root ou to avoid ChildNotFoundInOU errors
  hack/scripts/aws/verify-organization.sh "${OSD_STAGING_1_AWS_ACCOUNT_ID}" --profile osd-staging-1 --move

  oc process --local -p NAME="${ACCOUNT_CLAIM_NAMESPACE}" -f hack/templates/namespace.tmpl | oc apply -f -

  oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" 2>/dev/null
  ACCOUNT_CR_EXISTS=$?

  if [ $ACCOUNT_CR_EXISTS -ne 0 ]; then
      echo "Creating Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
      if ! oc process -p AWS_ACCOUNT_ID="${OSD_STAGING_1_AWS_ACCOUNT_ID}" -p ACCOUNT_CR_NAME="${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -p NAMESPACE="${NAMESPACE}" -f hack/templates/aws.managed.openshift.io_v1alpha1_account.tmpl | oc apply -f -; then
          echo "Failed to create account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
          exit "$EXIT_FAIL_UNEXPECTED_ERROR"
      fi
  else

      if ! STATUS=$(oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" -o json | jq -r '.status.state'); then
          echo "Failed to get status of account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
          exit "$EXIT_FAIL_UNEXPECTED_ERROR"
      fi

      if [ "$STATUS" == "Ready" ]; then
          echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} is ready."
      elif [ "$STATUS" == "Failed" ]; then
          echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} failed to create"
          exit $EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED
      else
          echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} status is ${STATUS}, waiting for it to become ready or fail."
          exit "$EXIT_RETRY"
      fi
  fi

  oc get accountclaim "${ACCOUNT_CLAIM_NAME}" -n "${ACCOUNT_CLAIM_NAMESPACE}" 2>/dev/null
  ACCOUNT_CLAIM_CR_EXISTS=$?

  if [ $ACCOUNT_CLAIM_CR_EXISTS -ne 0 ]; then
      echo "Creating Account Claim ${ACCOUNT_CLAIM_NAME}"
      if ! oc process --local -p NAME="${ACCOUNT_CLAIM_NAME}" -p NAMESPACE="${ACCOUNT_CLAIM_NAMESPACE}" -f hack/templates/aws.managed.openshift.io_v1alpha1_accountclaim_cr.tmpl | oc apply -f -; then
          echo "Failed to create account claim ${ACCOUNT_CLAIM_NAME}"
          exit "$EXIT_FAIL_UNEXPECTED_ERROR"
      fi
  else
      if ! STATUS=$(oc get accountclaim "${ACCOUNT_CLAIM_NAME}" -n "${ACCOUNT_CLAIM_NAMESPACE}" -o json | jq -r '.status.state'); then
          echo "Failed to get status of account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
          exit "$EXIT_FAIL_UNEXPECTED_ERROR"
      fi

      if [ "$STATUS" == "Ready" ]; then
          echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} is ready."
      elif [ "$STATUS" == "Failed" ]; then
          echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} failed to create"
          exit $EXIT_TEST_FAIL_ACCOUNT_PROVISIONING_FAILED
      else
          echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} status is ${STATUS}, waiting for it to become ready or fail."
          exit "$EXIT_RETRY"
      fi
  fi

  # Create S3 Bucket
  AWS_ACCESS_KEY_ID=$(oc get secret "${IAM_USER_SECRET}" -n "${NAMESPACE}" -o json | jq -r '.data.aws_access_key_id' | base64 -d)
  export AWS_ACCESS_KEY_ID

  AWS_SECRET_ACCESS_KEY=$(oc get secret "${IAM_USER_SECRET}" -n "${NAMESPACE}" -o json | jq -r '.data.aws_secret_access_key' | base64 -d)
  export AWS_SECRET_ACCESS_KEY

  aws s3api create-bucket --bucket "${REUSE_BUCKET_NAME}" --region=us-east-1

  # TODO: Might need to remove accountclaim finalizers
  oc process --local -p NAME="${ACCOUNT_CLAIM_NAME}" -p NAMESPACE="${ACCOUNT_CLAIM_NAMESPACE}" -f hack/templates/aws.managed.openshift.io_v1alpha1_accountclaim_cr.tmpl | oc delete --now --ignore-not-found -f -

  # Delete reuse namespace
  oc process --local -p NAME="${ACCOUNT_CLAIM_NAMESPACE}" -f hack/templates/namespace.tmpl | oc delete -f -
}

function cleanupTestPhase {
    if ! oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" 2>/dev/null; then
        oc patch account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" -p '{"metadata":{"finalizers":null}}' --type=merge
        oc process -p AWS_ACCOUNT_ID="${OSD_STAGING_1_AWS_ACCOUNT_ID}" -p ACCOUNT_CR_NAME="${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -p NAMESPACE="${NAMESPACE}" -f hack/templates/aws.managed.openshift.io_v1alpha1_account.tmpl | oc delete --now --ignore-not-found -f -

        if ! oc get account "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -n "${NAMESPACE}" 2>/dev/null; then
            echo "Failed to delete account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}"
            exit "$EXIT_FAIL_UNEXPECTED_ERROR"
        fi
    fi

    exit "$EXIT_PASS"
}

function testPhase {
	# Validate re-use
	IS_READY=$(oc get account -n aws-account-operator "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -o json | jq -r '.status.state')
	if [ "$IS_READY" != "Ready" ]; then
	  echo "Reused Account is not Ready"
	  exit $EXIT_TEST_FAIL_REUSED_ACCOUNT_NOT_READY
	fi

	IS_REUSED=$(oc get account -n aws-account-operator "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" -o json | jq -r '.status.reused')
	if [ "$IS_REUSED" != true ]; then
	  echo "Account is not Reused"
	  exit $EXIT_TEST_FAIL_ACCOUNT_NOT_REUSED
	fi

	# List S3 bucket
	BUCKETS=$(
    AWS_ACCESS_KEY_ID=$(oc get secret "${IAM_USER_SECRET}" -n "${NAMESPACE}" -o json | jq -r '.data.aws_access_key_id' | base64 -d)
    export AWS_ACCESS_KEY_ID
    AWS_SECRET_ACCESS_KEY=$(oc get secret "${IAM_USER_SECRET}" -n "${NAMESPACE}" -o json | jq -r '.data.aws_secret_access_key' | base64 -d)
    export AWS_SECRET_ACCESS_KEY

    aws s3api list-buckets | jq '[.Buckets[] | .Name] | length'
	)
	if [ "$BUCKETS" == 0 ]; then
	  echo "Reuse successfully complete"
	else
	  echo "Reuse failed"
	  exit 1
  fi

  exit 0
}

function explainExitCode {
    local exitCode=$1
    local message=${exitCodeMessages[$exitCode]}
    echo "$message"
}

# The phase are specific keys passed in by the test framework. You can change function names if you want
# but do not change the phase names used as keys in the switch statement.
PHASE=$1
case $PHASE in
setup)
  setupTestPhase
  ;;
cleanup)
  cleanupTestPhase
  ;;
test)
  testPhase
  ;;
explain)
    explainExitCode "$2"
    ;;
*)
  echo "Unknown test phase: '$PHASE'"
  exit 1
  ;;
esac
