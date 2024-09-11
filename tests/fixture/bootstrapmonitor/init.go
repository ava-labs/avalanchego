// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

const (
	initTimeout   = 2 * time.Minute
	retryInterval = 5 * time.Second

	recordedImageFilename = "bootstrap_image_name.txt"

	BootstrapStartingMessage = "Starting bootstrap test for image"
	BootstrapResumingMessage = "Resuming bootstrap test for image"
)

func NodeDataDir(path string) string {
	return path + "/avalanchego"
}

func InitBootstrapTest(log logging.Logger, namespace string, podName string, nodeContainerName string, dataDir string) error {
	var (
		clientset      *kubernetes.Clientset
		containerImage string
	)
	return wait.PollImmediateInfinite(retryInterval, func() (bool, error) {
		if clientset == nil {
			var err error
			if clientset, err = getClientset(log); err != nil {
				log.Error("failed to get clientset", zap.Error(err))
				return false, nil
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
		defer cancel()

		if len(containerImage) == 0 {
			// Retrieve the image used by the node container
			var err error
			log.Info("Retrieving pod to determine image used by container",
				zap.String("namespace", namespace),
				zap.String("pod", podName),
				zap.String("container", nodeContainerName),
			)
			if containerImage, err = GetContainerImage(ctx, clientset, namespace, podName, nodeContainerName); err != nil {
				log.Error("failed to get container image", zap.Error(err))
				return false, nil
			}
			log.Info("Image for container",
				zap.String("container", nodeContainerName),
				zap.String("image", containerImage),
			)
		}

		// If the image uses the latest tag, determine the latest image id and set the container image to that
		if strings.HasSuffix(containerImage, ":latest") {
			log.Info("Determining image id for image", zap.String("image", containerImage))
			imageID, err := getLatestImageID(ctx, log, clientset, namespace, containerImage, nodeContainerName)
			if err != nil {
				log.Error("failed to get latest image id", zap.Error(err))
				return false, nil
			}
			log.Info("Updating owning statefulset with image", zap.String("image", imageID))
			if err := setContainerImage(ctx, log, clientset, namespace, podName, nodeContainerName, imageID); err != nil {
				log.Error("failed to set container image", zap.Error(err))
				return false, nil
			}
		}

		// A bootstrap is being resumed if a version file exists and the image name it contains matches the container
		// image. If a bootstrap is being started, the version file should be created and the data path cleared.

		recordedImagePath := filepath.Join(dataDir, recordedImageFilename)

		var recordedImage string
		if recordedImageBytes, err := os.ReadFile(recordedImagePath); errors.Is(err, os.ErrNotExist) {
			log.Info("Recorded image file does not exist")
		} else if err != nil {
			log.Error("failed to read recorded image file", zap.Error(err))
			return false, nil
		} else {
			recordedImage = string(recordedImageBytes)
			log.Info("Recorded image name", zap.String("image", recordedImage))
		}

		if recordedImage == containerImage {
			log.Info("Recorded image name matches current image name")
			log.Info(BootstrapResumingMessage, zap.String("image", containerImage))
			return true, nil
		} else if len(recordedImage) > 0 {
			log.Info("Recorded image name differs from the current image name")
		}

		nodeDataDir := NodeDataDir(dataDir)
		log.Info("Removing node directory", zap.String("path", nodeDataDir))
		if err := os.RemoveAll(nodeDataDir); err != nil {
			log.Error("failed to remove contents of node directory", zap.Error(err))
			return false, nil
		}

		log.Info("Writing image name to file",
			zap.String("image", containerImage),
			zap.String("path", recordedImagePath),
		)
		if err := os.WriteFile(recordedImagePath, []byte(containerImage), perms.ReadWrite); err != nil {
			log.Error("failed to write image name to file", zap.Error(err))
			return false, nil
		}

		log.Info(BootstrapStartingMessage, zap.String("image", containerImage))

		return true, nil
	})
}
