# AWS-ACCOUNT-OPERATOR


## General Overview

This operator will be responsible for creating and maintaining a pool of AWS accounts. The operator will create the initial setup and configuration of the those accounts, IAM resources and expose credentials for a IAM user with enough permissions to provision a 4.0 cluster.


The operator is deployed to an openshift cluster. It will create a pool of AWS accounts and secrets containing IAM credentials inside of a namespace. The operator will then wait for a accountClaim to be created in any namespace. It will then tie that accountClaim to an account and credentials previously created, and it will put required infromation into the namespace where the accountClaim was created.


On our hive clusters there is a namespace called `aws-account-operator`. This is where the aws-account-operator runs and where the account CRs and the IAM user secrets are created.


When a cluster is requested from hive it will create a accountClaim whose name will be the requested clusters name. This will be put into a unique namespace for that cluster specified in the accountClaim. This will trigger the creation of secrets with data from one of the accounts in the `aws-account-operator` namespace that has not yet been used. This information will be used by hive to provision the resources required to bulid the request cluster on the supplied aws-account. 


For more information on how this process is done look at the controllers section. 




## Requirements

The operator requires a secret named `aws-account-operator-credentials` containing credentials to the AWS payer account you wish to create accounts in. The secret should contain credentials for an IAM user in the payer account with the data fields `aws_access_key_id` and `aws_secret_access_key`. The user should have the following IAM permissions:



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

# The Custom Resources

## AccountPool CR 

The AccountPool CR holds the information about the available number of accounts that can be claimed for cluster provisioning

```yaml
apiVersion: aws.managed.openshift.io/v1alpha1
kind: AccountPool
metadata:
  name: example-accountpool
  namespace: aws-account-operator
  selfLink: /apis/aws.managed.openshift.io/v1alpha1/namespaces/aws-account-operator/accountpools/example-accountpool
  uid: 9979786a-a8cb-11e9-a2a3-2a2ae2dbcce4
spec:
  poolSize: 50
status:
  claimedAccounts: 1946
  poolSize: 50
  unclaimedAccounts: 50
```

## Account CR

The Account CR holds the details about the account that was created , whether it is ready to be claimed, and whether it has been claimed

```yaml
apiVersion: aws.managed.openshift.io/v1alpha1
kind: Account
metadata:
  creationTimestamp: 2019-07-03T16:07:16Z
  finalizers:
  - finalizer.aws.managed.openshift.io
  generation: 4
  name: osd-{accountName}
  namespace: aws-account-operator
  ownerReferences:
  - apiVersion: aws.managed.openshift.io/v1alpha1
    blockOwnerDeletion: true
	controller: true
	kind: AccountPool
	name: example-accountpool
    uid: 9979786a-a8cb-11e9-a2a3-2a2ae2dbcce4
  resourceVersion: "49984188"
  selfLink: /apis/aws.managed.openshift.io/v1alpha1/namespaces/aws-account-operator/accounts/osd-{accountName}
  uid: a07a6867-9dac-11e9-b4bb-0e6ed767b7c0
spec:
  awsAccountID: "0000000000"
  claimLink: example-link
  iamUserSecret: osd-{accountName}-secret
status:
  claimed: true
  conditions:
  - lastProbeTime: 2019-07-03T16:14:50Z
    lastTransitioNTime: 2019-07-03T16:14:50Z
  	message: Attempting to create account
 	reason: Creating
 	status: "True"
 	type: Creating
  - lastProbeTime: 2019-07-03T16:18:55Z
    lastTransitioNTime: 2019-07-03T16:18:55Z
 	message: Account pending AWS limits verification
	reason: PendingVerification
	status: "True"
	type: PendingVerification
  - lastProbeTime: 2019-07-05T13:19:32Z
    lastTransitioNTime: 2019-07-05T13:19:32Z
	message: Account ready to be claimed
	reason: Ready
	status: "True"
	type: Ready
  state: Ready
  supportCaseID: case-000000000-muen-2019-000000000000
```


## AccountClaim CR

The AccountClaim CR holds the required data for cluster provisioning to build the cluster 

