#!/bin/bash

usage() {
    cat <<EOF
    usage: $0 [ OPTION ]

    Options

    -a         AWS Organization Account limit
    -o         AWS Account OU ID 
    -r         AWS Account OU ROOT ID
    -v         AWS VCPU Quota
EOF
}

while getopts ":a:o:r:v:h" opt; do
    case $opt in
        a)
            AWS_ACCOUNT_LIMIT="$OPTARG" >&2
            ;;
        o)
            AWS_BASE_OU="$OPTARG" >&2
            ;;
        r)
            AWS_ROOT_OU="$OPTARG" >&2
            ;;
        v)
            AWS_VCPU_QUOTA="$OPTARG" >&2
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

if [ -z "${AWS_ACCOUNT_LIMIT+x}" ]; then
    echo "AWS account limit for Oragnization not set"
    usage
    exit 1
fi

if [ -z "${AWS_BASE_OU+x}" ]; then
    echo "AWS base OU not set"
    usage
    exit 1
fi

if [ -z "${AWS_ROOT_OU+x}" ]; then
    echo "AWS root OU not set"
    usage
    exit 1
fi

if [ -z "${AWS_VCPU_QUOTA+x}" ]; then
    echo "AWS VCPU Quota not set"
    usage
    exit 1
fi

echo "Deploying AWS Account Operator Configmap"
oc process -p ROOT="${AWS_ROOT_OU}" -p BASE="${AWS_BASE_OU}" -p ACCOUNTLIMIT="${AWS_ACCOUNT_LIMIT}" -p VCPU_QUOTA="${AWS_VCPU_QUOTA}" -p OPERATOR_NAMESPACE=aws-account-operator -f hack/templates/aws.managed.openshift.io_v1alpha1_configmap.tmpl | oc apply -f -
