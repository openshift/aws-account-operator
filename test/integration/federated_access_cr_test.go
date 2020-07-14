package federatedaccesstesting

import (
	"encoding/base64"
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"os/exec"
	//	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
)

// Structs for Role CRs
type crStruct struct {
	Spec struct {
		Policy struct {
			Statements []statement `yaml:"awsStatements"`
		} `yaml:"awsCustomPolicy"`
	} `yaml:"spec"`
}

type statement struct {
	Effect    string                       `yaml:"effect"`
	Action    []string                     `yaml:"action"`
	Resource  []string                     `yaml:"resource"`
	Condition map[string]map[string]string `yaml:"condition"`
}

// Struct for FederatedAccountAccess CR
type federatedAccountAccess struct {
	Metadata struct {
		Labels struct {
			AccountID string `yaml:"awsAccountID"`
			UID       string `yaml:"uid"`
		} `yaml:"labels"`
	} `yaml:"metadata"`
	Spec struct {
		Role struct {
			Name string `yaml:"name"`
		} `yaml:"awsFederatedRole"`
	} `yaml:"spec"`
}

// Struct for Secret to use for aws calls
type awsUserSecret struct {
	Data struct {
		AccessKeyID     string `yaml:"aws_access_key_id"`
		SecretAccessKey string `yaml:"aws_secret_access_key"`
	} `yaml:"data"`
}

func TestFederatedAccessRolePermissions(t *testing.T) {
	cr := os.Getenv("TEST_CR")
	if cr == "" {
		t.Skip("TEST_CR is not set, skipping.")
	}

	crFile := os.Getenv("TEST_ROLE_FILE")
	if crFile == "" {
		t.Skip("TEST_ROLE_FILE is not set, skipping.")
	}

	crToTest := crStruct{}
	unmarshalFromFile(t, crFile, &crToTest)

	accountAccessCR := federatedAccountAccess{}
	getFederatedAccountAccessCR(t, cr, &accountAccessCR)

	awsSecret := awsUserSecret{}
	getSecretCredentials(t, &awsSecret)

	iamClient, err := getAWSIAMClient(t, awsSecret)
	if err != nil {
		t.Fatal("Unable to get AWS Client", err)
	}
	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s-%s", accountAccessCR.Metadata.Labels.AccountID, accountAccessCR.Spec.Role.Name, accountAccessCR.Metadata.Labels.UID)

	statements := crToTest.Spec.Policy.Statements
	for _, stmt := range statements {
		testAction(t, iamClient, roleARN, stmt)
	}
}

// builds the actions list for a statement
func buildActionList(stmt statement) []*string {
	actionList := []*string{}
	for _, action := range stmt.Action {
		actionList = append(actionList, aws.String(action))
	}
	return actionList
}

// builds the context list for a statement
func buildContextList(stmt statement) []*iam.ContextEntry {
	contextList := []*iam.ContextEntry{}

	// first check for conditions
	for key, condition := range stmt.Condition {
		if key == "StringEquals" {
			for contextKey, contextValue := range condition {
				contextList = append(contextList, &iam.ContextEntry{
					ContextKeyName:   aws.String(contextKey),
					ContextKeyType:   aws.String("string"),
					ContextKeyValues: []*string{aws.String(contextValue)},
				})
			}
		}
	}
	return contextList
}

// builds the list of resources for an action
func buildResourceList(stmt statement) []*string {
	resourceList := []*string{}
	for _, resource := range stmt.Resource {
		resourceList = append(resourceList, aws.String(resource))
	}
	return resourceList
}

// Runs the test simulating the policy
func testAction(t *testing.T, iamClient *iam.IAM, roleARN string, stmt statement) {
	actions := buildActionList(stmt)
	context := buildContextList(stmt)
	resources := buildResourceList(stmt)
	input := &iam.SimulatePrincipalPolicyInput{
		PolicySourceArn: aws.String(roleARN),
		ActionNames:     actions,
		ContextEntries:  context,
		ResourceArns:    resources,
	}
	err := iamClient.SimulatePrincipalPolicyPages(input, func(response *iam.SimulatePolicyResponse, lastPage bool) bool {
		for _, result := range response.EvaluationResults {
			if *result.EvalDecision != "allowed" {
				t.Errorf("%s is not allowed by RoleARN: %s\n-- Possibly a missing context or the action does not exist?\n%+v", *result.EvalActionName, roleARN, *result)
			}
		}
		return !lastPage
	})
	if err != nil {
		t.Fatal("Could not simulate policy.", err)
	}
}

// Unmarshals YAML from File
func unmarshalFromFile(t *testing.T, cr string, crToTest *crStruct) {
	file := "../../" + cr

	yamlFile, err := ioutil.ReadFile(file)
	if err != nil {
		t.Fatal("Unable to read from file: "+file, err)
	}

	err = yaml.Unmarshal(yamlFile, &crToTest)
	if err != nil {
		t.Fatal("Unable to Unmarshal YAML", err)
	}
}

// Fills federatedAccountAccess struct from given cr
func getFederatedAccountAccessCR(t *testing.T, cr string, accountAccessCR *federatedAccountAccess) {
	ocGet := exec.Command("oc", "get", "awsfederatedaccountaccess", "-n", "aws-account-operator", "-o", "yaml", cr)
	accountAccessYAML, err := ocGet.CombinedOutput()
	if err != nil {
		t.Fatal("Error getting AccountAccessYAML from oc get command")
	}
	err = yaml.Unmarshal(accountAccessYAML, accountAccessCR)
	if err != nil {
		t.Fatal("Unable to Unmarshal awsfederatedaccountaccess YAML", err)
	}
}

// Fills awsUserSecret struct from the secret
func getSecretCredentials(t *testing.T, secret *awsUserSecret) {
	ocSecret := exec.Command("oc", "get", "secret", "-n", "aws-account-operator", "-o", "yaml", "osd-creds-mgmt-osd-staging-1-osdmanagedadminsre-secret")
	secretYAML, err := ocSecret.CombinedOutput()
	if err != nil {
		t.Fatal("Unable to obtain sre-cli-credentials")
	}
	err = yaml.Unmarshal(secretYAML, secret)
	if err != nil {
		t.Fatal("Unable to Unmarshal Secret YAML")
	}
}

// Gets AWS Client using passed in credentials struct
func getAWSIAMClient(t *testing.T, awsCreds awsUserSecret) (*iam.IAM, error) {
	accessKeyID, err := base64.StdEncoding.DecodeString(awsCreds.Data.AccessKeyID)
	if err != nil {
		return nil, err
	}

	secretAccessKey, err := base64.StdEncoding.DecodeString(awsCreds.Data.SecretAccessKey)
	if err != nil {
		return nil, err
	}

	s, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewCredentials(&credentials.StaticProvider{
			Value: credentials.Value{
				AccessKeyID:     string(accessKeyID),
				SecretAccessKey: string(secretAccessKey),
			},
		}),
	})
	if err != nil {
		return nil, err
	}
	return iam.New(s), nil
}