```yaml
apiVersion: aws.managed.openshift.io/v1alpha1
kind: AccountClaim
metadata:
  creationTimestamp: 2019-07-16T13:52:02Z
  generation: 2
  labels:
    api.openshift.com/id: 00000000000000000000
    api.openshift.com/name: example-link
  name: example-link
  namespace: {NameSpace cluster is being built in}
  resourceVersion: "54324077"
  selfLink: /apis/aws.managed.openshift.io/v1alpha1/namespaces/uhc-staging-16toh05i9h8ook5d7ej4al7ejec17rug/accountclaims/razevedo-test2
  uid: b7113d4a-a8cb-11e9-a2a3-2a2ae2dbcce4
spec:
  accountLink: osd-{accountName} (From AccountClaim)
  aws:
    regions:
	- name: us-east-1
  awsCredentialSecret:
	 name: aws
     namespace: {NameSpace cluster is being built in}
   legalEntity:
	 id: 00000000000000
	 name: {Legal Entity Name}
status:
  conditions:
  - lastProbeTime: 2019-07-16T13:52:02Z
    lastTransitionTime: 2019-07-16T13:52:02Z
	message: Attempting to claim account
	reason: AccountClaimed
	status: "True"
	type: Unclaimed
  - lastProbeTime: 2019-07-16T13:52:03Z
    lastTransitionTime: 2019-07-16T13:52:03Z
	message: Account claimed by osd-creds-mgmt-fhq2d2
	reason: AccountClaimed
	status: "True"
	type: Claimed
  state: Ready
```
# The controllers

## AccountPool Controller

The accountpool-controller is triggered by an accountpool CR or an account CR. It is responsible for generating new account CRs. 

It looks at the accountpool CR *spec.poolSize* and it ensures that the number of unclaimed accounts matchs the number of the poolsize. If the number of unclaimed accounts is less then the poolsize it creates a new account CR for the account-controller to process.

### Constants and Globals
```
emailID = "osd-creds-mgmt"
````

### Status

Updates accountPool cr 
```
  claimedAccounts: 4
  poolSize: 3
  unclaimedAccounts: 3
```

*claimedAccounts* are any accounts with the `status.Claimed=true`
*unclaimedAccounts* are any accounts with `status.Claimed=false` and `status.State!="Failed"`
*poolSize* is the poolsize from the accountPool spec

### Metrics

Updated in the accountPool-controller

```
MetricTotalAccountCRs
MetricTotalAccountCRsUnclaimed
MetricTotalAccountCRsClaimed
MetricTotalAccountPendingVerification
MetricTotalAccountCRsFailed
MetricTotalAccountCRsReady
MetricTotalAccountClaimCRs

```

#### Account Controller

The account-controller is triggered by an account CR. It is responsible for following behaviors

If the *awsLimit* set in the constants is not exceeded
1. Creates a new account in the organization belonging to credentials in secret `aws-account-operator-credentials` 
2. Configure Users from *iamUserNameUHC* and *iamUserNameSRE*
    * Creates IAM user in new account 
    * Attaches Admin policy
    * Generates a secret access key for the user 
    * Stores user secret in a AWS secret
3. Creates STS CLI Credentials for SRE
4. Creates and Destroys EC2 instances
5. Creates aws support case to increase account limits

##### Additional Functionailty 

* If `status.RotateCredentials == true` the account-controller will refresh the STS Cli Credentials
* If the account `status.State == "Creating"` and the account is older then the *createPendTime* constant the account will be put into a `failed` state
* if the account `status.State == AccountReady && spec.ClaimLink != ""` it sets `status.Claimed = true`

### Constants and Globals

```
awsLimit                = 2000
awsCredsUserName        = "aws_user_name"
awsCredsSecretIDKey     = "aws_access_key_id"
awsCredsSecretAccessKey = "aws_secret_access_key"
iamUserNameUHC          = "osdManagedAdmin"
iamUserNameSRE          = "osdManagedAdminSRE"
awsSecretName           = "aws-account-operator-credentials"
awsAMI                  = "ami-000db10762d0c4c05"
awsInstanceType         = "t2.micro"
createPendTime          = 10 * time.Minute
// Fields used to create/monitor AWS case

caseCategoryCode              = "other-account-issues"
caseServiceCode               = "customer-account"
caseIssueType                 = "customer-service"
caseSeverity                  = "urgent"
caseDesiredInstanceLimit      = 25
caseStatusResolved            = "resolved"
intervalAfterCaseCreationSecs = 30
intervalBetweenChecksSecs     = 30

