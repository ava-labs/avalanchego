// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/logging"

	corev1 "k8s.io/api/core/v1"
)

const (
	contextDuration = 30 * time.Second

	ImageUnchanged = "Image unchanged"
)

func WaitForCompletion(
	log logging.Logger,
	namespace string,
	podName string,
	nodeContainerName string,
	dataDir string,
	healthCheckInterval time.Duration,
	imageCheckInterval time.Duration,
) error {
	clientset, err := getClientset(log)
	if err != nil {
		return fmt.Errorf("failed to get clientset: %w", err)
	}

	// Avoid checking node health before it reports initial ready
	log.Info("Waiting for pod readiness")
	if err := wait.PollImmediateInfinite(healthCheckInterval, func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), contextDuration)
		defer cancel()
		err := WaitForPodCondition(ctx, clientset, namespace, podName, corev1.PodReady)
		if err != nil {
			log.Error("failed to wait for pod condition", zap.Error(err))
			return false, nil
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to wait for pod readiness: %w", err)
	}

	var (
		containerImage string
		nodeURL        = fmt.Sprintf("http://localhost:%d", config.DefaultHTTPPort)
	)
	log.Info("Waiting for node to report healthy")
	if err := wait.PollImmediateInfinite(healthCheckInterval, func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), contextDuration)
		defer cancel()

		if len(containerImage) == 0 {
			log.Info("Retrieving pod to determine image of container",
				zap.String("namespace", namespace),
				zap.String("pod", podName),
				zap.String("container", nodeContainerName),
			)
			var err error
			containerImage, err = GetContainerImage(ctx, clientset, namespace, podName, nodeContainerName)
			if err != nil {
				log.Error("failed to get container image", zap.Error(err))
				return false, nil
			}
			log.Info("Image for container",
				zap.String("container", nodeContainerName),
				zap.String("image", containerImage),
			)
		}

		// Check whether the node is reporting healthy which indicates that bootstrap is complete
		if healthy, err := tmpnet.CheckNodeHealth(ctx, nodeURL); err != nil {
			log.Error("failed to check node health", zap.Error(err))
			return false, nil
		} else {
			reportDiskUsage(log, dataDir)

			if !healthy.Healthy {
				log.Info("Node reported unhealthy")
				return false, nil
			}

			log.Info("Node reported healthy")
		}

		log.Info("Bootstrap completed successfully for image", zap.String("image", containerImage))

		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to wait for node to report healthy: %w", err)
	}

	log.Info("Waiting for new image to test")
	if err := wait.PollImmediateInfinite(imageCheckInterval, func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), contextDuration)
		defer cancel()

		log.Info("Starting pod to get the image id for the `latest` tag")
		latestImageID, err := getLatestImageID(ctx, log, clientset, namespace, containerImage, nodeContainerName)
		if err != nil {
			log.Error("failed to get latest image id", zap.Error(err))
			return false, nil
		}

		if latestImageID == containerImage {
			log.Info(ImageUnchanged)
			return false, nil
		}

		log.Info("Found updated image", zap.String("image", latestImageID))

		log.Info("Updating StatefulSet to trigger a new test")
		if err := setContainerImage(ctx, log, clientset, namespace, podName, nodeContainerName, latestImageID); err != nil {
			log.Error("failed to set container image", zap.Error(err))
			return false, nil
		}

		// Statefulset will restart the pod with the new image
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to wait for new image to test: %w", err)
	}

	// Avoid exiting immediately to avoid container restart before the pod is recreated with the new image
	time.Sleep(5 * time.Minute)
	return nil
}

// Logs the current disk usage for the specified directory
func reportDiskUsage(log logging.Logger, dir string) {
	cmd := exec.Command("du", "-sh", dir)

	// Create a buffer to capture stderr in case an unexpected error occurs
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if !ok {
			log.Error("Error executing du", zap.Error(err))
			return
		}
		switch exitError.ExitCode() {
		case 1:
			// Exit code 1 usually indicates that files cannot be accessed. Since avalanchego will
			// regularly delete files in the db dir, this can be safely ignored and the regular disk
			// usage message can be printed.
		case 2:
			log.Error("Incorrect usage of du command for dir",
				zap.String("dir", dir),
				zap.String("stderr", stderr.String()),
			)
			return
		default:
			log.Error("du command failed for dir",
				zap.Int("exitCode", exitError.ExitCode()),
				zap.String("dir", dir),
			)
			return
		}
	}

	usageParts := strings.Split(string(output), "\t")
	if len(usageParts) != 2 {
		log.Error("Unexpected output from du command",
			zap.String("output", string(output)),
		)
	}

	log.Info("Disk usage",
		zap.String("quantity", usageParts[0]),
		zap.String("dir", dir),
	)
}
