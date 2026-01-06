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

#############################################################################################
# Tests
#############################################################################################

.PHONY: test-integration
test-integration: test-awsfederatedaccountaccess test-awsfederatedrole test-integration-new ## Runs all integration tests (uses new self-contained pattern)

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

#############################################################################################
# Self-contained test pattern (test/integration/tests/*.sh)
#############################################################################################

# NOTE: test-sts is commented out due to CI infrastructure misconfiguration.
# The STS_ROLE_ARN in aao-aws-creds points to OSD_STAGING_1 (xxxx...x0068)
# but the test uses OSD_STAGING_2 (xxxx...x1834), causing the operator to
# refuse processing the claim. See integration-test-bootstrap.sh for details.
#.PHONY: test-sts
#test-sts: ## Test STS (Security Token Service) AccountClaim workflow
#	test/integration/tests/test_sts_accountclaim.sh setup
#	test/integration/tests/test_sts_accountclaim.sh test
#	test/integration/tests/test_sts_accountclaim.sh cleanup

.PHONY: test-fake-accountclaim
test-fake-accountclaim: ## Test FAKE AccountClaim workflow (no real AWS account)
	test/integration/tests/test_fake_accountclaim.sh setup
	test/integration/tests/test_fake_accountclaim.sh test
	test/integration/tests/test_fake_accountclaim.sh cleanup

.PHONY: test-kms
test-kms: ## Test KMS key encryption in CCS AccountClaims
	test/integration/tests/test_kms_accountclaim.sh setup
	test/integration/tests/test_kms_accountclaim.sh test
	test/integration/tests/test_kms_accountclaim.sh cleanup

.PHONY: test-nonccs-account-creation
test-nonccs-account-creation: ## Test non-CCS account creation and AWS credential generation
	test/integration/tests/test_nonccs_account_creation.sh setup
	test/integration/tests/test_nonccs_account_creation.sh test
	test/integration/tests/test_nonccs_account_creation.sh cleanup

.PHONY: test-nonccs-account-reuse
test-nonccs-account-reuse: ## Test account cleanup and reuse (S3 bucket deletion)
	test/integration/tests/test_nonccs_account_reuse.sh setup
	test/integration/tests/test_nonccs_account_reuse.sh test
	test/integration/tests/test_nonccs_account_reuse.sh cleanup

.PHONY: test-aws-ou-logic
test-aws-ou-logic: ## Test AWS OU logic for claimed accounts
	test/integration/tests/test_aws_ou_logic.sh setup
	test/integration/tests/test_aws_ou_logic.sh test
	test/integration/tests/test_aws_ou_logic.sh cleanup

.PHONY: test-finalizer-cleanup
test-finalizer-cleanup: ## Test finalizer behavior and cleanup process
	test/integration/tests/test_finalizer_cleanup.sh setup
	test/integration/tests/test_finalizer_cleanup.sh test
	test/integration/tests/test_finalizer_cleanup.sh cleanup

# Meta target to run all new pattern tests
.PHONY: test-integration-new
test-integration-new: test-fake-accountclaim test-kms test-nonccs-account-creation test-nonccs-account-reuse test-aws-ou-logic test-finalizer-cleanup ## Run all new self-contained integration tests
