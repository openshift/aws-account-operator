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

function parseArgs {
    PARSED_ARGUMENTS=$(getopt -o 'm,p:' --long 'move,profile:' -- "$@")
    eval set -- "$PARSED_ARGUMENTS"

    while :
    do
        case "$1" in
            -m|--move)
                SHOULD_MOVE=1;	shift
                ;;
            -p|--profile)
                AWS_PROFILE="$2";	shift 2
                ;;
            --)
                shift
                break
                ;;
            *)
                echo "Unexpected option: $1."
                break
                ;;
        esac
    done
    shift "$(($OPTIND -1))"
    ACCOUNT_ID="${1}"
    if [ -z $AWS_PROFILE ]; then
        AWS_PROFILE=osd-staging-1
    fi
    echo "ACCOUNT_ID=${ACCOUNT_ID}"
    echo "AWS_PROFILE=${AWS_PROFILE}"
    echo "SHOULD_MOVE=${SHOULD_MOVE}"
}


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
        last_index=$(($NUM_ELEMENTS - 1))
        echo -e "${YELLOW}The account is not under the root organization and will not work for STS integration tests."
        if [ "$SHOULD_MOVE" == "1" ]; then
            echo -e "${GREEN}Moving account to the root organization.${CLEAR}"
            aws --output json --profile "$AWS_PROFILE" organizations move-account --account-id "$ACCOUNT_ID" --source-parent-id "${PARENTS[0]}" --destination-parent-id "${PARENTS[$last_index]}"
        else
            echo -e "You can move the account with the following command: ${CLEAR}"
        
            echo "aws --profile $AWS_PROFILE organizations move-account --account-id $ACCOUNT_ID --source-parent-id  ${PARENTS[0]} --destination-parent-id ${PARENTS[$last_index]}"
            echo -e "\nOr, rerun this script with the -m/--move flag${CLEAR}"
        fi
    else
        echo -e "${GREEN}The account is under the root organization and ready to be used for testing.${CLEAR}"
    fi
}

parseArgs $@

echo "Finding all parents for account $ACCOUNT_ID"
find_parents "$ACCOUNT_ID"

echo "Verifying suitability for testing"
verify_account_in_org
