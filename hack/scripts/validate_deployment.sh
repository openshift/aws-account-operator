#!/bin/bash

usage() {
  cat <<EOF
  usage: $0 [ OPTIONS ]

  validate_deployment.sh will create a cluster using OCM in each of 'non-ccs' and 'ccs' types. This script should be used to test aws-account-operator deployments, and should be run after new pods are deployed to the different environments.

  This script assumes that you are already logged into an OCM environment, and that you have a set of osdCcsAdmin credentials in your ~/.aws/credentials directory for the account you want to create a CCS cluster in.
  Options
  -e  --environment  Which OCM environment to run tests against
  -h  --help         Show this message and exit
  -p  --profile      Which AWS Profile should we use?
EOF
}

# Ensure there's a parameter passed in
if [ $# -lt 2 ]; then
  usage
  exit 1
fi

while [ "$1" != "" ]; do
  case "$1" in
    -e | --env | --environment) OCM_ENVIRONMENT=$2
                                shift
                                ;;
    -h | --help )           usage
                            exit 0
                            ;;
    -p | --profile )        PROFILE=$2
                            shift
                            ;;

    * ) echo "Unexpected parameter $1"
        usage
        exit 1
  esac
  shift
done

export YELLOW="\033[33m"
export RED="\033[31m"
export GREEN="\033[32m"
export RESET="\033[0m"

export BULLET="\xe2\x97\x89"
export ARROW="\xe2\x96\xb8"
export PENDING="${YELLOW}${ARROW}${RESET}"
export ERROR="${RED}!!${RESET}"
export SUCCESS="${GREEN}${BULLET}${RESET}"

## Define all functions, scroll to the bottom for the entrypoint of the script

run_test()
{
  local TEST_NAME=$1
  local TEST_CASE=$2
  local DATETIME=$(date +%m%d)

  case $TEST_NAME in
    non-ccs)
      local CLUSTER_NAME="aao-$DATETIME"
      echo -e "$PENDING Creating Non-CCS Cluster $CLUSTER_NAME"
      OCM_CREATE=$(ocm create cluster --region us-east-1 $CLUSTER_NAME)
      if [[ $? -ne 0 ]]; then
        echo -e "$ERROR $TEST_NAME failed.  Unable to create cluster."
        exit 1
      fi

      # get the cluster ID from the OCM output
      local CLUSTER_ID=$(echo $OCM_CREATE | grep '^ID:' | awk '{print $2}')

      echo -e "$PENDING - Non-CCS $CLUSTER_NAME created. Cluster ID $CLUSTER_ID"

      ocm_cluster_test $CLUSTER_ID
      ;;
    ccs)
      local CLUSTER_NAME="aao-ccs-$DATETIME"
      echo -e "$PENDING Creating CCS Cluster $CLUSTER_NAME"
      eval $(get_aws_credentials)

      echo -e "$PENDING Getting AWS Account Info from Profile"
      ACCOUNT=$(aws sts get-caller-identity | jq -r .Account)
      echo -e "$PENDING AWS Account $ACCOUNT info collected."

      OCM_CREATE=$(ocm create cluster --region us-east-1 --ccs --aws-access-key-id $AWS_ACCESS_KEY_ID --aws-secret-access-key $AWS_SECRET_ACCESS_KEY --aws-account-id $ACCOUNT $CLUSTER_NAME)
      if [[ $? -ne 0 ]]; then
        echo -e "$ERROR $TEST_NAME failed.  Unable to create cluster."
        exit 1
      fi

      # get the cluster ID from the OCM output
      local CLUSTER_ID=$(echo $OCM_CREATE | grep '^ID:' | awk '{print $2}')

      echo -e "$PENDING - CCS $CLUSTER_NAME created. Cluster ID $CLUSTER_ID"

      ocm_cluster_test $CLUSTER_ID
      ;;
    sts)
      local CLUSTER_NAME="aao-sts-$DATETIME"
      ;;
    *)
      echo -e "$ERROR $TEST_NAME is not a valid test case" >2
      exit 1
      ;;
  esac

  echo -e " $SUCCESS $TEST_NAME is complete."
}

install_wait_loop()
{
  local CLUSTER_ID=$1
  local TIMEOUT=$2
  local CHECK_INTERVAL=$3
  # Loop 
  while true; do
    sleep $CHECK_INTERVAL
    TIMEOUT=$(( TIMEOUT - CHECK_INTERVAL ))
    local STATUS=$(ocm get /api/clusters_mgmt/v1/clusters/$CLUSTER_ID/status | jq -r .state)
    if [[ $STATUS == "installing" ]]; then
      echo -e "$SUCCESS $TEST_NAME has passed. Beginning Cleanup..."
      break
    fi

    if [[ $TIMEOUT -le 0 ]]; then
      echo -e "$RED$BULLET$RESET $TEST_NAME has timed out.  Beginning Cleanup..."
      break
    fi

    if [[ $STATUS == "pending" ]]; then
      echo -e "$PENDING $TEST_NAME is still $STATUS..."
    fi
  done
}

