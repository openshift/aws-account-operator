package validation

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/openshift/aws-account-operator/pkg/apis"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func emptyOrganisation(ctrl *gomock.Controller) *mock.MockClient {
	mockClient := mock.NewMockClient(ctrl)
	mockClient.EXPECT().ListParents(&organizations.ListParentsInput{
		ChildId: aws.String("111111"),
	}).Return(&organizations.ListParentsOutput{
		Parents: []*organizations.Parent{},
	}, nil)
	return mockClient
}

func singleOrganisation(ctrl *gomock.Controller) *mock.MockClient {
	mockClient := mock.NewMockClient(ctrl)
	mockClient.EXPECT().ListParents(&organizations.ListParentsInput{
		ChildId: aws.String("111111"),
	}).Return(&organizations.ListParentsOutput{
		Parents: []*organizations.Parent{
			{
				Id:   aws.String("1"),
				Type: aws.String(""),
			}},
	}, nil)
	return mockClient
}

func multipleOrganisation(ctrl *gomock.Controller) *mock.MockClient {
	mockClient := mock.NewMockClient(ctrl)
	mockClient.EXPECT().ListParents(&organizations.ListParentsInput{
		ChildId: aws.String("111111"),
	}).Return(&organizations.ListParentsOutput{
		Parents: []*organizations.Parent{
			{
				Id:   aws.String("1"),
				Type: aws.String(""),
			},
			{
				Id:   aws.String("2"),
				Type: aws.String(""),
			},
		},
	}, nil)
	return mockClient
}

func alwaysTrue(s string) bool {
	return true
}

func alwaysFalse(s string) bool {
	return false
}

