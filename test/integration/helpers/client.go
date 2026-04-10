package helpers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
)

// GetKubeClient returns a controller-runtime client for Kubernetes
func GetKubeClient() (client.Client, error) {
	config, err := GetKubeConfig()
	if err != nil {
		return nil, err
	}

	// Create scheme and add AWS Account Operator types
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		return nil, fmt.Errorf("failed to add core types to scheme: %w", err)
	}
	if err := awsv1alpha1.AddToScheme(s); err != nil {
		return nil, fmt.Errorf("failed to add AWS Account Operator types to scheme: %w", err)
	}

	// Create controller-runtime client
	c, err := client.New(config, client.Options{Scheme: s})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return c, nil
}

// GetKubeConfig returns the Kubernetes REST config
func GetKubeConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to kubeconfig
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
	}

	return config, nil
}

// CreateNamespace creates a namespace if it doesn't exist
func CreateNamespace(ctx context.Context, c client.Client, name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	err := c.Create(ctx, ns)
	if err != nil && client.IgnoreAlreadyExists(err) != nil {
		return fmt.Errorf("failed to create namespace %s: %w", name, err)
	}

	return nil
}

// DeleteNamespace deletes a namespace and waits for it to be removed
func DeleteNamespace(ctx context.Context, c client.Client, name string, timeout time.Duration) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	// Delete the namespace
	err := c.Delete(ctx, ns)
	if err != nil && client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete namespace %s: %w", name, err)
	}

	// Wait for namespace to be deleted
	return wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		err := c.Get(ctx, client.ObjectKey{Name: name}, ns)
		if client.IgnoreNotFound(err) == nil {
			return true, nil // Namespace is gone
		}
		return false, nil // Still exists, keep polling
	})
}

// RemoveFinalizers removes all finalizers from a resource
func RemoveFinalizers(ctx context.Context, c client.Client, obj client.Object) error {
	patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
	obj.SetFinalizers([]string{})
	return c.Patch(ctx, obj, patch)
}