// AccountPending indicates an account is pending
AccountPending = "Pending"
// AccountCreating indicates an account is being created
AccountCreating = "Creating"
// AccountFailed indicates account creation has failed
AccountFailed = "Failed"
// AccountReady indicates account creation is ready
AccountReady = "Ready"
// AccountPendingVerification indicates verification (of AWS limits and Enterprise Support) is pending
AccountPendingVerification = "PendingVerification"
// IAM Role name for IAM user creating resources in account
accountOperatorIAMRole = "OrganizationAccountAccessRole"

var awsAccountID string
var desiredInstanceType = "m5.xlarge"
var coveredRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"ca-central-1",
	"eu-central-1",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-south-1",
	"ap-southeast-1",
	"ap-southeast-2",
	"sa-east-1",
}
```

### Spec

Updates the Account CR

```
spec:
  awsAccountID: "000000112120"
  claimLink: "claim-name"
  iamUserSecret: accountName-secret
```

*awsAccountID* is updated with the account ID of the aws account that is created by the account controller
*claimLink* holds the name of the accountClaim that has claimed this accountCR
*iamUserSecret* holds the iam user credentials that is created by the account controller

### Status

Updates the Account CR
```
claimed: false
conditions:
- lastProbeTime: 2019-07-18T22:04:38Z
  lastTransitioNTime: 2019-07-18T22:04:38Z
  message: Attempting to create account
  reason: Creating
  status: "True"
  type: Creating
rotateCredentials: false
state: Failed
supportCaseID: "00000000"
```

*state* can be any of the account states defined in the constants
*claimed* is true if `currentAcctInstance.Status.State == AccountReady && currentAcctInstance.Spec.ClaimLink != "`
*rotateCredentials* when true rotates the temporary credentials on the next run
*supportCaseID* is the ID of the aws support case to increase limits
*conditions* indicates the last state the account had and supporting details

### Metrics

Update in the account-controller
```
MetricTotalAWSAccounts
```

#### AccountClaim Controller

The accountClaim-controller is triggered when an accountClaim is created in any namespace.It is responsible for following behaviors

1. Sets account `spec.ClaimLink` to the name of the accountClaim
2. Sets accountClaim `spec.AccountLink` to the name of an unclaimed Account 
3. Creates a secret in the accountClaim namespace that contains the credentials tied to the aws account in the accountCR
4. Sets accountClaim `status.State = "Ready"` 

### Constants and Globals
```
AccountClaimed          = "AccountClaimed"
AccountUnclaimed        = "AccountUnclaimed"
awsCredsUserName        = "aws_user_name"
awsCredsAccessKeyId     = "aws_access_key_id"
awsCredsSecretAccessKey = "aws_secret_access_key"
```

### Spec

Updates the accountClaim CR
```
spec:
  accountLink: osd-{accountName} 
  aws:
    regions:
	- name: us-east-1
  awsCredentialSecret:
	 name: aws
     namespace: {NameSpace}
   legalEntity:
	 id: 00000000000000
	 name:{Legal Entity Name}
```

*awsCredentialSecret* holds the name and namespace of the credentials created for the accountClaim

### Status

Updates the accountClaim CR
```
conditions:
  - lastProbeTime: 2019-07-16T13:52:02Z
    lastTransitionTime: 2019-07-16T13:52:02Z
	message: Attempting to claim account
	reason: AccountClaimed
	status: "True"
	type: Unclaimed
  - lastProbeTime: 2019-07-16T13:52:03Z
    lastTransitionTime: 2019-07-16T13:52:03Z
	message: Account claimed by osd-creds-mgmt-fhq2d2
	reason: AccountClaimed
	status: "True"
	type: Claimed
  state: Ready
```

*state* can be any of the ClaimStatus strings defined in accountclaim_types.go
*conditions* indicates the last state the account had and supporting details

### Metrics

Updated in the accountClaim-controller
```
MetricTotalAccountClaimCRs
```

# Special Items in main.go

* Initailzes a watcher that will look at all the secrets in the accountCRNamespace and if it exceeds the const `secretWatcherScanInterval` it will set the corresponsing accountCR `status.rotateCredentials = true`
* Starts a metric server with custom metrics defined in `localmetrics` pkg

### Constants

```
metricsPort               = "8080"
metricsPath               = "/metrics"
secretWatcherScanInterval = time.Duration(10) * time.Minute

```

*metricsPort* is the port used to start the metrics port
*metricsPath* it the path used as the metrics endpoint
*secretWatcherScanInterval* the interval used for the secrets watcher

