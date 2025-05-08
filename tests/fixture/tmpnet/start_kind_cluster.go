// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TODO(marun) This should be configurable
const DefaultTmpnetNamespace = "tmpnet"

// StartKindCluster starts a new kind cluster if one is not already running.
// TODO(marun) Maybe only start a kind cluster if the configContext is kind-kind?
func StartKindCluster(ctx context.Context, log logging.Logger, configPath string, configContext string) error {
	clientset, err := GetClientset(log, configPath, configContext)
	if err != nil {
		return err
	}

	// Check if the configured context can reach a cluster endpoint
	// TODO(marun) Maybe differentiate between configuration and endpoint errors
	_, err = clientset.Discovery().ServerVersion()
	if err == nil {
		log.Info("kubernetes cluster already running",
			zap.String("kubeconfig", configPath),
			zap.String("kubeconfigContext", configContext),
		)
	} else {
		log.Debug("kubernetes cluster not running, attempting to start local kind cluster",
			zap.String("kubeconfig", configPath),
			zap.String("kubeconfigContext", configContext),
			zap.Error(err),
		)

		// Start a new kind cluster
		startCtx, cancel := context.WithTimeout(ctx, DefaultNetworkTimeout)
		defer cancel()
		cmd := exec.CommandContext(startCtx, "kind-with-registry.sh")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to run kind-with-registry.sh: %w", err)
		}
	}

	_, err = clientset.CoreV1().Namespaces().Get(ctx, DefaultTmpnetNamespace, metav1.GetOptions{})
	if err == nil {
		log.Info("namespace already exists", zap.String("namespace", DefaultTmpnetNamespace))
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check for namespace %s: %w", DefaultTmpnetNamespace, err)
	}

	log.Info("namespace not found, creating", zap.String("namespace", DefaultTmpnetNamespace))
	_, err = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: DefaultTmpnetNamespace,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create namespace %s: %w", DefaultTmpnetNamespace, err)
	}
	log.Info("created namespace",
		zap.String("namespace", DefaultTmpnetNamespace),
	)

	return nil
}
