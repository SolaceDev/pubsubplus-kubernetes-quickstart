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
	pubsubplus "github.com/SolaceProducts/pubsubplus-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"testing"
	"time"
	//+kubebuilder:scaffold:imports
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	testEnv        *envtest.Environment
	k8sClient      runtimeClient.Client
	clientSet      *kubernetes.Clientset
	ctx            context.Context
	cancel         context.CancelFunc
	controllerName = "pubsubpluseventbroker-operator"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Pubsubplus Operator Test Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))

	ctx, cancel = context.WithCancel(ctrl.SetupSignalHandler())

	By("bootstrapping test environment")
	useCluster := true
	testEnv = &envtest.Environment{
		UseExistingCluster:       &useCluster,
		CRDDirectoryPaths:        []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing:    true,
		AttachControlPlaneOutput: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(scheme.AddToScheme(scheme.Scheme)).To(Succeed())
	err = pubsubplus.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	By("cleaning up any stale PubSubPlusEventBroker resources left behind by a previous run")
	cleanupClient, err := runtimeClient.New(cfg, runtimeClient.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	staleBrokers := &pubsubplus.PubSubPlusEventBrokerList{}
	Expect(cleanupClient.List(ctx, staleBrokers, runtimeClient.InNamespace("default"))).To(Succeed())
	for i := range staleBrokers.Items {
		Expect(runtimeClient.IgnoreNotFound(cleanupClient.Delete(ctx, &staleBrokers.Items[i]))).To(Succeed())
	}
	Eventually(func() int {
		remaining := &pubsubplus.PubSubPlusEventBrokerList{}
		Expect(cleanupClient.List(ctx, remaining, runtimeClient.InNamespace("default"))).To(Succeed())
		return len(remaining.Items)
	}).WithTimeout(60 * time.Second).WithPolling(2 * time.Second).Should(Equal(0))

	By("cleaning up any stale Secrets/ConfigMaps left behind by a previous run")
	staleSecretNames := []string{
		"monitoring-tls", "monitoring-user-secret", "monitoring-tls-new-config",
		"monitoring-ds-secret", "secret-s-tls", "broker-sample-secret",
		"preshared-sample-secret", "admin-sample-secret", "sample-secret",
		"s-test-ha-prod-tls-secret", "s-test-ha-prod-nodeconfig-tls-secret",
	}
	for _, name := range staleSecretNames {
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}}
		Expect(runtimeClient.IgnoreNotFound(cleanupClient.Delete(ctx, secret))).To(Succeed())
	}
	staleConfigMapNames := []string{"sample-config"}
	for _, name := range staleConfigMapNames {
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}}
		Expect(runtimeClient.IgnoreNotFound(cleanupClient.Delete(ctx, cm))).To(Succeed())
	}

	clientSet, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
	})
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClient = mgr.GetClient()
	Expect(k8sClient).ToNot(BeNil())

	err = (&PubSubPlusEventBrokerReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		Recorder:    mgr.GetEventRecorderFor("PubSubPlusEventBroker"),
		IsOpenShift: false,
	}).SetupWithManager(mgr)

	Expect(err).ToNot(HaveOccurred())

})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	Expect(testEnv.Stop()).To(Succeed())
})
