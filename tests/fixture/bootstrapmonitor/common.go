// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/rest"
)

// Path to write the details to on the data volume
func getTestDetailsPath(dataDir string) string {
	return filepath.Join(dataDir, "bootstrap_test_details.txt")
}

// Used to serialize test details to the data volume used for a given test to
// support resuming a previously started test and tracking test duration.
type bootstrapTestDetails struct {
	Image     string    `json:"image"`
	StartTime time.Time `json:"startTime"`
}

// WaitForPodCondition watches the specified pod until the status includes the specified condition.
func WaitForPodCondition(ctx context.Context, clientset *kubernetes.Clientset, namespace string, podName string, conditionType corev1.PodConditionType) error {
	return waitForPodStatus(
		ctx,
		clientset,
		namespace,
		podName,
		func(status *corev1.PodStatus) bool {
			for _, condition := range status.Conditions {
				if condition.Type == conditionType && condition.Status == corev1.ConditionTrue {
					return true
				}
			}
			return false
		},
	)
}

// waitForPodStatus watches the specified pod until the status is deemed acceptable by the provided test function.
func waitForPodStatus(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	name string,
	acceptable func(*corev1.PodStatus) bool,
) error {
	watch, err := clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.SingleObject(metav1.ObjectMeta{Name: name}))
	if err != nil {
		return fmt.Errorf("failed to initiate watch of pod %s/%s: %w", namespace, name, err)
	}

	for {
		select {
		case event := <-watch.ResultChan():
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}

			if acceptable(&pod.Status) {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for pod readiness: %w", ctx.Err())
		}
	}
}

// setImageDetails updates the pod's owning statefulset with the image of the specified container and associated version details
func setImageDetails(ctx context.Context, log logging.Logger, clientset *kubernetes.Clientset, namespace string, podName string, imageDetails *ImageDetails) error {
	// Determine the name of the statefulset to update
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get pod %s.%s: %w", namespace, podName, err)
	}
	if len(pod.OwnerReferences) != 1 {
		return errors.New("pod does not have exactly one owner reference")
	}
	ownerReference := pod.OwnerReferences[0]
	if ownerReference.Kind != "StatefulSet" {
		return errors.New("unexpected owner reference kind: " + ownerReference.Kind)
	}
	statefulSetName := ownerReference.Name

	// Marshal the versions to JSON
	versionJSONBytes, err := json.Marshal(imageDetails.Versions)
	if err != nil {
		return fmt.Errorf("failed to marshal versions: %w", err)
	}

	// Create the JSON patch
	patchData := []map[string]interface{}{
		{
			"op":    "replace",
			"path":  "/spec/template/spec/containers/0/image",
			"value": imageDetails.Image,
		},
		{
			"op":    "replace",
			"path":  "/spec/template/metadata/annotations/" + strings.ReplaceAll(VersionsAnnotationKey, "/", "~1"),
			"value": string(versionJSONBytes),
		},
	}

	// Convert patch data to JSON
	patchBytes, err := json.Marshal(patchData)
	if err != nil {
		return fmt.Errorf("failed to marshal patch data: %w", err)
	}

	// Apply the patch
	_, err = clientset.AppsV1().StatefulSets(namespace).Patch(context.TODO(), statefulSetName, types.JSONPatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch statefulset %s.%s: %w", namespace, statefulSetName, err)
	}
	log.Info("Updated statefulset to target new image",
		zap.String("namespace", namespace),
		zap.String("statefulSetName", statefulSetName),
		zap.String("image", imageDetails.Image),
		zap.Reflect("versions", imageDetails.Versions),
	)
	return nil
}

