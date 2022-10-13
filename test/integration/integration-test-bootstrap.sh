#!/usr/bin/env bash

set -eo pipefail

source test/integration/test_envs
source test/integration/integration-test-lib.sh

export TEST_START_TIME_SECONDS=$(date +%s)
export OPERATOR_START_TIME=$(date --rfc-3339=seconds)
export IMAGE_NAME=aws-account-operator
export BUILD_CONFIG=aws-account-operator
export OPERATOR_DEPLOYMENT=aws-account-operator
OC="oc"

declare -A testResults

function usage {
    cat <<EOF
    USAGE: $0 [ OPTION ]
    OPTIONS:
    -n|--namespace  Sets the namespace to use
    -p|--profile    One of: ['local', 'prow', 'stage']. Determines how we build, deploy and run tests.
    --skip-cleanup  Skips the cleanup if provided

    PROFILES:
    local           Use operator-sdk to build and run the operator locally. '.envrc' configures secrets and basic configuration.
    prow            Used by prow CI automation. Operator image is built on the cluster using a BuildConfig then deployed.
                    Configuration key/values are expected to be available at /tmp/secret/aao-aws-creds/ (e.g.
                    /tmp/secret/aao-aws-creds/OPERATOR_ACCESS_KEY_ID).
    stage           Run tests against a stage OSD cluster. Operator image is built on the cluster using a BuildConfig then deployed.
                    '.envrc' configures secrets and basic configuration.
EOF
}

function parseArgs {
    PARSED_ARGUMENTS=$(getopt -o 'n:,p:' --long 'namespace:,profile:,skip-cleanup' -- "$@")
    eval set -- "$PARSED_ARGUMENTS"

    while :
    do
        case "$1" in
            -n|--namespace)
                NAMESPACE="$2";	shift 2
                ;;
            -p|--profile)
                PROFILE="$2";	shift 2
                ;;
            --skip-cleanup)
                SKIP_CLEANUP=1; shift
                ;;
            --)
                shift
                break
                ;;
            *)
                echo "Unexpected option: $1."
                usage
                break
                ;;
        esac
    done
    if [ -z "$NAMESPACE" ]; then
        NAMESPACE=aws-account-operator
    fi
}

function sourceEnvrcConfig {
    if [ ! -f ".envrc" ]; then
        echo "ERROR - .envrc does not exist"
        return 1
    fi
    source .envrc
}

function sourceFromMountedKvStoreConfig {
    ## Prow CI uses existing secrets:- https://docs.ci.openshift.org/docs/how-tos/adding-a-new-secret-to-ci/
    OSD_STAGING_1_AWS_ACCOUNT_ID=$(cat /tmp/secret/aao-aws-creds/OSD_STAGING_1_AWS_ACCOUNT_ID)
    export OSD_STAGING_1_AWS_ACCOUNT_ID
    OSD_STAGING_2_AWS_ACCOUNT_ID=$(cat /tmp/secret/aao-aws-creds/OSD_STAGING_2_AWS_ACCOUNT_ID)
    export OSD_STAGING_2_AWS_ACCOUNT_ID
    STS_ROLE_ARN=$(cat /tmp/secret/aao-aws-creds/STS_ROLE_ARN)
    export STS_ROLE_ARN
    STS_JUMP_ARN=$(cat /tmp/secret/aao-aws-creds/STS_JUMP_ARN)
    export STS_JUMP_ARN
    OSD_STAGING_1_OU_ROOT_ID=$(cat /tmp/secret/aao-aws-creds/OSD_STAGING_1_OU_ROOT_ID)
    export OSD_STAGING_1_OU_ROOT_ID
    OSD_STAGING_1_OU_BASE_ID=$(cat /tmp/secret/aao-aws-creds/OSD_STAGING_1_OU_BASE_ID)
    export OSD_STAGING_1_OU_BASE_ID
    SUPPORT_JUMP_ROLE=$(cat /tmp/secret/aao-aws-creds/SUPPORT_JUMP_ROLE)
    export SUPPORT_JUMP_ROLE
    STS_JUMP_ROLE=$(cat /tmp/secret/aao-aws-creds/STS_JUMP_ROLE)
    export STS_JUMP_ROLE
    OPERATOR_ACCESS_KEY_ID=$(cat /tmp/secret/aao-aws-creds/OPERATOR_ACCESS_KEY_ID)
    export OPERATOR_ACCESS_KEY_ID
    OPERATOR_SECRET_ACCESS_KEY=$(cat /tmp/secret/aao-aws-creds/OPERATOR_SECRET_ACCESS_KEY)
    export OPERATOR_SECRET_ACCESS_KEY
}

