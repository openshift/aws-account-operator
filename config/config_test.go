package config

import (
	"testing"

	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
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
