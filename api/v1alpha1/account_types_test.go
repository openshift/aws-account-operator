package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestAccount_AllRegionsExistInOptInRegions(t *testing.T) {
	type fields struct {
		TypeMeta   metav1.TypeMeta
		ObjectMeta metav1.ObjectMeta
		Spec       AccountSpec
		Status     AccountStatus
	}
	type args struct {
		regionList []string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "Testing When Region List Is Nil",
			fields: fields{
				// Set the necessary field values for the test case
				ObjectMeta: metav1.ObjectMeta{OwnerReferences: nil},
				Spec:       AccountSpec{AccountPool: ""},
				Status: AccountStatus{
					OptInRegions: OptInRegions{
						"af-south-1": &OptInRegionStatus{
							Status: OptInRequestTodo,
						},
					},
				},
			},
			want: true,
			args: args{regionList: nil},
		},
		{
			name: "Testing When All Regions Present In Region List Exist In Account.Status.OptInRegions",
			fields: fields{
				// Set the necessary field values for the test case
				ObjectMeta: metav1.ObjectMeta{OwnerReferences: nil},
				Spec:       AccountSpec{AccountPool: ""},
				Status: AccountStatus{
					OptInRegions: OptInRegions{
						"af-south-1": &OptInRegionStatus{
							Status: OptInRequestTodo,
						},
					},
				},
			},
			want: true,
			args: args{
				regionList: []string{"af-south-1"},
			},
		},
		{
			name: "Testing When Region Is Present In Region List But Absent in Account.Status.OptInRegions",
			fields: fields{
				// Set the necessary field values for the test case
				ObjectMeta: metav1.ObjectMeta{OwnerReferences: nil},
				Spec:       AccountSpec{AccountPool: ""},
				Status: AccountStatus{
					OptInRegions: OptInRegions{
						"af-south-1": &OptInRegionStatus{
							Status: OptInRequestEnabled,
						},
					},
				},
			},
			want: false,
			args: args{
				regionList: []string{"af-south-1", "ap-southeast-4"},
			},
		},
		{
			name: "Testing Nil Account.Status against Multiple Regions Present in Region List",
			fields: fields{
				// Set the necessary field values for the test case
				ObjectMeta: metav1.ObjectMeta{OwnerReferences: nil},
				Spec:       AccountSpec{AccountPool: ""},
				Status:     AccountStatus{},
			},
			want: false,
			args: args{
				regionList: []string{"af-south-1", "ap-east-1", "ca-west-1"},
			},
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
			if got := a.AllRegionsExistInOptInRegions(tt.args.regionList); got != tt.want {
				t.Errorf("AllRegionsExistInOptInRegions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_Account_HasOpenQuotaIncreaseRequests(t *testing.T) {
	tests := []struct {
		name   string
		status AccountStatus
		want   bool
	}{
		{
			name:   "empty regional and global quotas",
			status: AccountStatus{},
			want:   false,
		},
		{
			name: "regional quota completed, no global",
			status: AccountStatus{
				RegionalServiceQuotas: RegionalServiceQuotas{
					"us-east-1": AccountServiceQuota{
						RunningStandardInstances: {Value: 100, Status: ServiceRequestCompleted},
					},
				},
			},
			want: false,
		},
		{
			name: "regional quota in progress, no global",
			status: AccountStatus{
				RegionalServiceQuotas: RegionalServiceQuotas{
					"us-east-1": AccountServiceQuota{
						RunningStandardInstances: {Value: 100, Status: ServiceRequestInProgress},
					},
				},
			},
			want: true,
		},
		{
			name: "regional completed, global TODO",
			status: AccountStatus{
				RegionalServiceQuotas: RegionalServiceQuotas{
					"us-east-1": AccountServiceQuota{
						RunningStandardInstances: {Value: 100, Status: ServiceRequestCompleted},
					},
				},
				GlobalServiceQuotas: AccountServiceQuota{
					IAMRolesPerAccount: {Value: 5000, Status: ServiceRequestTodo},
				},
			},
			want: true,
		},
		{
			name: "no regional, global in progress",
			status: AccountStatus{
				GlobalServiceQuotas: AccountServiceQuota{
					IAMRolesPerAccount: {Value: 5000, Status: ServiceRequestInProgress},
				},
			},
			want: true,
		},
		{
			name: "no regional, global completed",
			status: AccountStatus{
				GlobalServiceQuotas: AccountServiceQuota{
					IAMRolesPerAccount: {Value: 5000, Status: ServiceRequestCompleted},
				},
			},
			want: false,
		},
		{
			name: "no regional, global unknown (MANUAL_ACTION)",
			status: AccountStatus{
				GlobalServiceQuotas: AccountServiceQuota{
					IAMRolesPerAccount: {Value: 5000, Status: ServiceRequestUnknown},
				},
			},
			want: false,
		},
		{
			name: "no regional, global denied",
			status: AccountStatus{
				GlobalServiceQuotas: AccountServiceQuota{
					IAMRolesPerAccount: {Value: 5000, Status: ServiceRequestDenied},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Account{Status: tt.status}
			assert.Equal(t, tt.want, a.HasOpenQuotaIncreaseRequests())
		})
	}
}

func Test_Account_GetGlobalQuotaRequestsByStatus(t *testing.T) {
	tests := []struct {
		name      string
		global    AccountServiceQuota
		stati     []ServiceRequestStatus
		wantCount int
		wantCodes []SupportedServiceQuotas
	}{
		{
			name:      "nil global quotas",
			global:    nil,
			stati:     []ServiceRequestStatus{ServiceRequestTodo},
			wantCount: 0,
		},
		{
			name:      "empty global quotas",
			global:    AccountServiceQuota{},
			stati:     []ServiceRequestStatus{ServiceRequestTodo},
			wantCount: 0,
		},
		{
			name: "single match",
			global: AccountServiceQuota{
				IAMRolesPerAccount: {Value: 5000, Status: ServiceRequestTodo},
			},
			stati:     []ServiceRequestStatus{ServiceRequestTodo},
			wantCount: 1,
			wantCodes: []SupportedServiceQuotas{IAMRolesPerAccount},
		},
		{
			name: "no match",
			global: AccountServiceQuota{
				IAMRolesPerAccount: {Value: 5000, Status: ServiceRequestCompleted},
			},
			stati:     []ServiceRequestStatus{ServiceRequestTodo},
			wantCount: 0,
		},
		{
			name: "match with multiple filter statuses",
			global: AccountServiceQuota{
				IAMRolesPerAccount: {Value: 5000, Status: ServiceRequestInProgress},
			},
			stati:     []ServiceRequestStatus{ServiceRequestTodo, ServiceRequestInProgress},
			wantCount: 1,
			wantCodes: []SupportedServiceQuotas{IAMRolesPerAccount},
		},
		{
			name: "denied status filtered correctly",
			global: AccountServiceQuota{
				IAMRolesPerAccount: {Value: 5000, Status: ServiceRequestDenied},
			},
			stati:     []ServiceRequestStatus{ServiceRequestDenied},
			wantCount: 1,
			wantCodes: []SupportedServiceQuotas{IAMRolesPerAccount},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Account{
				Status: AccountStatus{GlobalServiceQuotas: tt.global},
			}
			count, result := a.GetGlobalQuotaRequestsByStatus(tt.stati...)
			assert.Equal(t, tt.wantCount, count)
			for _, code := range tt.wantCodes {
				assert.Contains(t, result, code)
			}
			if tt.wantCount == 0 {
				assert.Empty(t, result)
			}
		})
	}
}
