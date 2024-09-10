// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
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

const minContextDuration = 30 * time.Second

func BootstrapSucceededMessage(containerImage string) string {
	return "Bootstrap completed successfully for " + containerImage
}

func WaitForCompletion(namespace string, podName string, nodeContainerName string, dataDir string, interval time.Duration) error {
	clientset, err := getClientset()
	if err != nil {
		return fmt.Errorf("failed to get clientset: %w", err)
	}

	// Ensure a minimum context duration
	contextDuration := max(minContextDuration, interval)

	var (
		podReady          bool
		bootstrapComplete bool
		containerImage    string
	)
	err = wait.PollImmediateInfinite(interval, func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), contextDuration)
		defer cancel()

		// Avoid checking node health before it reports ready
		if !podReady {
			log.Println("Waiting for pod readiness")

			err := WaitForPodCondition(ctx, clientset, namespace, podName, corev1.PodReady)
			if err != nil {
				log.Printf("failed to wait for pod condition: %v", err)
				return false, nil
			}

			podReady = true

			// Ensure this is only printed once
			log.Println("Waiting for node to report healthy")
		}

		if !bootstrapComplete {
			// Check whether the node is reporting healthy which indicates that bootstrap is complete

			if healthy, err := tmpnet.CheckNodeHealth(ctx, fmt.Sprintf("http://localhost:%d", config.DefaultHTTPPort)); err != nil {
				log.Printf("failed to wait for node health: %v", err)
				return false, nil
			} else {
				cmd := exec.Command("du", "-sh", dataDir)
				if diskUsage, err := cmd.CombinedOutput(); err != nil {
					log.Printf("failed to check disk usage: %v", err)
				} else {
					log.Printf("Disk usage: %s", string(diskUsage))
				}

				if !healthy.Healthy {
					log.Println("Node reported unhealthy")
					return false, nil
				}

				log.Println("Node reported healthy")
			}

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

			log.Println(BootstrapSucceededMessage(containerImage))
			log.Println("Waiting for new image to test...")

			bootstrapComplete = true
		} else {
			// Look for a new image to test

			latestImageID, err := getLatestImageID(ctx, clientset, namespace, containerImage, nodeContainerName)
			if err != nil {
				log.Printf("failed to get latest image id: %v", err)
				return false, nil
			}

			if latestImageID == containerImage {
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
		}

		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for completion: %w", err)
	}

	// Avoid exiting immediately to avoid container restart before the pod is recreated with the new image
	time.Sleep(5 * time.Minute)
	return nil
}
