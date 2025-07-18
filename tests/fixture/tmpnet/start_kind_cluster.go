// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils/logging"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// TODO(marun) This should be configurable
	DefaultTmpnetNamespace = "tmpnet"

	KindKubeconfigContext = "kind-kind"

	// TODO(marun) Check for the presence of the context rather than string matching on this error
	missingContextMsg = `context "` + KindKubeconfigContext + `" does not exist`

	// Ingress controller constants
	ingressNamespace      = "ingress-nginx"
	ingressReleaseName    = "ingress-nginx"
	ingressChartRepo      = "https://kubernetes.github.io/ingress-nginx"
	ingressChartName      = "ingress-nginx/ingress-nginx"
	ingressControllerName = "ingress-nginx-controller"
)

//go:embed yaml/tmpnet-rbac.yaml
var tmpnetRBACManifest []byte

// StartKindCluster starts a new kind cluster with integrated registry if one is not already running.
func StartKindCluster(
	ctx context.Context,
	log logging.Logger,
	configPath string,
	startMetricsCollector bool,
	startLogsCollector bool,
) error {
	configContext := KindKubeconfigContext

	clusterRunning, err := isKindClusterRunning(log, configPath, configContext)
	if err != nil {
		return err
	}
	if clusterRunning {
		log.Info("local kind cluster already running",
			zap.String("kubeconfig", configPath),
			zap.String("kubeconfigContext", configContext),
		)
	} else {
		log.Info("attempting to start local kind cluster",
			zap.String("kubeconfig", configPath),
			zap.String("kubeconfigContext", configContext),
		)

		startCtx, cancel := context.WithTimeout(ctx, DefaultNetworkTimeout)
		defer cancel()
		cmd := exec.CommandContext(startCtx, "bash", "-x", "kind-with-registry.sh")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to run kind-with-registry.sh: %w", err)
		}
	}

	clientset, err := GetClientset(log, configPath, configContext)
	if err != nil {
		return err
	}
	if err := ensureNamespace(ctx, log, clientset, DefaultTmpnetNamespace); err != nil {
		return err
	}

	// Deploy RBAC resources for tmpnet
	if err := deployRBAC(ctx, log, configPath, configContext, DefaultTmpnetNamespace); err != nil {
		return fmt.Errorf("failed to deploy tmpnet RBAC: %w", err)
	}

	// Create service account kubeconfig context to enable checking that RBAC permissions are sufficient
	rbacContextName := KindKubeconfigContext + "-tmpnet"
	if err := createServiceAccountKubeconfig(ctx, log, configPath, configContext, DefaultTmpnetNamespace, rbacContextName); err != nil {
		return fmt.Errorf("failed to create service account kubeconfig context: %w", err)
	}

	if err := deployKubeCollectors(ctx, log, configPath, configContext, startMetricsCollector, startLogsCollector); err != nil {
		return fmt.Errorf("failed to deploy kube collectors: %w", err)
	}

	if err := deployIngressController(ctx, log, configPath, configContext); err != nil {
		return fmt.Errorf("failed to deploy ingress controller: %w", err)
	}

	if err := createDefaultsConfigMap(ctx, log, configPath, configContext, DefaultTmpnetNamespace); err != nil {
		return fmt.Errorf("failed to create defaults ConfigMap: %w", err)
	}

	return nil
}

// isKindClusterRunning determines if a kind cluster is running
func isKindClusterRunning(log logging.Logger, configPath string, configContext string) (bool, error) {
	_, err := os.Stat(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		log.Info("specified kubeconfig path does not exist",
			zap.String("kubeconfig", configPath),
		)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check kubeconfig path %s: %w", configPath, err)
	}

	clientset, err := GetClientset(log, configPath, configContext)
	if err != nil {
		if strings.Contains(err.Error(), missingContextMsg) {
			log.Info("specified kubeconfig context does not exist",
				zap.String("kubeconfig", configPath),
				zap.String("kubeconfigContext", configContext),
			)
			return false, nil
		} else {
			// All other errors are assumed fatal
			return false, err
		}
	}

	// Assume any errors in discovery indicate the cluster is not running
	//
	// TODO(marun) Maybe differentiate between configuration and endpoint errors?
	_, err = clientset.Discovery().ServerVersion()
	if err != nil {
		log.Info("failed to contact kubernetes cluster",
			zap.String("kubeconfig", configPath),
			zap.String("kubeconfigContext", configContext),
			zap.Error(err),
		)
		return false, nil
	}

	return true, nil
}

