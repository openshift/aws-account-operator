package validation

import (
	"context"
	"fmt"
	"testing"

	apis "github.com/openshift/aws-account-operator/api"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	TestAccountName = "testaccount"
	TestAccountID   = "1234567"
)

func setupDefaultMocks(localObjects []runtime.Object) client.WithWatch {

	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(localObjects...).Build()
}

func TestAccounPoolServiceQuota(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AccountPool Validation Suite")
}

var _ = Describe("AccountPool Validation", func() {
	var (
		accontPool      awsv1alpha1.AccountPool
		account         awsv1alpha1.Account
		expectedAccount awsv1alpha1.Account
		accountName     string
		accountID       string
		r               *AccountPoolValidationReconciler
		configMap       *v1.ConfigMap
	)

	BeforeEach(func() {
		accountName = TestAccountName
		accountID = TestAccountID
		account = awsv1alpha1.Account{
			ObjectMeta: metav1.ObjectMeta{
				Name:            accountName,
				Namespace:       awsv1alpha1.AccountCrNamespace,
				OwnerReferences: []metav1.OwnerReference{{Kind: "AccountPool"}},
			},
			Spec: awsv1alpha1.AccountSpec{
				AwsAccountID: accountID,
				AccountPool:  accountName,
				RegionalServiceQuotas: awsv1alpha1.RegionalServiceQuotas{
					"us-east-1": awsv1alpha1.AccountServiceQuota{
						awsv1alpha1.RunningStandardInstances: {
							Value: 100,
						},
					},
				},
			},
		}
		accontPool = awsv1alpha1.AccountPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      accountName,
				Namespace: awsv1alpha1.AccountCrNamespace,
			},
			Spec: awsv1alpha1.AccountPoolSpec{
				PoolSize: 1,
			}}
		err := apis.AddToScheme(scheme.Scheme)
		if err != nil {
			fmt.Printf("failed adding to scheme in account_controller_test.go")
		}

	})

	Context("Testing AccountPool Validation", func() {
		When("Configmap Service Quota and Account Spec ServiceQuota are the same", func() {
			It("Does nothing", func() {
				var accountPoolConfig = `
testaccount:
  servicequotas:
    us-east-1:
      L-1216C47A: '100'`

				configMap = &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
					Data: map[string]string{
						"accountpool":                    accountPoolConfig,
						"feature.accountpool_validation": "true",
					},
				}

				r = &AccountPoolValidationReconciler{
					Scheme: scheme.Scheme,
					Client: setupDefaultMocks([]runtime.Object{&account, &accontPool, configMap}),
				}

				_, _ = r.Reconcile(context.TODO(), ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: awsv1alpha1.AccountCrNamespace,
						Name:      accountName,
					},
				})
				err := r.Client.Get(context.TODO(), types.NamespacedName{
					Namespace: awsv1alpha1.AccountCrNamespace,
					Name:      accountName,
				}, &expectedAccount)

				Expect(err).To(Not(HaveOccurred()))
				Expect(expectedAccount.Spec.RegionalServiceQuotas).To(Equal(account.Spec.RegionalServiceQuotas))

			})
		})
		When(" Configmap SerivceQuota is greater than Account Spec ServiceQuota", func() {
			It("Increases ServiceQuota in Account Spec", func() {
				var accountPoolConfig = `
testaccount:
  servicequotas:
    us-east-1:
      L-1216C47A: '150'`
				configMap = &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
					Data: map[string]string{
						"accountpool":                    accountPoolConfig,
						"feature.accountpool_validation": "true",
					},
				}

				r := &AccountPoolValidationReconciler{
					Scheme: scheme.Scheme,
					Client: fake.NewClientBuilder().WithRuntimeObjects(&account, &accontPool, configMap).Build(),
				}
				_, _ = r.Reconcile(context.TODO(), ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: awsv1alpha1.AccountCrNamespace,
						Name:      accountName,
					},
				})
				err := r.Client.Get(context.TODO(), types.NamespacedName{
					Namespace: awsv1alpha1.AccountCrNamespace,
					Name:      accountName,
				}, &expectedAccount)

				Expect(err).To(Not(HaveOccurred()))
				Expect(expectedAccount.Spec.RegionalServiceQuotas["us-east-1"]["L-1216C47A"].Value).To(Equal(150))

			})
		})
		When("ConfigMap ServiceQuota is Decreased", func() {
			It("Decreases Account Spec ServiceQuota", func() {
				var accountPoolConfig = `
testaccount:
  servicequotas:
    us-east-1:
      L-1216C47A: '50'`
				configMap = &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
					Data: map[string]string{
						"accountpool":                    accountPoolConfig,
						"feature.accountpool_validation": "true",
					},
				}
				r := &AccountPoolValidationReconciler{
					Scheme: scheme.Scheme,
					Client: fake.NewClientBuilder().WithRuntimeObjects(&account, &accontPool, configMap).Build(),
				}
				_, _ = r.Reconcile(context.TODO(), ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: awsv1alpha1.AccountCrNamespace,
						Name:      accountName,
					},
				})
				err := r.Client.Get(context.TODO(), types.NamespacedName{
					Namespace: awsv1alpha1.AccountCrNamespace,
					Name:      accountName,
				}, &expectedAccount)

				Expect(err).To(Not(HaveOccurred()))
				Expect(expectedAccount.Spec.RegionalServiceQuotas["us-east-1"]["L-1216C47A"].Value).To(Equal(50))

			})
		})
		When("Account has pause-reconciliation annotation set to true", func() {
			It("Should skip updating service quotas for that account", func() {
				var accountPoolConfig = `
testaccount:
  servicequotas:
    us-east-1:
      L-1216C47A: '200'`
				pausedAccount := account.DeepCopy()
				pausedAccount.Annotations = map[string]string{
					PauseReconciliationAnnotation: "true",
				}
				configMap = &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
					Data: map[string]string{
						"accountpool":                    accountPoolConfig,
						"feature.accountpool_validation": "true",
					},
				}

				r := &AccountPoolValidationReconciler{
					Scheme: scheme.Scheme,
					Client: fake.NewClientBuilder().WithRuntimeObjects(pausedAccount, &accontPool, configMap).Build(),
				}
				_, _ = r.Reconcile(context.TODO(), ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: awsv1alpha1.AccountCrNamespace,
						Name:      accountName,
					},
				})
				err := r.Client.Get(context.TODO(), types.NamespacedName{
					Namespace: awsv1alpha1.AccountCrNamespace,
					Name:      accountName,
				}, &expectedAccount)

				Expect(err).To(Not(HaveOccurred()))
				// Service quota should remain unchanged (100) because account is paused
				Expect(expectedAccount.Spec.RegionalServiceQuotas["us-east-1"]["L-1216C47A"].Value).To(Equal(100))
			})
		})
		When("Account has pause-reconciliation annotation set to false", func() {
			It("Should proceed with updating service quotas", func() {
				var accountPoolConfig = `
testaccount:
  servicequotas:
    us-east-1:
      L-1216C47A: '200'`
				unpausedAccount := account.DeepCopy()
				unpausedAccount.Annotations = map[string]string{
					PauseReconciliationAnnotation: "false",
				}
				configMap = &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
					Data: map[string]string{
						"accountpool":                    accountPoolConfig,
						"feature.accountpool_validation": "true",
					},
				}

				r := &AccountPoolValidationReconciler{
					Scheme: scheme.Scheme,
					Client: fake.NewClientBuilder().WithRuntimeObjects(unpausedAccount, &accontPool, configMap).Build(),
				}
				_, _ = r.Reconcile(context.TODO(), ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: awsv1alpha1.AccountCrNamespace,
						Name:      accountName,
					},
				})
				err := r.Client.Get(context.TODO(), types.NamespacedName{
					Namespace: awsv1alpha1.AccountCrNamespace,
					Name:      accountName,
				}, &expectedAccount)

				Expect(err).To(Not(HaveOccurred()))
				// Service quota should be updated (200) because account is not paused
				Expect(expectedAccount.Spec.RegionalServiceQuotas["us-east-1"]["L-1216C47A"].Value).To(Equal(200))
			})
		})
	})
})
