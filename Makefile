FIPS_ENABLED=true
SHELL := /usr/bin/env bash

OPERATOR_DOCKERFILE = ./build/Dockerfile
OPERATOR_SDK ?= operator-sdk

# GOLANGCI_LINT_CACHE needs to be set to a directory which is writeable
# Relevant issue - https://github.com/golangci/golangci-lint/issues/734
GOLANGCI_LINT_CACHE ?= /tmp/golangci-cache

include test/integration/int-testing.mk

# Boilerplate
include boilerplate/generated-includes.mk

.PHONY: boilerplate-update
boilerplate-update:
	@boilerplate/update

# Extend Makefile after here

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: test-apis
test-apis:
	@pushd api; \
	go test ./... ; \
	popd

# Spell Check
.PHONY: check-spell
check-spell: # Check spelling
	./hack/scripts/misspell_check.sh
	GOLANGCI_LINT_CACHE=${GOLANGCI_LINT_CACHE} golangci-lint run ./...

# This *adds* `check-spell` ./hack/scripts/misspell_check.sh the existing `lint` provided by boilerplate
lint: check-spell

.PHONY: test-all
test-all: lint clean-operator test test-apis test-integration ## Runs all tests

#############################################################################################
# Sanity Checks
#############################################################################################

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

.PHONY: check-sts-setup
check-sts-setup: ## Checks if STS roles are set up correctly
ifndef STS_JUMP_ROLE
	$(error STS_JUMP_ROLE is undefined. STS_JUMP_ROLE is the ARN of the role we use as a bastion to access the installation role)
endif
ifndef STS_ROLE_ARN
	$(error STS_ROLE_ARN is undefined. STS_ROLE_ARN is the ARN of the installation role)
endif
	hack/scripts/aws/sts-infra-precheck.sh

.PHONY: check-aws-credentials
check-aws-credentials: ## Check AWS Credentials
ifndef OPERATOR_ACCESS_KEY_ID
	$(error OPERATOR_ACCESS_KEY_ID is undefined)
endif
ifndef OPERATOR_SECRET_ACCESS_KEY
	$(error OPERATOR_SECRET_ACCESS_KEY is undefined)
endif

#############################################################################################
# Deployment (stage/local)
#############################################################################################

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
	@oc process --local -p NAME="zero-size-accountpool" -p SIZE=0 -p TYPE="Default" -f hack/templates/aws.managed.openshift.io_v1alpha1_accountpool.tmpl | oc apply -f -	
	# Create zero size account pool
	@oc process --local -p NAME="hs-zero-size-accountpool" -p SIZE=1 -p TYPE="Hypershift" -f hack/templates/aws.managed.openshift.io_v1alpha1_accountpool.tmpl | oc apply -f -

.PHONY: validate-deployment
validate-deployment: check-aws-account-id-env check-sts-setup ## Validates deployment configuration

.PHONY: predeploy
predeploy: predeploy-aws-account-operator deploy-aws-account-operator-credentials create-ou-map validate-deployment ## Predeploy Operator

.PHONY: deploy-local
deploy-local: ## Deploy Operator locally
	@FORCE_DEV_MODE=local OPERATOR_NAMESPACE="$(OPERATOR_NAMESPACE)" WATCH_NAMESPACE="$(OPERATOR_NAMESPACE)" go run ./main.go --zap-devel

.PHONY: deploy-local-debug
deploy-local-debug: ## Deploy Operator locally with Delve enabled
	@FORCE_DEV_MODE=local ${OPERATOR_SDK} run --local --namespace=$(OPERATOR_NAMESPACE) --enable-delve

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

.PHONY: create-ou-map
create-ou-map: check-ou-mapping-configmap-env ## Test apply OU map CR
	# Create OU map
	@hack/scripts/set_operator_configmap.sh -a 0 -v 1 -r "${OSD_STAGING_1_OU_ROOT_ID}" -o "${OSD_STAGING_1_OU_BASE_ID}" -s "${STS_JUMP_ARN}" -m "${SUPPORT_JUMP_ROLE}" -a "${ACCOUNT_LIMIT}"

.PHONY: delete-ou-map
delete-ou-map: ## Test delete OU map CR
	# Delete OU map
	@oc process --local -p ROOT=${OSD_STAGING_1_OU_ROOT_ID} -p BASE=${OSD_STAGING_1_OU_BASE_ID} -p OPERATOR_NAMESPACE=aws-account-operator -f hack/templates/aws.managed.openshift.io_v1alpha1_configmap.tmpl | oc delete -f -

