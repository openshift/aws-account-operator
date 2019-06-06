// Copyright 2019 RedHat
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

import (
	"context"
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	v1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("metrics")

// Custom errors

// ErrMetricsFailedGenerateService indicates the metric service failed to generate
var ErrMetricsFailedGenerateService = errors.New("FailedGeneratingService")

// ErrMetricsFailedCreateService indicates that the service failed to create
var ErrMetricsFailedCreateService = errors.New("FailedCreateService")

// ErrMetricsFailedCreateServiceMonitor indicates that an account creation failed
var ErrMetricsFailedCreateServiceMonitor = errors.New("FailedCreateServiceMonitor")

// GenerateService returns the static service which exposes specifed port.
func GenerateService(port int32, portName string) (*v1.Service, error) {
	operatorName, err := k8sutil.GetOperatorName()
	if err != nil {
		return nil, err
	}
	namespace, err := k8sutil.GetOperatorNamespace()
	if err != nil {
		return nil, err
	}

	label := map[string]string{"name": operatorName}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorName,
			Namespace: namespace,
			Labels:    label,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Port:     port,
					Protocol: v1.ProtocolTCP,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: port,
					},
					Name: portName,
				},
			},
			Selector: label,
		},
	}

	return service, nil
}

// GenerateServiceMonitor generates a prometheus-operator ServiceMonitor object
// based on the passed Service object.
func GenerateServiceMonitor(s *v1.Service) *monitoringv1.ServiceMonitor {
	labels := make(map[string]string)
	for k, v := range s.ObjectMeta.Labels {
		labels[k] = v
	}

	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceMonitor",
			APIVersion: "monitoring.coreos.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.ObjectMeta.Name,
			Namespace: s.ObjectMeta.Namespace,
			Labels:    labels,
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: labels,
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port: s.Spec.Ports[0].Name,
				},
			},
		},
	}
}

// ConfigureMetrics generates metrics service and servicemonitor,
// creates the metrics service and service monitor,
// and finally it starts the metrics server
func ConfigureMetrics(ctx context.Context) error {
	log.Info("Starting prometheus metrics")
	StartMetrics()

	client, err := createClient()
	if err != nil {
		log.Info("Failed to create new client", "Error", err.Error())
		return nil
	}

	// Generate Service Object
	s, svcerr := GenerateService(8080, "metrics")
	if svcerr != nil {
		log.Info("Error generating metrics service object.", "Error", svcerr.Error())
		return ErrMetricsFailedGenerateService
	}
	log.Info("Generated metrics service object")

	// Create or update Service
	_, err = createOrUpdateService(ctx, client, s)
	if err != nil {
		log.Info("Error getting current metrics service", "Error", err.Error())
		return ErrMetricsFailedCreateService
	}

	log.Info("Created Service")

	// Generate ServiceMonitor Object
	sm := GenerateServiceMonitor(s)
	log.Info("Generated metrics servicemonitor object")

	// Create or Update the ServiceMonitor
	err = client.Create(ctx, sm)
	if err != nil {
		if k8serr.IsAlreadyExists(err) {
			// update the servicemonitor
			if smUpdateErr := client.Update(ctx, sm); smUpdateErr != nil {
				log.Info("Error creating metrics servicemonitor", "Error", smUpdateErr.Error())
				return ErrMetricsFailedCreateServiceMonitor
			}
			log.Info("Metrics ServiceMonitor object updated", "ServiceMonitor.Name", sm.Name, "ServiceMonitor.Namespace", sm.Namespace)
			return nil
		}
		log.Info("Error creating metrics servicemonitor", "Error", err.Error())
		return ErrMetricsFailedCreateServiceMonitor

	}
	log.Info("Metrics ServiceMonitor object Created", "ServiceMonitor.Name", sm.Name, "ServiceMonitor.Namespace", sm.Namespace)
	return nil
}

func createOrUpdateService(ctx context.Context, client client.Client, s *v1.Service) (*v1.Service, error) {
	if err := client.Create(ctx, s); err != nil {
		if !k8serr.IsAlreadyExists(err) {
			return nil, err
		}
		// Service already exists, we want to update it
		// as we do not know if any fields might have changed.
		existingService := &v1.Service{}
		err := client.Get(ctx, types.NamespacedName{
			Name:      s.Name,
			Namespace: s.Namespace,
		}, existingService)

		s.ResourceVersion = existingService.ResourceVersion
		if existingService.Spec.Type == v1.ServiceTypeClusterIP {
			s.Spec.ClusterIP = existingService.Spec.ClusterIP
		}
		err = client.Update(ctx, s)
		if err != nil {
			return nil, err
		}
		log.Info("Metrics Service object updated", "Service.Name", s.Name, "Service.Namespace", s.Namespace)
		return existingService, nil
	}

	log.Info("Metrics Service object created", "Service.Name", s.Name, "Service.Namespace", s.Namespace)
	return s, nil
}

func createClient() (client.Client, error) {
	config, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	client, err := client.New(config, client.Options{})
	if err != nil {
		return nil, err
	}

	return client, nil
}
