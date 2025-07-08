// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/rest"
)

// TODO(marun) need an easy way to cleanup stale nodes (either client side cli or a reaper)

const (
	containerName   = "avago"
	volumeName      = "data"
	volumeMountPath = "/data"

	statusCheckInterval = 500 * time.Millisecond

	// 2GB is the minimum size of a PersistentVolumeClaim used for a node's data directory:
	// - A value greater than 1GB must be used
	//   - A node will report unhealthy if it detects less than 1GiB available
	// - EBS volume sizes are in GB
	//   - The minimum number greater than 1GB is 2GB
	MinimumVolumeSizeGB = 2

	// All statefulsets configured for exclusive scheduling will use
	// anti-affinity with the following labeling to ensure their pods
	// are never scheduled to the same nodes.
	antiAffinityLabelKey   = "tmpnet-scheduling"
	antiAffinityLabelValue = "exclusive"

	// Name of config map containing tmpnet defaults
	defaultsConfigMapName = "tmpnet-defaults"
)

var errMissingSchedulingLabels = errors.New("--kube-scheduling-label-key and --kube-scheduling-label-value are required when exclusive scheduling is enabled")

type KubeRuntimeConfig struct {
	// Path to the kubeconfig file identifying the target cluster
	ConfigPath string `json:"configPath,omitempty"`
	// The context of the kubeconfig file to use
	ConfigContext string `json:"configContext,omitempty"`
	// Namespace in the target cluster in which resources will be
	// created. For simplicity all nodes are assumed to be deployed to
	// the same namespace to ensure network connectivity.
	Namespace string `json:"namespace,omitempty"`
	// The docker image to run for the node
	Image string `json:"image,omitempty"`
	// Size in gigabytes of the PersistentVolumeClaim  to allocate for the node
	VolumeSizeGB uint `json:"volumeSizeGB,omitempty"`
	// Whether to schedule each AvalancheGo node to a dedicated Kubernetes node
	UseExclusiveScheduling bool `json:"useExclusiveScheduling,omitempty"`
	// Label key to use for exclusive scheduling for node selection and toleration
	SchedulingLabelKey string `json:"schedulingLabelKey,omitempty"`
	// Label value to use for exclusive scheduling for node selection and toleration
	SchedulingLabelValue string `json:"schedulingLabelValue,omitempty"`
}

// ensureDefaults sets cluster-specific defaults for fields not already set by flags.
func (c *KubeRuntimeConfig) ensureDefaults(ctx context.Context, log logging.Logger) error {
	requireSchedulingDefaults := c.UseExclusiveScheduling && (len(c.SchedulingLabelKey) == 0 || len(c.SchedulingLabelValue) == 0)
	if !requireSchedulingDefaults {
		return nil
	}

	clientset, err := GetClientset(log, c.ConfigPath, c.ConfigContext)
	if err != nil {
		return err
	}

	log.Info("attempting to retrieve configmap containing tmpnet defaults",
		zap.String("namespace", c.Namespace),
		zap.String("configMap", defaultsConfigMapName),
	)

	configMap, err := clientset.CoreV1().ConfigMaps(c.Namespace).Get(ctx, defaultsConfigMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	var (
		schedulingLabelKey   = configMap.Data["schedulingLabelKey"]
		schedulingLabelValue = configMap.Data["schedulingLabelValue"]
	)
	if len(c.SchedulingLabelKey) == 0 && len(schedulingLabelKey) > 0 {
		log.Info("setting default value for SchedulingLabelKey",
			zap.String("schedulingLabelKey", schedulingLabelKey),
		)
		c.SchedulingLabelKey = schedulingLabelKey
	}
	if len(c.SchedulingLabelValue) == 0 && len(schedulingLabelValue) > 0 {
		log.Info("setting default value for SchedulingLabelValue",
			zap.String("schedulingLabelValue", schedulingLabelValue),
		)
		c.SchedulingLabelValue = schedulingLabelValue
	}

	// Validate that the scheduling labels are now set
	if len(c.SchedulingLabelKey) == 0 || len(c.SchedulingLabelValue) == 0 {
		return errMissingSchedulingLabels
	}

	return nil
}

type KubeRuntime struct {
	node *Node
}

// readState reads the URI and staking address for the node if the node is running.
func (p *KubeRuntime) readState(ctx context.Context) error {
	var (
		log             = p.node.network.log
		nodeID          = p.node.NodeID.String()
		runtimeConfig   = p.runtimeConfig()
		namespace       = runtimeConfig.Namespace
		statefulSetName = p.getStatefulSetName()
	)

	log.Debug("reading state for node",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)

	clientset, err := p.getClientset()
	if err != nil {
		return err
	}

	log.Debug("checking if StatefulSet exists",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)
	scale, err := clientset.AppsV1().StatefulSets(namespace).GetScale(ctx, statefulSetName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		log.Debug("StatefulSet not found",
			zap.String("nodeID", nodeID),
			zap.String("namespace", namespace),
			zap.String("statefulSet", statefulSetName),
		)
		p.setNotRunning()
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to retrieve scale of StatefulSet %s/%s for %s: %w", namespace, statefulSetName, nodeID, err)
	}

	if scale.Spec.Replicas == 0 {
		log.Debug("StatefulSet has no replicas",
			zap.String("nodeID", nodeID),
			zap.String("namespace", namespace),
			zap.String("statefulset", statefulSetName),
		)
		p.setNotRunning()
		return nil
	}

	if err := p.waitForPodReadiness(ctx); err != nil {
		p.setNotRunning()
		return fmt.Errorf("failed to wait for readiness of StatefulSet %s/%s for %s: %w", namespace, statefulSetName, nodeID, err)
	}

	return nil
}

// GetLocalURI retrieves a URI for the node intended to be accessible from this
// process until the provided cancel function is called.
func (p *KubeRuntime) GetLocalURI(ctx context.Context) (string, func(), error) {
	if len(p.node.URI) == 0 {
		// Assume that an empty URI indicates a need to read pod state
		if err := p.readState(ctx); err != nil {
			return "", func() {}, fmt.Errorf("failed to read Pod state: %w", err)
		}
	}

	// Use direct pod URI if running inside the cluster
	if IsRunningInCluster() {
		return p.node.URI, func() {}, nil
	}

	port, stopChan, err := p.forwardPort(ctx, config.DefaultHTTPPort)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port), func() { close(stopChan) }, nil
}