function sanityCheck {
    if [ -z "$OPERATOR_ACCESS_KEY_ID" ] || [ -z "$OPERATOR_SECRET_ACCESS_KEY" ]; then
        echo "ERROR: AWS credential variable OPERATOR_ACCESS_KEY_ID or OPERATOR_SECRET_ACCESS_KEY is missing."
        return 1
    fi
    REPO_ROOT=$(git rev-parse --show-toplevel)
    CURRENT_DIR=$(pwd)
    if [ "$REPO_ROOT" != "$CURRENT_DIR" ]; then
        echo "ERROR: Please execute the script from repository root folder."
        return 1
    fi
}

function dumpOperatorLogsToConsole {
    if [ "$LOCAL_LOG_FILE" ] && [ -f "$LOCAL_LOG_FILE" ];then
        echo -e "\n========================================================================"
        echo "= Operator Logs"
        echo "========================================================================"
        cat "$LOCAL_LOG_FILE"
    else
        echo -e "\nOPERATOR LOGS\nRetrieving from cluster..."
        getOperatorPodCommand="$OC_WITH_NAMESPACE get po -lname=aws-account-operator"
        if [[ $($getOperatorPodCommand --no-headers | wc -l) == 0 ]];
            then
                echo -e "\nNo operator pods found.\n"
                return 0
        fi

        echo -e "\n========================================================================"
        echo "= Operator Logs"
        echo "========================================================================"
        operatorPodName=$($getOperatorPodCommand -ojsonpath='{.items[0].metadata.name}')
        echo -e "\nStatus of the operator pod\n"
        $OC_WITH_NAMESPACE get po "$operatorPodName" -ojsonpath='{.status}'
        echo -e "\nOPERATOR LOGS\n"
        $OC_WITH_NAMESPACE logs "$operatorPodName" --since-time="$OPERATOR_START_TIME"
    fi
    
}

function cleanGeneratedBuildResources {
    echo "Cleaning up generated build resources"

    # remove dockerfile soft link
    if [ -f "Dockerfile" ]; then
        rm Dockerfile
    fi

    # kustomize files generated during operator build/deploy
    if [ -f "./deploy/kustomization.yaml" ]; then
        rm ./deploy/kustomization.yaml
    fi
    if [ -f "./deploy/modify_operator.yaml" ]; then
        rm ./deploy/modify_operator.yaml
    fi
}

## Cleanup is done via cleanupPre and cleanupPost functions
## Cleanup performs mandatory cleanup of Dockerfile Softlink, kustomization files & existing operator deployments or processes.
## If $SKIP_CLEANUP is not provided, existing aws-account-operator namespace is also cleaned up.
function cleanup {
    if [ -z $SKIP_CLEANUP ]; then
        cleanGeneratedBuildResources
        if [ "$localOperatorPID" ]; then
            echo "Removing leftover operator-sdk process with PID $localOperatorPID"
            kill "$localOperatorPID" 2>/dev/null || true
        fi
        if [ "$LOCAL_LOG_FILE" ] && [ -f "$LOCAL_LOG_FILE" ]; then
            echo "Removing previous log file $LOCAL_LOG_FILE"
            rm "$LOCAL_LOG_FILE"
        fi
        echo "Cleaning up cluster resources"
        $OC_WITH_NAMESPACE delete deployment $OPERATOR_DEPLOYMENT 2>/dev/null || true
        if [[ $($OC get namespace $NAMESPACE --no-headers 2>/dev/null | wc -l) == 0 ]];
            then
                echo -e "\nNo $NAMESPACE namespace found.\n"
                return
        fi

        $OC delete namespace $NAMESPACE --ignore-not-found=true || true
    else
        echo "Skipping test bootstrap cleanup due to --skip-cleanup flag"
        echo "Note: individual tests may still perform their own cleanup steps"
    fi
}

## cleanupPre runs at start.
function cleanupPre {
    echo -e "\n========================================================================"
    echo "= Before All Test Cleanup"
    echo "========================================================================"
    cleanup
}

## cleanupPost runs at end as a trap process for all types of exits.
function cleanupPost {
    echo -e "\n========================================================================"
    echo "= After All Test Cleanup"
    echo "========================================================================"
    if [ "$clusterUserName" ]; then
        $OC --as backplane-cluster-admin adm policy remove-cluster-role-from-user cluster-admin "$clusterUserName"
    fi
    cleanup

    echo -e "\n========================================================================"
    echo "= AWS Resource Leak Check"
    echo "========================================================================"
    make ci-aws-resources-cleanup
}

