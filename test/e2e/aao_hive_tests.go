//go:build osde2e

package osde2etests

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	aav1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// "AWS Account Operator Hive" validates the already-deployed, PKO-managed AAO instance on a
// live hive cluster (hivei01ue1 for int, hives02ue1 for stage). It uses the fake AccountClaim
// mode which does not create real AWS accounts, making it safe to run on shared hive clusters.
var _ = ginkgo.Describe("AWS Account Operator Hive", ginkgo.Ordered, func() {
	var (
		k8sClient     client.Client
		clientset     *kubernetes.Clientset
		testNamespace string
		claimName     string
		logger        = log.Log
	)

	const (
		operatorNamespace = "aws-account-operator"
		shortTimeout      = 5 * time.Minute
		claimTimeout      = 10 * time.Minute
	)

	ginkgo.BeforeAll(func(ctx context.Context) {
		log.SetLogger(ginkgo.GinkgoLogr)

		scheme := runtime.NewScheme()
		gomega.Expect(aav1alpha1.AddToScheme(scheme)).To(gomega.Succeed())
		gomega.Expect(corev1.AddToScheme(scheme)).To(gomega.Succeed())

		cfg, err := ctrl.GetConfig()
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to get kubeconfig")

		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to create k8s client")

		clientset, err = kubernetes.NewForConfig(cfg)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to create clientset")

		runSuffix := rand.String(6)
		testNamespace = fmt.Sprintf("aao-e2e-%s", runSuffix)
		claimName = fmt.Sprintf("aao-e2e-fake-%s", runSuffix)

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
		gomega.Expect(k8sClient.Create(ctx, ns)).To(gomega.Succeed(), "failed to create test namespace")

		logger.Info("AAO Hive suite setup complete", "namespace", testNamespace, "claim", claimName)
	})

	ginkgo.It("aws-account-operator pod is running and healthy", func(ctx context.Context) {
		gomega.Eventually(func() bool {
			pods, err := clientset.CoreV1().Pods(operatorNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: "name=aws-account-operator",
			})
			if err != nil || len(pods.Items) == 0 {
				return false
			}
			pod := pods.Items[0]
			if pod.Status.Phase != corev1.PodRunning {
				return false
			}
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady {
					return cond.Status == corev1.ConditionTrue
				}
			}
			return false
		}, shortTimeout, 15*time.Second).Should(gomega.BeTrue(),
			"aws-account-operator pod should be running and ready")
	})

	ginkgo.It("should process a fake AccountClaim without creating a real AWS Account", func(ctx context.Context) {
		claim := &aav1alpha1.AccountClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      claimName,
				Namespace: testNamespace,
				Annotations: map[string]string{
					"managed.openshift.com/fake": "true",
				},
			},
			Spec: aav1alpha1.AccountClaimSpec{
				LegalEntity: aav1alpha1.LegalEntity{
					ID:   "111111",
					Name: claimName,
				},
				AwsCredentialSecret: aav1alpha1.SecretRef{
					Name:      "aws",
					Namespace: testNamespace,
				},
				Aws: aav1alpha1.Aws{
					Regions: []aav1alpha1.AwsRegions{{Name: "us-east-1"}},
				},
				AccountLink: "",
			},
		}
		gomega.Expect(k8sClient.Create(ctx, claim)).To(gomega.Succeed(), "failed to create fake AccountClaim")

		// Wait for claim to become Ready
		gomega.Eventually(func() aav1alpha1.ClaimStatus {
			current := &aav1alpha1.AccountClaim{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: claimName, Namespace: testNamespace}, current); err != nil {
				return ""
			}
			return current.Status.State
		}, claimTimeout, 15*time.Second).Should(gomega.Equal(aav1alpha1.ClaimStatusReady),
			"fake AccountClaim should reach Ready state")

		// Verify the claim has finalizers (operator is managing it)
		current := &aav1alpha1.AccountClaim{}
		gomega.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: claimName, Namespace: testNamespace}, current)).
			To(gomega.Succeed())
		gomega.Expect(current.Finalizers).NotTo(gomega.BeEmpty(),
			"fake AccountClaim should have finalizers set by the operator")

		// Verify no accountLink - fake claims should not create real Account CRs
		gomega.Expect(current.Spec.AccountLink).To(gomega.BeEmpty(),
			"fake AccountClaim should not have an accountLink (no Account CR should be created)")

		// Verify the credential secret was created in the claim namespace
		gomega.Eventually(func() error {
			_, err := clientset.CoreV1().Secrets(testNamespace).Get(ctx, "aws", metav1.GetOptions{})
			return err
		}, shortTimeout, 10*time.Second).Should(gomega.Succeed(),
			"operator should create the 'aws' credential secret in the claim namespace")

		logger.Info("Fake AccountClaim test passed", "claim", claimName, "namespace", testNamespace)
	})

	ginkgo.AfterAll(func(ctx context.Context) {
		logger.Info("Cleanup: removing fake AccountClaim and test namespace", "namespace", testNamespace)

		// Delete the AccountClaim; the operator will remove finalizers and clean up
		claim := &aav1alpha1.AccountClaim{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: claimName, Namespace: testNamespace}, claim); err == nil {
			if err := k8sClient.Delete(ctx, claim); err != nil && !apierrors.IsNotFound(err) {
				logger.Info("WARNING: failed to delete fake AccountClaim", "error", err)
			}
		}

		// Wait for claim to be gone (operator removes finalizers)
		gomega.Eventually(func() bool {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: claimName, Namespace: testNamespace}, &aav1alpha1.AccountClaim{})
			return apierrors.IsNotFound(err)
		}, shortTimeout, 10*time.Second).Should(gomega.BeTrue(), "fake AccountClaim should be fully deleted")

		// Delete the test namespace
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
		if err := k8sClient.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
			logger.Info("WARNING: failed to delete test namespace", "error", err)
		}

		// Wait for namespace to terminate
		gomega.Eventually(func() bool {
			_, err := clientset.CoreV1().Namespaces().Get(ctx, testNamespace, metav1.GetOptions{})
			return apierrors.IsNotFound(err)
		}, 2*time.Minute, 5*time.Second).Should(gomega.BeTrue(),
			"test namespace should terminate")
	})
})
