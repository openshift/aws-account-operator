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

## Documentation
For information on the inner-workings, installation, development and testing of the operator, please refer to our [Documentation](./docs/README.md).

## Boilerplate
This repository subscribes to the [openshift/golang-osd-operator](https://github.com/openshift/boilerplate/tree/master/boilerplate/openshift/golang-osd-operator) convention of [boilerplate](https://github.com/openshift/boilerplate/).
See the [README](boilerplate/openshift/golang-osd-operator/README.md) for details about the functionality that brings in.

