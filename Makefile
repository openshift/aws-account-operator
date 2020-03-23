SHELL := /usr/bin/env bash

OPERATOR_DOCKERFILE = ./build/Dockerfile

OSD_STAGING_1_ACCOUNT_CR_NAME_OSD = osd-creds-mgmt-osd-staging-1
AWS_FEDERATED_ROLE_NAME = read-only
IAM_USER_SECRET = ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}-secret
NAMESPACE = aws-account-operator
SLEEP_INTERVAL = 10
ACCOUNT_CLAIM_NAME = test-claim
ACCOUNT_CLAIM_NAMESPACE = test-claim-namespace
REUSE_UUID := $(shell uuidgen | awk -F- '{ print tolower($$2) }')
REUSE_BUCKET_NAME = test-reuse-bucket-${REUSE_UUID}
CCS_CLAIM_NAME = test-ccs
CCS_NAMESPACE_NAME = test-ccs-namespace
export AWS_IAM_ARN := $(shell aws sts get-caller-identity --profile=osd-staging-2 | jq -r '.Arn')
OSD_STAGING_2_ACCOUNT_CR_NAME_OSD = osd-creds-mgmt-osd-staging-2

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
	@for secret in $$(oc get secrets -n aws-account-operator | grep "${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD}" | awk '{print $$1}'); do oc delete secret $$secret -n aws-account-operator; done

