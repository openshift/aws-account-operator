# Generate creds based on Access Role & Jump Role for OSD_STAGING_1 & OSD_STAGING_2 respectively
function generateRoleCreds {
    if [ -z $1 -a -z $2 ]; then
        echo "ERROR: Either Role ARN or AWS Account ID not provided as function argument."
        return 1
    fi
    ROLE_ARN=$1
    AWS_ACCOUNT_ID=$2

    # TODO: remove profile
    echo "Assuming role $ROLE_ARN for account $AWS_ACCOUNT_ID"
    roleCreds=$(aws sts assume-role --role-arn ${ROLE_ARN} --role-session-name "ProwCIResourceCleanup" --output json )
    export AWS_ACCESS_KEY_ID=$(jq -r '.Credentials.AccessKeyId' <<< $roleCreds)
    export AWS_SECRET_ACCESS_KEY=$(jq -r '.Credentials.SecretAccessKey' <<< $roleCreds)
    export AWS_SESSION_TOKEN=$(jq -r '.Credentials.SessionToken' <<< $roleCreds)
}

function getEC2Instances {
    instanceIds=$(aws ec2 describe-instances --region $REGION --filters "Name=owner-id,Values=$AWS_ACCOUNT_ID" --filters 'Name=instance-state-name,Values=running,pending' | jq -r '.Reservations[].Instances | map(.InstanceId) | join(",")')
}

function terminateEC2Instances {
    if [ $instanceIds ];
        then
            echo "Cleaning up ec2 instances for $AWS_ACCOUNT_ID: $instanceIds"
            aws ec2 terminate-instances --region $REGION --instance-ids $instanceIds || (exitCode=$? && echo "ERROR during instances termination" && return $exitCode)
        else
            echo "No running ec2 instances to cleanup for account $AWS_ACCOUNT_ID"
    fi
}

function waitForInstancesTermination {
    if [ $instanceIds ]; then
        echo "Waiting for instances to terminate."
        aws ec2 wait instance-terminated --region $REGION --instance-ids $instanceIds || (echo "ec2 instances termination check failed for $AWS_ACCOUNT_ID" && return 1)
        echo "All ec2 instances terminated successfully for $AWS_ACCOUNT_ID"
    fi
}

function getVPCs {
    vpcIds=$(aws ec2 describe-vpcs --region $REGION --filters "Name=owner-id,Values=$AWS_ACCOUNT_ID" --filters "Name=is-default,Values=false" | jq -r '.Vpcs[].VpcId')
}

function deleteVPCs {
    if [ $vpcIds ];
        then
            for vpcId in $vpcIds
            do
                echo "Cleaning up vpc for $AWS_ACCOUNT_ID: $vpcId"
                aws ec2 delete-vpc --region $REGION --vpc-id $vpcId || (exitCode=$? && echo "ERROR during $vpcId deletion" && return $exitCode)
            done
        else
            echo "No vpcs to cleanup for account $AWS_ACCOUNT_ID"
    fi
}

echo -e "\nSTARTING AWS RESOURCES CLEANUP FOR CI\n"

## Currently the int tests only interact with us-east-1 region (specified in the AccountClaim CRs the tests create)
## for now we only clean up this region, but a more thorough implementation should probably check all regions or
## better yet just use the Account Shredder (https://github.com/openshift/aws-account-shredder) or whatever replaces it.
REGION=us-east-1

## aws-cli uses below credentials instead of profile for access
export AWS_ACCESS_KEY_ID=$OPERATOR_ACCESS_KEY_ID
export AWS_SECRET_ACCESS_KEY=$OPERATOR_SECRET_ACCESS_KEY

generateRoleCreds $1 $2
getEC2Instances
terminateEC2Instances
waitForInstancesTermination
getVPCs
deleteVPCs

echo -e "\nAWS RESOURCES CLEANUP COMPLETED\n"
