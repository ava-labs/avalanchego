// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"

	corev1 "k8s.io/api/core/v1"
)

const (
	contextDuration = 30 * time.Second

	ImageUnchanged = "Image unchanged"
)

func WaitForCompletion(
	namespace string,
	podName string,
	nodeContainerName string,
	dataDir string,
	healthCheckInterval time.Duration,
	imageCheckInterval time.Duration,
) error {
	clientset, err := getClientset()
	if err != nil {
		return fmt.Errorf("failed to get clientset: %w", err)
	}

	// Avoid checking node health before it reports initial ready
	log.Println("Waiting for pod readiness")
	if err := wait.PollImmediateInfinite(healthCheckInterval, func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), contextDuration)
		defer cancel()
		err := WaitForPodCondition(ctx, clientset, namespace, podName, corev1.PodReady)
		if err != nil {
			log.Printf("failed to wait for pod condition: %v", err)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to wait for pod readiness: %w", err)
	}

	var containerImage string
	log.Println("Waiting for node to report healthy")
	if err := wait.PollImmediateInfinite(healthCheckInterval, func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), contextDuration)
		defer cancel()

		if len(containerImage) == 0 {
			var err error
			log.Printf("Retrieving pod %s.%s to determine the image of container %q", namespace, podName, nodeContainerName)
			containerImage, err = GetContainerImage(ctx, clientset, namespace, podName, nodeContainerName)
			if err != nil {
				log.Printf("failed to get container image: %v", err)
				return false, nil
			}
			log.Printf("Image for container %q: %s", nodeContainerName, containerImage)
		}

		// Check whether the node is reporting healthy which indicates that bootstrap is complete
		if healthy, err := tmpnet.CheckNodeHealth(ctx, fmt.Sprintf("http://localhost:%d", config.DefaultHTTPPort)); err != nil {
			log.Printf("failed to wait for node health: %v", err)
			return false, nil
		} else {
			reportDiskUsage(dataDir)

			if !healthy.Healthy {
				log.Println("Node reported unhealthy")
				return false, nil
			}

			log.Println("Node reported healthy")
		}

		log.Println("Bootstrap completed successfully for " + containerImage)

		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to wait for node to report healthy: %w", err)
	}

	log.Println("Waiting for new image to test")
	if err := wait.PollImmediateInfinite(imageCheckInterval, func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), contextDuration)
		defer cancel()

		log.Println("Starting pod to check the image id for the `latest` tag")
		latestImageID, err := getLatestImageID(ctx, clientset, namespace, containerImage, nodeContainerName)
		if err != nil {
			log.Printf("failed to get latest image id: %v", err)
			return false, nil
		}

		if latestImageID == containerImage {
			log.Println(ImageUnchanged)
			return false, nil
		}

		log.Printf("Found updated image %s", latestImageID)

		log.Println("Updating StatefulSet to trigger a new test")
		if err := setContainerImage(ctx, clientset, namespace, podName, nodeContainerName, latestImageID); err != nil {
			log.Printf("failed to set container image: %v", err)
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
func reportDiskUsage(dir string) {
	cmd := exec.Command("du", "-sh", dir)

	// Create a buffer to capture stderr in case an unexpected error occurs
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if !ok {
			log.Printf("Error executing du: %v", err)
			return
		}
		switch exitError.ExitCode() {
		case 1:
			// Exit code 1 usually indicates that files cannot be accessed. Since avalanchego will
			// regularly delete files in the db dir, this can be safely ignored and the regular disk
			// usage message can be printed.
		case 2:
			log.Printf("Error: Incorrect usage of du command for %s", dir)
			log.Printf("Stderr: %s", stderr.String())
			return
		default:
			log.Printf("Error: du command failed with exit code %d for %s", exitError.ExitCode(), dir)
			return
		}
	}
	log.Printf("Disk usage: %s", string(output))
}
