package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func Test_Account_IsOwnedByAccountPool(t *testing.T) {
	type fields struct {
		TypeMeta   metav1.TypeMeta
		ObjectMeta metav1.ObjectMeta
		Spec       AccountSpec
		Status     AccountStatus
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "Testing Missing Account OwnerRerference and Account.Spec.AccounPool Field",
			fields: fields{
				// Set the necessary field values for the test case
				ObjectMeta: metav1.ObjectMeta{OwnerReferences: nil},
				Spec:       AccountSpec{AccountPool: ""},
			},
			want: false,
		},
		{
			name: "Testing Missing OwnerRerference and Valid Account.Spec.AccounPool Field",
			fields: fields{
				// Set the necessary field values for the test case
				ObjectMeta: metav1.ObjectMeta{OwnerReferences: nil},
				Spec:       AccountSpec{AccountPool: "test-account-pool"},
			},
			want: true,
		},
		{
			name: "Testing Missing Account.Spec.AccounPool Field and Valid OwnerRerference",
			fields: fields{
				TypeMeta: metav1.TypeMeta{},
				// Set the necessary field values for the test case
				Spec: AccountSpec{AccountPool: ""},
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{{
						Kind: "AccountPool",
					},
					},
				},
			},
			want: true,
		},
		{
			name: "Testing Valid OwnerRerference and Account.Spec.AccounPool",
			fields: fields{
				TypeMeta: metav1.TypeMeta{},
				// Set the necessary field values for the test case
				Spec: AccountSpec{AccountPool: "test-account-pool"},
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{{
						Kind: "AccountPool",
					},
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Account{
				TypeMeta:   tt.fields.TypeMeta,
				ObjectMeta: tt.fields.ObjectMeta,
				Spec:       tt.fields.Spec,
				Status:     tt.fields.Status,
			}
			if got := a.IsOwnedByAccountPool(); got != tt.want {
				t.Errorf("IsOwnedByAccountPool() = %v, want %v", got, tt.want)
			}
		})
	}
}
