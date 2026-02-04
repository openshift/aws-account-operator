package validation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	organizationstypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/google/go-cmp/cmp"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apis "github.com/openshift/aws-account-operator/api"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
)

func emptyOrganisation(ctrl *gomock.Controller) *mock.MockClient {
	mockClient := mock.NewMockClient(ctrl)
	mockClient.EXPECT().ListParents(gomock.Any(), &organizations.ListParentsInput{
		ChildId: aws.String("111111"),
	}).Return(&organizations.ListParentsOutput{
		Parents: []organizationstypes.Parent{},
	}, nil)
	return mockClient
}

func singleOrganisation(ctrl *gomock.Controller) *mock.MockClient {
	mockClient := mock.NewMockClient(ctrl)
	mockClient.EXPECT().ListParents(gomock.Any(), &organizations.ListParentsInput{
		ChildId: aws.String("111111"),
	}).Return(&organizations.ListParentsOutput{
		Parents: []organizationstypes.Parent{
			{
				Id:   aws.String("1"),
				Type: organizationstypes.ParentType(""),
			}},
	}, nil)
	return mockClient
}

func multipleOrganisation(ctrl *gomock.Controller) *mock.MockClient {
	mockClient := mock.NewMockClient(ctrl)
	mockClient.EXPECT().ListParents(gomock.Any(), &organizations.ListParentsInput{
		ChildId: aws.String("111111"),
	}).Return(&organizations.ListParentsOutput{
		Parents: []organizationstypes.Parent{
			{
				Id:   aws.String("1"),
				Type: organizationstypes.ParentType(""),
			},
			{
				Id:   aws.String("2"),
				Type: organizationstypes.ParentType(""),
			},
		},
	}, nil)
	return mockClient
}