// GetLocalStakingAddress retrieves a StakingAddress for the node intended to be
// accessible from this process until the provided cancel function is called.
func (p *KubeRuntime) GetLocalStakingAddress(ctx context.Context) (netip.AddrPort, func(), error) {
	if p.node.StakingAddress == (netip.AddrPort{}) {
		// Assume that an empty staking address indicates a need to retrieve pod state
		if err := p.readState(ctx); err != nil {
			return netip.AddrPort{}, func() {}, fmt.Errorf("failed to read Pod state: %w", err)
		}
	}

	// Use direct pod staking address if running inside the cluster
	if IsRunningInCluster() {
		return p.node.StakingAddress, func() {}, nil
	}

	port, stopChan, err := p.forwardPort(ctx, config.DefaultStakingPort)
	if err != nil {
		return netip.AddrPort{}, nil, err
	}
	return netip.AddrPortFrom(
		netip.AddrFrom4([4]byte{127, 0, 0, 1}),
		port,
	), func() { close(stopChan) }, nil
}

// Start the node as a Kubernetes StatefulSet.
func (p *KubeRuntime) Start(ctx context.Context) error {
	var (
		log             = p.node.network.log
		nodeID          = p.node.NodeID.String()
		runtimeConfig   = p.runtimeConfig()
		namespace       = runtimeConfig.Namespace
		statefulSetName = p.getStatefulSetName()
	)

	log.Trace("starting node",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)

	clientset, err := p.getClientset()
	if err != nil {
		return err
	}

	log.Debug("attempting to retrieve existing StatefulSet",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)
	_, err = clientset.AppsV1().StatefulSets(namespace).Get(ctx, statefulSetName, metav1.GetOptions{})
	if err == nil {
		// Stateful exists - make sure it is scaled up and running

		log.Debug("attempting to retrieve scale for existing StatefulSet",
			zap.String("nodeID", nodeID),
			zap.String("namespace", namespace),
			zap.String("statefulSet", statefulSetName),
		)
		scale, err := clientset.AppsV1().StatefulSets(namespace).GetScale(ctx, statefulSetName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to retrieve scale for StatefulSet %s/%s: %w", namespace, statefulSetName, err)
		}

		if scale.Spec.Replicas != 0 {
			log.Debug("StatefulSet is already running",
				zap.String("nodeID", nodeID),
				zap.String("namespace", namespace),
				zap.String("statefulSet", statefulSetName),
			)
			return nil
		}

		log.Debug("attempting to scale up StatefulSet",
			zap.String("nodeID", nodeID),
			zap.String("namespace", namespace),
			zap.String("statefulSet", statefulSetName),
		)
		scale.Spec.Replicas = 1
		_, err = clientset.AppsV1().StatefulSets(runtimeConfig.Namespace).UpdateScale(
			ctx,
			statefulSetName,
			scale,
			metav1.UpdateOptions{},
		)
		if err != nil {
			return fmt.Errorf("failed to scale up StatefulSet for %s: %w", p.node.NodeID.String(), err)
		}

		log.Debug("scaled up StatefulSet",
			zap.String("nodeID", nodeID),
			zap.String("namespace", namespace),
			zap.String("statefulSet", statefulSetName),
		)

		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to retrieve StatefulSet %s/%s: %w", namespace, statefulSetName, err)
	}

	// StatefulSet does not exist - create it

	flags, err := p.getFlags()
	if err != nil {
		return err
	}

	log.Debug("creating StatefulSet",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)
	statefulSet := NewNodeStatefulSet(
		statefulSetName,
		false, // generateName
		runtimeConfig.Image,
		containerName,
		volumeName,
		fmt.Sprintf("%dGi", runtimeConfig.VolumeSizeGB),
		volumeMountPath,
		flags,
		p.node.getMonitoringLabels(),
	)

	if runtimeConfig.UseExclusiveScheduling {
		labelKey := runtimeConfig.SchedulingLabelKey
		labelValue := runtimeConfig.SchedulingLabelValue
		log.Debug("configuring exclusive scheduling",
			zap.String("nodeID", nodeID),
			zap.String("namespace", namespace),
			zap.String("statefulSet", statefulSetName),
			zap.String("schedulingLabelKey", labelKey),
			zap.String("schedulingLabelValue", labelValue),
		)
		if labelKey == "" || labelValue == "" {
			return errors.New("scheduling label key and value must be non-empty when exclusive scheduling is enabled")
		}
		configureExclusiveScheduling(&statefulSet.Spec.Template, labelKey, labelValue)
	}

	_, err = clientset.AppsV1().StatefulSets(runtimeConfig.Namespace).Create(
		ctx,
		statefulSet,
		metav1.CreateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to create StatefulSet: %w", err)
	}
	log.Debug("created StatefulSet",
		zap.String("nodeID", nodeID),
		zap.String("namespace", runtimeConfig.Namespace),
		zap.String("statefulSet", statefulSetName),
	)

	return p.ensureBootstrapIP(ctx)
}