.PHONY: clean-operator
clean-operator: ## Clean Operator
	# Delete reuse namespace
	@kubectl get namespace ${ACCOUNT_CLAIM_NAMESPACE} -o json | tr -d "\n" | sed "s/\"finalizers\": \[[^]]\+\]/\"finalizers\": []/" | kubectl replace --raw /api/v1/namespaces/${ACCOUNT_CLAIM_NAMESPACE}/finalize -f -
	@oc process --local -p NAME=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/namespace.tmpl | oc delete --now --ignore-not-found -f -

	# Delete STS namespace
	@oc process --local -p NAME=${STS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete --now --ignore-not-found -f -
	
	# Delete CCS Namespace
	@oc process --local -p NAME=${CCS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete --now --ignore-not-found -f -

	# Delete Fake Account namespace
	@oc process --local -p NAME=${FAKE_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete --now --ignore-not-found -f -
	
	# Delete KMS Namespace
	@oc process --local -p NAME=${KMS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete --now --ignore-not-found -f -

	# Candidate for removal
	$(MAKE) delete-ccs-2-namespace || true

	oc delete accounts --now --ignore-not-found --all -n ${NAMESPACE}
	oc delete awsfederatedaccountaccess --now --ignore-not-found --all -n ${NAMESPACE}
	oc delete awsfederatedrole --now --ignore-not-found --all -n ${NAMESPACE}

#############################################################################################
# Local Dev/Testing
#############################################################################################

.PHONY: create-account
create-account: check-aws-account-id-env ## Create account
	# Create Account
	test/local/create_account.sh

.PHONY: delete-account
delete-account: ## Delete account
	# Delete Account
	test/local/delete_account.sh || true
	# Delete Secrets
	test/local/delete_account_secrets.sh

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

.PHONY: create-fleetmanager-accountclaim
create-fleet-accountclaim: ## Delete AccountClaim
	# Create fleetmanager accountclaim
	@oc process --local -p NAME=${FM_ACCOUNT_CLAIM_NAME} -p NAMESPACE=${ACCOUNT_CLAIM_NAMESPACE} -p TRUSTED_ARN=${TRUSTED_ARN} -f hack/templates/aws.managed.openshift.io_v1alpha1_fleetmanager_accountclaim_cr.tmpl | oc apply -f -
    # Wait for accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${FM_ACCOUNT_CLAIM_NAME} -n ${ACCOUNT_CLAIM_NAMESPACE} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${FM_ACCOUNT_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-fleetmanager-accountclaim
delete-fleet-accountclaim: ## Delete AccountClaim
	# Delete fleetmanager accountclaim
	@oc process --local -p NAME=${FM_ACCOUNT_CLAIM_NAME} -p NAMESPACE=${ACCOUNT_CLAIM_NAMESPACE} -p TRUSTED_ARN=${TRUSTED_ARN} -f hack/templates/aws.managed.openshift.io_v1alpha1_fleetmanager_accountclaim_cr.tmpl | oc delete -f -

.PHONY: create-awsfederatedrole
create-awsfederatedrole: ## Create awsFederatedRole "Read Only"
	# Create Federated role
	@test/local/create_federated_role.sh
	# Wait for awsFederatedRole CR to become ready
	@while true; do STATUS=$$(oc get awsfederatedrole -n ${NAMESPACE} ${AWS_FEDERATED_ROLE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Valid" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "awsFederatedRole CR ${AWS_FEDERATED_ROLE_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-awsfederatedrole
delete-awsfederatedrole: ## Delete awsFederatedRole "Read Only"
	# Delete Federated role
	oc delete awsfederatedrole -n aws-account-operator read-only network-mgmt

.PHONY: create-awsfederatedaccountaccess
create-awsfederatedaccountaccess: check-aws-account-id-env ## Create awsFederatedAccountAccess - This uses a AWS Account ID from your environment
	# Create account access
	test/integration/create_awsfederatedaccountaccess.sh --role read-only --name test-federated-user

.PHONY: delete-awsfederatedaccountaccess
delete-awsfederatedaccountaccess: check-aws-account-id-env ## Delete awsFederatedAccountAccess - This uses a AWS Account ID from your environment
	test/integration/delete_awsfederatedaccountaccess.sh --role read-only --name test-federated-user

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
	# Wait for AWS to propagate IAM credentials
	sleep ${SLEEP_INTERVAL}


.PHONY: create-ccs-2-secret
create-ccs-2-secret: ## Create CCS (BYOC) Secret
	# Create CCS Secret
	./hack/scripts/aws/rotate_iam_access_keys.sh -p osd-staging-2 -u osdCcsAdmin -a ${OSD_STAGING_2_AWS_ACCOUNT_ID} -n ${CCS_NAMESPACE_NAME_2} -o /dev/stdout | oc apply -f -
	# Wait for AWS to propagate IAM credentials
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

.PHONY: create-kms-ccs-secret
create-kms-ccs-secret: ## Create CCS (BYOC) Secret
	# Create CCS Secret
	./hack/scripts/aws/rotate_iam_access_keys.sh -p osd-staging-2 -u osdCcsAdmin -a ${OSD_STAGING_2_AWS_ACCOUNT_ID} -n ${KMS_NAMESPACE_NAME} -o /dev/stdout | oc apply -f -
	# Wait for AWS to propagate IAM credentials
	sleep ${SLEEP_INTERVAL}

.PHONY: delete-kms-ccs-secret
delete-kms-ccs-secret: # Delete CCS (BYOC) Secret
	# Delete CCS Secret
	@oc delete secret byoc -n ${KMS_NAMESPACE_NAME}

.PHONY: create-kms-accountclaim-namespace
create-kms-accountclaim-namespace:
	@oc process --local -p NAME=${KMS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc apply -f -

.PHONY: delete-kms-accountclaim-namespace
delete-kms-accountclaim-namespace:
	@oc process --local -p NAME=${KMS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete -f -

.PHONY: create-kms-accountclaim
create-kms-accountclaim:
	@oc process --local -p NAME=${KMS_CLAIM_NAME} -p NAMESPACE=${KMS_NAMESPACE_NAME} -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p KMS_KEY_ID=${KMS_KEY_ID} -f hack/templates/aws.managed.openshift.io_v1alpha1_kms_accountclaim_cr.tmpl | oc apply -f -
	# Wait for KMS Accountclaim to succeed
	@while true; do STATUS=$$(oc get accountclaim ${KMS_CLAIM_NAME} -n ${KMS_NAMESPACE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${KMS_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-kms-accountclaim
delete-kms-accountclaim:
	@oc process --local -p NAME=${KMS_CLAIM_NAME} -p NAMESPACE=${KMS_NAMESPACE_NAME} -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p KMS_KEY_ID=${KMS_KEY_ID} -f hack/templates/aws.managed.openshift.io_v1alpha1_kms_accountclaim_cr.tmpl | oc delete -f -

### Fake Account Test Workflow
# Create fake account claim namespace
.PHONY: create-fake-accountclaim-namespace
create-fake-accountclaim-namespace: ## Creates namespace for FAKE accountclaim
	@oc process --local -p NAME=${FAKE_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc apply -f -

# Delete FAKE account claim namespace
.PHONY: delete-fake-accountclaim-namespace
delete-fake-accountclaim-namespace: ## Deletes namespace for FAKE accountclaim
	@oc process --local -p NAME=${FAKE_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete -f -

.PHONY: create-fake-accountclaim
create-fake-accountclaim: ## Creates a templated FAKE accountclaim
	# Create FAKE accountclaim
	@oc process --local -p NAME=${FAKE_CLAIM_NAME} -p NAMESPACE=${FAKE_NAMESPACE_NAME} -f hack/templates/aws.managed.openshift.io_v1alpha1_fake_accountclaim_cr.tmpl | oc apply -f -
	# Wait for fake accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${FAKE_CLAIM_NAME} -n ${FAKE_NAMESPACE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${FAKE_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: delete-fake-accountclaim
delete-fake-accountclaim: ## Deletes a templated FAKE accountclaim
	# Delete fake accountclaim
	@oc process --local -p NAME=${FAKE_CLAIM_NAME} -p NAMESPACE=${FAKE_NAMESPACE_NAME} -f hack/templates/aws.managed.openshift.io_v1alpha1_fake_accountclaim_cr.tmpl | oc delete -f -


