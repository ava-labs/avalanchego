// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	containerName   = "avago"
	volumeName      = "data"
	volumeMountPath = "/data"

	// TODO(marun) Need to make this configurable
	volumeSize = "128Mi"
)

type PodContext struct {
	// TODO(marun) Should the context of the kubeconfig be included?
	Kubeconfig  string
	Namespace   string
	StatefulSet string
}

type NodePod struct {
	node *Node
}

func (p *NodePod) readState() error {
	// TODO(marun)
	return nil
}

func (p *NodePod) SetDefaultFlags() {
	p.node.Flags.SetDefaults(DefaultKubeFlags())
}

func (p *NodePod) getStatefulSetName() string {
	nodeIDString := p.node.NodeID.String()
	unwantedNodeIDPrefix := "NodeID-"
	startIndex := len(unwantedNodeIDPrefix)
	endIndex := len(unwantedNodeIDPrefix) + 8
	return p.node.NetworkUUID + "-" + strings.ToLower(nodeIDString[startIndex:endIndex])
}

// Start the node as a kubernetes statefulset.
func (p *NodePod) Start(log logging.Logger) error {
	// Create a statefulset for the pod and wait for it to become ready
	runtimeConfig := p.node.RuntimeConfig.KubeRuntimeConfig
	statefulSetFlags := p.node.Flags.Copy()
	statefulSetFlags[config.DataDirKey] = volumeMountPath
	statefulSet := NewNodeStatefulSet(
		p.getStatefulSetName(),
		false, // generateName
		runtimeConfig.ImageName,
		containerName,
		volumeName,
		volumeSize,
		volumeMountPath,
		statefulSetFlags,
	)

	clientset, err := p.getClientset()
	if err != nil {
		return err
	}
	createdStatefulSet, err := clientset.AppsV1().StatefulSets(runtimeConfig.Namespace).Create(
		context.Background(),
		statefulSet,
		metav1.CreateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to create statefulset: %w", err)
	}
	log.Debug("created statefulset",
		zap.String("namespace", runtimeConfig.Namespace),
		zap.String("name", createdStatefulSet.Name),
	)
	podContext := PodContext{
		Kubeconfig:  runtimeConfig.Kubeconfig,
		Namespace:   runtimeConfig.Namespace,
		StatefulSet: createdStatefulSet.Name,
	}
	bytes, err := DefaultJSONMarshal(podContext)
	if err != nil {
		return fmt.Errorf("failed to marshal pod context: %w", err)
	}
	// TODO(marun) Define the file path once for reuse
	if err := perms.WriteFile(filepath.Join(p.node.GetDataDir(), "pod.json"), bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write pod context: %w", err)
	}

	return nil
}

func (p *NodePod) getClientset() (*kubernetes.Clientset, error) {
	kubeconfig, err := clientcmd.BuildConfigFromFlags("", p.node.RuntimeConfig.KubeRuntimeConfig.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return clientset, nil
}

// Stop the pod by setting the replicas to zero on the statefulset.
func (p *NodePod) InitiateStop() error {
	// clientset, err := p.getClientset()
	// if err != nil {
	// 	return err
	// }

	// Discover the stateful set to target
	// runtimeConfig := p.node.RuntimeConfig.KubeRuntimeConfig

	// TODO(marun) Scale down the statefulset

	// createdStatefulSet, err := clientset.AppsV1().StatefulSets(runtimeConfig.Namespace).Get(context.Background(),
	// 	statefulSet,
	// 	metav1.CreateOptions{},
	// )
	// if err != nil {
	// 	return fmt.Errorf("failed to create statefulset: %w", err)
	// }

	return nil
}

// Waits for the node process to stop.
func (p *NodePod) WaitForStopped(_ context.Context) error {
	// Wait for the status on the replicaset to indicate no replicas
	return nil
}

func (p *NodePod) IsHealthy(ctx context.Context, log logging.Logger) (bool, error) {
	runtimeConfig := p.node.RuntimeConfig.KubeRuntimeConfig
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = runtimeConfig.Kubeconfig
	}
	kubeconfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return false, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	podName := p.getStatefulSetName() + "-0"

	clientset, err := p.getClientset()
	if err != nil {
		return false, err
	}

	// Wait for the pod to become ready (otherwise it won't be accepting network connections)
	if err := WaitForPodCondition(ctx, clientset, runtimeConfig.Namespace, podName, corev1.PodReady); err != nil {
		return false, err
	}

	// A forwarded connection enables connectivity without exposing the node external to the kube cluster
	// TODO(marun) Detect if running in a cluster, where this won't be necessary
	localPort, localPortStopChan, err := enableLocalForwardForPod(
		kubeconfig,
		runtimeConfig.Namespace,
		podName,
		config.DefaultHTTPPort,
		io.Discard, // Ignore stdout output
		os.Stderr,
	)
	if err != nil {
		return false, fmt.Errorf("failed to enable local forward for pod: %w", err)
	}
	defer close(localPortStopChan)
	localNodeURI := fmt.Sprintf("http://127.0.0.1:%d", localPort)
	healthReply, err := CheckNodeHealth(ctx, localNodeURI)
	if errors.Is(ErrUnrecoverableNodeHealthCheck, err) {
		return false, err
	}
	return healthReply.Healthy, nil
}
