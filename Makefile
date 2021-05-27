SHELL := /usr/bin/env bash

OPERATOR_DOCKERFILE = ./build/Dockerfile
REUSE_UUID := $(shell uuidgen | awk -F- '{ print tolower($$2) }')
REUSE_BUCKET_NAME=test-reuse-bucket-${REUSE_UUID}

include hack/scripts/test_envs

# Boilerplate
include boilerplate/generated-includes.mk

.PHONY: boilerplate-update
boilerplate-update:
	@boilerplate/update

# Extend Makefile after here

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: check-aws-account-id-env
check-aws-account-id-env: ## Check if AWS Account Env vars are set
ifndef OSD_STAGING_1_AWS_ACCOUNT_ID
	$(error OSD_STAGING_1_AWS_ACCOUNT_ID is undefined)
endif
ifndef OSD_STAGING_2_AWS_ACCOUNT_ID
	$(error OSD_STAGING_2_AWS_ACCOUNT_ID is undefined)
endif
ifndef AWS_IAM_ARN
	$(eval export AWS_IAM_ARN := $(shell aws sts get-caller-identity --profile=osd-staging-2 | jq -r '.Arn'))
	@if [[ -z "$(AWS_IAM_ARN)" ]]; then echo "AWS_IAM_ARN unset and could not be calculated!"; exit 1; fi
endif

.PHONY: check-ou-mapping-configmap-env
check-ou-mapping-configmap-env: ## Check if OU mapping ConfigMap Env vars are set
ifndef OSD_STAGING_1_OU_ROOT_ID
	$(error OSD_STAGING_1_OU_ROOT_ID is undefined)
endif
ifndef OSD_STAGING_1_OU_BASE_ID
	$(error OSD_STAGING_1_OU_BASE_ID is undefined)
endif

.PHONY: create-account
create-account: check-aws-account-id-env ## Create account
	# Create Account
	test/integration/api/create_account.sh

.PHONY: delete-account
delete-account: ## Delete account
	# Delete Account
	test/integration/api/delete_account.sh || true
	# Delete Secrets
	test/integration/api/delete_account_secrets.sh

.PHONY: test-account-creation
test-account-creation: delete-account create-account test-secrets ## Test Account creation
	test/integration/api/delete_account.sh || true

.PHONY: create-accountclaim-namespace
create-accountclaim-namespace: ## Create account claim namespace
	# Create reuse namespace
	@oc process --local -p NAME=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/namespace.tmpl | oc apply -f -

.PHONY: delete-accountclaim-namespace
delete-accountclaim-namespace: ## Delete account claim namespace
	# Delete reuse namespace
	@oc process --local -p NAME=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/namespace.tmpl | oc delete -f -

.PHONY: create-accountclaim
create-accountclaim: ## Create AccountClaim
	$(MAKE) create-account
	# Create accountclaim
	@oc process --local -p NAME=${ACCOUNT_CLAIM_NAME} -p NAMESPACE=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/aws.managed.openshift.io_v1alpha1_accountclaim_cr.tmpl | oc apply -f -
	# Wait for accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${ACCOUNT_CLAIM_NAME} -n ${ACCOUNT_CLAIM_NAMESPACE} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${ACCOUNT_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-accountclaim
delete-accountclaim: ## Delete AccountClaim
	# Delete accountclaim
	@oc process --local -p NAME=${ACCOUNT_CLAIM_NAME} -p NAMESPACE=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/aws.managed.openshift.io_v1alpha1_accountclaim_cr.tmpl | oc delete -f -

