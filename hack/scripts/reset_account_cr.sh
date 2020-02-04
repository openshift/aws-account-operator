#!/bin/bash
set -o nounset
set -o pipefail

RESET_CLUSTER_ACCOUNT_CR=false

usage() {
    cat <<EOF
    usage: $0 [ OPTION ]

    Options
    -a         AWS Account CR Name on cluster
    -n         Cluster kubeadmin context name
    Can be retrieved with the follwoing command:
    \$ kubectl config view -o jsonpath='{"Cluster name\tServer\n"}{range .clusters[*]}{.name}{"\t"}{.cluster.server}{"\n"}{end}'

    -r         Reset cluster account CR status

    example: $0 -a osd-creds-mgmt-4sf3x -n internal-api-cluster-name-openshift-com -r

    You must set '-r' to reset then account CR

    This will delete secrets in the aws-account-operator namespace

    eg. the following secrets would be deleted: oc get secrets -n aws-acocunt-operator | grep osd-creds-mgmt-4sf3*

    This will reset the following fields in the .spec and .status to:

    .spec.claimLink: ""
    .spec.claimLinkNamespace: ""
    .spec.iamUserSecret: ""

    .status.claimed: false
    .status.state: ""
    .status.rotateCredentials: false
    .status.rotateConsoleCredentials: false

EOF
}

if ( ! getopts ":a:n:rh" opt); then
    echo -e "\n    $0 requries an argument!\n"
    usage
    exit 1 
fi

while getopts ":a:n:rh" opt; do
    case $opt in
        a)
            AWS_ACCOUNT_CR_NAME="$OPTARG" >&2
            ;;
        n)
            CLUSTER_CONTEXT_NAME="$OPTARG" >&2
            ;;
        r)
            RESET_CLUSTER_ACCOUNT_CR=true >&2
            ;;
        h)
            echo "Invalid option: -$OPTARG" >&2
            usage
            exit 1
            ;;
        \?)
            echo "Invalid option: -$OPTARG" >&2
            usage
            exit 1
            ;;
        :)
            echo "$0 Requires an argument" >&2
            usage
            exit 1
            ;;
    esac
done

if [ -z "${CLUSTER_CONTEXT_NAME+x}" ]; then
    usage
    exit 1
fi

if [ -z "${RESET_CLUSTER_ACCOUNT_CR+x}" ]; then
    usage
    exit 1
fi

if ! "$RESET_CLUSTER_ACCOUNT_CR"; then
    usage
    echo "You must set '-r' to reset account $AWS_ACCOUNT_CR_NAME"
    exit 1
fi


if ! [ -z "${RESET_CLUSTER_ACCOUNT_CR+x}" ]; then
    echo "Reseting $AWS_ACCOUNT_CR_NAME status"

    if [ -z "${AWS_ACCOUNT_ID_ARG+x}" ]; then
        for secret in $(oc get secrets -n aws-account-operator --no-headers | grep "${AWS_ACCOUNT_CR_NAME}" | awk '{print $1}'); do
            echo "Deleting secret $secret"
            oc delete secret "$secret" -n aws-account-operator
        done
    fi

    oc patch account ${AWS_ACCOUNT_CR_NAME} -n aws-account-operator -p '{"spec":{"claimLink":"", "claimLinkNamespace":"", "iamUserSecret":""}}' --type=merge

    if ! [ -z "${AWS_ACCOUNT_CR_NAME+x}" ]; then

        APISERVER=$(kubectl config view -o jsonpath="{.clusters[?(@.name==\"$CLUSTER_CONTEXT_NAME\")].cluster.server}")
        echo "$APISERVER"

        # Service account for aws-account-operator in the aws-account-operator namespace
        AAO_SERVICE_ACCOUNT_NAME=$(oc get sa $(oc get pod -l name=aws-account-operator -n aws-account-operator --no-headers -o json | jq -r '.items[].spec.serviceAccountName') -n aws-account-operator -o json | jq -r '.secrets[].name | match("aws-account-operator-token.*").string')

        TOKEN=$(oc get secret "${AAO_SERVICE_ACCOUNT_NAME}" -n aws-account-operator -o json | jq -r '.data.token' | base64 -d)

        RETURN_CODE=$(curl -s -I -X GET $APISERVER/api --header "Authorization: Bearer $TOKEN" --insecure | grep -oE "HTTP\/1.1\ +[0-9]{3}")
        #RETURN_CODE_DEBUG=$(curl -s -I -X GET $APISERVER/api --header "Authorization: Bearer $TOKEN" --insecure)
        #echo "$RETURN_CODE_DEBUG"

        if ! [ "$RETURN_CODE" = 'HTTP/1.1 200' ]; then
            echo "Return code: $RETURN_CODE"
            echo "Authentication failure?"
            exit 1 
        fi

        #{"op": "add", "path": "/status/conditions", "value": []}
        PATCH_DATA='[
          {"op": "add", "path": "/status/rotateCredentials", "value": false},
          {"op": "add", "path": "/status/rotateConsoleCredentials", "value": false},
          {"op": "add", "path": "/status/claimed", "value": false},
          {"op": "add", "path": "/status/state", "value": ""}
        ]'

        curl --header "Content-Type: application/json-patch+json" \
        --request PATCH \
        --header "Authorization: Bearer $TOKEN" \
        --insecure \
        --data "${PATCH_DATA}" \
        "${APISERVER}"/apis/aws.managed.openshift.io/v1alpha1/namespaces/aws-account-operator/accounts/"${AWS_ACCOUNT_CR_NAME}"/status
    fi
    echo "Done"
fi
