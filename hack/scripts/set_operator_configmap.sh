#!/bin/bash

usage() {
    cat <<EOF
    usage: $0 [ OPTION ]

    Options

    -a         AWS Organization Account limit
    -o         AWS Account OU BASE ID
    -r         AWS Account OU ROOT ID
    -s         AWS Account STS Jump Role ARN
    -m         AWS Account Support Jump Role ARN
    -v         AWS VCPU Quota
EOF
}

while getopts ":a:o:r:v:s:m:h" opt; do
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
        s)
            STS_JUMP_ARN="$OPTARG" >&2
            ;;
        m)
            SUPPORT_JUMP_ROLE="$OPTARG" >&2
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

if [ -z "${STS_JUMP_ARN+x}" ]; then
    echo "AWS STS ARN for Jump Role not set"
    usage
    exit 1
fi

if [ -z "${SUPPORT_JUMP_ROLE+x}" ]; then
    echo "AWS SUPPORT ARN for Jump Role not set"
    usage
    exit 1
fi

ACCOUNTPOOL_CONFIG="
zero-size-accountpool: 
  default: true
hs-zero-size-accountpool:
  servicequotas:
    default:
      L-1216C47A: '750'
      L-0EA8095F: '200'
      L-69A177A2: '255'
      L-0263D0A3: '6'
    us-east-1:
      L-1216C47A: '11'"

echo "Deploying AWS Account Operator Configmap"
oc process --local -p ROOT="${AWS_ROOT_OU}" -p BASE="${AWS_BASE_OU}" -p ACCOUNT_LIMIT="${AWS_ACCOUNT_LIMIT}" -p VCPU_QUOTA="${AWS_VCPU_QUOTA}" -p OPERATOR_NAMESPACE=aws-account-operator -p STS_JUMP_ARN="${STS_JUMP_ARN}" -p SUPPORT_JUMP_ROLE="${SUPPORT_JUMP_ROLE}" -p ACCOUNTPOOL_CONFIG="${ACCOUNTPOOL_CONFIG}" -f hack/templates/aws.managed.openshift.io_v1alpha1_configmap.tmpl | oc apply -f -