// Stop the Pod by setting the replicas to zero on the StatefulSet.
func (p *KubeRuntime) InitiateStop(ctx context.Context) error {
	var (
		log             = p.node.network.log
		nodeID          = p.node.NodeID.String()
		runtimeConfig   = p.runtimeConfig()
		namespace       = runtimeConfig.Namespace
		statefulSetName = p.getStatefulSetName()
	)

	log.Trace("initiating node stop",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)

	clientset, err := p.getClientset()
	if err != nil {
		return err
	}
	log.Debug("retrieving StatefulSet scale",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)
	scale, err := clientset.AppsV1().StatefulSets(namespace).GetScale(ctx, statefulSetName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to retrieve scale for StatefulSet %s/%s: %w", namespace, statefulSetName, err)
	}

	if scale.Spec.Replicas == 0 {
		p.setNotRunning()
		return nil
	}

	log.Debug("setting StatefulSet replicas to zero",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)
	scale.Spec.Replicas = 0
	_, err = clientset.AppsV1().StatefulSets(p.runtimeConfig().Namespace).UpdateScale(
		ctx,
		statefulSetName,
		scale,
		metav1.UpdateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to replicas to zero for StatefulSet %s/%s for %s: %w", namespace, statefulSetName, nodeID, err)
	}

	log.Debug("StatefulSet replicas set to zero",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)

	p.setNotRunning()

	return nil
}

