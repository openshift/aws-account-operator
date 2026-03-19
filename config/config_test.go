package config

import (
	"testing"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetDefaultRegion(t *testing.T) {
	tt := []struct {
		Name               string
		IsFedramp          bool
		ExpectedRegionName string
	}{
		{
			Name:               "not govcloud",
			IsFedramp:          false,
			ExpectedRegionName: awsv1alpha1.AwsUSEastOneRegion,
		},
		{
			Name:               "govcloud",
			IsFedramp:          true,
			ExpectedRegionName: awsv1alpha1.AwsUSGovEastOneRegion,
		},
	}

	for _, test := range tt {
		isFedramp = test.IsFedramp

		actualRegionName := GetDefaultRegion()
		if actualRegionName != test.ExpectedRegionName {
			t.Errorf("%s: expected: %s, got %s\n", test.Name, test.ExpectedRegionName, actualRegionName)
		}
	}
}

func TestGetIAMArn(t *testing.T) {
	tt := []struct {
		Name          string
		IsFedramp     bool
		AwsAccountID  string
		AwsType       string
		AwsResourceID string
		ExpectedArn   string
	}{
		{
			Name:          "not govcloud",
			IsFedramp:     false,
			AwsAccountID:  "123456789",
			AwsType:       "role",
			AwsResourceID: "DelegatedAdmin",
			ExpectedArn:   "arn:aws:iam::123456789:role/DelegatedAdmin",
		},
		{
			Name:          "govcloud",
			IsFedramp:     true,
			AwsAccountID:  "987654321",
			AwsType:       "role",
			AwsResourceID: "DelegatedFedrampAdmin",
			ExpectedArn:   "arn:aws-us-gov:iam::987654321:role/DelegatedFedrampAdmin",
		},
		{
			Name:          "any account admin access",
			IsFedramp:     false,
			AwsAccountID:  "aws",
			AwsType:       "policy",
			AwsResourceID: "AdministratorAccess",
			ExpectedArn:   "arn:aws:iam::aws:policy/AdministratorAccess",
		},
	}

	for _, test := range tt {
		isFedramp = test.IsFedramp

		actualArn := GetIAMArn(test.AwsAccountID, test.AwsType, test.AwsResourceID)
		if actualArn != test.ExpectedArn {
			t.Errorf("%s: expected %s, got %s\n", test.Name, test.ExpectedArn, actualArn)
		}
	}
}

func TestGetPayerAccountIDs(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name           string
		configMap      *corev1.ConfigMap
		createConfigMap bool
		expectedIDs    []string
		expectError    bool
	}{
		{
			name: "valid configmap with multiple account IDs",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"payer-account-ids": "111111111111,222222222222,333333333333",
				},
			},
			createConfigMap: true,
			expectedIDs:     []string{"111111111111", "222222222222", "333333333333"},
			expectError:     false,
		},
		{
			name: "valid configmap with single account ID",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"payer-account-ids": "111111111111",
				},
			},
			createConfigMap: true,
			expectedIDs:     []string{"111111111111"},
			expectError:     false,
		},
		{
			name: "configmap with whitespace around account IDs",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"payer-account-ids": " 111111111111 , 222222222222 , 333333333333 ",
				},
			},
			createConfigMap: true,
			expectedIDs:     []string{"111111111111", "222222222222", "333333333333"},
			expectError:     false,
		},
		{
			name: "configmap with empty strings in list",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"payer-account-ids": "111111111111,,222222222222",
				},
			},
			createConfigMap: true,
			expectedIDs:     []string{"111111111111", "222222222222"},
			expectError:     false,
		},
		{
			name: "configmap missing payer-account-ids field",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"other-field": "value",
				},
			},
			createConfigMap: true,
			expectedIDs:     []string{},
			expectError:     false,
		},
		{
			name: "configmap with empty payer-account-ids value",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"payer-account-ids": "",
				},
			},
			createConfigMap: true,
			expectedIDs:     []string{},
			expectError:     false,
		},
		{
			name:            "missing configmap returns empty list gracefully",
			configMap:       nil,
			createConfigMap: false,
			expectedIDs:     []string{},
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with or without ConfigMap
			var kubeClient client.Client
			if tt.createConfigMap {
				kubeClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(tt.configMap).
					Build()
			} else {
				kubeClient = fake.NewClientBuilder().
					WithScheme(scheme).
					Build()
			}

			// Call function
			accountIDs, err := GetPayerAccountIDs(kubeClient)

			// Check error
			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Check account IDs
			if len(accountIDs) != len(tt.expectedIDs) {
				t.Errorf("expected %d account IDs, got %d: %v", len(tt.expectedIDs), len(accountIDs), accountIDs)
				return
			}

			for i, expectedID := range tt.expectedIDs {
				if accountIDs[i] != expectedID {
					t.Errorf("expected account ID %s at index %d, got %s", expectedID, i, accountIDs[i])
				}
			}
		})
	}
}

func TestIsPayerAccount(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name            string
		accountID       string
		configMap       *corev1.ConfigMap
		createConfigMap bool
		expectedResult  bool
		expectError     bool
	}{
		{
			name:      "account ID in payer blocklist",
			accountID: "111111111111",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"payer-account-ids": "111111111111,222222222222,333333333333",
				},
			},
			createConfigMap: true,
			expectedResult:  true,
			expectError:     false,
		},
		{
			name:      "account ID not in payer blocklist",
			accountID: "123456789012",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"payer-account-ids": "111111111111,222222222222,333333333333",
				},
			},
			createConfigMap: true,
			expectedResult:  false,
			expectError:     false,
		},
		{
			name:      "second account ID in blocklist",
			accountID: "222222222222",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"payer-account-ids": "111111111111,222222222222,333333333333",
				},
			},
			createConfigMap: true,
			expectedResult:  true,
			expectError:     false,
		},
		{
			name:      "third account ID in blocklist",
			accountID: "333333333333",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"payer-account-ids": "111111111111,222222222222,333333333333",
				},
			},
			createConfigMap: true,
			expectedResult:  true,
			expectError:     false,
		},
		{
			name:      "empty account ID not in blocklist",
			accountID: "",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"payer-account-ids": "111111111111,222222222222,333333333333",
				},
			},
			createConfigMap: true,
			expectedResult:  false,
			expectError:     false,
		},
		{
			name:            "missing configmap returns false gracefully",
			accountID:       "111111111111",
			configMap:       nil,
			createConfigMap: false,
			expectedResult:  false,
			expectError:     false,
		},
		{
			name:      "missing payer-account-ids field returns false",
			accountID: "111111111111",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"other-field": "value",
				},
			},
			createConfigMap: true,
			expectedResult:  false,
			expectError:     false,
		},
		{
			name:      "account ID with whitespace in blocklist",
			accountID: "111111111111",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "aws-account-operator-configmap",
					Namespace: OperatorNamespace,
				},
				Data: map[string]string{
					"payer-account-ids": " 111111111111 , 222222222222 ",
				},
			},
			createConfigMap: true,
			expectedResult:  true,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with or without ConfigMap
			var kubeClient client.Client
			if tt.createConfigMap {
				kubeClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(tt.configMap).
					Build()
			} else {
				kubeClient = fake.NewClientBuilder().
					WithScheme(scheme).
					Build()
			}

			// Call function
			isPayer, err := IsPayerAccount(tt.accountID, kubeClient)

			// Check error
			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Check result
			if isPayer != tt.expectedResult {
				t.Errorf("expected %v, got %v", tt.expectedResult, isPayer)
			}
		})
	}
}
