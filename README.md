# aws-account-operator

This operator will be responsible for creating and maintaining a pool of AWS accounts. The operator will create the initial setup and configuration of the those accounts, IAM resources and expose credentials for a IAM user with enough permissions to provision a 4.0 cluster.

The operator requries a secret named `aws-account-operator-credentials` containing credentials to the AWS payer account you wish to create accounts in. The secret should contain credentials for an IAM user in the payer account with the data fields `aws_access_key_id` and `aws_secret_access_key`. The user should have the following IAM permissions:


Permissions to allow the user to assume the `OrganizationAccountAccessRole` role in any account created:
```
{
   "Version": "2012-10-17",
   "Statement": {
       "Effect": "Allow",
       "Action": "sts:AssumeRole",
       "Resource": "arn:aws:iam::*:role/OrganizationAccountAccessRole"
   }
}
```

Permissions to allow the user to interact with the support center:
```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "support:*"
            ],
            "Resource": "*"
        }
    ]
}
```


## Testing your AWS account credentials with the CLI
The below commands can be used to test payer account credentials where we create new accounts inside the payer accounts organization. Once the account is created in the first step we wait until the account is created with step 2 and retrieve its account ID. Using the account ID we can then test our IAM user has sts:AssumeRole permissions to Assume the OrganizationAccountAccessRole in the new account. The OrganizationAccountAccessRole is created automatically when a new account is created under the organization.

1. `aws organizations create-account --email "username+cli-test@redhat.com" --account-name "username-cli-test" --profile=orgtest`

2. `aws organizations list-accounts --profile=orgtest | jq '.[][] | select(.Name=="username-cli-test")'`

3. `aws sts assume-role --role-arn arn:aws:iam::<ID>:role/OrganizationAccountAccessRole --role-session-name username-cli-test --profile=orgtest`