// Waits for the node process to stop.
// TODO(marun) Consider using a watch instead
func (p *KubeRuntime) WaitForStopped(ctx context.Context) error {
	var (
		log             = p.node.network.log
		nodeID          = p.node.NodeID.String()
		runtimeConfig   = p.runtimeConfig()
		namespace       = runtimeConfig.Namespace
		statefulSetName = p.getStatefulSetName()
	)

	log.Trace("waiting for node to stop",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)

	clientset, err := p.getClientset()
	if err != nil {
		return err
	}

	err = wait.PollUntilContextCancel(
		ctx,
		statusCheckInterval,
		true, // immediate
		func(ctx context.Context) (bool, error) {
			scale, err := clientset.AppsV1().StatefulSets(namespace).GetScale(ctx, statefulSetName, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				log.Debug("node stopped: StatefulSet not found",
					zap.String("nodeID", nodeID),
					zap.String("namespace", namespace),
					zap.String("statefulSet", statefulSetName),
				)
				p.setNotRunning()
				return true, nil
			}
			if err != nil {
				log.Warn("failed to retrieve StatefulSet scale",
					zap.String("nodeID", nodeID),
					zap.String("namespace", namespace),
					zap.String("statefulSet", statefulSetName),
					zap.Error(err),
				)
				return false, nil
			}
			if scale.Status.Replicas == 0 {
				log.Debug("node stopped: StatefulSet scaled to zero replicas",
					zap.String("nodeID", nodeID),
					zap.String("namespace", namespace),
					zap.String("statefulSet", statefulSetName),
				)
				p.setNotRunning()
				return true, nil
			}
			return false, nil
		},
	)
	if err != nil {
		return fmt.Errorf("failed to wait for StatefulSet %s/%s for %s to stop: %w", namespace, statefulSetName, nodeID, err)
	}

	return nil
}

// Restarts the node. Does not wait for readiness or health.
func (p *KubeRuntime) Restart(ctx context.Context) error {
	var (
		log             = p.node.network.log
		nodeID          = p.node.NodeID.String()
		runtimeConfig   = p.runtimeConfig()
		namespace       = runtimeConfig.Namespace
		statefulSetName = p.getStatefulSetName()
	)

	log.Trace("initiating node restart",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)

	clientset, err := p.getClientset()
	if err != nil {
		return err
	}

	statefulset, err := clientset.AppsV1().StatefulSets(namespace).Get(ctx, statefulSetName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// TODO(marun) Maybe optionally avoid restart if the patches will be no-op?

	// Collect patches to apply to the StatefulSet
	patches := []map[string]any{}

	flags, err := p.getFlags()
	if err != nil {
		return err
	}
	nodeEnv := flagsToEnvVarSlice(flags)
	patches = append(patches, map[string]any{
		"op":    "replace",
		"path":  "/spec/template/spec/containers/0/env",
		"value": envVarsToJSONValue(nodeEnv),
	})

	nodeImage := p.runtimeConfig().Image
	patches = append(patches, map[string]any{
		"op":    "replace",
		"path":  "/spec/template/spec/containers/0/image",
		"value": nodeImage,
	})

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return err
	}

	log.Debug("ensuring StatefulSet is up to date",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)
	updatedStatefulSet, err := clientset.AppsV1().StatefulSets(runtimeConfig.Namespace).Patch(
		ctx,
		p.getStatefulSetName(),
		types.JSONPatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return err
	}
	updatedGeneration := updatedStatefulSet.Generation

	if updatedGeneration == statefulset.Generation {
		log.Debug("StatefulSet generation unchanged. Forcing restart.",
			zap.String("nodeID", nodeID),
			zap.String("namespace", namespace),
			zap.String("statefulSet", statefulSetName),
		)

		// Force a restart by scaling up and down
		if err := p.InitiateStop(ctx); err != nil {
			return fmt.Errorf("failed to stop StatefulSet %s/%s for %s: %w", namespace, statefulSetName, nodeID, err)
		}
		if err := p.WaitForStopped(ctx); err != nil {
			return fmt.Errorf("failed to wait for StatefulSet %s/%s for %s to stop: %w", namespace, statefulSetName, nodeID, err)
		}
		return p.Start(ctx)
	}

	replicas := int32(1)
	err = wait.PollUntilContextCancel(
		ctx,
		statusCheckInterval,
		true, // immediate
		func(ctx context.Context) (bool, error) {
			statefulSet, err := clientset.AppsV1().StatefulSets(namespace).Get(ctx, statefulSetName, metav1.GetOptions{})
			if err != nil {
				log.Debug("failed to retrieve StatefulSet",
					zap.String("nodeID", nodeID),
					zap.String("namespace", namespace),
					zap.String("statefulSet", statefulSetName),
					zap.Error(err),
				)
				return false, nil
			}
			status := statefulSet.Status
			finishedRollingOut := (status.ObservedGeneration >= updatedStatefulSet.Generation &&
				status.Replicas == replicas &&
				status.ReadyReplicas == replicas &&
				status.CurrentReplicas == replicas &&
				status.UpdatedReplicas == replicas)
			if finishedRollingOut {
				log.Debug("StatefulSet finished rolling out",
					zap.String("nodeID", nodeID),
					zap.String("namespace", namespace),
					zap.String("name", statefulSetName),
				)
			}
			return finishedRollingOut, nil
		},
	)
	if err != nil {
		return fmt.Errorf("failed to wait for StatefulSet to finish rolling out: %w", err)
	}

	p.setNotRunning()

	return p.ensureBootstrapIP(ctx)
}