delete_wait_loop()
{
  local CLUSTER_ID=$1
  local TIMEOUT=$2
  local CHECK_INTERVAL=$3
  # Begin Cleanup Loop
  while true; do
    sleep $CHECK_INTERVAL
    TIMEOUT=$(( TIMEOUT - CHECK_INTERVAL ))
    local STATUS=$(ocm get /api/clusters_mgmt/v1/clusters/$CLUSTER_ID/status 2>&1)

    if [[ $? -ne 0 ]]; then
      echo -e "$SUCCESS $TEST_NAME has successfully finished cleaning up."
      break
    fi

    # Sometimes the above command doesn't work, so let's also check the status that comes back in stderr
    local STATUS_KIND=$(jq -r .kind <<< $STATUS)
    local STATUS_ID=$(jq -r .id <<< $STATUS)

    if [[ $STATUS_KIND == "Error" ]] && [[ $STATUS_ID == "404" ]]; then
      echo -e "$SUCCESS $TEST_NAME has successfully finished cleaning up."
      break
    fi

    # If we hit our timeout, let's exit.
    if [[ $TIMEOUT -le 0 ]]; then
      echo -e "$ERROR $TEST_NAME cleanup has timed out.  This could mean that resources are abandoned in your account and require manual cleanup."
      break
    fi

    # Check to see if we're still uninstalling and let the user know
    local STATE=$(jq -r .state <<< $STATUS)

    if [[ $STATE == "uninstalling" ]]; then
      echo -e "$PENDING $TEST_NAME is still $STATE..."
    fi
  done  
}

ocm_cluster_test()
{
  echo -e "$PENDING $TEST_NAME Started."
  local CLUSTER_ID=$1

  # Set the timeout to be 900 seconds, or 15m
  local CREATE_TIMEOUT=900
  local DELETE_TIMEOUT=900
  
  # Check Interval is how long between checks we go, in seconds
  local CHECK_INTERVAL=5
  
  install_wait_loop $CLUSTER_ID $CREATE_TIMEOUT $CHECK_INTERVAL

  ocm delete cluster $CLUSTER_ID

  delete_wait_loop $CLUSTER_ID $DELETE_TIMEOUT $CHECK_INTERVAL
}

validate_aws_credentials()
{
  echo -e "$PENDING Validating AWS Credentials..."

  creds=$(get_aws_credentials)
  if [[ $? -ne 0 ]]; then
    echo $creds
    exit 1
  fi

  eval $creds

  CALLER_IDENTITY=$(aws sts get-caller-identity 2>&1)

  if [[ $? -ne 0 ]] ; then
    echo -e "$ERROR AWS Credentials cannot be validated"
    echo "$CALLER_IDENTITY"
    exit 1
  fi

  echo -e "$SUCCESS AWS Credentials are valid."
}

get_aws_credentials()
{
  CREDSFILE="$HOME/.aws/credentials"

  CREDS=$(grep "\[$PROFILE\]" "$CREDSFILE")

  if [[ -z $CREDS ]]; then
    echo -e "$ERROR AWS Credentials cannot be found in $CREDSFILE"
    exit 1
  fi
  
  ACCESS_KEY_ID=$(grep -A2 "\[$PROFILE\]" $CREDSFILE | grep aws_access_key_id | awk -F= '{print $2}' | awk '{print $1}')
  SECRET_ACCESS_KEY=$(grep -A2 "\[$PROFILE\]" $CREDSFILE | grep aws_secret_access_key | awk -F= '{print $2}' | awk '{print $1}')

  echo "export AWS_ACCESS_KEY_ID=$ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY=$SECRET_ACCESS_KEY"
}

validate_ocm_environment()
{
  local OCM_API=$(ocm config get url)

  if [[ -z $OCM_ENVIRONMENT ]]; then
    echo -e "$ERROR Empty OCM Environment parameter."
    usage
    exit 1
  fi

  case $OCM_ENVIRONMENT in
    s | st | stage | staging)
      if ! grep stage <<< $OCM_API 2>&1 >/dev/null; then
        echo -e "$ERROR OCM Environment 'staging' specified, but 'ocm config get url' returns a different environment: $OCM_API"
        exit 1
      fi
      ;;
    p | pr | prod | production)
      if grep stage <<< $OCM_API 2>&1 >/dev/null; then
        echo -e "$ERROR OCM Environment 'production' specified, but 'ocm config get url' returns a different environment: $OCM_API"
        exit 1
      fi
      ;;
    *)
      echo -e "$ERROR Unsupported OCM Environment"
      exit 1
      ;;
  esac
}

export -f run_test ocm_cluster_test install_wait_loop delete_wait_loop get_aws_credentials

if [[ -z $PROFILE ]]; then
  echo -e "$ERROR Profile Must be set."
  usage
  exit 1
fi

validate_ocm_environment
validate_aws_credentials

tests=("non-ccs" "ccs")
TEST_START_TIME=$(date +%s)
printf "%s\n" "${tests[@]}" | parallel -u 'run_test {} {#}'
