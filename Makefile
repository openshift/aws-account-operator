SHELL := /usr/bin/env bash

OPERATOR_DOCKERFILE = ./build/Dockerfile
REUSE_UUID := $(shell uuidgen | awk -F- '{ print tolower($$2) }')
REUSE_BUCKET_NAME=test-reuse-bucket-${REUSE_UUID}

include hack/scripts/test_envs

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

.PHONY: .check-ou-mapping-configmap-env
check-ou-mapping-configmap-env:
ifndef OSD_STAGING_1_OU_ROOT_ID
	$(error OSD_STAGING_1_OU_ROOT_ID is undefined)
endif
ifndef OSD_STAGING_1_OU_BASE_ID
	$(error OSD_STAGING_1_OU_BASE_ID is undefined)
endif

.PHONY: docker-build
docker-build: build

# Create account
.PHONY: create-account
create-account: check-aws-account-id-env
	# Create Account
	test/integration/api/create_account.sh

# Delete account
.PHONY: delete-account
delete-account:
	# Create Account
	test/integration/api/delete_account.sh
	# Delete Secrets
	test/integration/api/delete_account_secrets.sh

# Test Account creation
.PHONY: test-account-creation
test-account-creation: create-account delete-account

# Create account claim namespace
.PHONY: create-account-claim-namespace
create-accountclaim-namespace:
	# Create reuse namespace
	@oc process -p NAME=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/namespace.tmpl | oc apply -f -

# Delete account claim namespace
.PHONY: delete-account-claim-namespace
delete-accountclaim-namespace:
	# Delete reuse namespace
	@oc process -p NAME=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/namespace.tmpl | oc delete -f -

.PHONY: create-accountclaim
create-accountclaim: create-ou-map
	# Create Account
	test/integration/api/create_account.sh
	# Create accountclaim
	@oc process -p NAME=${ACCOUNT_CLAIM_NAME} -p NAMESPACE=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/aws_v1alpha1_accountclaim_cr.tmpl | oc apply -f -
	# Wait for accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${ACCOUNT_CLAIM_NAME} -n ${ACCOUNT_CLAIM_NAMESPACE} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${ACCOUNT_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-accountclaim
delete-accountclaim: delete-ou-map
	# Delete accountclaim
	@oc process -p NAME=${ACCOUNT_CLAIM_NAME} -p NAMESPACE=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/aws_v1alpha1_accountclaim_cr.tmpl | oc delete -f -

