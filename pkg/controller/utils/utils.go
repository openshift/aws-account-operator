package utils

import (
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go/aws/awserr"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	EmailID = "osd-creds-mgmt"
)

func MarshalIAMPolicy(role awsv1alpha1.AWSFederatedRole) (string, error) {
	// The JSON tags as captials due to requirements for the policydoc
	type awsStatement struct {
		Effect    string                 `json:"Effect"`
		Action    []string               `json:"Action"`
		Resource  []string               `json:"Resource,omitempty"`
		Principal *awsv1alpha1.Principal `json:"Principal,omitempty"`
	}

	statements := []awsStatement{}

	for _, statement := range role.Spec.AWSCustomPolicy.Statements {
		statements = append(statements, awsStatement(statement))
	}

	// Create a aws policydoc formated struct
	policyDoc := struct {
		Version   string
		Statement []awsStatement
	}{
		Version:   "2012-10-17",
		Statement: statements,
	}

	// Marshal policydoc to json
	jsonPolicyDoc, err := json.Marshal(&policyDoc)
	if err != nil {
		return "", err
	}

	return string(jsonPolicyDoc), nil
}

// GenerateAccountCR returns new account CR struct
func GenerateAccountCR(namespace string) *awsv1alpha1.Account {

	uuid := rand.String(6)
	accountName := EmailID + "-" + uuid

	return &awsv1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name:      accountName,
			Namespace: namespace,
		},
		Spec: awsv1alpha1.AccountSpec{
			AwsAccountID:  "",
			IAMUserSecret: "",
			ClaimLink:     "",
		},
	}
}

// AddFinalizer adds a finalizer to an object
func AddFinalizer(object metav1.Object, finalizer string) {
	finalizers := sets.NewString(object.GetFinalizers()...)
	finalizers.Insert(finalizer)
	object.SetFinalizers(finalizers.List())
}

// LogAwsError formats and logs aws error and returns if err was an awserr
func LogAwsError(logger logr.Logger, errMsg string, customError error, err error) {
	if aerr, ok := err.(awserr.Error); ok {
		if customError == nil {
			customError = aerr
		}

		logger.Error(customError,
			fmt.Sprintf(`%s,
				AWS Error Code: %s, 
				AWS Error Message: %s`,
				errMsg,
				aerr.Code(),
				aerr.Message()))
	}
}
