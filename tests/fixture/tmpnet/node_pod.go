// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"io"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	containerName   = "avago"
	volumeName      = "data"
	volumeMountPath = "/data"
)

type NodePod struct {
	node *Node
}

// Start the node as a kubernetes statefulset.
func (p *NodePod) Start(w io.Writer) error {
	// Create a statefulset for the pod and wait for it to become ready
	nodeIDString := p.node.NodeID.String()
	name := p.node.NetworkUUID + "-" + nodeIDString[:8]
	runtimeConfig := p.node.RuntimeConfig.KubeRuntimeConfig
	// TODO(marun) Figure out how to best to configure flags
	flags := DefaultKubeFlags(volumeMountPath).SetDefaults(p.node.Flags)
	statefulSet := NewNodeStatefulSet(
		name,
		runtimeConfig.ImageName,
		containerName,
		volumeName,
		volumeMountPath,
		flags,
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

	return nil
}

func (p *NodePod) getClientset() (*kubernetes.Clientset, error) {
	kubeconfig, err := clientcmd.BuildConfigFromFlags("", runtimeConfig.Kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to build kubeconfig: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}
	return clientset
}

// Stop the pod by setting the replicas to zero on the statefulset.
func (p *NodePod) InitiateStop() error {
	clientset, err := p.getClientset()
	if err != nil {
		return err
	}

	runtimeConfig := p.node.RuntimeConfig.KubeRuntimeConfig

	createdStatefulSet, err := clientset.AppsV1().StatefulSets(runtimeConfig.Namespace).Get(context.Background(),
		statefulSet,
		metav1.CreateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to create statefulset: %w", err)
	}

	return nil
}

// Waits for the node process to stop.
func (p *NodePod) WaitForStopped(_ context.Context) error {
	// Wait for the status on the replicaset to indicate no replicas
	return nil
}

func (p *NodePod) IsHealthy(_ context.Context) (bool, error) {
	return false, nil
}
