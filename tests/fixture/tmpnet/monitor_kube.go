// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"

	"go.uber.org/zap"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	_ "embed"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:embed yaml/promtail-daemonset.yaml
var promtailManifest []byte

//go:embed yaml/prometheus-agent.yaml
var prometheusManifest []byte

// This must match the namespace defined in the manifests
const monitoringNamespace = "ci-monitoring"

type kubeCollectorConfig struct {
	name         string
	target       string
	secretPrefix string
	manifest     []byte
}

// deployKubeCollectors deploys collectors of logs and metrics to a Kubernetes cluster.
func deployKubeCollectors(
	ctx context.Context,
	log logging.Logger,
	configPath string,
	configContext string,
	startMetricsCollector bool,
	startLogsCollector bool,
) error {
	if !startMetricsCollector && !startLogsCollector {
		// Nothing to do
		return nil
	}

	clientConfig, err := GetClientConfig(log, configPath, configContext)
	if err != nil {
		return stacktrace.Errorf("failed to get client config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return stacktrace.Errorf("failed to create clientset: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return stacktrace.Errorf("failed to create dynamic client: %w", err)
	}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: monitoringNamespace,
		},
	}
	_, err = clientset.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return stacktrace.Errorf("failed to create namespace %s: %w", monitoringNamespace, err)
	}

	collectorConfigs := []kubeCollectorConfig{
		{
			name:         promtailCmd,
			target:       "logs",
			secretPrefix: "loki",
			manifest:     promtailManifest,
		},
		{
			name:         prometheusCmd,
			target:       "metrics",
			secretPrefix: prometheusCmd,
			manifest:     prometheusManifest,
		},
	}
	for _, collectorConfig := range collectorConfigs {
		log.Info("deploying kube collector",
			zap.String("cmd", collectorConfig.name),
			zap.String("target", collectorConfig.target),
		)
		if err := deployKubeCollector(ctx, log, clientset, dynamicClient, collectorConfig); err != nil {
			return stacktrace.Wrap(err)
		}
	}

	return nil
}

// deployKubeCollector deploys a named collector to a Kubernetes cluster via the provided manifest bytes.
func deployKubeCollector(
	ctx context.Context,
	log logging.Logger,
	clientset *kubernetes.Clientset,
	dynamicClient dynamic.Interface,
	collectorConfig kubeCollectorConfig,
) error {
	config, err := getCollectorConfig(collectorConfig.name)
	if err != nil {
		return stacktrace.Errorf("failed to get collector config for %s: %w", collectorConfig.name, err)
	}

	if err := createCollectorConfigSecret(ctx, log, clientset, collectorConfig.secretPrefix, config); err != nil {
		return stacktrace.Errorf("failed to create config secret for %s: %w", collectorConfig.name, err)
	}

	if err := applyManifest(ctx, log, dynamicClient, collectorConfig.manifest, monitoringNamespace); err != nil {
		return stacktrace.Errorf("failed to apply manifest for %s: %w", collectorConfig.name, err)
	}
	return nil
}

// createCollectorConfigSecret creates a secret with the provided urls, username and password for a collector
func createCollectorConfigSecret(
	ctx context.Context,
	log logging.Logger,
	clientset *kubernetes.Clientset,
	namePrefix string,
	config collectorConfig,
) error {
	secretName := namePrefix + "-config"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
		},
		StringData: map[string]string{
			"url":      config.pushURL,
			"username": config.username,
			"password": config.password,
		},
	}
	_, err := clientset.CoreV1().Secrets(monitoringNamespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			log.Info("secret already exists",
				zap.String("namespace", monitoringNamespace),
				zap.String("name", secretName),
			)
			return nil
		}
		return stacktrace.Errorf("failed to create secret %s/%s: %w", monitoringNamespace, secretName, err)
	}

	log.Info("created secret",
		zap.String("namespace", monitoringNamespace),
		zap.String("name", secretName),
	)

	return nil
}
