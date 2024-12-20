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

	// TODO(marun) Consider adding loggers everywhere instead of passing them in
	// log logging.Logger
}

func (p *NodePod) getPodContextPath() string {
	return filepath.Join(p.node.GetDataDir(), "pod.json")
}

func (p *NodePod) setNodeAddresses(uri string, stakingAddress netip.AddrPort) {
	p.node.URI = uri
	p.node.StakingAddress = stakingAddress
}

// TODO(marun) Factor out common elements from node process and node pod
// On restart, readState is always called
// TODO(marun) When the node is not found to be running, clear the node URI?
func (p *NodePod) readState(ctx context.Context) error {
	clientset, _, err := p.getClientsetAndConfig()
	if err != nil {
		return err
	}

	statefulSetName := p.getStatefulSetName()
	namespace := p.runtimeConfig().Namespace

	// Check if the statefulset exists
	scale, err := clientset.AppsV1().StatefulSets(namespace).GetScale(ctx, statefulSetName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		p.setNodeAddresses("", netip.AddrPort{})
		return nil
	}
	if err != nil {
		return err
	}

	// Check if the statefulset is scaled down
	if scale.Status.Replicas == 0 {
		p.setNodeAddresses("", netip.AddrPort{})
		return nil
	}

	podName := statefulSetName + "-0"

	// Wait for the pod to become ready (otherwise it won't be accepting network connections)
	if err := WaitForPodCondition(ctx, clientset, namespace, podName, corev1.PodReady); err != nil {
		return err
	}

	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(pod.Status.PodIP)
	if err != nil {
		return fmt.Errorf("failed to parse pod IP: %w", err)
	}

	// Assume standard ports. No reason to vary when pods don't share port space.
	p.node.URI = fmt.Sprintf("http://%s:%d", pod.Status.PodIP, config.DefaultHTTPPort)
	p.node.StakingAddress = netip.AddrPortFrom(addr, config.DefaultStakingPort)

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
	return p.node.getNetwork().UUID + "-" + strings.ToLower(nodeIDString[startIndex:endIndex])
}

// Start the node as a kubernetes statefulset.
func (p *NodePod) Start(ctx context.Context) error {
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
	p.node.getNetwork().Log.Debug("created statefulset",
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

// Restarts the node
func (p *NodePod) Restart(ctx context.Context) error {
	// How to apply a configuration change?
	// As soon as a configuration change is applied the pod will be restarted.

	return nil
}

func (p *NodePod) IsHealthy(ctx context.Context) (bool, error) {
	err := p.readState(ctx)
	if err != nil {
		return false, err
	}
	if len(p.node.URI) == 0 {
		return false, errNotRunning
	}

	// TODO(marun) Reuse this forwarded connection for more than a single health check
	uri, cancel, err := p.GetLocalURI(ctx)
	if err != nil {
		return false, err
	}
	defer cancel()

	healthReply, err := CheckNodeHealth(ctx, uri)
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

func (p *NodePod) forwardPort(ctx context.Context, port int) (uint16, chan struct{}, error) {
	clientset, kubeconfig, err := p.getClientsetAndConfig()
	if err != nil {
		return 0, nil, err
	}

	statefulSetName := p.getStatefulSetName()
	namespace := p.runtimeConfig().Namespace

	podName := statefulSetName + "-0"

	// Wait for the pod to become ready (otherwise it won't be accepting network connections)
	if err := WaitForPodCondition(ctx, clientset, namespace, podName, corev1.PodReady); err != nil {
		return 0, nil, err
	}

	forwardedPort, stopChan, err := enableLocalForwardForPod(
		kubeconfig,
		namespace,
		podName,
		port,
		io.Discard, // Ignore stdout output
		os.Stderr,
	)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to enable local forward for pod: %w", err)
	}
	return forwardedPort, stopChan, nil
}

func (p *NodePod) GetLocalURI(ctx context.Context) (string, func(), error) {
	if len(p.node.URI) == 0 {
		return "", func() {}, errNotRunning
	}

	// TODO(marun) Auto-detect whether this test code is running inside the cluster
	//             and use the URI directly

	port, stopChan, err := p.forwardPort(ctx, config.DefaultHTTPPort)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port), func() { close(stopChan) }, nil
}

func (p *NodePod) GetLocalStakingAddress(ctx context.Context) (netip.AddrPort, func(), error) {
	if len(p.node.URI) == 0 {
		return netip.AddrPort{}, func() {}, errNotRunning
	}

	// TODO(marun) Auto-detect whether this test code is running inside the cluster
	//             and use the URI directly

	port, stopChan, err := p.forwardPort(ctx, config.DefaultStakingPort)
	if err != nil {
		return netip.AddrPort{}, nil, err
	}
	return netip.AddrPortFrom(
		netip.AddrFrom4([4]byte{127, 0, 0, 1}),
		port,
	), func() { close(stopChan) }, nil
}