# Create awsfederatedrole "Read Only"
.PHONY: create-awsfederatedrole
create-awsfederatedrole:
	# Create Account
	test/integration/api/create_account.sh
	# Create Federated role
	@oc apply -f deploy/crds/aws_v1alpha1_awsfederatedrole_readonly_cr.yaml
	# Wait for awsFederatedAccountAccess CR to become ready
	@while true; do STATUS=$$(oc get awsfederatedrole -n ${NAMESPACE} ${AWS_FEDERATED_ROLE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Valid" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "awsFederatedRole CR ${AWS_FEDERATED_ROLE_NAME} failed to create"; exit 1; fi; sleep 1; done

# Delete awsfederatedrole "Read Only"
.PHONY: delete-awsfederatedrole
delete-awsfederatedrole:
	# Delete Federated role
	@oc delete -f deploy/crds/aws_v1alpha1_awsfederatedrole_readonly_cr.yaml
	# Delete Account
	test/integration/api/delete_account.sh
	# Delete Secrets
	test/integration/api/delete_account_secrets.sh

# Create awsFederatedAccountAccess
# This uses a AWS Account ID from your environment
.PHONY: create-awsfederatedaccountaccess
create-awsfederatedaccountaccess: check-aws-account-id-env
	# Create awsFederatedAccountAccess CR
	oc process -p AWS_IAM_ARN=${AWS_IAM_ARN} -p IAM_USER_SECRET=${IAM_USER_SECRET} -p AWS_FEDERATED_ROLE_NAME=${AWS_FEDERATED_ROLE_NAME} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws_v1alpha1_awsfederatedaccountaccess_cr.tmpl | oc apply -f -
	# Wait for awsFederatedAccountAccess CR to become ready
	@while true; do STATUS=$$(oc get awsfederatedaccountaccess -n ${NAMESPACE} ${FED_USER} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "awsFederatedAccountAccess CR ${FED_USER} failed to create"; exit 1; fi; sleep 1; done
	# Print out AWS Console URL
	@echo $$(oc get awsfederatedaccountaccess -n ${NAMESPACE} ${FED_USER} -o json | jq -r '.status.consoleURL')
	# Wait ${SLEEP_INTERVAL} seconds for AWS to register role
	@sleep ${SLEEP_INTERVAL}

.PHONY: test-switch-role
test-switch-role:
	# Retrieve role UID
	$(eval UID=$(shell oc get awsfederatedaccountaccesses.aws.managed.openshift.io -n ${NAMESPACE} ${FED_USER} -o=json |jq -r .metadata.labels.uid))
	# Test Assume role
	aws sts assume-role --role-arn arn:aws:iam::${OSD_STAGING_1_AWS_ACCOUNT_ID}:role/read-only-$(UID) --role-session-name RedHatTest --profile osd-staging-2

# Delete awsFederatedAccountAccess
# This uses a AWS Account ID from your environment
.PHONY: delete-awsfederatedaccountaccess
delete-awsfederatedaccountaccess: check-aws-account-id-env
# Delete federatedaccountaccess with secret
	@oc process -p AWS_IAM_ARN=${AWS_IAM_ARN} -p IAM_USER_SECRET=${IAM_USER_SECRET} -p AWS_FEDERATED_ROLE_NAME=${AWS_FEDERATED_ROLE_NAME} -p NAMESPACE=${NAMESPACE} -f hack/templates/aws_v1alpha1_awsfederatedaccountaccess_cr.tmpl | oc delete -f -

.PHONY: test-awsfederatedaccountaccess
test-awsfederatedaccountaccess: check-aws-account-id-env create-awsfederatedrole create-awsfederatedaccountaccess test-switch-role delete-awsfederatedaccountaccess delete-awsfederatedrole

# Create CCS (BYOC) namespace
.PHONY: create-ccs-namespace
create-ccs-namespace:
	# Create CCS namespace
	@oc process -p NAME=${CCS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc apply -f -

# Create CCS (BYOC) namespace
.PHONY: create-ccs-2-namespace
create-ccs-2-namespace:
	# Create CCS namespace
	@oc process -p NAME=${CCS_NAMESPACE_NAME_2} -f hack/templates/namespace.tmpl | oc apply -f -

# Delete CCS (BYOC) namespace
.PHONY: delete-ccs-namespace
delete-ccs-namespace:
	# Delete CCS namespace
	@oc process -p NAME=${CCS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete -f -

# Delete CCS (BYOC) namespace
.PHONY: delete-ccs-2-namespace
delete-ccs-2-namespace:
	# Delete CCS namespace
	@oc process -p NAME=${CCS_NAMESPACE_NAME_2} -f hack/templates/namespace.tmpl | oc delete -f -

# Create CCS (BYOC) Secret
.PHONY: create-ccs-secret
create-ccs-secret:
	# Create CCS Secret
	./hack/scripts/aws/rotate_iam_access_keys.sh -p osd-staging-2 -u osdCcsAdmin -a ${OSD_STAGING_2_AWS_ACCOUNT_ID} -n ${CCS_NAMESPACE_NAME} -o /dev/stdout | oc apply -f -
	# Wait for AWS to propogate IAM credentials
	sleep ${SLEEP_INTERVAL}

# Create CCS (BYOC) Secret
.PHONY: create-ccs-2-secret
create-ccs-2-secret:
	# Create CCS Secret
	./hack/scripts/aws/rotate_iam_access_keys.sh -p osd-staging-2 -u osdCcsAdmin -a ${OSD_STAGING_2_AWS_ACCOUNT_ID} -n ${CCS_NAMESPACE_NAME_2} -o /dev/stdout | oc apply -f -
	# Wait for AWS to propogate IAM credentials
	sleep ${SLEEP_INTERVAL}

# Delete CCS (BYOC) Secret
.PHONY: delete-ccs-secret
delete-ccs-secret:
	# Delete CCS Secret
	@oc delete secret byoc -n ${CCS_NAMESPACE_NAME}

# Delete CCS (BYOC) Secret
.PHONY: delete-ccs-2-secret
delete-ccs-2-secret:
	# Delete CCS Secret
	@oc delete secret byoc -n ${CCS_NAMESPACE_NAME_2}

.PHONY: create-ccs-accountclaim
create-ccs-accountclaim:
	# Create ccs accountclaim
	@oc process -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME} -f hack/templates/aws_v1alpha1_ccs_accountclaim_cr.tmpl | oc apply -f -
	# Wait for accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${CCS_CLAIM_NAME} -n ${CCS_NAMESPACE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${CCS_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: create-ccs-2-accountclaim
create-ccs-2-accountclaim:
	# Create ccs accountclaim
	@oc process -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME_2} -f hack/templates/aws_v1alpha1_ccs_accountclaim_cr.tmpl | oc apply -f -
	# Wait for accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${CCS_CLAIM_NAME} -n ${CCS_NAMESPACE_NAME_2} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${CCS_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-ccs-accountclaim
delete-ccs-accountclaim:
	# Delete ccs accountclaim
	@oc process -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME} -f hack/templates/aws_v1alpha1_ccs_accountclaim_cr.tmpl | oc delete -f -

.PHONY: delete-ccs-2-accountclaim
delete-ccs-2-accountclaim:
	# Delete ccs accountclaim
	@oc process -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME_2} -f hack/templates/aws_v1alpha1_ccs_accountclaim_cr.tmpl | oc delete -f -

# Test CCS
.PHONY: test-ccs
test-ccs: create-ccs-namespace create-ccs-secret create-ccs-accountclaim delete-ccs-accountclaim delete-ccs-secret delete-ccs-namespace

# Create S3 bucket
.PHONY: create-s3-bucket
create-s3-bucket:
	# Get credentials
	@export AWS_ACCESS_KEY_ID=$(shell oc get secret ${OSD_MANAGED_ADMIN_SECRET} -n ${NAMESPACE} -o json | jq -r '.data.aws_access_key_id' | base64 -d); \
	export AWS_SECRET_ACCESS_KEY=$(shell oc get secret ${OSD_MANAGED_ADMIN_SECRET} -n ${NAMESPACE} -o json | jq -r '.data.aws_secret_access_key' | base64 -d); \
	aws s3api create-bucket --bucket ${REUSE_BUCKET_NAME} --region=us-east-1

# List S3 bucket
.PHONY: list-s3-bucket
list-s3-bucket:
	# Get credentials
	BUCKETS=$(shell export AWS_ACCESS_KEY_ID=$(shell oc get secret ${OSD_MANAGED_ADMIN_SECRET} -n ${NAMESPACE} -o json | jq -r '.data.aws_access_key_id' | base64 -d); export AWS_SECRET_ACCESS_KEY=$(shell oc get secret ${OSD_MANAGED_ADMIN_SECRET} -n ${NAMESPACE} -o json | jq -r '.data.aws_secret_access_key' | base64 -d); aws s3api list-buckets | jq '[.Buckets[] | .Name] | length'); \
	if [ $$BUCKETS == 0 ]; then echo "Reuse successfully complete"; else echo "Reuse failed"; exit 1; fi

# Test reuse
.PHONY: test-reuse
test-reuse: check-aws-account-id-env create-accountclaim-namespace create-accountclaim create-s3-bucket delete-accountclaim delete-accountclaim-namespace list-s3-bucket
	# Delete reuse account
	test/integration/api/delete_account.sh
	# Delete reuse account secrets
	test/integration/api/delete_account_secrets.sh

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

# Test apply ou map cr
.PHONY: create-ou-map
create-ou-map:
	# Create OU map
	@oc process -p ROOT=${OSD_STAGING_1_OU_ROOT_ID} -p BASE=${OSD_STAGING_1_OU_BASE_ID} -f hack/templates/aws_v1alpha1_configmap.tmpl | oc apply -f -

# Test delete ou map cr
.PHONY: delete-ou-map
delete-ou-map:
# Delete OU map
	@oc process -p ROOT=${OSD_STAGING_1_OU_ROOT_ID} -p BASE=${OSD_STAGING_1_OU_BASE_ID} -f hack/templates/aws_v1alpha1_configmap.tmpl | oc delete -f -

# Test aws ou logic
.PHONY: test-aws-ou-logic
test-aws-ou-logic: check-ou-mapping-configmap-env check-ou-mapping-configmap-env create-account-claim-namespace create-accountclaim
	# Check that account was moved correctly
	@sleep 2; TYPE=$$(aws organizations list-parents --child-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --profile osd-staging-1 | jq -r ".Parents[0].Type"); if [ "$$TYPE" == "ORGANIZATIONAL_UNIT" ]; then echo "Account move successfully"; exit 0; elif [ "$$TYPE" == "ROOT" ]; then echo "Failed to move account out of root"; exit 1; fi;
	@aws organizations list-parents --child-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --profile osd-staging-1
	# Move account back into Root and delete test OU
	@ROOT_ID=$$(aws organizations list-roots --profile osd-staging-1 | jq -r ".Roots[0].Id"); OU=$$(aws organizations list-parents --child-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --profile osd-staging-1 | jq -r ".Parents[0].Id"); aws organizations move-account --account-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --source-parent-id "$$OU" --destination-parent-id "$$ROOT_ID" --profile osd-staging-1; aws organizations delete-organizational-unit --organizational-unit-id "$$OU" --profile osd-staging-1;
	@echo "Successfully moved account back and deleted the test OU"

#s Test all
.PHONY: test-all
test-all: test-account-creation test-ccs test-reuse test-awsfederatedaccountaccess test-aws-ou-logic