.PHONY: create-awsfederatedrole
create-awsfederatedrole: ## Create awsFederatedRole "Read Only"
	# Create Account
	test/integration/api/create_account.sh
	# Create Federated role
	@oc apply -f test/deploy/aws.managed.openshift.io_v1alpha1_awsfederatedrole_readonly_cr.yaml
	# Wait for awsFederatedRole CR to become ready
	@while true; do STATUS=$$(oc get awsfederatedrole -n ${NAMESPACE} ${AWS_FEDERATED_ROLE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Valid" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "awsFederatedRole CR ${AWS_FEDERATED_ROLE_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-awsfederatedrole
delete-awsfederatedrole: ## Delete awsFederatedRole "Read Only"
	# Delete Federated role
	@oc delete -f test/deploy/aws.managed.openshift.io_v1alpha1_awsfederatedrole_readonly_cr.yaml
	$(MAKE) delete-account || true

.PHONY: create-awsfederatedaccountaccess
create-awsfederatedaccountaccess: check-aws-account-id-env ## Create awsFederatedAccountAccess - This uses a AWS Account ID from your environment
	# Create account access
	test/integration/create_awsfederatedaccountaccess.sh --role read-only --name test-federated-user

.PHONY: delete-awsfederatedaccountaccess
delete-awsfederatedaccountaccess: check-aws-account-id-env ## Delete awsFederatedAccountAccess - This uses a AWS Account ID from your environment
	test/integration/delete_awsfederatedaccountaccess.sh --role read-only --name test-federated-user

.PHONY: test-awsfederatedrole
test-awsfederatedrole: check-aws-account-id-env ## Test Federated Access Roles
	# Create Account if not already created
	$(MAKE) create-account
	# Create Federated Roles if not created
	@oc apply -f test/deploy/aws.managed.openshift.io_v1alpha1_awsfederatedrole_readonly_cr.yaml
	@oc apply -f test/deploy/aws.managed.openshift.io_v1alpha1_awsfederatedrole_networkmgmt_cr.yaml
	# Wait for readonly CR to become ready
	@while true; do STATUS=$$(oc get awsfederatedrole -n ${NAMESPACE} read-only -o json | jq -r '.status.state'); if [ "$$STATUS" == "Valid" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "awsFederatedRole CR read-only failed to create"; exit 1; fi; sleep 1; done
	# Wait for networkmgmt CR to become ready
	@while true; do STATUS=$$(oc get awsfederatedrole -n ${NAMESPACE} network-mgmt -o json | jq -r '.status.state'); if [ "$$STATUS" == "Valid" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "awsFederatedRole CR network-mgmt failed to create"; exit 1; fi; sleep 1; done
	# Test Federated Account Access
	test/integration/create_awsfederatedaccountaccess.sh --role read-only --name test-federated-user-readonly
	test/integration/create_awsfederatedaccountaccess.sh --role network-mgmt --name test-federated-user-network-mgmt
	TEST_CR=test-federated-user-readonly TEST_ROLE_FILE=test/deploy/aws.managed.openshift.io_v1alpha1_awsfederatedrole_readonly_cr.yaml go test github.com/openshift/aws-account-operator/test/integration
	TEST_CR=test-federated-user-network-mgmt TEST_ROLE_FILE=test/deploy/aws.managed.openshift.io_v1alpha1_awsfederatedrole_networkmgmt_cr.yaml go test github.com/openshift/aws-account-operator/test/integration
	test/integration/delete_awsfederatedaccountaccess.sh --role read-only --name test-federated-user-readonly
	test/integration/delete_awsfederatedaccountaccess.sh --role network-mgmt --name test-federated-user-network-mgmt
	# Delete network-mgmt role
	@oc delete awsfederatedrole -n aws-account-operator network-mgmt
	# Delete read-only role
	@oc delete awsfederatedrole -n aws-account-operator read-only
	$(MAKE) delete-account || true

.PHONY: test-switch-role
test-switch-role: ## Test switch role
	# Retrieve role UID
	$(eval UID=$(shell oc get awsfederatedaccountaccesses.aws.managed.openshift.io -n ${NAMESPACE} ${FED_USER} -o=json |jq -r .metadata.labels.uid))
	# Test Assume role
	aws sts assume-role --role-arn arn:aws:iam::${OSD_STAGING_1_AWS_ACCOUNT_ID}:role/read-only-$(UID) --role-session-name RedHatTest --profile osd-staging-2

.PHONY: test-awsfederatedaccountaccess
test-awsfederatedaccountaccess: check-aws-account-id-env create-awsfederatedrole create-awsfederatedaccountaccess test-switch-role delete-awsfederatedaccountaccess delete-awsfederatedrole ## Test awsFederatedAccountAccess

.PHONY: create-ccs-namespace
create-ccs-namespace: ## Create CCS (BYOC) namespace
	# Create CCS namespace
	@oc process --local -p NAME=${CCS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc apply -f -

.PHONY: create-ccs-2-namespace
create-ccs-2-namespace: ## Create CCS (BYOC) namespace
	# Create CCS namespace
	@oc process --local -p NAME=${CCS_NAMESPACE_NAME_2} -f hack/templates/namespace.tmpl | oc apply -f -

.PHONY: delete-ccs-namespace
delete-ccs-namespace: ## Delete CCS (BYOC) namespace
	# Delete CCS namespace
	@oc process --local -p NAME=${CCS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete -f -

.PHONY: delete-ccs-2-namespace
delete-ccs-2-namespace: ## Delete CCS (BYOC) namespace
	# Delete CCS namespace
	@oc process --local -p NAME=${CCS_NAMESPACE_NAME_2} -f hack/templates/namespace.tmpl | oc delete -f -

.PHONY: create-ccs-secret
create-ccs-secret: ## Create CCS (BYOC) Secret
	# Create CCS Secret
	./hack/scripts/aws/rotate_iam_access_keys.sh -p osd-staging-2 -u osdCcsAdmin -a ${OSD_STAGING_2_AWS_ACCOUNT_ID} -n ${CCS_NAMESPACE_NAME} -o /dev/stdout | oc apply -f -
	# Wait for AWS to propogate IAM credentials
	sleep ${SLEEP_INTERVAL}


.PHONY: create-ccs-2-secret
create-ccs-2-secret: ## Create CCS (BYOC) Secret
	# Create CCS Secret
	./hack/scripts/aws/rotate_iam_access_keys.sh -p osd-staging-2 -u osdCcsAdmin -a ${OSD_STAGING_2_AWS_ACCOUNT_ID} -n ${CCS_NAMESPACE_NAME_2} -o /dev/stdout | oc apply -f -
	# Wait for AWS to propogate IAM credentials
	sleep ${SLEEP_INTERVAL}

.PHONY: delete-ccs-secret
delete-ccs-secret: # Delete CCS (BYOC) Secret
	# Delete CCS Secret
	@oc delete secret byoc -n ${CCS_NAMESPACE_NAME}

.PHONY: delete-ccs-2-secret
delete-ccs-2-secret: ## Delete CCS (BYOC) Secret
	# Delete CCS Secret
	@oc delete secret byoc -n ${CCS_NAMESPACE_NAME_2}

.PHONY: create-ccs-accountclaim
create-ccs-accountclaim: ## Create CSS AccountClaim
	# Create ccs accountclaim
	@oc process --local -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME} -f hack/templates/aws.managed.openshift.io_v1alpha1_ccs_accountclaim_cr.tmpl | oc apply -f -
	# Wait for accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${CCS_CLAIM_NAME} -n ${CCS_NAMESPACE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${CCS_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: create-ccs-2-accountclaim
create-ccs-2-accountclaim: ## Create CSS AccountClaim
	# Create ccs accountclaim
	@oc process --local -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME_2} -f hack/templates/aws.managed.openshift.io_v1alpha1_ccs_accountclaim_cr.tmpl | oc apply -f -
	# Wait for accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${CCS_CLAIM_NAME} -n ${CCS_NAMESPACE_NAME_2} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${CCS_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-ccs-accountclaim
delete-ccs-accountclaim: ## Delete CCS AccountClaim
	# Delete ccs accountclaim
	@oc process --local -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME} -f hack/templates/aws.managed.openshift.io_v1alpha1_ccs_accountclaim_cr.tmpl | oc delete -f -

.PHONY: delete-ccs-2-accountclaim
delete-ccs-2-accountclaim: ## Delete CSS AccountClaim
	# Delete ccs accountclaim
	@oc process --local -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME_2} -f hack/templates/aws.managed.openshift.io_v1alpha1_ccs_accountclaim_cr.tmpl | oc delete -f -

.PHONY: test-ccs
test-ccs: create-ccs delete-ccs ## Test CCS

.PHONY: create-ccs
create-ccs: create-ccs-namespace create-ccs-secret create-ccs-accountclaim ## Deploy a test CCS account

.PHONY: delete-ccs
delete-ccs: delete-ccs-accountclaim delete-ccs-secret delete-ccs-namespace ## Teardown the test CCS account

.PHONY: create-s3-bucket
create-s3-bucket: ## Create S3 bucket
	# Get credentials
	@export AWS_ACCESS_KEY_ID=$(shell oc get secret ${OSD_MANAGED_ADMIN_SECRET} -n ${NAMESPACE} -o json | jq -r '.data.aws_access_key_id' | base64 -d); \
	export AWS_SECRET_ACCESS_KEY=$(shell oc get secret ${OSD_MANAGED_ADMIN_SECRET} -n ${NAMESPACE} -o json | jq -r '.data.aws_secret_access_key' | base64 -d); \
	aws s3api create-bucket --bucket ${REUSE_BUCKET_NAME} --region=us-east-1

.PHONY: list-s3-bucket
list-s3-bucket:  ## List S3 bucket
	# Get credentials
	BUCKETS=$(shell export AWS_ACCESS_KEY_ID=$(shell oc get secret ${OSD_MANAGED_ADMIN_SECRET} -n ${NAMESPACE} -o json | jq -r '.data.aws_access_key_id' | base64 -d); export AWS_SECRET_ACCESS_KEY=$(shell oc get secret ${OSD_MANAGED_ADMIN_SECRET} -n ${NAMESPACE} -o json | jq -r '.data.aws_secret_access_key' | base64 -d); aws s3api list-buckets | jq '[.Buckets[] | .Name] | length'); \
	if [ $$BUCKETS == 0 ]; then echo "Reuse successfully complete"; else echo "Reuse failed"; exit 1; fi

.PHONY: test-reuse
test-reuse: check-aws-account-id-env create-accountclaim-namespace create-accountclaim create-s3-bucket delete-accountclaim delete-accountclaim-namespace list-s3-bucket
	$(MAKE) delete-account ## Test reuse

.PHONY: test-secrets
test-secrets: ## Test secrets are what we expect them to be
	# Test Secrets
	test/integration/test_secrets.sh

.PHONY: deploy-aws-account-operator-credentials
deploy-aws-account-operator-credentials:  ## Deploy the operator secrets, CRDs and namespace.
	hack/scripts/set_operator_credentials.sh osd-staging-1

.PHONY: predeploy-aws-account-operator
predeploy-aws-account-operator: ## Predeploy AWS Account Operator
	# Create aws-account-operator namespace
	@oc get namespace ${NAMESPACE} && oc project ${NAMESPACE} || oc create namespace ${NAMESPACE}
	# Create aws-account-operator CRDs
	@ls deploy/crds/*.yaml | xargs -L1 oc apply -f
	# Create zero size account pool
	@oc apply -f hack/files/aws.managed.openshift.io_v1alpha1_zero_size_accountpool.yaml

.PHONY: predeploy
predeploy: predeploy-aws-account-operator deploy-aws-account-operator-credentials create-ou-map ## Predeploy Operator

.PHONY: deploy-local
deploy-local: ## Deploy Operator locally
	@FORCE_DEV_MODE=local operator-sdk run --local --namespace=$(OPERATOR_NAMESPACE)

.PHONY: deploy-local-debug
deploy-local-debug: ## Deploy Operator locally with Delve enabled
	@FORCE_DEV_MODE=local operator-sdk run --local --namespace=$(OPERATOR_NAMESPACE) --enable-delve

.PHONY: deploy-cluster
deploy-cluster: FORCE_DEV_MODE?=cluster
deploy-cluster: isclean ## Deploy to cluster
	# Deploy things like service account, roles, etc.
# TODO(efried): Filtering out operator.yaml here is icky, but necessary so we can do the substitutions.
#               Revisit when templating mentioned below is done.
	@ls deploy/*.yaml | grep -v operator.yaml | xargs -L1 oc apply -f
	# Deploy the operator resource, using our dev image and the appropriate (or requested) dev mode
# TODO(efried): template this, but without having to maintain an almost-copy of operator.yaml
	@hack/scripts/edit_operator_yaml_for_dev.py $(OPERATOR_IMAGE_URI) "$(FORCE_DEV_MODE)" | oc apply -f -

.PHONY: check-aws-credentials
check-aws-credentials: ## Check AWS Credentials
ifndef OPERATOR_ACCESS_KEY_ID
	$(error OPERATOR_ACCESS_KEY_ID is undefined)
endif
ifndef OPERATOR_SECRET_ACCESS_KEY
	$(error OPERATOR_SECRET_ACCESS_KEY is undefined)
endif

.PHONY: create-ou-map
create-ou-map: check-ou-mapping-configmap-env ## Test apply OU map CR
	# Create OU map
	@hack/scripts/set_operator_configmap.sh -a 0 -v 1 -r "${OSD_STAGING_1_OU_ROOT_ID}" -o "${OSD_STAGING_1_OU_BASE_ID}" -s "${STS_JUMP_ARN}"

.PHONY: delete-ou-map
delete-ou-map: ## Test delete OU map CR
	# Delete OU map
	@oc process --local -p ROOT=${OSD_STAGING_1_OU_ROOT_ID} -p BASE=${OSD_STAGING_1_OU_BASE_ID} -p OPERATOR_NAMESPACE=aws-account-operator -f hack/templates/aws.managed.openshift.io_v1alpha1_configmap.tmpl | oc delete -f -

.PHONY: test-aws-ou-logic
test-aws-ou-logic: check-ou-mapping-configmap-env create-accountclaim-namespace create-accountclaim ## Test AWS OU logic
	# Check that account was moved correctly
	@sleep 2; TYPE=$$(aws organizations list-parents --child-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --profile osd-staging-1 | jq -r ".Parents[0].Type"); if [ "$$TYPE" == "ORGANIZATIONAL_UNIT" ]; then echo "Account move successfully"; exit 0; elif [ "$$TYPE" == "ROOT" ]; then echo "Failed to move account out of root"; exit 1; fi;
	@aws organizations list-parents --child-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --profile osd-staging-1
	# Move account back into Root and delete test OU
	@ROOT_ID=$$(aws organizations list-roots --profile osd-staging-1 | jq -r ".Roots[0].Id"); OU=$$(aws organizations list-parents --child-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --profile osd-staging-1 | jq -r ".Parents[0].Id"); aws organizations move-account --account-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --source-parent-id "$$OU" --destination-parent-id "$$ROOT_ID" --profile osd-staging-1; aws organizations delete-organizational-unit --organizational-unit-id "$$OU" --profile osd-staging-1;
	@echo "Successfully moved account back and deleted the test OU"

# Create STS account claim namespace
.PHONY: create-sts-accountclaim-namespace
create-sts-accountclaim-namespace: ## Creates namespace for STS accountclaim
	# Create reuse namespace
	@oc process --local -p NAME=${STS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc apply -f -

# Delete STS account claim namespace
.PHONY: delete-sts-accountclaim-namespace
delete-sts-accountclaim-namespace: ## Deletes namespace for STS accountclaim
	# Delete reuse namespace
	@oc process --local -p NAME=${STS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete -f -

.PHONY: create-sts-accountclaim
create-sts-accountclaim: ## Creates a templated STS accountclaim
	# Create STS accountclaim
	@oc process --local -p NAME=${STS_CLAIM_NAME} -p NAMESPACE=${STS_NAMESPACE_NAME} -p STS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p STS_ROLE_ARN=${STS_ROLE_ARN} -f hack/templates/aws.managed.openshift.io_v1alpha1_sts_accountclaim_cr.tmpl | oc apply -f -
	# Wait for sts accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${STS_CLAIM_NAME} -n ${STS_NAMESPACE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${STS_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-sts-accountclaim
delete-sts-accountclaim: ## Deletes a templated STS accountclaim
	# Delete sts accountclaim
	@oc process --local -p NAME=${STS_CLAIM_NAME} -p NAMESPACE=${STS_NAMESPACE_NAME} -p STS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p STS_ROLE_ARN=${STS_ROLE_ARN} -f hack/templates/aws.managed.openshift.io_v1alpha1_sts_accountclaim_cr.tmpl | oc delete -f -

.PHONY: test-sts-accountclaim
test-sts-accountclaim: create-sts-accountclaim-namespace create-sts-accountclaim delete-sts-accountclaim delete-sts-accountclaim-namespace ## Runs a full integration test for STS workflow

#s Test all
.PHONY: test-all
test-all: lint clean-operator test test-account-creation test-ccs test-reuse test-awsfederatedaccountaccess test-awsfederatedrole test-aws-ou-logic test-sts-accountclaim ## Runs all integration tests

.PHONY: clean-operator
clean-operator: ## Clean Operator
	$(MAKE) delete-accountclaim-namespace || true
	$(MAKE) delete-ccs-namespace || true
	$(MAKE) delete-ccs-2-namespace || true
	oc delete accounts --all -n ${NAMESPACE}
	oc delete awsfederatedaccountaccess --all -n ${NAMESPACE}
	oc delete awsfederatedrole --all -n ${NAMESPACE}