function buildOperatorImage {
    # quirk with oc build process which requires the dockerfile be in the project root directory
    ln ./build/Dockerfile Dockerfile

    $OC_WITH_NAMESPACE new-build --binary --strategy=docker --build-arg=FIPS_ENABLED=false  --name $BUILD_CONFIG || true
    $OC_WITH_NAMESPACE start-build $BUILD_CONFIG --from-dir . -F --exclude='(^|/)((\.git)|(\.venv)|(\.idea))(/|$)'
    
    # allow image lookup for the images produced by the build config
    $OC_WITH_NAMESPACE set image-lookup $BUILD_CONFIG
}

function verifyBuildSuccess {
    echo "Verifying build success"
    sleep 5
    local latestJobName phase
    latestJobName="$BUILD_CONFIG"-$($OC_WITH_NAMESPACE get buildconfig "$BUILD_CONFIG" -ojsonpath='{.status.lastVersion}') 
    phase=$($OC_WITH_NAMESPACE get build "$latestJobName" -ojsonpath='{.status.phase}')
    if [[ $phase != "Complete" ]]; then
        echo "ERROR - build was not completed fully, the state was $phase but expected to be 'Complete'"
        echo "the logs for the failed job are:"

        $OC_WITH_NAMESPACE logs "${latestJobName}-build"
        return 1
    fi
}

function configureKustomization {
    # make the AAO image (quay.io/app-sre/aws-account-operator:latest) resolve to the image we just built
    cat <<EOF >./deploy/kustomization.yaml
resources:
- operator.yaml
images:
- name: quay.io/app-sre/aws-account-operator:latest
  newName: $BUILD_CONFIG
patchesJson6902:
- target:
    group: apps
    version: v1
    kind: Deployment
    name: aws-account-operator
  path: modify_operator.yaml
EOF

    cat <<EOF >./deploy/modify_operator.yaml
- op: add
  path: /spec/template/spec/containers/0/env/0
  value:
    name: FORCE_DEV_MODE
    value: $FORCE_DEV_MODE
EOF
}

function deployOperator {
    configureKustomization
    $OC apply -k ./deploy
}

function waitForDeployment {
    echo "Waiting for operator deployment to finish (timeout in 60s)"
    $OC_WITH_NAMESPACE wait --for=condition=available --timeout=60s deployment $OPERATOR_DEPLOYMENT || (echo -e '\nERROR - Waited for operator deployment to complete for 60s\n' && return 1)
    echo "Deployment Completed"
}

# note: needed for running builds on prow infrastructure
function installJq {
    echo "Installing dependency: jq"
    curl -sfL https://github.com/stedolan/jq/releases/download/jq-1.6/jq-linux64 --output /tmp/jq
    chmod a+x /tmp/jq
}

# note: needed for running builds on prow infrastructure
function installAWS {
    echo "Installing dependency: aws-cli"
    curl -sfL "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" --output /tmp/awscliv2.zip
    unzip -qq /tmp/awscliv2.zip
    ./aws/install --install-dir /tmp/aws-cli -b /tmp

    echo "Configuring aws-cli credentials"
    cat <<EOF >/tmp/credentials
[osd-staging-1]
aws_access_key_id = $OPERATOR_ACCESS_KEY_ID
aws_secret_access_key = $OPERATOR_SECRET_ACCESS_KEY

[osd-staging-2]
aws_access_key_id = $OPERATOR_ACCESS_KEY_ID
aws_secret_access_key = $OPERATOR_SECRET_ACCESS_KEY

[default]
aws_access_key_id = $OPERATOR_ACCESS_KEY_ID
aws_secret_access_key = $OPERATOR_SECRET_ACCESS_KEY
EOF
    export AWS_SHARED_CREDENTIALS_FILE=/tmp/credentials
}

function installProwCIDependencies {
    installJq
    installAWS
    PATH=$PATH:/tmp
}