func designatedOrganization(ctrl *gomock.Controller, ouID string) *mock.MockClient {
	mockClient := mock.NewMockClient(ctrl)
	mockClient.EXPECT().ListParents(gomock.Any(), &organizations.ListParentsInput{
		ChildId: aws.String("111111"),
	}).Return(&organizations.ListParentsOutput{
		Parents: []organizationstypes.Parent{
			{
				Id:   aws.String(ouID),
				Type: organizationstypes.ParentType(""),
			}},
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

func TestIsAccountInCorrectOU(t *testing.T) {
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
			got := IsAccountInCorrectOU(tt.args.account, tt.args.client, tt.args.isRootOU)
			if got != tt.expected {
				t.Errorf("IsAccountInCorrectOU() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestMoveAccount(t *testing.T) {
	ctrl := gomock.NewController(t)
	errorListParents := func(ctrl *gomock.Controller) *mock.MockClient {
		client := mock.NewMockClient(ctrl)
		client.EXPECT().ListParents(gomock.Any(), gomock.Any()).Return(nil, errors.New("something went wrong"))
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
				client.EXPECT().MoveAccount(gomock.Any(), &organizations.MoveAccountInput{
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

func TestValidateAccountOU(t *testing.T) {
	ctrl := gomock.NewController(t)

	testPoolOUID := "ou-abcd-efghijk"
	testBaseOUID := "ou-lmno-qrstuvwxyz"
	testLegalEntityOUID := "ou-aabb-ccddeeff"

	legalEntity := awsv1alpha1.LegalEntity{
		ID:   "abcdefg",
		Name: "Test Entity ID",
	}

	notHandledError := fmt.Errorf("Some random error")
	tests := []struct {
		name      string
		awsClient awsclient.Client
		account   awsv1alpha1.Account
		wantErr   error
		ouMap     map[string]string
	}{
		{
			name:      "Account that has never been claimed and is in pool OU should return no errors",
			awsClient: designatedOrganization(ctrl, testPoolOUID),
			account: awsv1alpha1.Account{
				Spec: awsv1alpha1.AccountSpec{
					AwsAccountID: "111111",
				},
			},
			wantErr: nil,
			ouMap:   map[string]string{},
		}, {
			name: "Account that has been claimed before and is in legalEntity OU should return no error",
			awsClient: func(client *mock.MockClient) *mock.MockClient {
				client.EXPECT().ListOrganizationalUnitsForParent(gomock.Any(), &organizations.ListOrganizationalUnitsForParentInput{
					ParentId: aws.String(testBaseOUID),
				}).Return(&organizations.ListOrganizationalUnitsForParentOutput{
					OrganizationalUnits: []organizationstypes.OrganizationalUnit{
						{
							Name: aws.String(legalEntity.ID),
							Id:   aws.String(testLegalEntityOUID),
						},
					},
				}, nil)
				return client
			}(designatedOrganization(ctrl, testLegalEntityOUID)),
			account: awsv1alpha1.Account{
				Spec: awsv1alpha1.AccountSpec{
					AwsAccountID: "111111",
					LegalEntity:  legalEntity,
				},
			},
			wantErr: nil,
			ouMap:   map[string]string{},
		}, {
			name: "Account has been claimed before and is in OU Map",
			awsClient: func(client *mock.MockClient) *mock.MockClient {
				return client
			}(designatedOrganization(ctrl, testLegalEntityOUID)),
			account: awsv1alpha1.Account{
				Spec: awsv1alpha1.AccountSpec{
					AwsAccountID: "111111",
					LegalEntity:  legalEntity,
				},
			},
			wantErr: nil,
			ouMap:   map[string]string{legalEntity.ID: testLegalEntityOUID},
		}, {
			name: "When encountering an error listing parents when getting OU ID from name it should return the error",
			awsClient: func() *mock.MockClient {
				mockClient := mock.NewMockClient(ctrl)
				mockClient.EXPECT().ListOrganizationalUnitsForParent(gomock.Any(), &organizations.ListOrganizationalUnitsForParentInput{
					ParentId: aws.String(testBaseOUID),
				}).Return(&organizations.ListOrganizationalUnitsForParentOutput{}, notHandledError)
				return mockClient
			}(),
			account: awsv1alpha1.Account{
				Spec: awsv1alpha1.AccountSpec{
					AwsAccountID: "111111",
					LegalEntity:  legalEntity,
				},
			},
			wantErr: fmt.Errorf("unexpected error attempting to get OU ID for legal entity"),
			ouMap:   map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &AccountValidationReconciler{}
			r.OUNameIDMap = tt.ouMap
			err := r.ValidateAccountOU(tt.awsClient, tt.account, testPoolOUID, testBaseOUID)
			if err != tt.wantErr {
				var ave *AccountValidationError
				if errors.As(err, &ave) {
					if ave.Err.Error() == tt.wantErr.Error() {
						return
					}
				}
				t.Errorf("Error validating account OU. Got: %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAccountOrigin(t *testing.T) {
	type args struct {
		account awsv1alpha1.Account
	}
	tests := []struct {
		name        string
		args        args
		wantErr     bool
		expectedErr string
	}{
		{
			name: "Account is BYOC",
			args: args{
				account: awsv1alpha1.Account{
					ObjectMeta: v1.ObjectMeta{
						Name:      "testaccount",
						Namespace: "testnamespace",
					},
					Spec: awsv1alpha1.AccountSpec{
						BYOC: true,
					},
					Status: awsv1alpha1.AccountStatus{
						State: string(awsv1alpha1.AccountReady),
					},
				},
			},
			wantErr:     true,
			expectedErr: "account is a CCS account",
		},
		{
			name: "Account is not in ready state",
			args: args{
				account: awsv1alpha1.Account{
					ObjectMeta: v1.ObjectMeta{
						Name:      "testaccount",
						Namespace: "testnamespace",
					},
					Spec: awsv1alpha1.AccountSpec{
						BYOC: false,
					},
					Status: awsv1alpha1.AccountStatus{
						State: string(awsv1alpha1.AccountCreating),
					},
				},
			},
			wantErr:     true,
			expectedErr: "account is not in a ready state",
		},
		{
			name: "Valid account origin",
			args: args{
				account: awsv1alpha1.Account{
					ObjectMeta: v1.ObjectMeta{
						Name:      "testaccount",
						Namespace: "testnamespace",
					},
					Spec: awsv1alpha1.AccountSpec{
						BYOC: false,
					},
					Status: awsv1alpha1.AccountStatus{
						State: string(awsv1alpha1.AccountReady),
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateAccountOrigin(tt.args.account); (err != nil) != tt.wantErr {
				t.Errorf("ValidateAccountOrigin() error = %v, wantErr %v", err, tt.wantErr)
			} else {
				if tt.wantErr {
					err, ok := err.(*AccountValidationError)
					if !ok {
						t.Errorf("ValidateAccountOrigin() error, expected AccountValidationError")
					}
					if err.Type != InvalidAccount {
						t.Errorf("ValidateAccountOrigin() error, expected error of type InvalidAccount but was %v", err.Type)
					}
					if err.Err.Error() != tt.expectedErr {
						t.Errorf("ValidateAccountOrigin() error, did not get correct error message")
					}
				}
			}
		})
	}
}

func TestValidateAccount_ValidateAccountTags(t *testing.T) {
	ctrl := gomock.NewController(t)

	makeClient := func(output *organizations.ListTagsForResourceOutput, err error, willTag bool, tagErr error, willUntag bool, untagErr error) awsclient.Client {
		mockClient := mock.NewMockClient(ctrl)
		mockClient.EXPECT().ListTagsForResource(gomock.Any(), gomock.Any()).Return(output, err)
		// AlexVulaj: The `Times` values here don't seem to be honored, but I can't really figure out why.
		if willTag {
			mockClient.EXPECT().TagResource(gomock.Any(), gomock.Any()).Return(nil, tagErr).Times(1)
		}
		if willUntag {
			mockClient.EXPECT().UntagResource(gomock.Any(), gomock.Any()).Return(nil, untagErr).Times(1)
		}
		return mockClient
	}

	type args struct {
		client            awsclient.Client
		accountId         *string
		shardName         string
		accountTagEnabled bool
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "No owner tag - don't tag account",
			args: args{
				client: makeClient(&organizations.ListTagsForResourceOutput{
					Tags: []organizationstypes.Tag{},
				}, &AccountValidationError{
					Type: MissingTag,
					Err:  errors.New("account is not tagged with an owner"),
				}, false, nil, false, nil),
				accountId:         aws.String("1234"),
				shardName:         "",
				accountTagEnabled: false,
			},
			wantErr: true,
		},
		{
			name: "Incorrect owner tag - don't tag account",
			args: args{
				client: makeClient(&organizations.ListTagsForResourceOutput{
					Tags: []organizationstypes.Tag{
						{
							Key:   aws.String("owner"),
							Value: aws.String("shard1"),
						},
					},
				}, nil, false, nil, false, nil),
				accountId:         aws.String("1234"),
				shardName:         "shard2",
				accountTagEnabled: false,
			},
			wantErr: false,
		},
		{
			name: "No owner tag - tag account successfully",
			args: args{
				client: makeClient(&organizations.ListTagsForResourceOutput{
					Tags: []organizationstypes.Tag{},
				}, nil, true, nil, false, nil),
				accountId:         aws.String("1234"),
				shardName:         "shard1",
				accountTagEnabled: true,
			},
			wantErr: false,
		},
		{
			name: "No owner tag - tag account unsuccessfully",
			args: args{
				client: makeClient(&organizations.ListTagsForResourceOutput{
					Tags: []organizationstypes.Tag{},
				}, nil, true, errors.New("failed"), false, nil),
				accountId:         aws.String("1234"),
				shardName:         "shard1",
				accountTagEnabled: true,
			},
			wantErr: true,
		},
		{
			name: "Incorrect owner tag - tag account successfully",
			args: args{
				client: makeClient(&organizations.ListTagsForResourceOutput{
					Tags: []organizationstypes.Tag{
						{
							Key:   aws.String("owner"),
							Value: aws.String("shard1"),
						},
					},
				}, nil, true, nil, true, nil),
				accountId:         aws.String("1234"),
				shardName:         "shard2",
				accountTagEnabled: true,
			},
			wantErr: false,
		},
		{
			name: "Incorrect owner tag - untag account unsuccessfully",
			args: args{
				client: makeClient(&organizations.ListTagsForResourceOutput{
					Tags: []organizationstypes.Tag{
						{
							Key:   aws.String("owner"),
							Value: aws.String("shard1"),
						},
					},
				}, nil, false, nil, true, errors.New("failed")),
				accountId:         aws.String("1234"),
				shardName:         "shard2",
				accountTagEnabled: true,
			},
			wantErr: true,
		},
		{
			name: "Incorrect owner tag - tag account unsuccessfully",
			args: args{
				client: makeClient(&organizations.ListTagsForResourceOutput{
					Tags: []organizationstypes.Tag{
						{
							Key:   aws.String("owner"),
							Value: aws.String("shard1"),
						},
					},
				}, nil, true, errors.New("failed"), true, nil),
				accountId:         aws.String("1234"),
				shardName:         "shard2",
				accountTagEnabled: true,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateAccountTags(tt.args.client, tt.args.accountId, tt.args.shardName, tt.args.accountTagEnabled); (err != nil) != tt.wantErr {
				t.Errorf("ValidateAccountTags() error = %v, wantErr %v", err, tt.wantErr)
			} else {
				if tt.wantErr {
					err, ok := err.(*AccountValidationError)
					if !ok {
						t.Errorf("ValidateAccountTags() error, expected error of type AccountValidationError but was %v", err.Type)
					}
					if err.Type == MissingTag && err.Err.Error() != "account is not tagged with an owner" {
						t.Errorf("ValidateAccountTags() error, did not get correct error message")
					}
					if err.Type == IncorrectOwnerTag && err.Err.Error() != "account is not tagged with the correct owner" {
						t.Errorf("ValidateAccountTags() error, did not get correct error message")
					}
					if err.Type == AccountTagFailed && err.Err.Error() != "failed" {
						t.Errorf("ValidateAccountTags() error, did not get correct error message")
					}
				}
			}
		})
	}
}

func TestValidateAccount_ValidateComplianceTags(t *testing.T) {
	ctrl := gomock.NewController(t)

	makeClient := func(output *organizations.ListTagsForResourceOutput, err error, willTag bool, tagErr error) awsclient.Client {
		mockClient := mock.NewMockClient(ctrl)
		mockClient.EXPECT().ListTagsForResource(gomock.Any(), gomock.Any()).Return(output, err)
		if willTag {
			mockClient.EXPECT().TagResource(gomock.Any(), gomock.Any()).Return(nil, tagErr).Times(1)
		}
		return mockClient
	}

	type args struct {
		client                awsclient.Client
		accountId             *string
		shardName             string
		accountTagEnabled     bool
		appCode               string
		servicePhase          string
		costCenter            string
		complianceTagsEnabled bool
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Feature disabled - should skip validation",
			args: args{
				client: func() awsclient.Client {
					// When feature is disabled, ListTagsForResource should not be called
					mockClient := mock.NewMockClient(ctrl)
					return mockClient
				}(),
				accountId:             aws.String("1234"),
				shardName:             "shard1",
				accountTagEnabled:     true,
				appCode:               "OSD-002",
				servicePhase:          "prod",
				costCenter:            "148",
				complianceTagsEnabled: false,
			},
			wantErr: false,
		},
		{
			name: "Compliance tags missing - should add them",
			args: args{
				client: makeClient(&organizations.ListTagsForResourceOutput{
					Tags: []organizationstypes.Tag{
						{
							Key:   aws.String("owner"),
							Value: aws.String("shard1"),
						},
					},
				}, nil, true, nil),
				accountId:             aws.String("1234"),
				shardName:             "shard1",
				accountTagEnabled:     true,
				appCode:               "OSD-002",
				servicePhase:          "prod",
				costCenter:            "148",
				complianceTagsEnabled: true,
			},
			wantErr: false,
		},
		{
			name: "Compliance tags incorrect - should update them",
			args: args{
				client: makeClient(&organizations.ListTagsForResourceOutput{
					Tags: []organizationstypes.Tag{
						{
							Key:   aws.String("app-code"),
							Value: aws.String("WRONG-CODE"),
						},
						{
							Key:   aws.String("service-phase"),
							Value: aws.String("stage"),
						},
					},
				}, nil, true, nil),
				accountId:             aws.String("1234"),
				shardName:             "shard1",
				accountTagEnabled:     true,
				appCode:               "OSD-002",
				servicePhase:          "prod",
				costCenter:            "148",
				complianceTagsEnabled: true,
			},
			wantErr: false,
		},
		{
			name: "All compliance tags correct - no changes needed",
			args: args{
				client: makeClient(&organizations.ListTagsForResourceOutput{
					Tags: []organizationstypes.Tag{
						{
							Key:   aws.String("app-code"),
							Value: aws.String("OSD-002"),
						},
						{
							Key:   aws.String("service-phase"),
							Value: aws.String("prod"),
						},
						{
							Key:   aws.String("cost-center"),
							Value: aws.String("148"),
						},
					},
				}, nil, false, nil),
				accountId:             aws.String("1234"),
				shardName:             "shard1",
				accountTagEnabled:     true,
				appCode:               "OSD-002",
				servicePhase:          "prod",
				costCenter:            "148",
				complianceTagsEnabled: true,
			},
			wantErr: false,
		},
		{
			name: "Tags missing but tagging disabled - should not error",
			args: args{
				client: makeClient(&organizations.ListTagsForResourceOutput{
					Tags: []organizationstypes.Tag{},
				}, nil, false, nil),
				accountId:             aws.String("1234"),
				shardName:             "shard1",
				accountTagEnabled:     false,
				appCode:               "OSD-002",
				servicePhase:          "prod",
				costCenter:            "148",
				complianceTagsEnabled: true,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateComplianceTags(tt.args.client, tt.args.accountId, tt.args.shardName, tt.args.accountTagEnabled, tt.args.appCode, tt.args.servicePhase, tt.args.costCenter, tt.args.complianceTagsEnabled); (err != nil) != tt.wantErr {
				t.Errorf("ValidateComplianceTags() error = %v, wantErr %v", err, tt.wantErr)
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
			Client: fake.NewClientBuilder().WithRuntimeObjects([]runtime.Object{
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
						AwsAccountID: "123456",
						BYOC:         true,
					},
				}, &corev1.ConfigMap{
					ObjectMeta: v1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					}}}...).Build(),
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
		{name: "Will not attempt to reconcile a non-account pool account.", fields: fields{
			Client: fake.NewClientBuilder().WithRuntimeObjects([]runtime.Object{
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
						AwsAccountID: "123456",
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: v1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
				}}...).Build(),
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
		{name: "Will not attempt to reconcile a account without an AwsAccountID.", fields: fields{
			Client: fake.NewClientBuilder().WithRuntimeObjects([]runtime.Object{
				&awsv1alpha1.Account{
					TypeMeta: v1.TypeMeta{
						Kind:       "Account",
						APIVersion: "v1alpha1",
					},
					ObjectMeta: v1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: v1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
				}}...).Build(),
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
		{name: "Will not attempt to reconcile a account thas is being deleted.", fields: fields{
			Client: fake.NewClientBuilder().WithRuntimeObjects([]runtime.Object{
				&awsv1alpha1.Account{
					TypeMeta: v1.TypeMeta{
						Kind:       "Account",
						APIVersion: "v1alpha1",
					},
					ObjectMeta: v1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
						DeletionTimestamp: &v1.Time{
							Time: time.Now(),
						},
					},
					Spec: awsv1alpha1.AccountSpec{
						AwsAccountID: "123456",
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: v1.ObjectMeta{
						Name:      awsv1alpha1.DefaultConfigMap,
						Namespace: awsv1alpha1.AccountCrNamespace,
					},
				}}...).Build(),
			scheme:           scheme.Scheme,
			awsClientBuilder: nil,
		}, args: args{
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "default",
					Name:      "test",
				},
			},
		}, want: reconcile.Result{Requeue: false}, wantErr: false},
		{
			name: "When an account has no AWS account ID and is failed it stop reconciliation",
			fields: fields{
				Client: fake.NewClientBuilder().WithRuntimeObjects([]runtime.Object{
					&awsv1alpha1.Account{
						TypeMeta: v1.TypeMeta{
							Kind:       "Account",
							APIVersion: "v1alpha1",
						},
						ObjectMeta: v1.ObjectMeta{
							Name:      "test",
							Namespace: "default",
							DeletionTimestamp: &v1.Time{
								Time: time.Now(),
							},
						},
						Spec: awsv1alpha1.AccountSpec{
							AwsAccountID: "",
						},
						Status: awsv1alpha1.AccountStatus{
							State: string(awsv1alpha1.AccountFailed),
						},
					},
					&corev1.ConfigMap{
						ObjectMeta: v1.ObjectMeta{
							Name:      awsv1alpha1.DefaultConfigMap,
							Namespace: awsv1alpha1.AccountCrNamespace,
						},
					}}...).Build(),
				scheme:           scheme.Scheme,
				awsClientBuilder: nil,
			},
			args: args{
				request: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "test",
					},
				},
			},
			want: reconcile.Result{Requeue: false}, wantErr: false},
		{
			name: "Reconcile reads compliance tags from ConfigMap when feature flag is enabled (CCS account)",
			fields: fields{
				Client: fake.NewClientBuilder().WithRuntimeObjects([]runtime.Object{
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
							AwsAccountID: "123456789012",
							BYOC:         true, // CCS account won't be validated
						},
						Status: awsv1alpha1.AccountStatus{
							State: string(awsv1alpha1.AccountReady),
						},
					},
					&corev1.ConfigMap{
						ObjectMeta: v1.ObjectMeta{
							Name:      awsv1alpha1.DefaultConfigMap,
							Namespace: awsv1alpha1.AccountCrNamespace,
						},
						Data: map[string]string{
							"root":                    "ou-root-123",
							"base":                    "ou-base-456",
							"shard-name":              "test-shard",
							"feature.compliance_tags": "true",
							"app-code":                "OSD-002",
							"service-phase":           "prod",
							"cost-center":             "148",
						},
					}}...).Build(),
				scheme:           scheme.Scheme,
				awsClientBuilder: newBuilder(ctrl),
			},
			args: args{
				request: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "test",
					},
				},
			},
			want: reconcile.Result{Requeue: false}, wantErr: false},
		{
			name: "Reconcile skips compliance tags when feature flag is disabled (CCS account)",
			fields: fields{
				Client: fake.NewClientBuilder().WithRuntimeObjects([]runtime.Object{
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
							AwsAccountID: "123456789012",
							BYOC:         true, // CCS account won't be validated
						},
						Status: awsv1alpha1.AccountStatus{
							State: string(awsv1alpha1.AccountReady),
						},
					},
					&corev1.ConfigMap{
						ObjectMeta: v1.ObjectMeta{
							Name:      awsv1alpha1.DefaultConfigMap,
							Namespace: awsv1alpha1.AccountCrNamespace,
						},
						Data: map[string]string{
							"root":                    "ou-root-123",
							"base":                    "ou-base-456",
							"shard-name":              "test-shard",
							"feature.compliance_tags": "false",
							"app-code":                "OSD-002",
							"service-phase":           "prod",
							"cost-center":             "148",
						},
					}}...).Build(),
				scheme:           scheme.Scheme,
				awsClientBuilder: newBuilder(ctrl),
			},
			args: args{
				request: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "test",
					},
				},
			},
			want: reconcile.Result{Requeue: false}, wantErr: false},
		{
			name: "Reconcile handles missing compliance tag values gracefully (CCS account)",
			fields: fields{
				Client: fake.NewClientBuilder().WithRuntimeObjects([]runtime.Object{
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
							AwsAccountID: "123456789012",
							BYOC:         true, // CCS account won't be validated
						},
						Status: awsv1alpha1.AccountStatus{
							State: string(awsv1alpha1.AccountReady),
						},
					},
					&corev1.ConfigMap{
						ObjectMeta: v1.ObjectMeta{
							Name:      awsv1alpha1.DefaultConfigMap,
							Namespace: awsv1alpha1.AccountCrNamespace,
						},
						Data: map[string]string{
							"root":                    "ou-root-123",
							"base":                    "ou-base-456",
							"shard-name":              "test-shard",
							"feature.compliance_tags": "true",
							// Don't set app-code, service-phase, cost-center
						},
					}}...).Build(),
				scheme:           scheme.Scheme,
				awsClientBuilder: newBuilder(ctrl),
			},
			args: args{
				request: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "test",
					},
				},
			},
			want: reconcile.Result{Requeue: false}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &AccountValidationReconciler{
				Client:           tt.fields.Client,
				Scheme:           tt.fields.scheme,
				awsClientBuilder: tt.fields.awsClientBuilder,
			}
			got, err := r.Reconcile(context.TODO(), tt.args.request)
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

func TestValidateAwsAccountId(t *testing.T) {
	type args struct {
		account awsv1alpha1.Account
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Throws an error if no AwsAccountId is found",
			args: args{
				account: awsv1alpha1.Account{
					TypeMeta: v1.TypeMeta{
						Kind:       "Account",
						APIVersion: "v1alpha1",
					},
					ObjectMeta: v1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Returns nil when an AwsAccountId is present",
			args: args{
				account: awsv1alpha1.Account{
					TypeMeta:   v1.TypeMeta{Kind: "Account", APIVersion: "v1alpha1"},
					ObjectMeta: v1.ObjectMeta{Name: "test", Namespace: "default"},
					Spec: awsv1alpha1.AccountSpec{
						AwsAccountID: "123456",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateAwsAccountId(tt.args.account); (err != nil) != tt.wantErr {
				t.Errorf("ValidateAwsAccountAssociated() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRemoval(t *testing.T) {
	type args struct {
		account awsv1alpha1.Account
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "will not remove a non-failed account",
			args: args{
				account: awsv1alpha1.Account{
					TypeMeta:   v1.TypeMeta{},
					ObjectMeta: v1.ObjectMeta{},
					Spec:       awsv1alpha1.AccountSpec{},
					Status: awsv1alpha1.AccountStatus{
						State: "Ready",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "will not remove an account with an AWS account ID",
			args: args{
				account: awsv1alpha1.Account{
					TypeMeta:   v1.TypeMeta{},
					ObjectMeta: v1.ObjectMeta{},
					Spec: awsv1alpha1.AccountSpec{
						AwsAccountID: "1234567",
					},
					Status: awsv1alpha1.AccountStatus{
						State: "Failed",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "will remove a failed account without AWS account ID",
			args: args{
				account: awsv1alpha1.Account{
					TypeMeta:   v1.TypeMeta{},
					ObjectMeta: v1.ObjectMeta{},
					Spec: awsv1alpha1.AccountSpec{
						AwsAccountID: "",
					},
					Status: awsv1alpha1.AccountStatus{
						State: "Failed",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateRemoval(tt.args.account); (err != nil) != tt.wantErr {
				t.Errorf("ValidateRemoval() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
