// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils/logging"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/rest"
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

	localURIPort         uint16
	localURIPortStopChan chan struct{}
}

func (p *NodePod) getPodContextPath() string {
	return filepath.Join(p.node.GetDataDir(), "pod.json")
}

// TODO(marun) Factor out common elements from node process and node pod
// On restart, readState is always called
// TODO(marun) When the node is not found to be running, clear the node URI?
func (p *NodePod) readState(ctx context.Context) error {
	clientset, kubeconfig, err := p.getClientsetAndConfig()
	if err != nil {
		return err
	}

	statefulSetName := p.getStatefulSetName()
	namespace := p.runtimeConfig().Namespace

	// Check if the statefulset exists
	scale, err := clientset.AppsV1().StatefulSets(namespace).GetScale(ctx, statefulSetName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// TODO(marun) Perform cleanup - the statefulset does not exist
		return nil
	}
	if err != nil {
		return err
	}

	// Check if the statefulset is scaled up
	if scale.Status.Replicas == 0 {
		return nil
	}

	podName := statefulSetName + "-0"

	// Wait for the pod to become ready (otherwise it won't be accepting network connections)
	// TODO(marun) What to do when the pod does not actually exist (hasn't started)?
	if err := WaitForPodCondition(ctx, clientset, namespace, podName, corev1.PodReady); err != nil {
		return err
	}

	// if inside cluster
	//   - TODO(marun) support this
	//   - use the pod IP
	//   - use the standard http port
	// if outside cluster
	//   - use 127.0.0.1
	//   - forward a local port to the pod IP + standard port

	// TODO(marun) Handle restarts

	// A forwarded connection enables connectivity without exposing the node external to the kube cluster
	// TODO(marun) Detect if running in a cluster, where this won't be necessary
	// TODO(marun) Without being able to reuse the forwarded connection, it won't be possible to interact directly with the node
	//             So we need to be able to reuse that connection. Now, how to get it closed?

	// TODO(marun) Need to forward the staking port as well (e.g. for L1 test)
	p.localURIPort, p.localURIPortStopChan, err = enableLocalForwardForPod(
		kubeconfig,
		namespace,
		podName,
		config.DefaultHTTPPort,
		io.Discard, // Ignore stdout output
		os.Stderr,
	)
	if err != nil {
		return fmt.Errorf("failed to enable local forward for pod: %w", err)
	}
	localNodeURI := fmt.Sprintf("http://127.0.0.1:%d", p.localURIPort)

	// TODO(marun) Clear on error
	p.node.URI = localNodeURI
	// The staking address will only be accessible inside the cluster (i.e. to bootstrap )
	p.node.StakingAddress = netip.AddrPortFrom(
		netip.AddrFrom4([4]byte{127, 0, 0, 1}),
		9651,
	)

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

	clientset, _, err := p.getClientsetAndConfig()
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

	return nil
}

// Stop the pod by setting the replicas to zero on the statefulset.
func (p *NodePod) InitiateStop(ctx context.Context) error {
	clientset, _, err := p.getClientsetAndConfig()
	if err != nil {
		return err
	}
	statefulSetName := p.getStatefulSetName()
	scale, err := clientset.AppsV1().StatefulSets(p.runtimeConfig().Namespace).GetScale(ctx, statefulSetName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if scale.Spec.Replicas == 0 {
		// TODO(marun) Ensure local state is cleared
		return nil
	}
	scale.Spec.Replicas = 0
	_, err = clientset.AppsV1().StatefulSets(p.runtimeConfig().Namespace).UpdateScale(
		ctx,
		statefulSetName,
		scale,
		metav1.UpdateOptions{},
	)
	return err
}

// Waits for the node process to stop.
// TODO(marun) Consider using a watch instead
func (p *NodePod) WaitForStopped(ctx context.Context) error {
	clientset, _, err := p.getClientsetAndConfig()
	if err != nil {
		return err
	}
	statefulSetName := p.getStatefulSetName()
	namespace := p.runtimeConfig().Namespace

	ticker := time.NewTicker(defaultNodeTickerInterval)
	defer ticker.Stop()
	for {
		scale, err := clientset.AppsV1().StatefulSets(namespace).GetScale(ctx, statefulSetName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			// TODO(marun) Perform cleanup - the statefulset does not exist
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to retrieve scale of statefulset: %w", err)
		}
		if scale.Status.Replicas == 0 {
			// TODO(marun) Perform cleanup - the statefulset has been scaled down
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to see statefulset for node %q scale down before timeout: %w", p.node.NodeID, ctx.Err())
		case <-ticker.C:
		}
	}

	return nil
}

func (p *NodePod) IsHealthy(ctx context.Context, log logging.Logger) (bool, error) {
	err := p.readState(ctx)
	if err != nil {
		return false, err
	}
	if len(p.node.URI) == 0 {
		return false, errNotRunning
	}

	healthReply, err := CheckNodeHealth(ctx, p.node.URI)
	if errors.Is(ErrUnrecoverableNodeHealthCheck, err) {
		return false, err
	}
	return healthReply.Healthy, nil
}

func (p *NodePod) getClientsetAndConfig() (*kubernetes.Clientset, *restclient.Config, error) {
	runtimeConfig := p.runtimeConfig()
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = runtimeConfig.Kubeconfig
	}
	kubeconfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return clientset, kubeconfig, nil
}

func (p *NodePod) runtimeConfig() KubeRuntimeConfig {
	return p.node.RuntimeConfig.KubeRuntimeConfig
}

func (p *NodePod) getStatefulSet(ctx context.Context) (*appsv1.StatefulSet, error) {
	clientset, _, err := p.getClientsetAndConfig()
	if err != nil {
		return nil, err
	}
	return clientset.AppsV1().StatefulSets(p.runtimeConfig().Namespace).Get(
		ctx,
		p.getStatefulSetName(),
		metav1.GetOptions{},
	)
}