# Create AWS account
.PHONY: create-account
create-account:
	# Create Account CR
	@oc process -p AWS_ACCOUNT_ID=${OSD_STAGING_1_AWS_ACCOUNT_ID} -p ACCOUNT_CR_NAME=${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws_v1alpha1_account.tmpl | oc apply -f -
	# Wait for account to become ready
	@while true; do STATUS=$$(oc get account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -n aws-account-operator -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account ${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} failed to create"; exit 1; fi; sleep 1; done

# Delete AWS account
.PHONY: delete-account
delete-account: delete-account-secrets
	# Delete Account CR
	@oc process -p AWS_ACCOUNT_ID=${OSD_STAGING_1_AWS_ACCOUNT_ID} -p ACCOUNT_CR_NAME=${OSD_STAGING_1_ACCOUNT_CR_NAME_OSD} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws_v1alpha1_account.tmpl | oc delete -f -

# Create account claim namespace
.PHONY: create-account-claim-namespace
create-accountclaim-namespace:
	# Create namespace
	@oc process -p NAME=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/namespace.tmpl | oc apply -f -

# Delete account claim namespace
.PHONY: delete-account-claim-namespace
delete-accountclaim-namespace:
	# Delete namespace
	@oc process -p NAME=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/namespace.tmpl | oc delete -f -

.PHONY: create-accountclaim
create-accountclaim:
	# Create accountclaim
	@oc process -p NAME=${ACCOUNT_CLAIM_NAME} -p NAMESPACE=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/aws_v1alpha1_accountclaim_cr.tmpl | oc apply -f -
	# Wait for accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${ACCOUNT_CLAIM_NAME} -n ${ACCOUNT_CLAIM_NAMESPACE} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${ACCOUNT_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-accountclaim
delete-accountclaim:
	# Delete accountclaim
	@oc process -p NAME=${ACCOUNT_CLAIM_NAME} -p NAMESPACE=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/aws_v1alpha1_accountclaim_cr.tmpl | oc delete -f -

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

# Create CCS (BYOC) namespace
.PHONY: create-ccs-namespace
create-ccs-namespace:
	# Create namespace
	@oc process -p NAME=${CCS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc apply -f -

# Delete CCS (BYOC) namespace
.PHONY: delete-ccs-namespace
delete-ccs-namespace:
	# Create namespace
	@oc process -p NAME=${CCS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete -f -

# Create CCS (BYOC) Secret
.PHONY: create-ccs-secret
create-ccs-secret:
	# Create CCS Secret
	./hack/scripts/aws/rotate_iam_access_keys.sh -p osd-staging-2 -u osdCcsAdmin -a ${OSD_STAGING_2_AWS_ACCOUNT_ID} -o /dev/stdout | oc apply -f -
	# Wait for AWS to propogate IAM credentials
	sleep ${SLEEP_INTERVAL}

# Delete CCS (BYOC) Secret
.PHONY: delete-ccs-secret
delete-ccs-secret:
	# Delete CCS Secret
	@oc delete secret byoc -n ${CCS_NAMESPACE_NAME}

.PHONY: create-ccs-accountclaim
create-ccs-accountclaim:
	# Create ccs accountclaim
	@oc process -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME} -f hack/templates/aws_v1alpha1_ccs_accountclaim_cr.tmpl | oc apply -f -
	# Wait for accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${CCS_CLAIM_NAME} -n ${CCS_NAMESPACE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${CCS_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-ccs-accountclaim
delete-ccs-accountclaim:
	# Delete ccs accountclaim
	@oc process -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME} -f hack/templates/aws_v1alpha1_ccs_accountclaim_cr.tmpl | oc delete -f -

# Test CCS
.PHONY: test-ccs
test-ccs: create-ccs-namespace create-ccs-secret create-ccs-accountclaim delete-ccs-accountclaim delete-ccs-secret delete-ccs-namespace

# Create S3 bucket
.PHONY: create-s3-bucket
create-s3-bucket:
	# Get credentials
	@export AWS_ACCESS_KEY_ID=$(shell oc get secret osd-creds-mgmt-osd-staging-1-osdmanagedadminsre-secret -n aws-account-operator -o json | jq -r '.data.aws_access_key_id' | base64 -d); \
	export AWS_SECRET_ACCESS_KEY=$(shell oc get secret osd-creds-mgmt-osd-staging-1-osdmanagedadminsre-secret -n aws-account-operator -o json | jq -r '.data.aws_secret_access_key' | base64 -d); \
	aws s3api create-bucket --bucket ${REUSE_BUCKET_NAME} --region=us-east-1

# List S3 bucket
.PHONY: list-s3-bucket
list-s3-bucket:
	# Get credentials
	BUCKETS=$(shell export AWS_ACCESS_KEY_ID=$(shell oc get secret osd-creds-mgmt-osd-staging-1-osdmanagedadminsre-secret -n aws-account-operator -o json | jq -r '.data.aws_access_key_id' | base64 -d); export AWS_SECRET_ACCESS_KEY=$(shell oc get secret osd-creds-mgmt-osd-staging-1-osdmanagedadminsre-secret -n aws-account-operator -o json | jq -r '.data.aws_secret_access_key' | base64 -d); aws s3api list-buckets | jq '[.Buckets[] | .Name] | length'); \
	if [ $$BUCKETS == 0 ]; then echo "Reuse successfully complete"; else echo "Reuse failed"; exit 1; fi

# Test reuse
.PHONY: test-reuse
test-reuse: check-aws-account-id-env create-account create-accountclaim-namespace create-accountclaim create-s3-bucket delete-accountclaim delete-accountclaim-namespace list-s3-bucket delete-account

# Deploy the operator secrets, CRDs and namesapce.
.PHONY: deploy-aws-account-operator-credentials
deploy-aws-account-operator-credentials:
# Base64 Encode the AWS Credentials
	$(eval ID=$(shell echo -n ${OPERATOR_ACCESS_KEY_ID} | base64 ))
	$(eval KEY=$(shell echo -n ${OPERATOR_SECRET_ACCESS_KEY} | base64))

.PHONY: deploy-aws-account-operator
deploy-aws-account-operator:
# Create aws-account-operator namespace
	@oc get namespace ${NAMESPACE} || oc new-project ${NAMESPACE}
# Create the aws-account-operator-credentials secret
	@oc process -p OPERATOR_ACCESS_KEY_ID=${ID} -p OPERATOR_SECRET_ACCESS_KEY=${KEY} -f hack/templates/aws_v1alpha1_aws_account_operator_credentials.tmpl | oc apply -f -
# Create aws-account-operator CRDs
	@ls deploy/crds/*crd.yaml | xargs -L1 oc apply -f
# Create zero size account pool
	@oc apply -f hack/files/aws_v1alpha1_zero_size_accountpool.yaml

.PHONY: deploy
deploy: check-aws-credentials deploy-aws-account-operator-credentials deploy-aws-account-operator

.PHONY: .check-aws-credentials
check-aws-credentials:
ifndef OPERATOR_ACCESS_KEY_ID
	$(error OPERATOR_ACCESS_KEY_ID is undefined)
endif
ifndef OPERATOR_SECRET_ACCESS_KEY
	$(error OPERATOR_SECRET_ACCESS_KEY is undefined)
endif
