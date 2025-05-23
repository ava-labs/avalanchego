// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.uber.org/zap"
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
)

// StartKindCluster starts a new kind cluster with integrated registry if one is not already running.
func StartKindCluster(ctx context.Context, log logging.Logger, configPath string) error {
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

	return ensureNamespace(ctx, log, clientset, DefaultTmpnetNamespace)
}

// isKindClusterRunning determines if a kind cluster is running with context kind-kind
func isKindClusterRunning(log logging.Logger, configPath string) (bool, error) {
	configContext := KindKubeconfigContext

	_, err := os.Stat(configPath)
	if os.IsNotExist(err) {
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
