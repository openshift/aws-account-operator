package utils

import (
	"encoding/json"
	"reflect"
	"testing"

	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
)

func createRoleMock(statements []awsv1alpha1.StatementEntry) awsv1alpha1.AWSFederatedRole {
	return awsv1alpha1.AWSFederatedRole{
		Spec: awsv1alpha1.AWSFederatedRoleSpec{
			AWSCustomPolicy: awsv1alpha1.AWSCustomPolicy{
				Name:        "MyPolicy",
				Description: "A policy for Testing",
				Statements:  statements,
			},
		},
	}
}

func TestMarshallingIAMPolicy(t *testing.T) {
	expected := awsStatement{
		Effect:   "Allow",
		Action:   []string{"ec2:DescribeInstances"},
		Resource: []string{"*"},
	}

	// Create AWSFederatedRole and pass that through the MarshalIAMPolicy fun to test for correctness
	statements := []awsv1alpha1.StatementEntry{
		{
			Effect: "Allow",
			Action: []string{
				"ec2:DescribeInstances",
			},
			Resource: []string{
				"*",
			},
		},
	}

	role := createRoleMock(statements)

	policyJSON, err := MarshalIAMPolicy(role)
	if err != nil {
		t.Errorf("There was an error marshalling the IAM Policy. %s", err)
	}

	// Convert the policy back to an object so we can run comparisons easier than
	// trying to do the same with a string.
	var policy awsPolicy
	err = json.Unmarshal([]byte(policyJSON), &policy)
	if err != nil {
		t.Errorf("There was an error unmarshalling the IAM Policy. %s", err)
	}

	if len(policy.Statement) != 1 {
		t.Errorf("Unexpected Statement Length.  Expected 1.  Got %d", len(policy.Statement))
	}

	statement := policy.Statement[0]

	if statement.Effect != expected.Effect {
		t.Errorf("Unexpected Effect.  Got: \n%s\n\n Expected:\n%s\n", statement.Effect, expected.Effect)
	}

	if len(statement.Action) != len(expected.Action) {
		t.Errorf("Unexpected Action Length.  Got: \n%s\n\nExpected: \n%s\n", statement.Action, expected.Action)
	}
	if statement.Action[0] != expected.Action[0] {
		t.Errorf("Unexected Action. Got: \n%s\n\n Expected:\n%s\n", statement.Action, expected.Action)
	}

	if len(statement.Resource) != len(expected.Resource) {
		t.Errorf("Unexpected Resource Length.  Got: \n%s\n\nExpected: \n%s\n", statement.Resource, expected.Resource)
	}
	if statement.Resource[0] != expected.Resource[0] {
		t.Errorf("Unexpected Resource. Got: \n%s\n\nExpected:\n%s\n", statement.Resource, expected.Resource)
	}
}

func TestMarshalingMultipleStatements(t *testing.T) {
	expectedList := []awsStatement{
		{
			Effect:   "Allow",
			Action:   []string{"ec2:DescribeInstances"},
			Resource: []string{"*"},
		},
		{
			Effect:   "Deny",
			Action:   []string{"iam:CreateRole"},
			Resource: []string{"*"},
		},
	}

	statements := []awsv1alpha1.StatementEntry{
		{
			Effect:   "Allow",
			Action:   []string{"ec2:DescribeInstances"},
			Resource: []string{"*"},
		},
		{
			Effect:   "Deny",
			Action:   []string{"iam:CreateRole"},
			Resource: []string{"*"},
		},
	}

	role := createRoleMock(statements)

	policyJSON, err := MarshalIAMPolicy(role)
	if err != nil {
		t.Errorf("There was an error marshalling the IAM Policy. %s", err)
	}

	// Convert the policy back to an object so we can run comparisons easier than
	// trying to do the same with a string.
	var policy awsPolicy
	err = json.Unmarshal([]byte(policyJSON), &policy)
	if err != nil {
		t.Errorf("There was an error unmarshalling the IAM Policy. %s", err)
	}

	if len(policy.Statement) != len(expectedList) {
		t.Errorf("Unexpected Statement Length.  Expected %d.  Got %d", len(expectedList), len(policy.Statement))
	}
}

func TestAddingConditionsToStatements(t *testing.T) {
	condition := &awsv1alpha1.Condition{
		StringEquals: map[string]string{"ram:RequestedResourceType": "route53resolver:ResolverRule"},
	}
	expected := awsStatement{
		Effect:    "Allow",
		Action:    []string{"ec2:DescribeInstances"},
		Resource:  []string{"*"},
		Condition: condition,
	}

	// Create AWSFederatedRole and pass that through the MarshalIAMPolicy fun to test for correctness
	statements := []awsv1alpha1.StatementEntry{
		{
			Effect: "Allow",
			Action: []string{
				"ec2:DescribeInstances",
			},
			Resource: []string{
				"*",
			},
			Condition: condition,
		},
	}

	role := createRoleMock(statements)

	policyJSON, err := MarshalIAMPolicy(role)
	if err != nil {
		t.Errorf("There was an error marshalling the IAM Policy. %s", err)
	}

	// Convert the policy back to an object so we can run comparisons easier than
	// trying to do the same with a string.
	var policy awsPolicy
	err = json.Unmarshal([]byte(policyJSON), &policy)
	if err != nil {
		t.Errorf("There was an error unmarshalling the IAM Policy. %s", err)
	}

	statement := policy.Statement[0]

	if statement.Condition.StringEquals == nil {
		t.Errorf("Condition Operator StringEquals not found.  Got:\n%s\n\nExpected:\n%s\n", expected.Condition, statement.Condition)
	}

	for key, value := range statement.Condition.StringEquals {
		if statement.Condition.StringEquals[key] == "" {
			t.Errorf("Conditional is not found.  Looking for: %s in %s", key, statement.Condition.StringEquals)
		}

		if statement.Condition.StringEquals[key] != value {
			t.Errorf("Unexected Condition. Got: \n%s\n\n Expected:\n%s\n", statement.Condition.StringEquals, expected.Condition.StringEquals)
		}
	}
}

func TestContains(t *testing.T) {
	tables := []struct {
		list   []string
		find   string
		result bool
	}{
		{[]string{}, "hello", false},
		{[]string{"hello"}, "hello", true},
		{[]string{"hello"}, "world", false},
	}

	for _, table := range tables {
		contained := Contains(table.list, table.find)
		if contained != table.result {
			var expected string
			var opposite string
			if table.result {
				expected = "found"
				opposite = "not found"
			} else {
				expected = "not found"
				opposite = "found"
			}
			t.Errorf("Expected %s to be %s.  Was %s in %s.", table.find, expected, opposite, table.list)
		}
	}
}

func TestRemove(t *testing.T) {
	tables := []struct {
		list   []string
		value  string
		result []string
	}{
		{[]string{}, "hello", []string{}},
		{[]string{"hello"}, "world", []string{"hello"}},
		{[]string{"hello", "world"}, "hello", []string{"world"}},
	}

	for _, table := range tables {
		postRemoveList := Remove(table.list, table.value)
		if !reflect.DeepEqual(postRemoveList, table.result) {
			t.Errorf("Unexpected Result.  Expected %s got %s", table.result, postRemoveList)
		}
	}
}
