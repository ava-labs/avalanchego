// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	_ "embed"

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
	secretPrefix string
	manifest     []byte
}

// DeployKubeCollectors deploys collectors of logs and metrics to a Kubernetes cluster.
func DeployKubeCollectors(ctx context.Context, log logging.Logger, configPath string, configContext string) error {
	log.Info("deploying kube collectors for logs and metrics")

	clientConfig, err := GetClientConfig(log, configPath, configContext)
	if err != nil {
		return fmt.Errorf("failed to get client config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: monitoringNamespace,
		},
	}
	_, err = clientset.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create namespace %s: %w", monitoringNamespace, err)
	}

	collectorConfigs := []kubeCollectorConfig{
		{
			name:         promtailCmd,
			secretPrefix: "loki",
			manifest:     promtailManifest,
		},
		{
			name:         prometheusCmd,
			secretPrefix: prometheusCmd,
			manifest:     prometheusManifest,
		},
	}
	for _, collectorConfig := range collectorConfigs {
		if err := deployKubeCollector(ctx, log, clientset, dynamicClient, collectorConfig); err != nil {
			return err
		}
	}

	return nil
}

// deployKubeCollector deploys a the named collector to a Kubernetes cluster via the provided manifest bytes.
func deployKubeCollector(
	ctx context.Context,
	log logging.Logger,
	clientset *kubernetes.Clientset,
	dynamicClient dynamic.Interface,
	collectorConfig kubeCollectorConfig,
) error {
	username, password, err := getCollectorCredentials(collectorConfig.name)
	if err != nil {
		return fmt.Errorf("failed to get credentials for %s: %w", collectorConfig.name, err)
	}

	if err := createCredentialSecret(ctx, log, clientset, collectorConfig.secretPrefix, username, password); err != nil {
		return fmt.Errorf("failed to create credential secret for %s: %w", collectorConfig.name, err)
	}

	if err := applyManifest(ctx, log, dynamicClient, collectorConfig.manifest); err != nil {
		return fmt.Errorf("failed to apply manifest for %s: %w", collectorConfig.name, err)
	}
	return nil
}

// createCredentialSecret creates a secret with the provided username and password for a collector
func createCredentialSecret(
	ctx context.Context,
	log logging.Logger,
	clientset *kubernetes.Clientset,
	namePrefix string,
	username string,
	password string,
) error {
	secretName := namePrefix + "-credentials"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
		},
		StringData: map[string]string{
			"username": username,
			"password": password,
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
		return fmt.Errorf("failed to create secret %s/%s: %w", monitoringNamespace, secretName, err)
	}

	log.Info("created secret",
		zap.String("namespace", monitoringNamespace),
		zap.String("name", secretName),
	)

	return nil
}

// applyManifest creates the resources defined by the provided manifest in a manner similar to `kubectl apply -f`
func applyManifest(
	ctx context.Context,
	log logging.Logger,
	dynamicClient dynamic.Interface,
	manifest []byte,
) error {
	// Split the manifest into individual resources
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	documents := strings.Split(string(manifest), "\n---\n")

	for _, doc := range documents {
		doc := strings.TrimSpace(doc)
		if strings.TrimSpace(doc) == "" || strings.HasPrefix(doc, "#") {
			continue
		}

		obj := &unstructured.Unstructured{}
		_, gvk, err := decoder.Decode([]byte(doc), nil, obj)
		if err != nil {
			return fmt.Errorf("failed to decode manifest: %w", err)
		}

		gvr := schema.GroupVersionResource{
			Group:    gvk.Group,
			Version:  gvk.Version,
			Resource: strings.ToLower(gvk.Kind) + "s",
		}

		var resourceInterface dynamic.ResourceInterface
		if strings.HasPrefix(gvk.Kind, "Cluster") || gvk.Kind == "Namespace" {
			resourceInterface = dynamicClient.Resource(gvr)
		} else {
			resourceInterface = dynamicClient.Resource(gvr).Namespace(monitoringNamespace)
		}

		_, err = resourceInterface.Create(ctx, obj, metav1.CreateOptions{})
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				log.Info("resource already exists",
					zap.String("kind", gvk.Kind),
					zap.String("namespace", monitoringNamespace),
					zap.String("name", obj.GetName()),
				)
				continue
			}
			return fmt.Errorf("failed to create %s %s/%s: %w", gvk.Kind, monitoringNamespace, obj.GetName(), err)
		}
		log.Info("created resource",
			zap.String("kind", gvk.Kind),
			zap.String("namespace", monitoringNamespace),
			zap.String("name", obj.GetName()),
		)
	}

	return nil
}
