SHELL := /usr/bin/env bash

OPERATOR_DOCKERFILE = ./build/Dockerfile

ACCOUNT_CR_NAME = osd-creds-mgmt-test
AWS_FEDERATED_ROLE_NAME = read-only
IAM_USER_SECRET = ${ACCOUNT_CR_NAME}-secret
NAMESPACE = aws-account-operator
SLEEP_INTERVAL = 10
export AWS_IAM_ARN := $(shell aws sts get-caller-identity --profile=osd-staging-2 | jq -r '.Arn')

# Include shared Makefiles
include project.mk
include standard.mk

default: gobuild

# Extend Makefile after here

.PHONY: .check-aws-account-id-env
check-aws-account-id-env:
ifndef OSD_STAGING_1_AWS_ACCOUNT_ID
	$(error OSD_STAGING_1_AWS_ACCOUNT_ID is undefined)
endif
ifndef OSD_STAGING_2_AWS_ACCOUNT_ID
	$(error OSD_STAGING_2_AWS_ACCOUNT_ID is undefined)
endif
ifndef AWS_IAM_ARN
	$(error AWS_IAM_ARN is undefined)
endif

.PHONY: docker-build
docker-build: build

# Delete secrets created by account
.PHONY: delete-account-secrets
delete-account-secrets:
	@for secret in $$(oc get secrets -n aws-account-operator | grep "osd-creds-mgmt-test" | awk '{print $$1}'); do oc delete secret $$secret -n aws-account-operator; done

# Create AWS account
.PHONY: create-account
create-account:
	# Create Account CR
	@oc process -p AWS_ACCOUNT_ID=${OSD_STAGING_1_AWS_ACCOUNT_ID} -p ACCOUNT_CR_NAME=${ACCOUNT_CR_NAME} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws_v1alpha1_account.tmpl | oc apply -f -
	# Wait for account to become ready
	@while true; do STATUS=$$(oc get account ${ACCOUNT_CR_NAME} -n aws-account-operator -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account ${ACCOUNT_CR_NAME} failed to create"; exit 1; fi; sleep 1; done

# Delete AWS account
.PHONY: delete-account
delete-account: delete-account-secrets
	# Delete Account CR
	@oc process -p AWS_ACCOUNT_ID=${OSD_STAGING_1_AWS_ACCOUNT_ID} -p ACCOUNT_CR_NAME=${ACCOUNT_CR_NAME} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws_v1alpha1_account.tmpl | oc delete -f -

# Create awsfederatedrole "Read Only"
.PHONY: create-awsfederatedrole
create-awsfederatedrole:
	@oc apply -f deploy/crds/aws_v1alpha1_awsfederatedrole_readonly_cr.yaml
	# Wait for awsFederatedAccountAccess CR to become ready
	@while true; do STATUS=$$(oc get awsfederatedrole -n aws-account-operator ${AWS_FEDERATED_ROLE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Valid" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "awsFederatedRole CR ${AWS_FEDERATED_ROLE_NAME} failed to create"; exit 1; fi; sleep 1; done

# Delete awsfederatedrole "Read Only"
.PHONY: delete-awsfederatedrole
delete-awsfederatedrole:
	@oc delete -f deploy/crds/aws_v1alpha1_awsfederatedrole_readonly_cr.yaml

# Create awsFederatedAccountAccess
# This uses a AWS Account ID from your environment
.PHONY: create-awsfederatedaccountaccess
create-awsfederatedaccountaccess: check-aws-account-id-env
	# Create awsFederatedAccountAccess CR
	@oc process -p AWS_IAM_ARN=${AWS_IAM_ARN} -p IAM_USER_SECRET=${IAM_USER_SECRET} -p AWS_FEDERATED_ROLE_NAME=${AWS_FEDERATED_ROLE_NAME} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws_v1alpha1_awsfederatedaccountaccess_cr.tmpl | oc apply -f -
	# Wait for awsFederatedAccountAccess CR to become ready
	@while true; do STATUS=$$(oc get awsfederatedaccountaccess -n aws-account-operator test-federated-user -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "awsFederatedAccountAccess CR test-federated-user failed to create"; exit 1; fi; sleep 1; done
	# Print out AWS Console URL
	@echo $$(oc get awsfederatedaccountaccess -n aws-account-operator test-federated-user -o json | jq -r '.status.consoleURL')
	# Wait ${SLEEP_INTERVAL} seconds for AWS to register role
	@sleep ${SLEEP_INTERVAL}

.PHONY: test-switch-role
test-switch-role:
	# Retrieve role UID
	$(eval UID=$(shell oc get awsfederatedaccountaccesses.aws.managed.openshift.io -n aws-account-operator test-federated-user -o=json |jq -r .metadata.labels.uid))
	# Test Assume role
	aws sts assume-role --role-arn arn:aws:iam::${OSD_STAGING_1_AWS_ACCOUNT_ID}:role/read-only-$(UID) --role-session-name RedHatTest --profile osd-staging-2

# Delete awsFederatedAccountAccess
# This uses a AWS Account ID from your environment
.PHONY: delete-awsfederatedaccountaccess
delete-awsfederatedaccountaccess: check-aws-account-id-env
	# Delete federatedaccountaccess with secret
	@oc process -p AWS_IAM_ARN=${AWS_IAM_ARN} -p IAM_USER_SECRET=${IAM_USER_SECRET} -p AWS_FEDERATED_ROLE_NAME=${AWS_FEDERATED_ROLE_NAME} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws_v1alpha1_awsfederatedaccountaccess_cr.tmpl | oc delete -f -

.PHONY: test-awsfederatedaccountaccess
test-awsfederatedaccountaccess: check-aws-account-id-env create-account create-awsfederatedrole create-awsfederatedaccountaccess test-switch-role delete-awsfederatedaccountaccess delete-awsfederatedrole delete-account
