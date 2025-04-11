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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const DefaultTmpnetNamespace = "tmpnet"

// CheckClusterRunning checks if the configured cluster is accessible.
// TODO(marun) Maybe differentiate between configuration and endpoint errors
func CheckClusterRunning(log logging.Logger, configPath string, configContext string) error {
	clientset, err := GetClientset(log, configPath, configContext)
	if err != nil {
		return err
	}
	// Check if the configured context can reach a cluster endpoint
	_, err = clientset.Discovery().ServerVersion()
	return err
}

// StartKindCluster starts a new kind cluster if one is not already running.
func StartKindCluster(ctx context.Context, log logging.Logger, configPath string, configContext string) error {
	err := CheckClusterRunning(log, configPath, configContext)
	if err == nil {
		log.Info("kubernetes cluster already running",
			zap.String("kubeconfig", configPath),
			zap.String("kubeconfigContext", configContext),
		)
		return nil
	}

	log.Debug("kubernetes cluster not running",
		zap.String("kubeconfig", configPath),
		zap.String("kubeconfigContext", configContext),
		zap.Error(err),
	)

	// Start a new kind cluster
	ctx, cancel := context.WithTimeout(ctx, DefaultNetworkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kind-with-registry.sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run kind-with-registry.sh: %w", err)
	}

	// Ensure the tmpnet namespace exists
	clientset, err := GetClientset(log, configPath, configContext)
	if err != nil {
		return fmt.Errorf("failed to get clientset: %w", err)
	}
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
