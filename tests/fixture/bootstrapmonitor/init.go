// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/ava-labs/avalanchego/utils/perms"
)

const (
	initTimeout   = 2 * time.Minute
	retryInterval = 5 * time.Second

	recordedImageFilename = "bootstrap_image_name.txt"
)

func InitBootstrapTest(namespace string, podName string, nodeContainerName string, dataDir string) error {
	var (
		clientset      *kubernetes.Clientset
		containerImage string
	)
	return wait.PollImmediateInfinite(retryInterval, func() (bool, error) {
		if clientset == nil {
			var err error
			if clientset, err = getClientset(); err != nil {
				log.Printf("failed to get clientset: %v", err)
				return false, nil
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
		defer cancel()

		if len(containerImage) == 0 {
			// Retrieve the image used by the node container
			var err error
			log.Printf("Retrieving pod %s.%s to determine the image of container %q", namespace, podName, nodeContainerName)
			if containerImage, err = GetContainerImage(ctx, clientset, namespace, podName, nodeContainerName); err != nil {
				log.Printf("failed to get container image: %v", err)
				return false, nil
			}
			log.Printf("Image for container %q: %s", nodeContainerName, containerImage)
		}

		// If the image uses the latest tag, determine the latest image id and set the container image to that
		if strings.HasSuffix(containerImage, ":latest") {
			log.Printf("Determining image id for image %q", containerImage)
			imageID, err := getLatestImageID(ctx, clientset, namespace, containerImage, nodeContainerName)
			if err != nil {
				log.Printf("failed to get latest image id: %v", err)
				return false, nil
			}
			log.Printf("Updating owning statefulset with image %q", containerImage)
			if err := setContainerImage(ctx, clientset, namespace, podName, nodeContainerName, imageID); err != nil {
				log.Printf("failed to set container image: %v", err)
				return false, nil
			}
		}

		// A bootstrap is being resumed if a version file exists and the image name it contains matches the container
		// image. If a bootstrap is being started, the version file should be created and the data path cleared.

		recordedImagePath := filepath.Join(dataDir, recordedImageFilename)

		var recordedImage string
		if recordedImageBytes, err := os.ReadFile(recordedImagePath); errors.Is(err, os.ErrNotExist) {
			log.Println("Recorded image file does not exist")
		} else if err != nil {
			log.Printf("failed to read recorded image file: %v", err)
			return false, nil
		} else {
			recordedImage = string(recordedImageBytes)
			log.Printf("Recorded image name: %s", recordedImage)
		}

		if recordedImage == containerImage {
			log.Println("Recorded image name matches current image name")
			log.Println(BootstrapResumingMessage(containerImage))
			return true, nil
		} else if len(recordedImage) > 0 {
			log.Println("Recorded image name differs from the current image name")
		}
		log.Println(BootstrapStartingMessage(containerImage))

		nodeDataDir := NodeDataDir(dataDir)
		log.Printf("Removing contents of node directory %s", nodeDataDir)
		if err := os.RemoveAll(nodeDataDir); err != nil {
			log.Printf("failed to remove contents of node directory: %v", err)
			return false, nil
		}

		log.Printf("Writing %q to %s", containerImage, recordedImagePath)
		if err := os.WriteFile(recordedImagePath, []byte(containerImage), perms.ReadWrite); err != nil {
			log.Printf("failed to write version file: %v", err)
			return false, nil
		}

		return true, nil
	})
}

func BootstrapStartingMessage(containerImage string) string {
	return "Starting bootstrap test for image " + containerImage
}

func BootstrapResumingMessage(containerImage string) string {
	return "Resuming bootstrap test for image " + containerImage
}

func NodeDataDir(path string) string {
	return path + "/avalanchego"
}