# AWS-ACCOUNT-OPERATOR

## General Overview

This operator will be responsible for creating and maintaining a pool of AWS accounts. The operator will create the initial setup and configuration of the those accounts, IAM resources and expose credentials for a IAM user with enough permissions to provision a 4.0 cluster.

The operator is deployed to an openshift cluster. It will create a pool of AWS accounts and secrets containing IAM credentials inside of a namespace. The operator will then wait for a accountClaim to be created in any namespace. It will then tie that accountClaim to an account and credentials previously created, and it will put required infromation into the namespace where the accountClaim was created.

On our hive clusters there is a namespace called `aws-account-operator`. This is where the aws-account-operator runs and where the account CRs and the IAM user secrets are created.

When a cluster is requested from hive it will create a accountClaim whose name will be the requested clusters name. This will be put into a unique namespace for that cluster specified in the accountClaim. This will trigger the creation of secrets with data from one of the accounts in the `aws-account-operator` namespace that has not yet been used. This information will be used by hive to provision the resources required to bulid the request cluster on the supplied aws-account. 

For more information on how this process is done look at the controllers section. 

## Requirements

The operator requires a secret named `aws-account-operator-credentials` containing credentials to the AWS payer account you wish to create accounts in. The secret should contain credentials for an IAM user in the payer account with the data fields `aws_access_key_id` and `aws_secret_access_key`. The user should have the following IAM permissions:

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

# The Custom Resources

## AccountPool CR 

The AccountPool CR holds the information about the available number of accounts that can be claimed for cluster provisioning

```yaml
apiVersion: aws.managed.openshift.io/v1alpha1
kind: AccountPool
metadata:
  name: example-accountpool
  namespace: aws-account-operator
  selfLink: /apis/aws.managed.openshift.io/v1alpha1/namespaces/aws-account-operator/accountpools/example-accountpool
  uid: 9979786a-a8cb-11e9-a2a3-2a2ae2dbcce4
spec:
  poolSize: 50
status:
  claimedAccounts: 1946
  poolSize: 50
  unclaimedAccounts: 50
```

## Account CR

The Account CR holds the details about the account that was created , whether it is ready to be claimed, and whether it has been claimed


```yaml

apiVersion: aws.managed.openshift.io/v1alpha1
kind: Account
metadata:
  creationTimestamp: 2019-07-03T16:07:16Z
  finalizers:
  - finalizer.aws.managed.openshift.io
  generation: 4
  name: osd-{accountName}
  namespace: aws-account-operator
  ownerReferences:
  - apiVersion: aws.managed.openshift.io/v1alpha1
    blockOwnerDeletion: true
	controller: true
	kind: AccountPool
	name: example-accountpool
    uid: 9979786a-a8cb-11e9-a2a3-2a2ae2dbcce4
  resourceVersion: "49984188"
  selfLink: /apis/aws.managed.openshift.io/v1alpha1/namespaces/aws-account-operator/accounts/osd-{accountName}
  uid: a07a6867-9dac-11e9-b4bb-0e6ed767b7c0
spec:
  awsAccountID: "0000000000"
  claimLink: example-link
  iamUserSecret: osd-{accountName}-secret
status:
  claimed: true
  conditions:
  - lastProbeTime: 2019-07-03T16:14:50Z
    lastTransitioNTime: 2019-07-03T16:14:50Z
  	message: Attempting to create account
 	reason: Creating
 	status: "True"
 	type: Creating
  - lastProbeTime: 2019-07-03T16:18:55Z
    lastTransitioNTime: 2019-07-03T16:18:55Z
 	message: Account pending AWS limits verification
	reason: PendingVerification
	status: "True"
	type: PendingVerification
  - lastProbeTime: 2019-07-05T13:19:32Z
    lastTransitioNTime: 2019-07-05T13:19:32Z
	message: Account ready to be claimed
	reason: Ready
	status: "True"
	type: Ready
  state: Ready
  supportCaseID: case-000000000-muen-2019-000000000000
```

## AccountClaim CR

The AccountClaim CR holds the required data for cluster provisioning to build the cluster 

