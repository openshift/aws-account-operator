package tests

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/aws-account-operator/test/integration/helpers"
)

// TestFakeAccountClaim validates the FAKE AccountClaim workflow which creates an AccountClaim
// that does NOT create an actual AWS Account, but instead creates a secret with
// fake credentials for testing purposes.
//
// The test:
//  1. Creates a namespace for the FAKE AccountClaim
//  2. Creates a FAKE AccountClaim (accountOU: "fake")
//  3. Waits for the claim to become Ready
//  4. Verifies the claim has finalizers
//  5. Verifies the claim has NO accountLink (no Account CR created)
//  6. Verifies a secret was created in the claim namespace
//  7. Cleans up the claim and namespace
//
// This validates:
//  - FAKE AccountClaims don't create Account CRs
//  - FAKE AccountClaims create AWS credential secrets
//  - FAKE mode works for testing without real AWS resources
func TestFakeAccountClaim(t *testing.T) {
	const (
		claimName     = "test-fake"
		namespaceName = "test-fake-namespace"
		secretName    = "aws"

		// Timeouts
		accountClaimReadyTimeout = 1 * time.Minute
		resourceDeleteTimeout    = 5 * time.Minute
	)

	ctx := context.Background()

	// Get Kubernetes client
	kubeClient, err := helpers.GetKubeClient()
	if err != nil {
		t.Fatalf("Failed to get kube client: %v", err)
	}

	// Setup: Create namespace and FAKE AccountClaim
	t.Run("Setup", func(t *testing.T) {
		t.Logf("Creating namespace: %s", namespaceName)
		err := helpers.CreateNamespace(ctx, kubeClient, namespaceName)
		if err != nil {
			t.Fatalf("Failed to create namespace: %v", err)
		}

		t.Logf("Creating FAKE AccountClaim: %s", claimName)
		_, err = helpers.CreateFakeAccountClaim(ctx, kubeClient, claimName, namespaceName)
		if err != nil {
			t.Fatalf("Failed to create FAKE AccountClaim: %v", err)
		}

		t.Logf("Waiting for FAKE AccountClaim to become Ready (timeout: %v)", accountClaimReadyTimeout)
		err = helpers.WaitForAccountClaimReady(ctx, kubeClient, claimName, namespaceName, accountClaimReadyTimeout)
		if err != nil {
			t.Fatalf("FAKE AccountClaim did not become ready: %v", err)
		}

		t.Log("✓ Setup complete")
	})

	// Test: Validate FAKE AccountClaim
	t.Run("Validate", func(t *testing.T) {
		t.Logf("Getting AccountClaim: %s", claimName)
		claim, err := helpers.GetAccountClaim(ctx, kubeClient, claimName, namespaceName)
		if err != nil {
			t.Fatalf("Failed to get AccountClaim: %v", err)
		}

		// Verify finalizers are present
		t.Log("Validating AccountClaim has finalizers...")
		if len(claim.Finalizers) < 1 {
			t.Error("FAIL: No finalizers set on FAKE AccountClaim")
		} else {
			t.Logf("✓ Finalizers present: %d", len(claim.Finalizers))
		}

		// Verify there is NO accountLink
		t.Log("Validating there is NO accountLink...")
		if claim.Spec.AccountLink != "" {
			t.Errorf("FAIL: AccountLink should be empty but is: %s (FAKE AccountClaims should NOT create Account CRs)", claim.Spec.AccountLink)
		} else {
			t.Log("✓ No accountLink (as expected for FAKE claims)")
		}

		// Verify secret exists
		t.Log("Validating secret exists in namespace...")
		err = helpers.VerifySecretExists(ctx, kubeClient, secretName, namespaceName)
		if err != nil {
			t.Errorf("FAIL: %v", err)
		} else {
			t.Logf("✓ Secret '%s' exists in namespace %s", secretName, namespaceName)
		}

		t.Log("")
		t.Log("========================================")
		t.Log("FAKE ACCOUNTCLAIM TEST PASSED!")
		t.Log("========================================")
		t.Log("✓ FAKE AccountClaim has finalizers")
		t.Log("✓ FAKE AccountClaim has no accountLink (no Account CR created)")
		t.Log("✓ FAKE AccountClaim created AWS credentials secret")
	})

	// Cleanup: Remove test resources
	t.Cleanup(func() {
		t.Log("=============================================================")
		t.Log("CLEANUP: Removing test resources")
		t.Log("=============================================================")

		// Delete AccountClaim (with finalizer removal for faster cleanup)
		t.Log("Deleting FAKE AccountClaim...")
		err := helpers.DeleteAccountClaim(ctx, kubeClient, claimName, namespaceName, resourceDeleteTimeout, true)
		if err != nil {
			t.Errorf("WARNING: Failed to delete FAKE AccountClaim: %v", err)
		}

		// Delete namespace
		t.Log("Deleting namespace...")
		err = helpers.DeleteNamespace(ctx, kubeClient, namespaceName, resourceDeleteTimeout)
		if err != nil {
			t.Errorf("WARNING: Failed to delete namespace: %v", err)
		}

		t.Log("✓ Cleanup complete")
	})
}
