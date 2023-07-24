package awsfederatedrole

import (
	"context"
	"fmt"
	apis "github.com/openshift/aws-account-operator/api"
	"github.com/openshift/aws-account-operator/api/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

const testRoleName = "test-role"

func setupKubeClientMock(localObjects []runtime.Object) client.WithWatch {
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(localObjects...).Build()
}

func generateAccountAccesses(num int) []runtime.Object {
	var objects []runtime.Object
	for i := 0; i < num; i++ {
		objects = append(objects, &v1alpha1.AWSFederatedAccountAccess{
			ObjectMeta: v1.ObjectMeta{
				Labels:    map[string]string{v1alpha1.FederatedRoleNameLabel: testRoleName},
				Name:      fmt.Sprintf("testAccount%v", i),
				Namespace: "testNamespace",
			},
		})
	}
	return objects
}

func TestAWSFederatedRoleReconciler_annotateAccountAccesses(t *testing.T) {
	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in awsfederatedrole_controller_test.go")
	}

	tests := []struct {
		name         string
		localObjects []runtime.Object
	}{
		{
			name:         "no account accesses",
			localObjects: []runtime.Object{},
		},
		{
			name:         "single account access",
			localObjects: generateAccountAccesses(1),
		},
		{
			name:         "multiple account accesses",
			localObjects: generateAccountAccesses(10),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeKubeClient := setupKubeClientMock(tt.localObjects)
			if err := annotateAccountAccesses(fakeKubeClient, testRoleName); err != nil {
				t.Errorf("annotateAccountAccesses() error = %v, wantErr %v", err, false)
			}

			accountAccesses := &v1alpha1.AWSFederatedAccountAccessList{}
			_ = fakeKubeClient.List(context.TODO(), accountAccesses, client.MatchingLabels{v1alpha1.FederatedRoleNameLabel: testRoleName})
			for _, account := range accountAccesses.Items {
				if _, ok := account.Annotations[v1alpha1.LastRoleUpdateAnnotation]; !ok {
					t.Errorf("annotateAccountAccesses() failed to add annotation to account %s", account.Name)
				}
			}
		})
	}
}