```yaml
apiVersion: aws.managed.openshift.io/v1alpha1
kind: AccountClaim
metadata:
  creationTimestamp: 2019-07-16T13:52:02Z
  generation: 2
  labels:
    api.openshift.com/id: 00000000000000000000
    api.openshift.com/name: example-link
  name: example-link
  namespace: {NameSpace cluster is being built in}
  resourceVersion: "54324077"
  selfLink: /apis/aws.managed.openshift.io/v1alpha1/namespaces/uhc-staging-16toh05i9h8ook5d7ej4al7ejec17rug/accountclaims/razevedo-test2
  uid: b7113d4a-a8cb-11e9-a2a3-2a2ae2dbcce4
spec:
  accountLink: osd-{accountName} (From AccountClaim)
  aws:
    regions:
	- name: us-east-1
  awsCredentialSecret:
	 name: aws
     namespace: {NameSpace cluster is being built in}
   legalEntity:
	 id: 00000000000000
	 name: {Legal Entity Name}
status:
  conditions:
  - lastProbeTime: 2019-07-16T13:52:02Z
    lastTransitionTime: 2019-07-16T13:52:02Z
	message: Attempting to claim account
	reason: AccountClaimed
	status: "True"
	type: Unclaimed
  - lastProbeTime: 2019-07-16T13:52:03Z
    lastTransitionTime: 2019-07-16T13:52:03Z
	message: Account claimed by osd-creds-mgmt-fhq2d2
	reason: AccountClaimed
	status: "True"
	type: Claimed
  state: Ready
```

# The controllers

## AccountPool Controller

The accountpool-controller is triggered by an accountpool CR or an account CR. It is responsible for generating new account CRs. 
It looks at the accountpool CR *spec.poolSize* and it ensures that the number of unclaimed accounts matchs the number of the poolsize. If the number of unclaimed accounts is less then the poolsize it creates a new account CR for the account-controller to process.

### Constants and Globals

```
emailID = "osd-creds-mgmt"
````

### Status

Updates accountPool cr 

```
  claimedAccounts: 4
  poolSize: 3
  unclaimedAccounts: 3
```

*claimedAccounts* are any accounts with the `status.Claimed=true`
*unclaimedAccounts* are any accounts with `status.Claimed=false` and `status.State!="Failed"`
*poolSize* is the poolsize from the accountPool spec

### Metrics

Updated in the accountPool-controller

```
MetricTotalAccountCRs
MetricTotalAccountCRsUnclaimed
MetricTotalAccountCRsClaimed
MetricTotalAccountPendingVerification
MetricTotalAccountCRsFailed
MetricTotalAccountCRsReady
MetricTotalAccountClaimCRs
```

#### Account Controller

The account-controller is triggered by an account CR. It is responsible for following behaviors

If the *awsLimit* set in the constants is not exceeded
1. Creates a new account in the organization belonging to credentials in secret `aws-account-operator-credentials` 
2. Configure Users from *iamUserNameUHC* and *iamUserNameSRE*
    * Creates IAM user in new account 
    * Attaches Admin policy
    * Generates a secret access key for the user 
    * Stores user secret in a AWS secret
3. Creates STS CLI Credentials for SRE
4. Creates and Destroys EC2 instances
5. Creates aws support case to increase account limits

##### Additional Functionailty 

* If `status.RotateCredentials == true` the account-controller will refresh the STS Cli Credentials
* If the account `status.State == "Creating"` and the account is older then the *createPendTime* constant the account will be put into a `failed` state
* if the account `status.State == AccountReady && spec.ClaimLink != ""` it sets `status.Claimed = true`

### Constants and Globals
```
awsLimit                = 2000
awsCredsUserName        = "aws_user_name"
awsCredsSecretIDKey     = "aws_access_key_id"
awsCredsSecretAccessKey = "aws_secret_access_key"
iamUserNameUHC          = "osdManagedAdmin"
iamUserNameSRE          = "osdManagedAdminSRE"
awsSecretName           = "aws-account-operator-credentials"
awsAMI                  = "ami-000db10762d0c4c05"
awsInstanceType         = "t2.micro"
createPendTime          = 10 * time.Minute
// Fields used to create/monitor AWS case
caseCategoryCode              = "other-account-issues"
caseServiceCode               = "customer-account"
caseIssueType                 = "customer-service"
caseSeverity                  = "urgent"
caseDesiredInstanceLimit      = 25
caseStatusResolved            = "resolved"
intervalAfterCaseCreationSecs = 30
intervalBetweenChecksSecs     = 30

