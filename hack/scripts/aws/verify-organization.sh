#!/usr/bin/env bash

# This script checks that an account-id is under the right organizational root
# unit to be used in testing.

GREEN="\033[0;32m"
YELLOW="\033[0;33m"
CLEAR="\033[0m"

if [ $# -lt 1 ]; then
    echo "Usage: $0 account-id [aws-profile]"
    echo "Verify the account with 'account-id' is situated in the right place in the organization structure to be used in testing."
fi

ACCOUNT_ID="${1}"
AWS_PROFILE="${2:-osd-staging-1}"

find_parents() {
    CURRENT_ID=$1
    CURRENT_TYPE="start"
    PARENTS=()
    while [ $CURRENT_TYPE != "ROOT" ]; do
        echo "Retrieving parents for $CURRENT_ID"
        parent=$(aws --output json --profile "$AWS_PROFILE" organizations list-parents --child-id "$CURRENT_ID")
        CURRENT_ID=$(jq -r '.Parents[0].Id' <<< "$parent")
        CURRENT_TYPE=$(jq -r '.Parents[0].Type' <<< "$parent")
        PARENTS[${#PARENTS[@]}]="$CURRENT_ID"
    done
}

verify_account_in_org() {
    NUM_ELEMENTS="${#PARENTS[@]}"
    if [ "$NUM_ELEMENTS" != "1" ]; then
        echo -e "${YELLOW}The account is not under the root organization and will not work for STS integration tests."
        echo -e "You can move the account with the following command: ${CLEAR}"
        last_index=$(($NUM_ELEMENTS - 1))
        echo "aws --profile $AWS_PROFILE organizations move-account --account-id $ACCOUNT_ID --source-parent-id ${PARENTS[$last_index]} --destination-parent-id ${PARENTS[0]}"
    else
        echo -e "${GREEN}The account is under the root organization and ready to be used for testing.${CLEAR}"
    fi
}

echo "Finding all parents for account $ACCOUNT_ID"
find_parents "$ACCOUNT_ID"

echo "Verifying suitability for testing"
verify_account_in_org
