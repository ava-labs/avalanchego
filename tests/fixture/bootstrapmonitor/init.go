// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

const (
	initTimeout   = 2 * time.Minute
	retryInterval = 5 * time.Second

	recordedImageFilename = "bootstrap_image_name.txt"

	BootstrapStartingMessage = "Starting bootstrap test"
	BootstrapResumingMessage = "Resuming bootstrap test"
)

func NodeDataDir(path string) string {
	return path + "/avalanchego"
}

func InitBootstrapTest(log logging.Logger, namespace string, podName string, nodeContainerName string, dataDir string) error {
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

	// If the image uses the latest tag, determine the latest image id and set the container image to that
	if strings.HasSuffix(testConfig.Image, ":latest") {
		log.Info("Determining image id for image", zap.String("image", testConfig.Image))
		latestImageDetails, err := getLatestImageDetails(ctx, log, clientset, namespace, testConfig.Image, nodeContainerName)
		if err != nil {
			return fmt.Errorf("failed to get latest image details: %w", err)
		}
		log.Info("Updating owning statefulset with image details",
			zap.String("image", latestImageDetails.Image),
			zap.Reflect("versions", latestImageDetails.Versions),
		)
		if err := setImageDetails(ctx, log, clientset, namespace, podName, nodeContainerName, latestImageDetails); err != nil {
			return fmt.Errorf("failed to set container image: %w", err)
		}
	}

	// A bootstrap is being resumed if a version file exists and the image name it contains matches the container
	// image. If a bootstrap is being started, the version file should be created and the data path cleared.

	recordedImagePath := filepath.Join(dataDir, recordedImageFilename)

	var recordedImage string
	if recordedImageBytes, err := os.ReadFile(recordedImagePath); errors.Is(err, os.ErrNotExist) {
		log.Info("Recorded image file does not exist")
	} else if err != nil {
		return fmt.Errorf("failed to read recorded image file: %w", err)
	} else {
		recordedImage = string(recordedImageBytes)
		log.Info("Recorded image name", zap.String("image", recordedImage))
	}

	if recordedImage == testConfig.Image {
		log.Info("Recorded image name matches current image name")
		log.Info(BootstrapResumingMessage, zap.Reflect("testConfig", testConfig))
		return nil
	} else if len(recordedImage) > 0 {
		log.Info("Recorded image name differs from the current image name")
	}

	nodeDataDir := NodeDataDir(dataDir)
	log.Info("Removing node directory", zap.String("path", nodeDataDir))
	if err := os.RemoveAll(nodeDataDir); err != nil {
		return fmt.Errorf("failed to remove contents of node directory: %w", err)
	}

	log.Info("Writing image name to file",
		zap.String("image", testConfig.Image),
		zap.String("path", recordedImagePath),
	)
	if err := os.WriteFile(recordedImagePath, []byte(testConfig.Image), perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write image name to file: %w", err)
	}

	log.Info(BootstrapStartingMessage, zap.Reflect("testConfig", testConfig))

	return nil
}
