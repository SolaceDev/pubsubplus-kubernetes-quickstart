/*
Copyright 2023 Solace Corporation

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
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"hash/crc64"
	"strconv"

	eventbrokerv1beta1 "github.com/SolaceProducts/pubsubplus-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	//go:embed brokerscripts configs
	scripts embed.FS
)

// Returns the broker pod in the specified role
func (r *PubSubPlusEventBrokerReconciler) getBrokerPod(ctx context.Context, m *eventbrokerv1beta1.PubSubPlusEventBroker, brokerRole BrokerRole) (*corev1.Pod, error) {
	// List the pods for this pubsubpluseventbroker
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(m.Namespace),
		client.MatchingLabels(getBrokerPodSelector(m.Name, brokerRole)),
	}
	if err := r.List(ctx, podList, listOpts...); err != nil {
		return nil, err
	}
	if podList != nil && len(podList.Items) == 1 {
		return &podList.Items[0], nil
	}
	return nil, fmt.Errorf("filtered broker pod list for broker role %d didn't return exactly one pod", brokerRole)
}

// Returns a hash of the TLS secret contents plus its resourceVersion (kept only for upgrade compatibility, see tlsSecretUpToDate).
func (r *PubSubPlusEventBrokerReconciler) tlsSecretHash(ctx context.Context, m *eventbrokerv1beta1.PubSubPlusEventBroker) (contentHash string, resourceVersion string) {
	if !m.Spec.BrokerTLS.Enabled || m.Spec.BrokerTLS.ServerTLsConfigSecret == "" {
		return "", ""
	}
	foundSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: m.Spec.BrokerTLS.ServerTLsConfigSecret, Namespace: m.Namespace}, foundSecret)
	if err != nil {
		return "", ""
	}
	return secretDataHash(foundSecret), foundSecret.ResourceVersion
}

func secretDataHash(secret *corev1.Secret) string {
	serialized, err := json.Marshal(secret.Data)
	if err != nil {
		return ""
	}
	salted := fmt.Sprintf("%s/%s|%s", secret.Namespace, secret.Name, serialized)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(salted)))
}

func tlsSecretUpToDate(recorded string, expectedTlsSecretHash string, legacyResourceVersion string) bool {
	if expectedTlsSecretHash == "" {
		return true
	}
	return recorded == expectedTlsSecretHash ||
		(legacyResourceVersion != "" && recorded == legacyResourceVersion)
}

func brokerSpecHash(s eventbrokerv1beta1.EventBrokerSpec) string {
	brokerSpecSubset := s.DeepCopy()
	// Mask anything that is not relevant to the StatefulSet / broker Pods
	brokerSpecSubset.Monitoring = eventbrokerv1beta1.Monitoring{}
	brokerSpecSubset.Service.Annotations = nil
	brokerSpecSubset.Service.ServiceType = corev1.ServiceTypeLoadBalancer       // cannot use nil, setting it a constant value
	brokerSpecSubset.Redundancy = false                                         // change of redundancy is not supported for now
	brokerSpecSubset.ServiceAccount = eventbrokerv1beta1.BrokerServiceAccount{} // change of SA is not supported
	brokerSpecSubset.AdminCredentialsSecret = ""
	brokerSpecSubset.PreSharedAuthKeySecret = ""
	brokerSpecSubset.PodDisruptionBudgetForHA = false // does not affect the statefulset/pod
	return hash(brokerSpecSubset.String())
}

func monitoringSpecHash(s eventbrokerv1beta1.EventBrokerSpec) string {
	brokerSpecSubset := s.DeepCopy()
	return hash(brokerSpecSubset.Monitoring.String())
}

func brokerServiceHash(s eventbrokerv1beta1.EventBrokerSpec) string {
	brokerServiceSubset := s.Service.DeepCopy()
	return hash(brokerServiceSubset.String())
}

func brokerServiceOutdated(service *corev1.Service, expectedBrokerServiceHash string) bool {
	result := service.ObjectMeta.Annotations[brokerServiceSignatureAnnotationName] != expectedBrokerServiceHash
	return result
}

func brokerStsOutdated(sts *appsv1.StatefulSet, expectedBrokerSpecHash string, expectedTlsSecretHash string, legacyTlsSecretResourceVersion string) bool {
	if sts.Spec.Template.ObjectMeta.Annotations[brokerSpecSignatureAnnotationName] != expectedBrokerSpecHash {
		return true
	}
	return !tlsSecretUpToDate(sts.Spec.Template.ObjectMeta.Annotations[tlsSecretSignatureAnnotationName], expectedTlsSecretHash, legacyTlsSecretResourceVersion)
}

func brokerPodOutdated(pod *corev1.Pod, expectedBrokerSpecHash string, expectedTlsSecretHash string, legacyTlsSecretResourceVersion string) bool {
	if pod.ObjectMeta.Annotations[brokerSpecSignatureAnnotationName] != expectedBrokerSpecHash {
		return true
	}
	return !tlsSecretUpToDate(pod.ObjectMeta.Annotations[tlsSecretSignatureAnnotationName], expectedTlsSecretHash, legacyTlsSecretResourceVersion)
}

func brokerMonitoringOutdated(monitoring *appsv1.Deployment, expectedMonitoringSpecHash string) bool {
	return monitoring.ObjectMeta.Annotations[monitoringSpecSignatureAnnotationName] != expectedMonitoringSpecHash
}

func convertToByteArray(e any) []byte {
	var network bytes.Buffer        // Stand-in for a network connection
	enc := gob.NewEncoder(&network) // Will write to network.
	err := enc.Encode(e)
	if err != nil {
		return nil
	}
	return network.Bytes()
}

func hash(s any) string {
	crc64Table := crc64.MakeTable(crc64.ECMA)
	return strconv.FormatUint(crc64.Checksum(convertToByteArray(s), crc64Table), 16)
}

func parseScalingParameterWithUnKnownFieldsToMap(scalingParameter *eventbrokerv1beta1.SystemScaling) map[string]interface{} {
	var scalingParamMap map[string]interface{}
	marshalScalingParameter, _ := json.Marshal(scalingParameter)
	json.Unmarshal(marshalScalingParameter, &scalingParamMap)
	return scalingParamMap
}
