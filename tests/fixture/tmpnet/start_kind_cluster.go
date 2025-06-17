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
	"k8s.io/client-go/kubernetes"

	"github.com/ava-labs/avalanchego/utils/logging"

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

// StartKindCluster starts a new kind cluster with integrated registry if one is not already running.
func StartKindCluster(
	ctx context.Context,
	log logging.Logger,
	configPath string,
	startMetricsCollector bool,
	startLogsCollector bool,
) error {
	configContext := KindKubeconfigContext

	clusterRunning, err := isKindClusterRunning(log, configPath)
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
		cmd := exec.CommandContext(startCtx, "kind-with-registry.sh")
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

	if err := deployKubeCollectors(ctx, log, configPath, configContext, startMetricsCollector, startLogsCollector); err != nil {
		return fmt.Errorf("failed to deploy kube collectors: %w", err)
	}

	if err := deployNginxIngress(ctx, log, configPath, configContext); err != nil {
		return fmt.Errorf("failed to deploy nginx ingress: %w", err)
	}

	return nil
}

// isKindClusterRunning determines if a kind cluster is running with context kind-kind
func isKindClusterRunning(log logging.Logger, configPath string) (bool, error) {
	configContext := KindKubeconfigContext

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

// DeployNginxIngress deploys the nginx ingress controller using Helm.
func deployNginxIngress(ctx context.Context, log logging.Logger, configPath string, configContext string) error {
	log.Info("checking if nginx ingress controller is already deployed")

	// Check if ingress controller is already running
	isRunning, err := isIngressControllerRunning(ctx, log, configPath, configContext)
	if err != nil {
		return fmt.Errorf("failed to check ingress controller status: %w", err)
	}
	if isRunning {
		log.Info("nginx ingress controller already running")
		return nil
	}

	log.Info("deploying nginx ingress controller using Helm")

	// Add helm repo if not already added
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
			if apierrors.IsNotFound(err) {
				log.Debug("nginx ingress controller deployment not found yet",
					zap.String("namespace", ingressNamespace),
					zap.String("deployment", ingressControllerName),
				)
				return false, nil
			}
			return false, err
		}

		if deployment.Status.ReadyReplicas > 0 {
			log.Info("nginx ingress controller is ready",
				zap.String("namespace", ingressNamespace),
				zap.String("deployment", ingressControllerName),
				zap.Int32("readyReplicas", deployment.Status.ReadyReplicas),
			)
			return true, nil
		}

		log.Debug("waiting for nginx ingress controller to become ready",
			zap.String("namespace", ingressNamespace),
			zap.String("deployment", ingressControllerName),
			zap.Int32("readyReplicas", deployment.Status.ReadyReplicas),
			zap.Int32("replicas", deployment.Status.Replicas),
		)
		return false, nil
	})
}

// runHelmCommand runs a Helm command with the given arguments.
func runHelmCommand(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "helm", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