// ensureNamespace ensures that the specified namespace exists in cluster targeted by the clientset.
func ensureNamespace(ctx context.Context, log logging.Logger, clientset *kubernetes.Clientset, namespace string) error {
	_, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		log.Info("namespace already exists",
			zap.String("namespace", namespace),
		)
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check for namespace %s: %w", namespace, err)
	}

	log.Info("namespace not found, creating",
		zap.String("namespace", namespace),
	)
	_, err = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create namespace %s: %w", namespace, err)
	}
	log.Info("created namespace",
		zap.String("namespace", namespace),
	)

	return nil
}

// deployRBAC deploys the RBAC resources for tmpnet to a Kubernetes cluster.
func deployRBAC(
	ctx context.Context,
	log logging.Logger,
	configPath string,
	configContext string,
	namespace string,
) error {
	log.Info("deploying tmpnet RBAC resources",
		zap.String("namespace", namespace),
	)

	clientConfig, err := GetClientConfig(log, configPath, configContext)
	if err != nil {
		return fmt.Errorf("failed to get client config: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Apply the RBAC manifest
	if err := applyManifest(ctx, log, dynamicClient, tmpnetRBACManifest, ""); err != nil {
		return fmt.Errorf("failed to apply RBAC manifest: %w", err)
	}

	log.Info("successfully deployed tmpnet RBAC resources",
		zap.String("namespace", namespace),
	)

	return nil
}

// createServiceAccountKubeconfig creates a kubeconfig that uses the tmpnet service account token.
// It only creates the context if it doesn't already exist.
// This function is called from StartKindCluster after the kubeconfig and context have been verified.
func createServiceAccountKubeconfig(
	ctx context.Context,
	log logging.Logger,
	configPath string,
	configContext string,
	namespace string,
	newContextName string,
) error {
	// Get the existing kubeconfig
	config, err := clientcmd.LoadFromFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	if _, exists := config.Contexts[newContextName]; exists {
		log.Info("service account kubeconfig context exists, recreating to ensure consistency with cluster state",
			zap.String("kubeconfig", configPath),
			zap.String("context", newContextName),
			zap.String("namespace", namespace),
		)
	} else {
		log.Info("creating new service account kubeconfig context",
			zap.String("kubeconfig", configPath),
			zap.String("context", newContextName),
			zap.String("namespace", namespace),
		)
	}

	// Get the current context (already verified to exist by StartKindCluster)
	currentContext := config.Contexts[configContext]

	// Get clientset to retrieve service account token
	clientConfig, err := GetClientConfig(log, configPath, configContext)
	if err != nil {
		return fmt.Errorf("failed to get client config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	// Create a token for the service account (Kubernetes 1.24+)
	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			// Token will be valid for 1 year
			ExpirationSeconds: ptr.To[int64](365 * 24 * 60 * 60),
		},
	}
	token, err := clientset.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, "tmpnet", tokenRequest, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create service account token: %w", err)
	}

	// Create new context with the token
	config.AuthInfos[newContextName] = &api.AuthInfo{
		Token: token.Status.Token,
	}

	// Create new context
	config.Contexts[newContextName] = &api.Context{
		Cluster:   currentContext.Cluster,
		AuthInfo:  newContextName,
		Namespace: namespace,
	}

	// Save the updated kubeconfig
	if err := clientcmd.WriteToFile(*config, configPath); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	log.Info("created service account kubeconfig context",
		zap.String("kubeconfig", configPath),
		zap.String("context", newContextName),
		zap.String("namespace", namespace),
	)

	return nil
}

// deployIngressController deploys the nginx ingress controller using Helm.
func deployIngressController(ctx context.Context, log logging.Logger, configPath string, configContext string) error {
	log.Info("checking if nginx ingress controller is already running")

	isRunning, err := isIngressControllerRunning(ctx, log, configPath, configContext)
	if err != nil {
		return fmt.Errorf("failed to check nginx ingress controller status: %w", err)
	}
	if isRunning {
		log.Info("nginx ingress controller already running")
		return nil
	}

	log.Info("deploying nginx ingress controller using Helm")

	// Add the helm repo for ingress-nginx
	if err := runHelmCommand(ctx, "repo", "add", "ingress-nginx", ingressChartRepo); err != nil {
		return fmt.Errorf("failed to add helm repo: %w", err)
	}
	if err := runHelmCommand(ctx, "repo", "update"); err != nil {
		return fmt.Errorf("failed to update helm repos: %w", err)
	}

	// Install nginx-ingress with values set directly via flags
	// Using fixed nodePort 30791 for cross-platform compatibility
	args := []string{
		"install",
		ingressReleaseName,
		ingressChartName,
		"--namespace", ingressNamespace,
		"--create-namespace",
		"--wait",
		"--set", "controller.service.type=NodePort",
		// This port value must match the port configured in scripts/kind-with-registry.sh
		"--set", "controller.service.nodePorts.http=30791",
		"--set", "controller.admissionWebhooks.enabled=false",
		"--set", "controller.config.proxy-read-timeout=600",
		"--set", "controller.config.proxy-send-timeout=600",
		"--set", "controller.config.proxy-body-size=0",
		"--set", "controller.config.proxy-http-version=1.1",
		"--set", "controller.metrics.enabled=true",
	}

	if err := runHelmCommand(ctx, args...); err != nil {
		return fmt.Errorf("failed to install nginx-ingress: %w", err)
	}

	return waitForIngressController(ctx, log, configPath, configContext)
}

// isIngressControllerRunning checks if the nginx ingress controller is already running.
func isIngressControllerRunning(ctx context.Context, log logging.Logger, configPath string, configContext string) (bool, error) {
	clientset, err := GetClientset(log, configPath, configContext)
	if err != nil {
		return false, err
	}

	// TODO(marun) Handle the case of the deployment being in a failed state
	_, err = clientset.AppsV1().Deployments(ingressNamespace).Get(ctx, ingressControllerName, metav1.GetOptions{})
	isRunning := !apierrors.IsNotFound(err) || err == nil
	return isRunning, nil
}

// waitForIngressController waits for the nginx ingress controller to be ready.
func waitForIngressController(ctx context.Context, log logging.Logger, configPath string, configContext string) error {
	clientset, err := GetClientset(log, configPath, configContext)
	if err != nil {
		return fmt.Errorf("failed to get clientset: %w", err)
	}

	return wait.PollUntilContextCancel(ctx, statusCheckInterval, true /* immediate */, func(ctx context.Context) (bool, error) {
		deployment, err := clientset.AppsV1().Deployments(ingressNamespace).Get(ctx, ingressControllerName, metav1.GetOptions{})
		if err != nil {
			log.Debug("failed to get nginx ingress controller deployment",
				zap.String("namespace", ingressNamespace),
				zap.String("deployment", ingressControllerName),
				zap.Error(err),
			)
			return false, nil
		}
		if deployment.Status.ReadyReplicas == 0 {
			log.Debug("waiting for nginx ingress controller to become ready",
				zap.String("namespace", ingressNamespace),
				zap.String("deployment", ingressControllerName),
				zap.Int32("readyReplicas", deployment.Status.ReadyReplicas),
				zap.Int32("replicas", deployment.Status.Replicas),
			)
			return false, nil
		}

		log.Info("nginx ingress controller is ready",
			zap.String("namespace", ingressNamespace),
			zap.String("deployment", ingressControllerName),
			zap.Int32("readyReplicas", deployment.Status.ReadyReplicas),
		)
		return true, nil
	})
}

// runHelmCommand runs a Helm command with the given arguments.
func runHelmCommand(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "helm", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// createDefaultsConfigMap creates a ConfigMap containing defaults for the tmpnet namespace.
func createDefaultsConfigMap(ctx context.Context, log logging.Logger, configPath string, configContext string, namespace string) error {
	clientset, err := GetClientset(log, configPath, configContext)
	if err != nil {
		return fmt.Errorf("failed to get clientset: %w", err)
	}

	configMapName := defaultsConfigMapName

	// Check if configmap already exists
	_, err = clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err == nil {
		log.Info("defaults ConfigMap already exists",
			zap.String("namespace", namespace),
			zap.String("configMap", configMapName),
		)
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check for configmap %s/%s: %w", namespace, configMapName, err)
	}

	log.Info("creating defaults ConfigMap",
		zap.String("namespace", namespace),
		zap.String("configMap", configMapName),
	)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			ingressHostKey: "localhost:30791",
		},
	}

	_, err = clientset.CoreV1().ConfigMaps(namespace).Create(ctx, configMap, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create configmap %s/%s: %w", namespace, configMapName, err)
	}

	return nil
}
