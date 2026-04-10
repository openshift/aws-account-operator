package helpers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
)

// CreateFakeAccountClaim creates a FAKE AccountClaim for testing
func CreateFakeAccountClaim(ctx context.Context, c client.Client, name, namespace string) (*awsv1alpha1.AccountClaim, error) {
	claim := &awsv1alpha1.AccountClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"managed.openshift.com/fake": "true",
			},
		},
		Spec: awsv1alpha1.AccountClaimSpec{
			AccountLink: "",
			LegalEntity: awsv1alpha1.LegalEntity{
				ID:   "111111",
				Name: name,
			},
			Aws: awsv1alpha1.Aws{
				Regions: []awsv1alpha1.AwsRegions{
					{Name: "us-east-1"},
				},
			},
			AwsCredentialSecret: awsv1alpha1.SecretRef{
				Name:      "aws",
				Namespace: namespace,
			},
		},
	}

	err := c.Create(ctx, claim)
	if err != nil {
		return nil, fmt.Errorf("failed to create AccountClaim %s/%s: %w", namespace, name, err)
	}

	return claim, nil
}

// WaitForAccountClaimReady waits for an AccountClaim to reach Ready state
func WaitForAccountClaimReady(ctx context.Context, c client.Client, name, namespace string, timeout time.Duration) error {
	return wait.PollImmediate(5*time.Second, timeout, func() (bool, error) {
		claim := &awsv1alpha1.AccountClaim{}
		err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, claim)
		if err != nil {
			return false, err
		}

		// Check if status is Ready or if Claimed condition is true
		if claim.Status.State == awsv1alpha1.ClaimStatusReady {
			return true, nil
		}

		// Check for Claimed condition
		for _, condition := range claim.Status.Conditions {
			if condition.Type == awsv1alpha1.AccountClaimed {
				if condition.Status == corev1.ConditionTrue {
					return true, nil
				}
			}
		}

		// Check if status is Error/Failed
		if claim.Status.State == awsv1alpha1.ClaimStatusError {
			return false, fmt.Errorf("AccountClaim failed with error status")
		}

		return false, nil
	})
}

// DeleteAccountClaim deletes an AccountClaim
func DeleteAccountClaim(ctx context.Context, c client.Client, name, namespace string, timeout time.Duration, removeFinalizers bool) error {
	claim := &awsv1alpha1.AccountClaim{}
	err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, claim)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to get AccountClaim %s/%s: %w", namespace, name, err)
	}

	// Remove finalizers if requested
	if removeFinalizers && len(claim.Finalizers) > 0 {
		if err := RemoveFinalizers(ctx, c, claim); err != nil {
			return fmt.Errorf("failed to remove finalizers from AccountClaim %s/%s: %w", namespace, name, err)
		}
	}

	// Delete the claim
	err = c.Delete(ctx, claim)
	if err != nil && client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete AccountClaim %s/%s: %w", namespace, name, err)
	}

	// Wait for deletion
	return wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, claim)
		if client.IgnoreNotFound(err) == nil {
			return true, nil // Deleted
		}
		return false, nil // Still exists
	})
}

// GetAccountClaim retrieves an AccountClaim
func GetAccountClaim(ctx context.Context, c client.Client, name, namespace string) (*awsv1alpha1.AccountClaim, error) {
	claim := &awsv1alpha1.AccountClaim{}
	err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, claim)
	if err != nil {
		return nil, fmt.Errorf("failed to get AccountClaim %s/%s: %w", namespace, name, err)
	}
	return claim, nil
}

// VerifySecretExists checks if a secret exists in a namespace
func VerifySecretExists(ctx context.Context, c client.Client, name, namespace string) error {
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, secret)
	if err != nil {
		return fmt.Errorf("secret %s/%s does not exist: %w", namespace, name, err)
	}
	return nil
}