func TestParentsTillPredicate(t *testing.T) {
	ctrl := gomock.NewController(t)
	parents := []string{}
	type args struct {
		awsId   string
		client  awsclient.Client
		p       func(s string) bool
		parents *[]string
	}
	tests := []struct {
		name     string
		args     args
		expected *[]string
		wantErr  bool
	}{
		{
			name:     "No parents",
			args:     args{awsId: "111111", client: emptyOrganisation(ctrl), p: alwaysTrue, parents: &parents},
			expected: &[]string{},
			wantErr:  false,
		},
		{
			name:     "Single parent",
			args:     args{awsId: "111111", client: singleOrganisation(ctrl), p: alwaysTrue, parents: &parents},
			expected: &[]string{},
			wantErr:  false,
		},
		{
			name:     "Multiple parents are not expected",
			args:     args{awsId: "111111", client: multipleOrganisation(ctrl), p: alwaysTrue, parents: &parents},
			expected: &[]string{},
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parents = []string{}
			if err := ParentsTillPredicate(tt.args.awsId, tt.args.client, tt.args.p, tt.args.parents); (err != nil) != tt.wantErr {
				t.Errorf("ParentsTillP() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsAccountInRootOU(t *testing.T) {
	ctrl := gomock.NewController(t)
	// Simulate a root organization
	type args struct {
		account  awsv1alpha1.Account
		client   awsclient.Client
		isRootOU func(s string) bool
	}
	tests := []struct {
		name     string
		args     args
		expected bool
	}{
		{name: "Empty accountID", args: args{
			account:  awsv1alpha1.Account{},
			client:   mock.NewMockClient(ctrl),
			isRootOU: alwaysFalse,
		}, expected: false},
		{name: "Account is not in root", args: args{
			account: awsv1alpha1.Account{
				Spec: awsv1alpha1.AccountSpec{
					AwsAccountID: "111111",
				},
			},
			client:   emptyOrganisation(ctrl),
			isRootOU: alwaysFalse,
		}, expected: false},
		{name: "Account is in root", args: args{
			account: awsv1alpha1.Account{
				Spec: awsv1alpha1.AccountSpec{
					AwsAccountID: "111111",
				},
			},
			client:   singleOrganisation(ctrl),
			isRootOU: alwaysTrue,
		}, expected: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAccountInPoolOU(tt.args.account, tt.args.client, tt.args.isRootOU)
			if got != tt.expected {
				t.Errorf("IsAccountInRootOU() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestMoveAccount(t *testing.T) {
	ctrl := gomock.NewController(t)
	errorListParents := func(ctrl *gomock.Controller) *mock.MockClient {
		client := mock.NewMockClient(ctrl)
		client.EXPECT().ListParents(gomock.Any()).Return(nil, errors.New("something went wrong"))
		return client
	}
	type args struct {
		account     string
		client      awsclient.Client
		targetOU    string
		moveAccount bool
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{name: "No parent for account returns error", args: args{
			account:     "111111",
			client:      errorListParents(ctrl),
			targetOU:    "any",
			moveAccount: true,
		}, wantErr: true},
		{name: "Account not in target OU will trigger a move", args: args{
			account: "111111",
			client: func(client *mock.MockClient) *mock.MockClient {
				client.EXPECT().MoveAccount(&organizations.MoveAccountInput{
					AccountId:           aws.String("111111"),
					DestinationParentId: aws.String("targetOU"),
					SourceParentId:      aws.String("1"),
				}).Return(nil, nil)
				return client
			}(singleOrganisation(ctrl)),
			targetOU:    "targetOU",
			moveAccount: true,
		}, wantErr: false},
		{name: "Setting moveAccount false will prevent MoveAccount from being called", args: args{
			account:     "111111",
			client:      singleOrganisation(ctrl),
			targetOU:    "targetOU",
			moveAccount: false,
		}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := MoveAccount(tt.args.account, tt.args.client, tt.args.targetOU, tt.args.moveAccount); (err != nil) != tt.wantErr {
				t.Errorf("MoveAccount() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAccount_Reconcile(t *testing.T) {
	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in account_validation_controller_test.go")
	}
	ctrl := gomock.NewController(t)
	newBuilder := func(ctrl *gomock.Controller) awsclient.IBuilder {
		mockClient := mock.NewMockClient(ctrl)
		mockBuilder := mock.NewMockIBuilder(ctrl)
		mockBuilder.EXPECT().GetClient(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockClient, nil)
		return mockBuilder
	}
	type fields struct {
		Client           client.Client
		scheme           *runtime.Scheme
		awsClientBuilder awsclient.IBuilder
	}
	type args struct {
		request reconcile.Request
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    reconcile.Result
		wantErr bool
	}{
		{name: "Will not attempt to reconcile a CCS account.", fields: fields{
			Client: fake.NewFakeClient([]runtime.Object{
				&awsv1alpha1.Account{
					TypeMeta: v1.TypeMeta{
						Kind:       "Account",
						APIVersion: "v1alpha1",
					},
					ObjectMeta: v1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
					Spec: awsv1alpha1.AccountSpec{
						BYOC: true,
					},
				}, &corev1.ConfigMap{
					ObjectMeta: v1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					}}}...),
			scheme:           scheme.Scheme,
			awsClientBuilder: newBuilder(ctrl),
		}, args: args{
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "test",
				},
			},
		}, want: reconcile.Result{Requeue: false}, wantErr: false},
		{name: "Will not attempt to reconcile a account pool account.", fields: fields{
			Client: fake.NewFakeClient([]runtime.Object{
				&awsv1alpha1.Account{
					TypeMeta: v1.TypeMeta{
						Kind:       "Account",
						APIVersion: "v1alpha1",
					},
					ObjectMeta: v1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
						OwnerReferences: []v1.OwnerReference{
							{
								Kind: "AccountPool",
							},
						},
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: v1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
				}}...),
			scheme:           scheme.Scheme,
			awsClientBuilder: newBuilder(ctrl),
		}, args: args{
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "test",
				},
			},
		}, want: reconcile.Result{Requeue: false}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ValidateAccount{
				Client:           tt.fields.Client,
				scheme:           tt.fields.scheme,
				awsClientBuilder: tt.fields.awsClientBuilder,
			}
			got, err := r.Reconcile(tt.args.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAccount.Reconcile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !cmp.Equal(got, tt.want) {
				t.Errorf("ValidateAccount.Reconcile() = %v, want %v", got, tt.want)
			}
		})
	}
}