// AccountPending indicates an account is pending
AccountPending = "Pending"
// AccountCreating indicates an account is being created
AccountCreating = "Creating"
// AccountFailed indicates account creation has failed
AccountFailed = "Failed"
// AccountReady indicates account creation is ready
AccountReady = "Ready"
// AccountPendingVerification indicates verification (of AWS limits and Enterprise Support) is pending
AccountPendingVerification = "PendingVerification"
// IAM Role name for IAM user creating resources in account
accountOperatorIAMRole = "OrganizationAccountAccessRole"
var awsAccountID string
var desiredInstanceType = "m5.xlarge"
var coveredRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"ca-central-1",
	"eu-central-1",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-south-1",
	"ap-southeast-1",
	"ap-southeast-2",
	"sa-east-1",
}

```

### Spec

Updates the Account CR
```
spec:
  awsAccountID: "000000112120"
  claimLink: "claim-name"
  iamUserSecret: accountName-secret
```

*awsAccountID* is updated with the account ID of the aws account that is created by the account controller
*claimLink* holds the name of the accountClaim that has claimed this accountCR
*iamUserSecret* holds the iam user credentials that is created by the account controller

### Status

Updates the Account CR
```
claimed: false
conditions:
- lastProbeTime: 2019-07-18T22:04:38Z
  lastTransitioNTime: 2019-07-18T22:04:38Z
  message: Attempting to create account
  reason: Creating
  status: "True"
  type: Creating
rotateCredentials: false
state: Failed
supportCaseID: "00000000"
```

*state* can be any of the account states defined in the constants
*claimed* is true if `currentAcctInstance.Status.State == AccountReady && currentAcctInstance.Spec.ClaimLink != "`
*rotateCredentials* when true rotates the temporary credentials on the next run
*supportCaseID* is the ID of the aws support case to increase limits
*conditions* indicates the last state the account had and supporting details

### Metrics

Update in the account-controller
```
MetricTotalAWSAccounts
```

#### AccountClaim Controller

The accountClaim-controller is triggered when an accountClaim is created in any namespace.It is responsible for following behaviors

1. Sets account `spec.ClaimLink` to the name of the accountClaim
2. Sets accountClaim `spec.AccountLink` to the name of an unclaimed Account 
3. Creates a secret in the accountClaim namespace that contains the credentials tied to the aws account in the accountCR
4. Sets accountClaim `status.State = "Ready"` 

### Constants and Globals

```
AccountClaimed          = "AccountClaimed"
AccountUnclaimed        = "AccountUnclaimed"
awsCredsUserName        = "aws_user_name"
awsCredsAccessKeyId     = "aws_access_key_id"
awsCredsSecretAccessKey = "aws_secret_access_key"
```

### Spec

Updates the accountClaim CR

```
spec:
  accountLink: osd-{accountName} 
  aws:
    regions:
	- name: us-east-1
  awsCredentialSecret:
	 name: aws
     namespace: {NameSpace}
   legalEntity:
	 id: 00000000000000
	 name:{Legal Entity Name}
```

*awsCredentialSecret* holds the name and namespace of the credentials created for the accountClaim

### Status

Updates the accountClaim CR

```
conditions:
  - lastProbeTime: 2019-07-16T13:52:02Z
    lastTransitionTime: 2019-07-16T13:52:02Z
	message: Attempting to claim account
	reason: AccountClaimed
	status: "True"
	type: Unclaimed
  - lastProbeTime: 2019-07-16T13:52:03Z
    lastTransitionTime: 2019-07-16T13:52:03Z
	message: Account claimed by osd-creds-mgmt-fhq2d2
	reason: AccountClaimed
	status: "True"
	type: Claimed
  state: Ready
```

*state* can be any of the ClaimStatus strings defined in accountclaim_types.go
*conditions* indicates the last state the account had and supporting details

### Metrics

Updated in the accountClaim-controller

```
MetricTotalAccountClaimCRs
```

# Special Items in main.go

* Initailzes a watcher that will look at all the secrets in the accountCRNamespace and if it exceeds the const `secretWatcherScanInterval` it will set the corresponsing accountCR `status.rotateCredentials = true`
* Starts a metric server with custom metrics defined in `localmetrics` pkg

### Constants

```
metricsPort               = "8080"
metricsPath               = "/metrics"
secretWatcherScanInterval = time.Duration(10) * time.Minute
```

*metricsPort* is the port used to start the metrics port
*metricsPath* it the path used as the metrics endpoint
*secretWatcherScanInterval* the interval used for the secrets watcher