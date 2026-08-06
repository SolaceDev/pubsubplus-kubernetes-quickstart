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
	"context"
	"testing"

	eventbrokerv1beta1 "github.com/SolaceProducts/pubsubplus-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testTlsSecret(name, namespace, resourceVersion string, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
		},
		Data: data,
		Type: corev1.SecretTypeTLS,
	}
}

func testBrokerWithTls(namespace string, tlsEnabled bool, tlsSecretName string) *eventbrokerv1beta1.PubSubPlusEventBroker {
	return &eventbrokerv1beta1.PubSubPlusEventBroker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-broker",
			Namespace: namespace,
		},
		Spec: eventbrokerv1beta1.EventBrokerSpec{
			BrokerTLS: eventbrokerv1beta1.BrokerTLS{
				Enabled:               tlsEnabled,
				ServerTLsConfigSecret: tlsSecretName,
			},
		},
	}
}

func TestSecretDataHashStableAcrossMetadataChanges(t *testing.T) {
	data := map[string][]byte{
		"tls.crt": []byte("dummy-cert"),
		"tls.key": []byte("dummy-key"),
	}
	original := testTlsSecret("my-tls-secret", "default", "1000", data)
	rewritten := testTlsSecret("my-tls-secret", "default", "2000", data)
	rewritten.Labels = map[string]string{"rotated": "true"}
	rewritten.Annotations = map[string]string{"kubectl.kubernetes.io/last-applied-configuration": "{}"}

	if secretDataHash(original) != secretDataHash(rewritten) {
		t.Errorf("secretDataHash changed on a content-identical rewrite: %q vs %q",
			secretDataHash(original), secretDataHash(rewritten))
	}
	if secretDataHash(original) == "" {
		t.Error("secretDataHash returned empty string for a valid secret")
	}
}

func TestSecretDataHashChangesOnContentChange(t *testing.T) {
	base := map[string][]byte{
		"tls.crt": []byte("dummy-cert"),
		"tls.key": []byte("dummy-key"),
	}
	baseHash := secretDataHash(testTlsSecret("my-tls-secret", "default", "1000", base))

	valueChanged := map[string][]byte{
		"tls.crt": []byte("rotated-cert"),
		"tls.key": []byte("dummy-key"),
	}
	keyAdded := map[string][]byte{
		"tls.crt": []byte("dummy-cert"),
		"tls.key": []byte("dummy-key"),
		"ca.crt":  []byte("dummy-ca"),
	}
	keyRemoved := map[string][]byte{
		"tls.crt": []byte("dummy-cert"),
	}

	for name, data := range map[string]map[string][]byte{
		"value changed": valueChanged,
		"key added":     keyAdded,
		"key removed":   keyRemoved,
	} {
		if secretDataHash(testTlsSecret("my-tls-secret", "default", "1000", data)) == baseHash {
			t.Errorf("secretDataHash did not change when secret data had a %s", name)
		}
	}
}

func TestSecretDataHashSaltedWithNamespaceAndName(t *testing.T) {
	data := map[string][]byte{"tls.crt": []byte("dummy-cert")}
	a := secretDataHash(testTlsSecret("secret-a", "default", "1", data))
	b := secretDataHash(testTlsSecret("secret-b", "default", "1", data))
	c := secretDataHash(testTlsSecret("secret-a", "other-ns", "1", data))
	if a == b || a == c {
		t.Error("secretDataHash must differ for same data under different secret name or namespace")
	}
}

func TestTlsSecretHash(t *testing.T) {
	data := map[string][]byte{"tls.crt": []byte("dummy-cert"), "tls.key": []byte("dummy-key")}
	secret := testTlsSecret("my-tls-secret", "default", "1000", data)
	r := &PubSubPlusEventBrokerReconciler{
		Client: fake.NewClientBuilder().WithObjects(secret).Build(),
	}
	ctx := context.Background()

	t.Run("returns content hash and resourceVersion when TLS enabled and secret exists", func(t *testing.T) {
		contentHash, resourceVersion := r.tlsSecretHash(ctx, testBrokerWithTls("default", true, "my-tls-secret"))
		if contentHash != secretDataHash(secret) {
			t.Errorf("contentHash = %q, want secretDataHash of the secret %q", contentHash, secretDataHash(secret))
		}
		if resourceVersion == "" {
			t.Error("resourceVersion should be returned for upgrade compatibility")
		}
		if contentHash == resourceVersion {
			t.Error("contentHash must not be the resourceVersion — that is the bug being fixed")
		}
	})

	t.Run("returns empty when TLS disabled even if secret name is set", func(t *testing.T) {
		contentHash, resourceVersion := r.tlsSecretHash(ctx, testBrokerWithTls("default", false, "my-tls-secret"))
		if contentHash != "" || resourceVersion != "" {
			t.Errorf("expected empty results with TLS disabled, got (%q, %q)", contentHash, resourceVersion)
		}
	})

	t.Run("returns empty when no secret name configured", func(t *testing.T) {
		contentHash, resourceVersion := r.tlsSecretHash(ctx, testBrokerWithTls("default", true, ""))
		if contentHash != "" || resourceVersion != "" {
			t.Errorf("expected empty results with no secret configured, got (%q, %q)", contentHash, resourceVersion)
		}
	})

	t.Run("returns empty when secret is missing", func(t *testing.T) {
		contentHash, resourceVersion := r.tlsSecretHash(ctx, testBrokerWithTls("default", true, "no-such-secret"))
		if contentHash != "" || resourceVersion != "" {
			t.Errorf("expected empty results for missing secret, got (%q, %q)", contentHash, resourceVersion)
		}
	})
}