// getBaseImageName removes the tag from the image name
func getBaseImageName(log logging.Logger, imageName string) (string, error) {
	if strings.Contains(imageName, "@") {
		// Image name contains a digest, remove it
		return strings.Split(imageName, "@")[0], nil
	}

	imageNameParts := strings.Split(imageName, ":")
	switch len(imageNameParts) {
	case 1:
		// No tag or registry
		return imageName, nil
	case 2:
		// Ambiguous image name - could contain a tag or a registry
		log.Info("Derived tag-less image name from string",
			zap.String("tagLessImageName", imageNameParts[0]),
			zap.String("imageName", imageName),
		)
		return imageNameParts[0], nil
	case 3:
		// Image name contains a registry and a tag - remove the tag
		return strings.Join(imageNameParts[0:2], ":"), nil
	default:
		return "", fmt.Errorf("unexpected image name format: %q", imageName)
	}
}

type ImageDetails struct {
	Image    string
	Versions *version.Versions
}

// GetLatestImageDetails retrieves the image details for the avalanchego image with tag `latest`.
func getLatestImageDetails(
	ctx context.Context,
	log logging.Logger,
	clientset *kubernetes.Clientset,
	namespace string,
	imageName string,
	containerName string,
) (*ImageDetails, error) {
	baseImageName, err := getBaseImageName(log, imageName)
	if err != nil {
		return nil, err
	}

	// Start a new pod with the `latest`-tagged avalanchego image to discover its image ID
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "avalanchego-version-check-",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    containerName,
					Command: []string{"./avalanchego"},
					Args:    []string{"--version-json"},
					Image:   baseImageName + ":latest",
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
	createdPod, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to start pod %w", err)
	}
	qualifiedPodName := fmt.Sprintf("%s.%s", namespace, createdPod.Name)

	err = waitForPodStatus(ctx, clientset, namespace, createdPod.Name, func(status *corev1.PodStatus) bool {
		return status.Phase == corev1.PodSucceeded || status.Phase == corev1.PodFailed
	})
	if err != nil {
		return nil, fmt.Errorf("failed to wait for pod %s to terminate: %w", qualifiedPodName, err)
	}

	terminatedPod, err := clientset.CoreV1().Pods(namespace).Get(ctx, createdPod.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve terminated pod %s: %w", qualifiedPodName, err)
	}

	// Get the image id for the avalanchego image
	imageID := ""
	for _, status := range terminatedPod.Status.ContainerStatuses {
		if status.Name == containerName {
			imageID = status.ImageID
			break
		}
	}
	if len(imageID) == 0 {
		return nil, fmt.Errorf("failed to get image id for pod %s", qualifiedPodName)
	}

	// Get the logs for the pod
	req := clientset.CoreV1().Pods(namespace).GetLogs(createdPod.Name, &corev1.PodLogOptions{
		Container: containerName,
	})
	logStream, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for pod %s: %w", qualifiedPodName, err)
	}
	defer logStream.Close()
	logs, err := io.ReadAll(logStream)
	if err != nil {
		return nil, fmt.Errorf("failed to read logs for pod %s: %w", qualifiedPodName, err)
	}

	// Attempt to unmarshal the logs to a Versions instance
	versions := &version.Versions{}
	if err := json.Unmarshal(logs, versions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal logs for pod %s: %w", qualifiedPodName, err)
	}

	// Only delete the pod if successful to aid in debugging
	err = clientset.CoreV1().Pods(namespace).Delete(ctx, createdPod.Name, metav1.DeleteOptions{})
	if err != nil {
		return nil, err
	}

	return &ImageDetails{
		Image:    imageID,
		Versions: versions,
	}, nil
}

func getClientset(log logging.Logger) (*kubernetes.Clientset, error) {
	log.Info("Initializing clientset")
	kubeconfigPath := os.Getenv("KUBECONFIG")
	var (
		kubeconfig *restclient.Config
		err        error
	)
	if len(kubeconfigPath) > 0 {
		// Only use BuildConfigFromFlags if a path is provided to avoid the warning logs that
		// will be omitted in a format that differs from the avalanchego format.
		if kubeconfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath); err != nil {
			return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
		}
	} else {
		if kubeconfig, err = restclient.InClusterConfig(); err != nil {
			return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
		}
	}
	clientset, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return clientset, nil
}
