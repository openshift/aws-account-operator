package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	// Finalizer is a constant containing the Kubernetes finalizer used by the AWS Account Operator
	Finalizer = "finalizer.aws.managed.openshift.io"

	// WaitTime is the default wait time for an account to become ready, before erroring
	WaitTime = 25

	// envDevMode is the name of the env var we set to indicate we're running in a development
	// environment vs. production. Set it to one of the DevMode* consts defined below.
	// For example:
	// * Running locally: `FORCE_DEV_MODE=local operator-sdk run --local`
	// * Running in a cluster:
	//   . Edit deploy/operator.yaml. Under .spec.template.spec.env, add:
	//         - name: FORCE_DEV_MODE
	//           value: cluster
	//   . `oc apply` all the YAML files in deploy/, including the updated operator.yaml.
	envDevMode = "FORCE_DEV_MODE"

	// SREAssumeRole is the name of the role that SREs need to use to role-chain to get temporary
	// credentials for a CCS account
	SREAssumeRole = "RH-SREP-CCS-Access"

	// AwsSecretName is a constant for the name of the Kubernetes secret that holds the AWS Credentials
	AwsSecretName = "aws-account-operator-credentials" // #nosec G101 -- This is a false positive
)

// operatorStartTime is (roughly) the time at which the operator came up.
// It can be used e.g. to discover whether CR conditions occurred during the lifetime of this
// invocation or a previous one.
var operatorStartTime metav1.Time

// InitOperatorStartTime should be invoked exactly once, early in operator bringup, before
// controllers are started. The value can subsequently be retrieved via GetOperatorStartTime().
// Returns an error if the time was already initialized.
func InitOperatorStartTime() error {
	if !operatorStartTime.IsZero() {
		// Don't do this. No, really.
		return errors.New("Attempt to initialize start time twice")
	}
	operatorStartTime = metav1.Now()
	return nil
}

// GetOperatorStartTime returns the time at which the operator was brought up, assuming said
// bringup was well-behaved and invoked InitOperatorStartTime(). If not, this will return the
// zero time.
func GetOperatorStartTime() *metav1.Time {
	// Should we panic if operatorStartTime.IsZero()?
	return &operatorStartTime
}

// The JSON tags as capitals due to requirements for the policydoc
type awsStatement struct {
	Effect    string                 `json:"Effect"`
	Action    []string               `json:"Action"`
	Resource  []string               `json:"Resource,omitempty"`
	Condition *awsv1alpha1.Condition `json:"Condition,omitempty"`
	Principal *awsv1alpha1.Principal `json:"Principal,omitempty"`
}

// devMode exists so we can pseudo-enum allowable values for the FORCE_DEV_MODE environment variable.
type devMode string

const (
	// DevModeProduction (aka non-development mode) is the default running mode. Metrics are
	// served from the operator at the /metrics path under the route it creates. AWS support cases
	// are managed for real.
	DevModeProduction devMode = ""
	// DevModeLocal should be used when running via operator-sdk in "local" mode. Metrics are
	// served up at http://localhost:${metricsPort}/${metricsPath} (metricsP* defined in main.go).
	// All AWS support case interactions are skipped.
	DevModeLocal devMode = "local"
	// DevModeCluster should be used when doing development in a "real" cluster via a Deployment
	// such as the one in deploy/operator.yaml. Metrics are served as normal (see
	// DevModeProduction), but AWS support case interactions are skipped (see DevModeLocal).
	DevModeCluster devMode = "cluster"
)

// DetectDevMode gets the envDevMode environment variable to detect if we are running
// in production or a development environment.
var DetectDevMode devMode = devMode(strings.ToLower(os.Getenv(envDevMode)))

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

// AddLabels adds a map of labels to an object
func AddLabels(object metav1.Object, labels map[string]string) {
	existingLabels := object.GetLabels()
	object.SetLabels(JoinLabelMaps(labels, existingLabels))
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

// Contains returns true a list of strings includes a specific string
func Contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Remove removes a string from a list of strings
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
	return rand.String(6)
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

// GetOperatorConfigMap retrieves the default configMap data for the AWS Account Operator from Kubernetes
func GetOperatorConfigMap(kubeClient client.Client) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	err := kubeClient.Get(
		context.TODO(),
		types.NamespacedName{Namespace: awsv1alpha1.AccountCrNamespace,
			Name: awsv1alpha1.DefaultConfigMap}, configMap)
	return configMap, err
}

func GetEnvironmentBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	cast, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return cast
}

func DoNotRequeue() (reconcile.Result, error) {
	return reconcile.Result{Requeue: false}, nil
}

func RequeueWithError(err error) (reconcile.Result, error) {
	return reconcile.Result{}, err
}

func RequeueAfter(after time.Duration) (reconcile.Result, error) {
	return reconcile.Result{Requeue: true, RequeueAfter: after}, nil
}