// IsHealthy checks if the node is running and healthy.
//
// TODO(marun) Add WaitForHealthy as a runtime method to minimize API calls required and
// enable reuse of forwarded connection when running external to the kubernetes cluster
func (p *KubeRuntime) IsHealthy(ctx context.Context) (bool, error) {
	err := p.readState(ctx)
	if err != nil {
		return false, err
	}
	if len(p.node.URI) == 0 {
		return false, errNotRunning
	}

	uri, cancel, err := p.GetLocalURI(ctx)
	if err != nil {
		return false, err
	}
	defer cancel()

	healthReply, err := CheckNodeHealth(ctx, uri)
	if errors.Is(err, ErrUnrecoverableNodeHealthCheck) {
		return false, err
	} else if err != nil {
		p.node.network.log.Verbo("failed to check node health",
			zap.String("nodeID", p.node.NodeID.String()),
			zap.Error(err),
		)
		return false, nil
	}
	return healthReply.Healthy, nil
}

// ensureBootstrapIP waits for this pod to be ready if there are no other pods already
// running to ensure the availability of a bootstrap node.
func (p *KubeRuntime) ensureBootstrapIP(ctx context.Context) error {
	var (
		log             = p.node.network.log
		nodeID          = p.node.NodeID.String()
		runtimeConfig   = p.runtimeConfig()
		namespace       = runtimeConfig.Namespace
		statefulSetName = p.getStatefulSetName()
	)
	bootstrapIPs, _ := p.node.network.GetBootstrapIPsAndIDs(p.node)
	if len(bootstrapIPs) > 0 {
		log.Debug("bootstrap IPs are already available so no need to wait for StatefulSet Pod to become ready",
			zap.String("nodeID", nodeID),
			zap.String("namespace", namespace),
			zap.String("statefulSet", statefulSetName),
		)
		return nil
	}

	log.Trace("waiting for node readiness so that subsequent nodes will have a bootstrap target",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("statefulSet", statefulSetName),
	)
	return p.waitForPodReadiness(ctx)
}

// Waits for the node's Pod to be ready, indicating that its API and
// staking endpoints are capable of serving traffic.
func (p *KubeRuntime) waitForPodReadiness(ctx context.Context) error {
	var (
		log           = p.node.network.log
		nodeID        = p.node.NodeID.String()
		runtimeConfig = p.runtimeConfig()
		namespace     = runtimeConfig.Namespace
		podName       = p.getPodName()
	)

	log.Debug("waiting for Pod to become ready",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("pod", podName),
	)

	clientset, err := p.getClientset()
	if err != nil {
		return err
	}

	if err := WaitForPodCondition(ctx, clientset, namespace, podName, corev1.PodReady); err != nil {
		return err
	}
	log.Debug("pod is ready",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("pod", podName),
	)

	log.Debug("retrieving Pod IP",
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("pod", podName),
	)
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(pod.Status.PodIP)
	if err != nil {
		return fmt.Errorf("failed to parse Pod IP: %w", err)
	}

	var (
		readyMsg string
		// Assume default ports. No reason to vary when Pods don't share port space.
		uri            = fmt.Sprintf("http://%s:%d", pod.Status.PodIP, config.DefaultHTTPPort)
		stakingAddress = netip.AddrPortFrom(addr, config.DefaultStakingPort)
	)
	if uri == p.node.URI && stakingAddress == p.node.StakingAddress {
		readyMsg = "node was already ready"
	} else {
		readyMsg = "node is ready"
		p.node.URI = uri
		p.node.StakingAddress = stakingAddress
	}
	log.Debug(readyMsg,
		zap.String("nodeID", nodeID),
		zap.String("namespace", namespace),
		zap.String("pod", podName),
		zap.String("uri", uri),
		zap.Stringer("stakingAddress", stakingAddress),
	)

	return nil
}