func TestTlsSecretUpToDate(t *testing.T) {
	tests := []struct {
		name                  string
		recorded              string
		expectedTlsSecretHash string
		legacyResourceVersion string
		want                  bool
	}{
		{"content hash matches", "hash-abc", "hash-abc", "1000", true},
		{"legacy resourceVersion matches (pre-upgrade pod)", "1000", "hash-abc", "1000", true},
		{"neither matches (real cert change)", "hash-old", "hash-new", "2000", false},
		{"no check when expected hash empty", "anything", "", "", true},
		{"empty legacy resourceVersion is not a match", "", "hash-abc", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tlsSecretUpToDate(tt.recorded, tt.expectedTlsSecretHash, tt.legacyResourceVersion); got != tt.want {
				t.Errorf("tlsSecretUpToDate(%q, %q, %q) = %v, want %v",
					tt.recorded, tt.expectedTlsSecretHash, tt.legacyResourceVersion, got, tt.want)
			}
		})
	}
}

func TestBrokerPodOutdated(t *testing.T) {
	pod := func(specHash, tlsAnnotation string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					brokerSpecSignatureAnnotationName: specHash,
					tlsSecretSignatureAnnotationName:  tlsAnnotation,
				},
			},
		}
	}

	tests := []struct {
		name                  string
		pod                   *corev1.Pod
		expectedBrokerHash    string
		expectedTlsSecretHash string
		legacyResourceVersion string
		want                  bool
	}{
		{"up to date", pod("spec1", "hash-abc"), "spec1", "hash-abc", "1000", false},
		{"outdated on brokerSpec mismatch alone", pod("spec-old", "hash-abc"), "spec-new", "hash-abc", "1000", true},
		{"not outdated when annotation holds legacy resourceVersion", pod("spec1", "1000"), "spec1", "hash-abc", "1000", false},
		{"outdated on TLS content change", pod("spec1", "hash-old"), "spec1", "hash-new", "2000", true},
		{"TLS check skipped when secret unreadable", pod("spec1", "hash-abc"), "spec1", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := brokerPodOutdated(tt.pod, tt.expectedBrokerHash, tt.expectedTlsSecretHash, tt.legacyResourceVersion); got != tt.want {
				t.Errorf("brokerPodOutdated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBrokerStsOutdated(t *testing.T) {
	sts := func(specHash, tlsAnnotation string) *appsv1.StatefulSet {
		return &appsv1.StatefulSet{
			Spec: appsv1.StatefulSetSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							brokerSpecSignatureAnnotationName: specHash,
							tlsSecretSignatureAnnotationName:  tlsAnnotation,
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name                  string
		sts                   *appsv1.StatefulSet
		expectedBrokerHash    string
		expectedTlsSecretHash string
		legacyResourceVersion string
		want                  bool
	}{
		{"up to date", sts("spec1", "hash-abc"), "spec1", "hash-abc", "1000", false},
		{"outdated on brokerSpec mismatch alone", sts("spec-old", "hash-abc"), "spec-new", "hash-abc", "1000", true},
		{"not outdated when annotation holds legacy resourceVersion", sts("spec1", "1000"), "spec1", "hash-abc", "1000", false},
		{"outdated on TLS content change", sts("spec1", "hash-old"), "spec1", "hash-new", "2000", true},
		{"TLS check skipped when secret unreadable", sts("spec1", "hash-abc"), "spec1", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := brokerStsOutdated(tt.sts, tt.expectedBrokerHash, tt.expectedTlsSecretHash, tt.legacyResourceVersion); got != tt.want {
				t.Errorf("brokerStsOutdated() = %v, want %v", got, tt.want)
			}
		})
	}
}
