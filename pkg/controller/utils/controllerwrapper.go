package utils

import (
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	awsv1alpha1 "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var controllerMaxReconciles map[string]int = map[string]int{}

func NewControllerWithMaxReconciles(log logr.Logger, controllerName string, mgr manager.Manager, r reconcile.Reconciler) (controller.Controller, error) {
	maxConcurrentReconciles, err := getControllerMaxReconciles(controllerName)
	if err != nil {
		log.Error(err, "")
	}
	return controller.New(fmt.Sprintf("%s-controller", controllerName), mgr, controller.Options{Reconciler: r, MaxConcurrentReconciles: maxConcurrentReconciles})
}

func InitControllerMaxReconciles(kubeClient client.Client) []error {
	controllers := []string{
		"account",
		"accountclaim",
		"accountpool",
		"awsfederatedaccountaccess",
		"awsfederatedrole",
	}
	controllerErrors := []error{}
	cm, err := GetOperatorConfigMap(kubeClient)
	if err != nil {
		controllerErrors = append(controllerErrors, err)
		return controllerErrors
	}

	for _, controller := range controllers {
		val, err := getControllerMaxReconcilesFromCM(cm, controller)
		if err != nil {
			controllerErrors = append(controllerErrors, fmt.Errorf("Error getting Max Reconciles for %s controller", controller))
			continue
		}
		controllerMaxReconciles[controller] = val
	}

	return controllerErrors
}

// getControllerMaxReconcilesFromCM gets the max reconciles for a given controller out of the config map
func getControllerMaxReconcilesFromCM(cm *corev1.ConfigMap, controllerName string) (int, error) {
	cmKey := fmt.Sprintf("MaxReconciles.%s", controllerName)
	if val, ok := cm.Data[cmKey]; ok {
		return strconv.Atoi(val)
	}
	return 0, awsv1alpha1.ErrInvalidConfigMap
}

// getControllerMaxReconciles gets the default configMap and then gets the amount of concurrent reconciles to run from it
func getControllerMaxReconciles(controllerName string) (int, error) {
	if _, ok := controllerMaxReconciles[controllerName]; !ok {
		return 1, fmt.Errorf("Controller %s not present in config data", controllerName)
	}
	return controllerMaxReconciles[controllerName], nil
}
