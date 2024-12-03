// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	node       *Node
	podContext PodContext
}

func (p *NodePod) getPodContextPath() string {
	return filepath.Join(p.node.GetDataDir(), "pod.json")
}

// TODO(marun) Factor out common elements from node process and node pod
func (p *NodePod) readState() error {
	path := p.getPodContextPath()
	bytes, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		// The absence of the pod context file indicates the node is not running
		p.podContext = PodContext{}
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to read node pod context: %w", err)
	}
	podContext := PodContext{}
	if err := json.Unmarshal(bytes, &podContext); err != nil {
		return fmt.Errorf("failed to unmarshal node pod context: %w", err)
	}
	p.podContext = podContext

	return nil
}

func (p *NodePod) SetDefaultFlags() {
	p.node.Flags.SetDefaults(DefaultKubeFlags())
}

// TODO(marun) Maybe better with a timestamp used as input to generateName and include uuid and node id as labels?
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
	if err := perms.WriteFile(p.getPodContextPath(), bytes, perms.ReadWrite); err != nil {
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
	// TODO(marun) Implement
	// Need to

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
// TODO(marun) Implement
func (p *NodePod) WaitForStopped(_ context.Context) error {
	// Wait for the status on the replicaset to indicate no replicas
	return nil
}

func (p *NodePod) IsHealthy(ctx context.Context, log logging.Logger) (bool, error) {

	healthReply, err := CheckNodeHealth(ctx, localNodeURI)
	if errors.Is(ErrUnrecoverableNodeHealthCheck, err) {
		return false, err
	}
	return healthReply.Healthy, nil
}
