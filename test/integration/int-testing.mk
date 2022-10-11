REUSE_UUID := $(shell uuidgen | awk -F- '{ print tolower($$2) }')
REUSE_BUCKET_NAME=test-reuse-bucket-${REUSE_UUID}

include test/integration/test_envs

.PHONY: prow-ci-predeploy
prow-ci-predeploy: predeploy-aws-account-operator deploy-aws-account-operator-credentials create-ou-map
	@ls deploy/*.yaml | grep -v operator.yaml | xargs -L1 oc apply -f

.PHONY: local-ci-entrypoint
local-ci-entrypoint: ## Triggers integration test bootstrap bash script for local cluster
	test/integration/integration-test-bootstrap.sh -p local --skip-cleanup -n $(OPERATOR_NAMESPACE)

.PHONY: prow-ci-entrypoint
prow-ci-entrypoint: ## Triggers integration test bootstrap bash script for prow ci
	test/integration/integration-test-bootstrap.sh -p prow

.PHONY: stage-ci-entrypoint
stage-ci-entrypoint: ## Triggers integration test bootstrap bash script for staging cluster
	test/integration/integration-test-bootstrap.sh -p stage --skip-cleanup -n $(OPERATOR_NAMESPACE)

.PHONY: ci-aws-resources-cleanup
ci-aws-resources-cleanup: 
	hack/scripts/cleanup-aws-resources.sh "$(STS_ROLE_ARN)" "$(OSD_STAGING_1_AWS_ACCOUNT_ID)"
	hack/scripts/cleanup-aws-resources.sh "$(STS_JUMP_ARN)" "$(OSD_STAGING_2_AWS_ACCOUNT_ID)"

# working:
#   test-account-creation
#   test-reuse
.PHONY: ci-int-tests
ci-int-tests: test-reuse

.PHONY: test-integration
test-integration: test-ccs test-reuse test-awsfederatedaccountaccess test-awsfederatedrole test-aws-ou-logic test-sts test-fake-accountclaim test-kms ## Runs all integration tests

#############################################################################################
# Tests
#############################################################################################

.PHONY: test-ccs
test-ccs: create-ccs-secret create-ccs-accountclaim ## Test CCS
	test/integration/tests/validate_ccs_accountclaim.sh
	$(MAKE) delete-ccs

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

.PHONY: test-awsfederatedaccountaccess
test-awsfederatedaccountaccess: check-aws-account-id-env create-awsfederatedrole create-awsfederatedaccountaccess ## Test awsFederatedAccountAccess
	# Retrieve role UID
	$(eval UID=$(shell oc get awsfederatedaccountaccesses.aws.managed.openshift.io -n ${NAMESPACE} ${FED_USER} -o=json |jq -r .metadata.labels.uid))
	
	# Test Assume role
	aws sts assume-role --role-arn arn:aws:iam::${OSD_STAGING_1_AWS_ACCOUNT_ID}:role/read-only-$(UID) --role-session-name RedHatTest --profile osd-staging-2

	test/integration/delete_awsfederatedaccountaccess.sh --role read-only --name test-federated-user
	@oc delete -f test/deploy/aws.managed.openshift.io_v1alpha1_awsfederatedrole_readonly_cr.yaml
	$(MAKE) delete-account

.PHONY: test-aws-ou-logic
test-aws-ou-logic: check-ou-mapping-configmap-env ## Test AWS OU logic
	# Check that account was moved correctly
	@sleep 2; TYPE=$$(aws organizations list-parents --child-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --profile osd-staging-1 | jq -r ".Parents[0].Type"); if [ "$$TYPE" == "ORGANIZATIONAL_UNIT" ]; then echo "Account move successfully"; exit 0; elif [ "$$TYPE" == "ROOT" ]; then echo "Failed to move account out of root"; exit 1; fi;
	@aws organizations list-parents --child-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --profile osd-staging-1
	# Move account back into Root and delete test OU
	@ROOT_ID=$$(aws organizations list-roots --profile osd-staging-1 | jq -r ".Roots[0].Id"); OU=$$(aws organizations list-parents --child-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --profile osd-staging-1 | jq -r ".Parents[0].Id"); aws organizations move-account --account-id ${OSD_STAGING_1_AWS_ACCOUNT_ID} --source-parent-id "$$OU" --destination-parent-id "$$ROOT_ID" --profile osd-staging-1; aws organizations delete-organizational-unit --organizational-unit-id "$$OU" --profile osd-staging-1;
	@echo "Successfully moved account back and deleted the test OU"

.PHONY: test-sts
test-sts: create-sts-accountclaim ## Runs a full integration test for STS workflow
	test/integration/tests/validate_sts_accountclaim.sh
	@oc process --local -p NAME=${STS_CLAIM_NAME} -p NAMESPACE=${STS_NAMESPACE_NAME} -p STS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p STS_ROLE_ARN=${STS_ROLE_ARN} -f hack/templates/aws.managed.openshift.io_v1alpha1_sts_accountclaim_cr.tmpl | oc delete -f -
	@oc process --local -p NAME=${STS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete -f -
	
.PHONY: test-fake-accountclaim
test-fake-accountclaim: create-fake-accountclaim ## Runs a full integration test for FAKE workflow
	test/integration/tests/validate_fake_accountclaim.sh

	# Delete Namespace and Account
	@oc process --local -p NAME=${FAKE_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete -f -
	@oc process --local -p NAME=${FAKE_CLAIM_NAME} -p NAMESPACE=${FAKE_NAMESPACE_NAME} -f hack/templates/aws.managed.openshift.io_v1alpha1_fake_accountclaim_cr.tmpl | oc delete -f -

.PHONY: test-kms
test-kms: create-kms-ccs-secret create-kms-accountclaim validate-kms delete-kms-accountclaim delete-kms-ccs-secret delete-kms-accountclaim-namespace
	test/integration/tests/validate_kms_key.sh
	
	@oc process --local -p NAME=${KMS_CLAIM_NAME} -p NAMESPACE=${KMS_NAMESPACE_NAME} -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p KMS_KEY_ID=${KMS_KEY_ID} -f hack/templates/aws.managed.openshift.io_v1alpha1_kms_accountclaim_cr.tmpl | oc delete -f -
	@oc delete secret byoc -n ${KMS_NAMESPACE_NAME}
	@oc process --local -p NAME=${KMS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete -f -


#############################################################################################
# Test Setup
#############################################################################################

.PHONY: create-account
create-account: check-aws-account-id-env ## Create account
	# Create Account
	test/integration/api/create_account.sh
	
.PHONY: create-ccs-secret
create-ccs-secret: ## Create CCS (BYOC) Secret
	# Create CCS namespace
	@oc process --local -p NAME=${CCS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc apply -f -

	# Create CCS Secret
	./hack/scripts/aws/rotate_iam_access_keys.sh -p osd-staging-2 -u osdCcsAdmin -a ${OSD_STAGING_2_AWS_ACCOUNT_ID} -n ${CCS_NAMESPACE_NAME} -o /dev/stdout | oc apply -f -
	# Wait for AWS to propagate IAM credentials
	sleep ${SLEEP_INTERVAL}

.PHONY: create-ccs-accountclaim
create-ccs-accountclaim: ## Create CSS AccountClaim
	# Create ccs accountclaim
	@oc process --local -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME} -f hack/templates/aws.managed.openshift.io_v1alpha1_ccs_accountclaim_cr.tmpl | oc apply -f -
	# Wait for accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${CCS_CLAIM_NAME} -n ${CCS_NAMESPACE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${CCS_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: create-accountclaim
create-accountclaim: check-aws-account-id-env ## Create accountclaim
	# Create Account
	@oc process --local -p NAME=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/namespace.tmpl | oc apply -f -
	test/integration/api/create_account.sh
	test/integration/api/create_accountclaim.sh

#TODO: move to script
.PHONY: create-awsfederatedrole
create-awsfederatedrole: ## Create awsFederatedRole "Read Only"
	# Create Account
	test/integration/api/create_account.sh
	# Create Federated role
	@oc apply -f test/deploy/aws.managed.openshift.io_v1alpha1_awsfederatedrole_readonly_cr.yaml
	# Wait for awsFederatedRole CR to become ready
	@while true; do STATUS=$$(oc get awsfederatedrole -n ${NAMESPACE} ${AWS_FEDERATED_ROLE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Valid" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "awsFederatedRole CR ${AWS_FEDERATED_ROLE_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: create-awsfederatedaccountaccess
create-awsfederatedaccountaccess: check-aws-account-id-env ## Create awsFederatedAccountAccess - This uses a AWS Account ID from your environment
	# Create account access
	test/integration/create_awsfederatedaccountaccess.sh --role read-only --name test-federated-user

.PHONY: create-sts-accountclaim
create-sts-accountclaim: ## Creates a templated STS accountclaim
	# Create STS namepace
	@oc process --local -p NAME=${STS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc apply -f -

	# Create STS accountclaim
	@oc process --local -p NAME=${STS_CLAIM_NAME} -p NAMESPACE=${STS_NAMESPACE_NAME} -p STS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p STS_ROLE_ARN=${STS_ROLE_ARN} -f hack/templates/aws.managed.openshift.io_v1alpha1_sts_accountclaim_cr.tmpl | oc apply -f -
	
	# Wait for sts accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${STS_CLAIM_NAME} -n ${STS_NAMESPACE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${STS_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done

.PHONY: create-fake-accountclaim
create-fake-accountclaim: ## Creates a templated FAKE accountclaim
	# Create Fake Account Namespace
	@oc process --local -p NAME=${FAKE_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc apply -f -

	# Create FAKE accountclaim
	@oc process --local -p NAME=${FAKE_CLAIM_NAME} -p NAMESPACE=${FAKE_NAMESPACE_NAME} -f hack/templates/aws.managed.openshift.io_v1alpha1_fake_accountclaim_cr.tmpl | oc apply -f -
	# Wait for fake accountclaim to become ready
	@while true; do STATUS=$$(oc get accountclaim ${FAKE_CLAIM_NAME} -n ${FAKE_NAMESPACE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${FAKE_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done
	
.PHONY: create-kms-ccs-secret
create-kms-ccs-secret: ## Create CCS (BYOC) Secret
	# Create namespace
	@oc process --local -p NAME=${KMS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc apply -f -
	
	# create kms key
	hack/scripts/aws/create_kms_test_key.sh -a "${OSD_STAGING_2_AWS_ACCOUNT_ID}" -r "us-east-1" -p osd-staging-2
	
	# Create CCS Secret
	./hack/scripts/aws/rotate_iam_access_keys.sh -p osd-staging-2 -u osdCcsAdmin -a ${OSD_STAGING_2_AWS_ACCOUNT_ID} -n ${KMS_NAMESPACE_NAME} -o /dev/stdout | oc apply -f -
	# Wait for AWS to propagate IAM credentials
	sleep ${SLEEP_INTERVAL}

.PHONY: create-kms-accountclaim
create-kms-accountclaim:
	@oc process --local -p NAME=${KMS_CLAIM_NAME} -p NAMESPACE=${KMS_NAMESPACE_NAME} -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p KMS_KEY_ID=${KMS_KEY_ID} -f hack/templates/aws.managed.openshift.io_v1alpha1_kms_accountclaim_cr.tmpl | oc apply -f -
	
	# Wait for KMS Accountclaim to succeed
	@while true; do STATUS=$$(oc get accountclaim ${KMS_CLAIM_NAME} -n ${KMS_NAMESPACE_NAME} -o json | jq -r '.status.state'); if [ "$$STATUS" == "Ready" ]; then break; elif [ "$$STATUS" == "Failed" ]; then echo "Account claim ${KMS_CLAIM_NAME} failed to create"; exit 1; fi; sleep 1; done


#############################################################################################
# Test Cleanup
#############################################################################################
.PHONY: delete-account
delete-account: ## Delete account
	# Delete Account
	test/integration/api/delete_account.sh || true
	# Delete Secrets
	test/integration/api/delete_account_secrets.sh

.PHONY: delete-accountclaim-namespace
delete-accountclaim-namespace: ## Delete account claim namespace
	# Delete reuse namespace
	@oc process --local -p NAME=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/namespace.tmpl | oc delete -f -

.PHONY: delete-accountclaim
delete-accountclaim: ## Delete AccountClaim
	# Delete accountclaim
	@oc process --local -p NAME=${ACCOUNT_CLAIM_NAME} -p NAMESPACE=${ACCOUNT_CLAIM_NAMESPACE} -f hack/templates/aws.managed.openshift.io_v1alpha1_accountclaim_cr.tmpl | oc delete -f -

.PHONY: delete-ccs
delete-ccs: ## Teardown the test CCS account
	@oc process --local -p CCS_ACCOUNT_ID=${OSD_STAGING_2_AWS_ACCOUNT_ID} -p NAME=${CCS_CLAIM_NAME} -p NAMESPACE=${CCS_NAMESPACE_NAME} -f hack/templates/aws.managed.openshift.io_v1alpha1_ccs_accountclaim_cr.tmpl | oc delete -f -

	# Delete CCS Secret
	@oc delete secret byoc -n ${CCS_NAMESPACE_NAME}

	# Delete CCS Namespace
	@oc process --local -p NAME=${CCS_NAMESPACE_NAME} -f hack/templates/namespace.tmpl | oc delete -f -