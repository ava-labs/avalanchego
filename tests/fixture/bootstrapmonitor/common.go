// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ava-labs/avalanchego/utils/logging"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/rest"
)

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
			return errors.New("timeout waiting for pod readiness")
		}
	}
}

// getContainerImage retrieves the image of the specified container in the specified pod
func GetContainerImage(context context.Context, clientset *kubernetes.Clientset, namespace string, podName string, containerName string) (string, error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(context, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get pod %s.%s: %w", namespace, podName, err)
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName {
			return container.Image, nil
		}
	}
	return "", fmt.Errorf("failed to find container %q in pod %s.%s", containerName, namespace, podName)
}

// setContainerImage sets the image of the specified container of the pod's owning statefulset
func setContainerImage(ctx context.Context, log logging.Logger, clientset *kubernetes.Clientset, namespace string, podName string, containerName string, image string) error {
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

	// Define the strategic merge patch data updating the image
	patchData := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"name":  containerName,
							"image": image,
						},
					},
				},
			},
		},
	}

	// Convert patch data to JSON
	patchBytes, err := json.Marshal(patchData)
	if err != nil {
		return fmt.Errorf("failed to marshal patch data: %w", err)
	}

	// Apply the patch
	_, err = clientset.AppsV1().StatefulSets(namespace).Patch(context.TODO(), statefulSetName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch statefulset %s.%s: %w", namespace, statefulSetName, err)
	}
	log.Info("Updated statefulset to target new image",
		zap.String("namespace", namespace),
		zap.String("statefulSetName", statefulSetName),
		zap.String("image", image),
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

// getLatestImageID retrieves the image id for the avalanchego image with tag `latest`.
func getLatestImageID(
	ctx context.Context,
	log logging.Logger,
	clientset *kubernetes.Clientset,
	namespace string,
	imageName string,
	containerName string,
) (string, error) {
	baseImageName, err := getBaseImageName(log, imageName)
	if err != nil {
		return "", err
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
					Args:    []string{"--version"},
					Image:   baseImageName + ":latest",
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
	createdPod, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to start pod %w", err)
	}

	err = waitForPodStatus(ctx, clientset, namespace, createdPod.Name, func(status *corev1.PodStatus) bool {
		return status.Phase == corev1.PodSucceeded || status.Phase == corev1.PodFailed
	})
	if err != nil {
		return "", fmt.Errorf("failed to wait for pod termination: %w", err)
	}

	terminatedPod, err := clientset.CoreV1().Pods(namespace).Get(ctx, createdPod.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to retrieve terminated pod: %w", err)
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
		return "", fmt.Errorf("failed to get image id for pod %s.%s", namespace, createdPod.Name)
	}

	// Only delete the pod if successful to aid in debugging
	err = clientset.CoreV1().Pods(namespace).Delete(ctx, createdPod.Name, metav1.DeleteOptions{})
	if err != nil {
		return "", err
	}

	return imageID, nil
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