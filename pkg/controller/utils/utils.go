package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws/awserr"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	Finalizer = "finalizer.aws.managed.openshift.io"
	WaitTime  = 25

	// EnvDevMode is the name of the env var we set to run locally and to skip
	// initialization procedures that will error out and exit the operator.
	// ex: `FORCE_DEV_MODE=local operatorsdk up local`
	EnvDevMode = "FORCE_DEV_MODE"
)

// The JSON tags as captials due to requirements for the policydoc
type awsStatement struct {
	Effect    string                 `json:"Effect"`
	Action    []string               `json:"Action"`
	Resource  []string               `json:"Resource,omitempty"`
	Condition *awsv1alpha1.Condition `json:"Condition,omitempty"`
	Principal *awsv1alpha1.Principal `json:"Principal,omitempty"`
}

// DetectDevMode gets the environment variable to detect if we are running
// locally or (future) have some other environment-specific conditions.
var DetectDevMode string = strings.ToLower(os.Getenv(EnvDevMode))

type awsPolicy struct {
	Version   string
	Statement []awsStatement
}

// MarshalIAMPolicy converts a role CR into a JSON policy that is acceptable to AWS
func MarshalIAMPolicy(role awsv1alpha1.AWSFederatedRole) (string, error) {
	statements := []awsStatement{}

	for _, statement := range role.Spec.AWSCustomPolicy.Statements {
		statements = append(statements, awsStatement(statement))
	}

	// Create a aws policydoc formated struct
	policyDoc := awsPolicy{
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

func Contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func Remove(list []string, s string) []string {
	for i, v := range list {
		if v == s {
			list = append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// GenerateShortUID Generates a short UID
func GenerateShortUID() string {
	UID := rand.String(6)
	return fmt.Sprintf("%s", UID)
}

// GenerateLabel returns a ObjectMeta Labels
func GenerateLabel(key, value string) map[string]string {
	return map[string]string{key: value}
}

// JoinLabelMaps adds a label to CR
func JoinLabelMaps(m1, m2 map[string]string) map[string]string {

	for key, value := range m2 {
		m1[key] = value
	}
	return m1
}

// AccountCRHasIAMUserIDLabel check for label
func AccountCRHasIAMUserIDLabel(accountCR *awsv1alpha1.Account) bool {

	// Check if the UID label exists and is set
	if _, ok := accountCR.Labels[awsv1alpha1.IAMUserIDLabel]; ok {
		return true
	}

	return false
}
