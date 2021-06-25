# AWS Account Operator

[![codecov](https://codecov.io/gh/openshift/aws-account-operator/branch/master/graph/badge.svg)](https://codecov.io/gh/openshift/aws-account-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/openshift/aws-account-operator)](https://goreportcard.com/report/github.com/openshift/aws-account-operator)
[![GoDoc](https://godoc.org/github.com/openshift/aws-account-operator?status.svg)](https://pkg.go.dev/mod/github.com/openshift/aws-account-operator)
[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

## General Overview

The `aws-account-operator` is responsible for creating and maintaining a pool of [AWS](https://aws.amazon.com/) accounts and assigning accounts to AccountClaims.
The operator creates the account in AWS, does the initial setup and configuration of those accounts,
creates IAM resources and exposes credentials for an IAM user with enough permissions to provision an OpenShift 4.x cluster.

The operator is deployed to an OpenShift cluster in the `aws-account-operator` namespace.

## Quick Start

This Quick Start section assumes that you are working on a team that already has AWS Accounts set up for development or testing.
For first time setup, see the [Installation](docs/1.0-Installation.md) documentation page.

First, set up your required environment variables:

```bash
export AWS_PAGER= # This is set so that it doesn't page out to less and block integration testing
export FORCE_DEV_MODE=local # This flags the operator for local development for some code paths
export OSD_STAGING_1_AWS_ACCOUNT_ID= # Your assigned osd-staging-1 account ID
export OSD_STAGING_2_AWS_ACCOUNT_ID= # Your assigned osd-staging-2 account ID
export OSD_STAGING_1_OU_ROOT_ID= # Your assigned osd-staging-1 OU Root ID
export OSD_STAGING_1_OU_BASE_ID= # Your assigned osd-staging-1 OU Base ID
export STS_ROLE_ARN= # A role you create in your osd-staging-{1,2} account with minimal STS permissions
export STS_JUMP_ARN= # A role you create to simulate the role that we use as a bastion.
```

**Tip:** You can use [direnv](https://direnv.net) and add the above block (with variables filled in) into a `.envrc` file (make sure `.envrc` is in your global git ignore as well). Upon entry to the `aws-account-operator` folder, the env vars inside the file will be loaded automatically, and unset when you leave the folder.

Next, get your AWS Credentials for the payer account you will be using and export the access key and secret using the following environment variables:

```txt
OPERATOR_ACCESS_KEY_ID
OPERATOR_SECRET_ACCESS_KEY
```

These environment variables are needed to be set the first time you deploy the operator locally.
Then, run `make predeploy` to set up the required CRs.

Then, you should be able to run `operator-sdk run --local --namespace aws-account-operator` or `make deploy-local`, and you're up and running.

## Testing

To test that everything's working correctly, we have a set of "acceptance" tests that we've compiled into a single make target:

```shell
make test-all
```

If everything is set up correctly, this should verify that.

## Boilerplate
This repository subscribes to the [openshift/golang-osd-operator](https://github.com/openshift/boilerplate/tree/master/boilerplate/openshift/golang-osd-operator) convention of [boilerplate](https://github.com/openshift/boilerplate/).
See the [README](boilerplate/openshift/golang-osd-operator/README.md) for details about the functionality that brings in.

## Further Reading

To dive deeper into the documentation, visit our [`docs`](docs) folder.