// getStatefulSetName determines the name of the node's StatefulSet from the network UUID and node ID.
func (p *KubeRuntime) getStatefulSetName() string {
	nodeIDString := p.node.NodeID.String()
	startIndex := len(ids.NodeIDPrefix)
	endIndex := startIndex + 8
	return p.node.network.UUID + "-" + strings.ToLower(nodeIDString[startIndex:endIndex])
}

// The Pod name is the StatefulSet name with a suffix of "-0" to indicate the first Pod in the StatefulSet
func (p *KubeRuntime) getPodName() string {
	return p.getStatefulSetName() + "-0"
}

func (p *KubeRuntime) runtimeConfig() *KubeRuntimeConfig {
	return p.node.getRuntimeConfig().Kube
}

func (p *KubeRuntime) getKubeconfig() (*restclient.Config, error) {
	runtimeConfig := p.runtimeConfig()
	return GetClientConfig(
		p.node.network.log,
		runtimeConfig.ConfigPath,
		runtimeConfig.ConfigContext,
	)
}

func (p *KubeRuntime) getClientset() (*kubernetes.Clientset, error) {
	kubeconfig, err := p.getKubeconfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return clientset, nil
}

func (p *KubeRuntime) forwardPort(ctx context.Context, port int) (uint16, chan struct{}, error) {
	kubeconfig, err := p.getKubeconfig()
	if err != nil {
		return 0, nil, err
	}
	clientset, err := p.getClientset()
	if err != nil {
		return 0, nil, err
	}

	var (
		namespace = p.runtimeConfig().Namespace
		podName   = p.getPodName()
	)

	// Wait for the Pod to become ready (otherwise it won't be accepting network connections)
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
		return 0, nil, fmt.Errorf("failed to enable local forward for Pod: %w", err)
	}
	return forwardedPort, stopChan, nil
}

func (p *KubeRuntime) setNotRunning() {
	p.node.network.log.Debug("node is not running",
		zap.Stringer("nodeID", p.node.NodeID),
	)
	p.node.URI = ""
	p.node.StakingAddress = netip.AddrPort{}
}

// getFlags determines the set of avalanchego flags to configure the node with.
func (p *KubeRuntime) getFlags() (FlagsMap, error) {
	flags, err := p.node.composeFlags()
	if err != nil {
		return nil, err
	}
	// The data dir path is fixed for the Pod
	flags[config.DataDirKey] = volumeMountPath
	// The node must bind to the Pod IP to enable the kubelet to access the http port for the readiness check
	flags[config.HTTPHostKey] = "0.0.0.0"
	return flags, nil
}

// configureExclusiveScheduling ensures that the provided template schedules only to nodes with the provided
// labeling, tolerates a taint that matches the labeling, and uses anti-affinity to ensure only a single
// avalanchego pod is scheduled to a given target node.
func configureExclusiveScheduling(template *corev1.PodTemplateSpec, labelKey string, labelValue string) {
	podSpec := &template.Spec

	// Configure node selection
	if podSpec.NodeSelector == nil {
		podSpec.NodeSelector = make(map[string]string)
	}
	podSpec.NodeSelector[labelKey] = labelValue

	// Configure toleration. Nodes are assumed to have a taint with the same
	// key+value as the label used to select it.
	podSpec.Tolerations = []corev1.Toleration{
		{
			Key:      labelKey,
			Operator: corev1.TolerationOpEqual,
			Value:    labelValue,
			Effect:   corev1.TaintEffectNoExecute,
		},
	}

	// Configure anti-affinity to ensure only one pod per node
	templateMeta := &template.ObjectMeta
	if templateMeta.Labels == nil {
		templateMeta.Labels = make(map[string]string)
	}
	templateMeta.Labels[antiAffinityLabelKey] = antiAffinityLabelValue
	podSpec.Affinity = &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							antiAffinityLabelKey: antiAffinityLabelValue,
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}
}

// IsRunningInCluster detects if this code is running inside a Kubernetes cluster
// by checking for the presence of the service account token that's automatically
// mounted in every pod.
func IsRunningInCluster() bool {
	_, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token")
	return err == nil
}
