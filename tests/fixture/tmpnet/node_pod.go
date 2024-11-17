// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils/logging"
	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	containerName   = "avago"
	volumeName      = "data"
	volumeMountPath = "/data"

	// TODO(marun) Need to make this configurable
	volumeSize = "128Mi"
)

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

// Start the node as a kubernetes statefulset.
func (p *NodePod) Start(log logging.Logger) error {
	// Create a statefulset for the pod and wait for it to become ready
	nodeIDString := p.node.NodeID.String()
	unwantedNodeIDPrefix := "NodeID-"
	startIndex := len(unwantedNodeIDPrefix)
	endIndex := len(unwantedNodeIDPrefix) + 8
	name := p.node.NetworkUUID + "-" + strings.ToLower(nodeIDString[startIndex:endIndex])
	runtimeConfig := p.node.RuntimeConfig.KubeRuntimeConfig
	statefulSetFlags := p.node.Flags.Copy()
	statefulSetFlags[config.DataDirKey] = volumeMountPath
	statefulSet := NewNodeStatefulSet(
		name,
		runtimeConfig.ImageName,
		containerName,
		volumeName,
		volumeSize,
		volumeMountPath,
		statefulSetFlags,
	)

	// Serialize the StatefulSet to YAML
	yamlData, err := yaml.Marshal(statefulSet.Spec.Template.Spec.Containers[0].Env)
	if err != nil {
		return fmt.Errorf("failed to serialize StatefulSet to YAML: %w", err)
	}

	// Pretty-print the YAML
	fmt.Println(string(yamlData))

	// clientset, err := p.getClientset()
	// if err != nil {
	// 	return err
	// }

	// //createdStatefulSet, err := clientset.AppsV1().StatefulSets(runtimeConfig.Namespace).Create(
	// _, err = clientset.AppsV1().StatefulSets(runtimeConfig.Namespace).Create(
	// 	context.Background(),
	// 	statefulSet,
	// 	metav1.CreateOptions{},
	// )
	// if err != nil {
	// 	return fmt.Errorf("failed to create statefulset: %w", err)
	// }

	// Update pod.json
	// - add kubeconfig file
	// - add namespace
	// - add way to ID the statefulset (i.e. name or labels)

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

func (p *NodePod) IsHealthy(_ context.Context) (bool, error) {
	//
	return false, nil
}