function profileLocal {
    echo -e "\n========================================================================"
    echo "= Int Testing Bootstrap Profile - Local Cluster"
    echo "========================================================================"
    echo "Configuring local build environment"
    export LOCAL_LOG_FILE="localOperator.log"
    export FORCE_DEV_MODE=local
    sourceEnvrcConfig
    sanityCheck

    # cleanup resources from previous runs
    cleanupPre

    echo -e "\n========================================================================"
    echo "= Preparing Cluster for Build/Deploy"
    echo "========================================================================"
    $OC adm new-project "$NAMESPACE" 2>/dev/null || true
    make predeploy

    echo -e "\n========================================================================"
    echo "= Building/Running Operator using operator-sdk"
    echo "========================================================================"

    # start local operator if not already running
    #
    # not sure if theres a better way to detect this, but operator-sdk processes look 
    # like this when I was testing locally: 
    #   > ps aux | grep "[g]o-build"
    #   mstratto 3313211  0.2  0.1 2590492 117236 pts/7  Sl+  10:59   0:08 /tmp/go-build3762679696/b001/exe/main --zap-devel
    # There is some build information in that path, but I thought that was overkill:
    #   > cat /tmp/go-build3762679696/b001/importcfg.link | grep aws-account-operator | wc -l
    #   14
    if ! localOperatorPID=$(pgrep -f go-build); then
        echo "Building and deploying operator image"
        make deploy-local OPERATOR_NAMESPACE=$NAMESPACE > $LOCAL_LOG_FILE 2>&1 &
        localOperatorPID=$!
        echo "Operator running in background with PID $localOperatorPID"
        echo "You can follow operator logs with: tail -f $LOCAL_LOG_FILE"
    else
        echo "Local operator-sdk process (probably) already running with PID $localOperatorPID."
        localOperatorStdOut=$(ls -l /proc/$localOperatorPID/fd/1)
        if !  echo "$localOperatorStdOut" | grep "$LOCAL_LOG_FILE" &>/dev/null; then
            echo -e "Proc $localOperatorPID stdout doesnt seem to be going to the expected log file ($LOCAL_LOG_FILE):\n\t$localOperatorStdOut"
            echo "This might not be the operator process. Do you have multiple operator-sdk builds running?"
            echo "You may need to intervene to make sure the operator is running properly."
        else
            echo "You can follow operator logs with: tail -f $LOCAL_LOG_FILE"
        fi
    fi
}

function profileProw {
    echo -e "\n========================================================================"
    echo "= Int Testing Bootstrap Profile - Prow"
    echo "========================================================================"
    echo "Configuring local build environment"
    export FORCE_DEV_MODE=cluster
    sourceFromMountedKvStoreConfig
    installProwCIDependencies


    # cleanup resources from previous runs
    cleanupPre

    echo -e "\n========================================================================"
    echo "= Preparing Cluster for Build/Deploy"
    echo "========================================================================"
    $OC adm new-project "$NAMESPACE" 2>/dev/null || true
    make prow-ci-predeploy

    echo -e "\n========================================================================"
    echo "= Building Operator Image"
    echo "========================================================================"
    OPERATOR_START_TIME=$(date --rfc-3339=seconds)
    buildOperatorImage
    verifyBuildSuccess

    echo -e "\n========================================================================"
    echo "= Deploying Operator Image"
    echo "========================================================================"
    deployOperator
    waitForDeployment
}

function profileStage {
    echo -e "\n========================================================================"
    echo "= Int Testing Bootstrap Profile - Stage Cluster"
    echo "========================================================================"
    
    echo "Configuring local build environment"
    export FORCE_DEV_MODE=cluster
    sourceEnvrcConfig
    sanityCheck
    
    ## OSD Staging cluster require cluster-admin roles for accessing & applying some manifests like CRDs etc.
    ## So, cluster-admin role is added to the user for the script's lifetime.
    ## The role is removed as the part of mandatory cleanup.
    clusterUserName=$($OC whoami)
    $OC --as backplane-cluster-admin adm policy add-cluster-role-to-user cluster-admin "$clusterUserName"

    # cleanup resources from previous runs
    cleanupPre

    echo -e "\n========================================================================"
    echo "= Preparing Cluster for Build/Deploy"
    echo "========================================================================"
    $OC adm new-project "$NAMESPACE" 2>/dev/null || true
    make prow-ci-predeploy
    make validate-deployment

    echo -e "\n========================================================================"
    echo "= Building Operator Image"
    echo "========================================================================"
    echo "Checking for existing AAO deployment."
    OPERATOR_START_TIME=$(date --rfc-3339=seconds)
    aaoDeployment=$($OC_WITH_NAMESPACE get deployment $OPERATOR_DEPLOYMENT -o json --ignore-not-found=true | jq '.status.conditions[] | select( .type == "Available" and .status == "True" )')

    if [ -z "$aaoDeployment" ]; then
        buildOperatorImage
        verifyBuildSuccess

        echo -e "\n========================================================================"
        echo "= Deploying Operator Image"
        echo "========================================================================"
        deployOperator
        waitForDeployment
    else
        echo "Deployment found. Not rebuilding or deploying operator image."
    fi
}

