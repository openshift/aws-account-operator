#!/bin/bash
AWS_ACCOUNT_NAME=$1

if [ -z "$AWS_ACCOUNT_NAME" ]; then
    echo "No AWS account name specified"
    echo -e "$0 <aws-account-name>\n"
    exit 1
fi

echo "Running.."

# echo "1"
./scripts/scrub_aws_account.sh "$AWS_ACCOUNT_NAME"
# echo "2"
./scripts/remove_cluster_aws_account_secrets.sh "$AWS_ACCOUNT_NAME"
# echo "3"
./scripts/delete_cluster_aws_accounts.sh "$AWS_ACCOUNT_NAME"
# echo "4"
oc apply -f "$AWS_ACCOUNT_NAME".json
# echo "5"
