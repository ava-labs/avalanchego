// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
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

var nodeURL = fmt.Sprintf("http://localhost:%d", config.DefaultHTTPPort)

func WaitForCompletion(
	log logging.Logger,
	namespace string,
	podName string,
	nodeContainerName string,
	dataDir string,
	healthCheckInterval time.Duration,
	imageCheckInterval time.Duration,
) error {
	testDetailsPath := getTestDetailsPath(dataDir)
	var testDetails bootstrapTestDetails
	if testDetailsBytes, err := os.ReadFile(testDetailsPath); err != nil {
		return fmt.Errorf("failed to load test details file %s: %w", testDetailsPath, err)
	} else {
		if err := json.Unmarshal(testDetailsBytes, &testDetails); err != nil {
			return fmt.Errorf("failed to unmarshal test details: %w", err)
		}
		log.Info("Loaded test details", zap.Reflect("testDetails", testDetails))
	}

	clientset, err := getClientset(log)
	if err != nil {
		return fmt.Errorf("failed to get clientset: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()

	log.Info("Retrieving pod to determine bootstrap test config",
		zap.String("namespace", namespace),
		zap.String("pod", podName),
		zap.String("container", nodeContainerName),
	)
	testConfig, err := GetBootstrapTestConfigFromPod(ctx, clientset, namespace, podName, nodeContainerName)
	if err != nil {
		return fmt.Errorf("failed to determine bootstrap test config: %w", err)
	}
	log.Info("Retrieved bootstrap test config", zap.Reflect("testConfig", testConfig))

	// Avoid checking node health before it reports initial ready
	log.Info("Waiting for pod readiness")
	if err := tmpnet.WaitForPodCondition(ctx, clientset, namespace, podName, corev1.PodReady); err != nil {
		return fmt.Errorf("failed to wait for pod condition: %w", err)
	}

	log.Info("Waiting for node to report healthy")
	if err := wait.PollImmediateInfinite(healthCheckInterval, func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), contextDuration)
		defer cancel()

		// Define common fields for logging
		diskUsage := getDiskUsage(log, dataDir)
		commonFields := []zap.Field{
			zap.String("diskUsage", diskUsage),
			zap.Duration("duration", time.Since(testDetails.StartTime)),
		}

		// Check whether the node is reporting healthy which indicates that bootstrap is complete
		if healthy, err := tmpnet.CheckNodeHealth(ctx, nodeURL); err != nil {
			log.Error("failed to check node health", zap.Error(err))
			return false, nil
		} else {
			if !healthy.Healthy {
				log.Info("Node reported unhealthy", commonFields...)
				return false, nil
			}

			log.Info("Node reported healthy")
		}

		commonFields = append(commonFields, zap.Reflect("testConfig", testConfig))
		log.Info("Bootstrap completed successfully", commonFields...)

		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to wait for node to report healthy: %w", err)
	}

	log.Info("Waiting for new image to test")
	if err := wait.PollImmediateInfinite(imageCheckInterval, func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), contextDuration)
		defer cancel()

		log.Info("Starting pod to get the image id for the `master` tag")
		masterImageDetails, err := getMasterImageDetails(ctx, log, clientset, namespace, testConfig.Image, nodeContainerName)
		if err != nil {
			log.Error("failed to get master image id", zap.Error(err))
			return false, nil
		}

		if masterImageDetails.Image == testConfig.Image {
			log.Info(ImageUnchanged)
			return false, nil
		}

		log.Info("Found updated image",
			zap.String("image", masterImageDetails.Image),
			zap.Reflect("versions", masterImageDetails.Versions),
		)

		log.Info("Updating StatefulSet to trigger a new test")
		if err := setImageDetails(ctx, log, clientset, namespace, podName, masterImageDetails); err != nil {
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

// Determines the current disk usage for the specified directory
func getDiskUsage(log logging.Logger, dir string) string {
	cmd := exec.CommandContext(context.Background(), "du", "-sh", dir)

	// Create a buffer to capture stderr in case an unexpected error occurs
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if !ok {
			log.Error("Error executing du", zap.Error(err))
			return ""
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
				zap.Error(err),
			)
			return ""
		default:
			log.Error("du command failed for dir",
				zap.String("dir", dir),
				zap.String("stderr", stderr.String()),
				zap.Error(err),
			)
			return ""
		}
	}

	usageParts := strings.Split(string(output), "\t")
	if len(usageParts) != 2 {
		log.Error("Unexpected output from du command",
			zap.String("output", string(output)),
		)
	}

	return usageParts[0]
}
