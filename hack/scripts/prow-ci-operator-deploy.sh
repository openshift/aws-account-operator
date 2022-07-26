#!/bin/bash

set -eo pipefail

export IMAGE_NAME=aws-account-operator
export BUILD_CONFIG=aws-account-operator
export OPERATOR_DEPLOYMENT=aws-account-operator
OC="oc"
OC_WITH_NAMESPACE="$OC -n $NAMESPACE"

function usage {
    cat <<EOF
    USAGE: $0 [ OPTION ]

    OPTIONS:

    --use-envrc     Uses the .envrc for setting up configmap and secret otherwise pick up from ci vault secrets
    -n|--namespace  Sets the namespace to use
    --is-local      Uses settings and makefile targets for local deployment
    --is-staging    Uses settings and makefile targets for staging deployment
EOF
}

function parseArgs {
    PARSED_ARGUMENTS=$(getopt -o 'n:' --long 'namespace:,use-envrc,is-local,is-staging' -- "$@")
    eval set -- "$PARSED_ARGUMENTS"

    while :
    do
        case "$1" in
            --use-envrc)
                USE_ENVRC=1; shift
                ;;
            --is-local)
                IS_LOCAL=1; shift
                ;;
            --is-staging)
                IS_STAGING=1; shift
                ;;
            -n|--namespace)
                NAMESPACE="$2";	shift 2
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

    echo "USE_ENVRC=${USE_ENVRC}"
    echo "NAMESPACE=${NAMESPACE}"
    echo "IS_LOCAL=${IS_LOCAL}"
    echo "IS_STAGING=${IS_STAGING}"
}

function cleanup {
    echo -e "\nPERFORMING CLEANUP\n"
    ##  Add step to remove CRDs
    if [[ $($OC get namespace $NAMESPACE --no-headers | wc -l) == 0 ]];
        then
            echo -e "\nno $NAMESPACE namespace found\n"
            return
	fi

    $OC delete namespace $NAMESPACE || true
}

function cleanupPre {
    cleanup    
}

function getEnvVariables {
    if [[ -z $USE_ENVRC ]]; then
        ## Prow CI uses existing secrets:- https://docs.ci.openshift.org/docs/how-tos/adding-a-new-secret-to-ci/
        export FORCE_DEV_MODE=cluster
        export OSD_STAGING_1_AWS_ACCOUNT_ID=$(cat /tmp/secret/aao-aws-creds/OSD_STAGING_1_AWS_ACCOUNT_ID)
        export OSD_STAGING_2_AWS_ACCOUNT_ID=$(cat /tmp/secret/aao-aws-creds/OSD_STAGING_2_AWS_ACCOUNT_ID)
        export STS_ROLE_ARN=$(cat /tmp/secret/aao-aws-creds/STS_ROLE_ARN)
        export STS_JUMP_ARN=$(cat /tmp/secret/aao-aws-creds/STS_JUMP_ARN)
        export OSD_STAGING_1_OU_ROOT_ID=$(cat /tmp/secret/aao-aws-creds/OSD_STAGING_1_OU_ROOT_ID)
        export OSD_STAGING_1_OU_BASE_ID=$(cat /tmp/secret/aao-aws-creds/OSD_STAGING_1_OU_BASE_ID)
        export SUPPORT_JUMP_ROLE=$(cat /tmp/secret/aao-aws-creds/SUPPORT_JUMP_ROLE)
        export STS_JUMP_ROLE=$(cat /tmp/secret/aao-aws-creds/STS_JUMP_ROLE)
        export OPERATOR_ACCESS_KEY_ID=$(cat /tmp/secret/aao-aws-creds/OPERATOR_ACCESS_KEY_ID)
        export OPERATOR_SECRET_ACCESS_KEY=$(cat /tmp/secret/aao-aws-creds/OPERATOR_SECRET_ACCESS_KEY)
        return
    fi
    
    if [ ! -f ".envrc" ]; then
        echo "ERROR - .envrc does not exist"
        return 1
    fi
    source .envrc
}

function preDeploy {
    echo -e "\nDEPLOY CRDs, Secret, Config Map\n"
    $OC adm new-project "$NAMESPACE" || true
    if [ -z $IS_LOCAL -a -z $IS_STAGING ];
        then
            make prow-ci-predeploy
            return
        else
            make predeploy
    fi
    source .venv/bin/activate
}

function createDockerfileSoftLink {
    ln ./build/Dockerfile Dockerfile
}

function buildOperatorImage {
    echo -e "\nSTARTING BUILD IMAGE\n"
    $OC_WITH_NAMESPACE new-build --binary --strategy=docker --build-arg=FIPS_ENABLED=false  --name $BUILD_CONFIG || true
    $OC_WITH_NAMESPACE start-build $BUILD_CONFIG --from-dir . -F
    $OC_WITH_NAMESPACE set image-lookup $BUILD_CONFIG
}

function verifyBuildSuccess {
    sleep 5
    local latestJobName phase
    latestJobName="$BUILD_CONFIG"-$($OC_WITH_NAMESPACE get buildconfig "$BUILD_CONFIG" -ojsonpath='{.status.lastVersion}') 
    phase=$($OC_WITH_NAMESPACE get build "$latestJobName" -ojsonpath='{.status.phase}')
    if [[ $phase != "Complete" ]]; then
        echo "ERROR - build was not completed fully, the state was $phase but expected to be 'Complete'"
        echo "the logs for the failed job are:"

        $OC_WITH_NAMESPACE logs $latestJobName-build
        return 1
    fi
}

function deployOperator {
    echo -e "\nDEPLOYING OPERATOR\n"
    make prow-ci-deploy-image OPERATOR_IMAGE_URI=$BUILD_CONFIG
}

function waitForDeployment {
    echo -e "\nWaiting for operator deployment to finish\n"
    $OC_WITH_NAMESPACE wait --for=condition=available --timeout=5s deployment $OPERATOR_DEPLOYMENT || (echo -e '\nERROR - Waited for operator deployment to complete for 60s\n' && return 1)
    echo -e "\nDeployment Completed\n"
    ## Remove Sleep later
    sleep 20
}

function consoleOperatorLogs {
    getOperatorPodCommand="$OC_WITH_NAMESPACE get po -lname=aws-account-operator"
    if [[ $($getOperatorPodCommand --no-headers | wc -l) == 0 ]];
        then
            echo -e "\nno operator pods found\n"
            return
	fi
	operatorPodName=$($getOperatorPodCommand -ojsonpath='{.items[0].metadata.name}')
	echo -e "\nstatus of the operator pod\n"
	$OC_WITH_NAMESPACE get po "$operatorPodName" -ojsonpath='{.status}'
	echo -e "\npod logs\n"
	$OC_WITH_NAMESPACE logs $operatorPodName
}

function cleanupPost {
    consoleOperatorLogs
    cleanup
}

parseArgs $@

trap cleanupPost EXIT

cleanupPre

getEnvVariables

preDeploy

createDockerfileSoftLink

buildOperatorImage

verifyBuildSuccess

deployOperator

waitForDeployment