function explainExitCode {
    local script=$1
    local exitCode=$2
    message="UNKNOWN"

    if [ -z "$exitCode" ]; then
        message="No test results found"
    elif [ "$exitCode" -ne 0 ]; then
        message=$(/usr/bin/env bash "$script" explain "$exitCode")
        if [ -z "$message" ]; then
            # check the general exit code messages
            message=${COMMON_EXIT_CODE_MESSAGES[$exitCode]}
            if [ -z "$message" ]; then
                message="UNKNOWN"
            fi
        fi
    else
        message="PASS"
    fi
    echo $message
}


function runTest {
    local testScript=$1
    overall="PASS"

    echo -e "\n========================================================================"
    echo "= Test: $testScript"
    echo "= Phase: setup"
    echo "========================================================================"
    /usr/bin/env bash "$testScript" "setup"
    setupExitCode=$?
    setupExitMessage=$(explainExitCode "$testScript" "$setupExitCode")

    echo -e "\n========================================================================"
    echo "= Test: $testScript"
    echo "= Phase: test"
    echo "========================================================================"
    if [ $setupExitCode -eq "$EXIT_PASS" ]; then
        /usr/bin/env bash "$testScript" "test"
        testExitCode=$?
        testExitMessage=$(explainExitCode "$testScript" "$testExitCode")
        if [ "$testExitCode" -eq "$EXIT_PASS" ]; then
            overall="PASS"
        else
            overall="FAIL"
        fi
    else
        echo "Test setup failed, skipping test run."
        testExitCode=$EXIT_SKIP
        testExitMessage='Skipped (Setup Failed)'
        overall="SKIP"
    fi

    echo -e "\n========================================================================"
    echo "= Test: $testScript"
    echo "= Phase: cleanup"
    echo "========================================================================"
    /usr/bin/env bash "$testScript" "cleanup"

    # note: failure during cleanup doesnt currently affect the overall test result
    cleanupExitCode=$?
    cleanupExitMessage=$(explainExitCode "$testScript" "$cleanupExitCode")

    testResults[$testScript]=$(cat <<EOF
{
    "overall": "$overall",
    "setupExitCode": $setupExitCode,
    "setupExitMessage": "$setupExitMessage",
    "testExitCode": $testExitCode,
    "testExitMessage": "$testExitMessage",
    "cleanupExitCode": $cleanupExitCode,
    "cleanupExitMessage": "$cleanupExitMessage"
}
EOF
)
    echo -e "\n========================================================================"
    echo "= Test: $testScript"
    echo "= Phase: results"
    echo "========================================================================"
    echo "Test $testScript completed with overall result: $overall"
    echo "${testResults[$testScript]}"
}

function printTestResults {
    allPassed=1
    for t in "${!testResults[@]}"; do
        resultJson=${testResults[$t]}
        testExitCode=$(echo "$resultJson" | jq -r .testExitCode)
        overall=$(echo "$resultJson" | jq -r .overall)
        setupExitMessage=$(echo "$resultJson" | jq -r .setupExitMessage)
        testExitMessage=$(echo "$resultJson" | jq -r .testExitMessage)
        cleanupExitMessage=$(echo "$resultJson" | jq -r .cleanupExitMessage)

        if [ "$testExitCode" -ne 0 ]; then
            allPassed=0
        fi

        echo "[$overall] $t"
        echo "$resultJson"
    done

    if [[ $allPassed -eq 1 ]]; then
        echo -e "\nOverall Test Result: PASSED - All tests passed\n"
        exit 0
    else
        echo -e "\nOverall Test Result: FAILED - There are skipped tests and/or test failures, please investigate.\n"
        exit 1
    fi
}

parseArgs "$@"
OC_WITH_NAMESPACE="$OC -n $NAMESPACE"
trap cleanupPost EXIT
case $PROFILE in
    local)
        profileLocal
        ;;
    prow)
        profileProw
        ;;
    stage)
        profileStage
        ;;
    *)
        echo "Unknown profile: '$PROFILE'"
        exit 1
        ;;
esac

echo -e "\n========================================================================"
echo "= START INTEGRATION TESTS"
echo "========================================================================"
set +e
runTest "test/integration/tests/test_nonccs_account_creation.sh"
runTest "test/integration/tests/test_nonccs_account_reuse.sh"
runTest "test/integration/tests/test_aws_ou_logic.sh"
set -e

# we probably only want to dump the logs on prow for convenience
# just leaving it commented out for now.
#dumpOperatorLogsToConsole

echo -e "\n========================================================================"
echo "= INTEGRATION TEST RESULTS"
echo "========================================================================"
printTestResults



