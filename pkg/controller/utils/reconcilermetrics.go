package utils

import (
	"time"

	"github.com/go-logr/logr"
	"github.com/openshift/aws-account-operator/pkg/localmetrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// NewReconcilerWithMetrics wraps an existing Reconciler such that calls to Reconcile report the
// reconcileDuration metric.
func NewReconcilerWithMetrics(wrapped reconcile.Reconciler, controllerName string) reconcile.Reconciler {
	return &reconcilerWithMetrics{
		wrappedReconciler: wrapped,
		controllerName:    controllerName,
		logger:            logf.Log.WithName("controller_"+controllerName).WithValues("Controller", controllerName),
	}
}

type reconcilerWithMetrics struct {
	wrappedReconciler reconcile.Reconciler
	controllerName    string
	logger            logr.Logger
}

// Reconcile implements Reconciler. It logs and reports duration metrics for the wrapped Reconciler.
func (rwm *reconcilerWithMetrics) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := rwm.logger.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling")

	start := time.Now()
	result, err := rwm.wrappedReconciler.Reconcile(request)
	dur := time.Since(start)
	localmetrics.Collector.SetReconcileDuration(rwm.controllerName, dur.Seconds())

	rwm.logger.WithValues("Duration", dur).Info("Reconcile complete")
	return result, err
}
