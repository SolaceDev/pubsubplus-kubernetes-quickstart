/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	eventbrokerv1alpha1 "github.com/SolaceProducts/pubsubplus-operator/api/v1alpha1"
)

func (r *EventBrokerReconciler) discoveryserviceForEventBroker(m *eventbrokerv1alpha1.EventBroker) *corev1.Service {
	svcName := m.Name + "-pubsubplus-discovery"
	dep := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:                       svcName,
			Namespace:                  m.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/instance":   m.Name,
				"app.kubernetes.io/name":       "eventbroker",
				"app.kubernetes.io/managed-by": "solace-pubsubplus-operator",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports:                         []corev1.ServicePort{
				{
					Name:        "tcp-semp",
					Port:        8080,
				},
				{
					Name:        "tcp-ha-mate-link",
					Port:        8741,
				},
				{
					Name:        "tcp-ha-conf-sync0",
					Port:        8300,
				},
				{
					Name:        "tcp-ha-conf-sync1",
					Port:        8301,
				},
				{
					Name:        "tcp-ha-conf-sync2",
					Port:        8302,
				},
			},
			Selector:                      map[string]string{
				"app.kubernetes.io/instance":   m.Name,
				"app.kubernetes.io/name":       "eventbroker",
			},
			ClusterIP:                     corev1.ClusterIPNone,
			PublishNotReadyAddresses:      true,
		},
	}
	// Set EventBroker instance as the owner and controller
	ctrl.SetControllerReference(m, dep, r.Scheme)
	return dep
}