// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"

	chaosv1alpha1 "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

type recordingLogger struct {
	logging.NoLog
	warnings []string
}

func (l *recordingLogger) Warn(msg string, _ ...zap.Field) {
	l.warnings = append(l.warnings, msg)
}

func TestTopologyNetworkChaosObjects(t *testing.T) {
	require := require.New(t)

	network := NewDefaultNetwork("test")
	network.Nodes = NewNodesOrPanic(3)
	network.DefaultRuntimeConfig.Kube = &KubeRuntimeConfig{Namespace: "tmpnet", IngressHost: "localhost"}
	require.Len(network.Nodes, 3)
	require.NoError(network.EnsureDefaultConfig(t.Context(), logging.NoLog{}))
	for _, node := range network.Nodes {
		require.NoError(network.EnsureNodeConfig(node))
	}

	network.Topology = &Topology{
		Locations: []Location{
			{Name: "a", NodeIDs: []ids.NodeID{network.Nodes[0].NodeID, network.Nodes[1].NodeID}},
			{Name: "b", NodeIDs: []ids.NodeID{network.Nodes[2].NodeID}},
		},
		Connectivity: []Connection{{From: "a", To: "b", Latency: "10ms"}},
	}

	objects, err := network.topologyNetworkChaosObjects()
	require.NoError(err)
	require.Len(objects, 1)

	obj := objects[0]
	require.Equal("NetworkChaos", obj.Kind)
	require.Equal(chaosMeshNamespace, obj.Namespace)
	require.Equal(network.TopologyConnectionResourceName(Connection{From: "a", To: "b", Latency: "10ms"}), obj.Name)

	require.Equal(chaosv1alpha1.DelayAction, obj.Spec.Action)
	require.Equal(chaosv1alpha1.To, obj.Spec.Direction)
	require.NotNil(obj.Spec.Delay)
	require.Equal("10ms", obj.Spec.Delay.Latency)
	require.Equal([]string{"tmpnet"}, obj.Spec.Selector.Namespaces)
	require.Equal(map[string]string{
		"network_uuid":           network.UUID,
		topologyLocationLabelKey: "a",
	}, obj.Spec.Selector.LabelSelectors)
	require.NotNil(obj.Spec.Target)
	require.Equal(map[string]string{
		"network_uuid":           network.UUID,
		topologyLocationLabelKey: "b",
	}, obj.Spec.Target.Selector.LabelSelectors)
}

func TestNodeKubeLabelsIncludeTopologyLocation(t *testing.T) {
	require := require.New(t)

	network := NewDefaultNetwork("test")
	network.Nodes = NewNodesOrPanic(2)
	network.DefaultRuntimeConfig.Kube = &KubeRuntimeConfig{Namespace: "tmpnet", IngressHost: "localhost"}
	require.NoError(network.EnsureDefaultConfig(t.Context(), logging.NoLog{}))
	for _, node := range network.Nodes {
		require.NoError(network.EnsureNodeConfig(node))
	}
	firstNode := network.Nodes[0]
	network.Topology = &Topology{
		Locations: []Location{{Name: "chicago", NodeIDs: []ids.NodeID{firstNode.NodeID}}},
	}

	labels := firstNode.getKubeLabels()
	require.Equal("chicago", labels[topologyLocationLabelKey])
}

func TestTopologyDuplicateConnectionFails(t *testing.T) {
	require := require.New(t)

	network := NewDefaultNetwork("test")
	network.Nodes = NewNodesOrPanic(2)
	network.DefaultRuntimeConfig.Kube = &KubeRuntimeConfig{Namespace: "tmpnet", IngressHost: "localhost"}
	require.NoError(network.EnsureDefaultConfig(t.Context(), logging.NoLog{}))
	for _, node := range network.Nodes {
		require.NoError(network.EnsureNodeConfig(node))
	}
	network.Topology = &Topology{
		Locations: []Location{
			{Name: "a", NodeIDs: []ids.NodeID{network.Nodes[0].NodeID}},
			{Name: "b", NodeIDs: []ids.NodeID{network.Nodes[1].NodeID}},
		},
		Connectivity: []Connection{
			{From: "a", To: "b", Latency: "10ms"},
			{From: "a", To: "b", Latency: "20ms"},
		},
	}

	_, err := network.topologyNetworkChaosObjects()
	require.ErrorIs(err, errTopologyDuplicateConnection)
}

func TestTopologyDuplicateNodeMembershipFails(t *testing.T) {
	require := require.New(t)

	network := NewDefaultNetwork("test")
	network.Nodes = NewNodesOrPanic(1)
	network.DefaultRuntimeConfig.Kube = &KubeRuntimeConfig{Namespace: "tmpnet", IngressHost: "localhost"}
	require.NoError(network.EnsureDefaultConfig(t.Context(), logging.NoLog{}))
	for _, node := range network.Nodes {
		require.NoError(network.EnsureNodeConfig(node))
	}
	nodeID := network.Nodes[0].NodeID
	network.Topology = &Topology{
		Locations: []Location{
			{Name: "a", NodeIDs: []ids.NodeID{nodeID}},
			{Name: "b", NodeIDs: []ids.NodeID{nodeID}},
		},
	}

	_, err := network.topologyLocationNames()
	require.ErrorIs(err, errTopologyNodeAssignedMultipleLocation)
}

func TestTopologyModeDefaultsToRequired(t *testing.T) {
	require := require.New(t)

	network := &Network{}
	require.Equal(TopologyModeRequired, network.topologyMode())

	network.Topology = &Topology{}
	require.Equal(TopologyModeRequired, network.topologyMode())

	network.Topology.Mode = TopologyModeBestEffort
	require.Equal(TopologyModeBestEffort, network.topologyMode())
}

func TestTopologyApplicabilityRequiredFailsOnProcessRuntime(t *testing.T) {
	require := require.New(t)

	network := NewDefaultNetwork("test")
	network.log = logging.NoLog{}
	network.DefaultRuntimeConfig.Process = &ProcessRuntimeConfig{}
	for _, node := range network.Nodes {
		node.network = network
	}
	network.Topology = &Topology{}
	applicability, reason, err := network.topologyApplicability()
	require.ErrorIs(err, errTopologyRequiresKubeRuntime)
	require.Equal(topologyApplicabilityNotConfigured, applicability)
	require.Empty(reason)
}

func TestTopologyApplicabilityBestEffortSkipsOnProcessRuntime(t *testing.T) {
	require := require.New(t)

	network := NewDefaultNetwork("test")
	network.log = logging.NoLog{}
	network.DefaultRuntimeConfig.Process = &ProcessRuntimeConfig{}
	for _, node := range network.Nodes {
		node.network = network
	}
	network.Topology = &Topology{Mode: TopologyModeBestEffort}
	applicability, reason, err := network.topologyApplicability()
	require.NoError(err)
	require.Equal(topologyApplicabilitySkipBestEffort, applicability)
	require.Contains(reason, "does not use the kube runtime")
}

func TestApplyTopologyBestEffortWarnsAndSkipsOnProcessRuntime(t *testing.T) {
	require := require.New(t)

	log := &recordingLogger{}
	network := NewDefaultNetwork("test")
	network.log = log
	network.DefaultRuntimeConfig.Process = &ProcessRuntimeConfig{}
	for _, node := range network.Nodes {
		node.network = network
	}
	network.Topology = &Topology{Mode: TopologyModeBestEffort}

	require.NoError(network.ApplyTopology(t.Context()))
	require.Len(log.warnings, 1)
	require.Equal("skipping topology application", log.warnings[0])
}

func TestRemoveTopologyBestEffortWarnsAndSkipsOnProcessRuntime(t *testing.T) {
	require := require.New(t)

	log := &recordingLogger{}
	network := NewDefaultNetwork("test")
	network.log = log
	network.DefaultRuntimeConfig.Process = &ProcessRuntimeConfig{}
	for _, node := range network.Nodes {
		node.network = network
	}
	network.Topology = &Topology{Mode: TopologyModeBestEffort}

	require.NoError(network.RemoveTopology(t.Context()))
	require.Len(log.warnings, 1)
	require.Equal("skipping topology removal", log.warnings[0])
}

func TestTopologyApplicabilitySupportedForKubeRuntime(t *testing.T) {
	require := require.New(t)

	network := NewDefaultNetwork("test")
	network.DefaultRuntimeConfig.Kube = &KubeRuntimeConfig{Namespace: "tmpnet", IngressHost: "localhost"}
	network.log = logging.NoLog{}
	network.Topology = &Topology{}
	for _, node := range network.Nodes {
		require.NoError(network.EnsureNodeConfig(node))
	}

	applicability, reason, err := network.topologyApplicability()
	require.NoError(err)
	require.Equal(topologyApplicabilitySupported, applicability)
	require.Empty(reason)
}

func TestTopologyConnectionResourceNameIsDeterministic(t *testing.T) {
	require := require.New(t)

	network := NewDefaultNetwork("test")
	first := network.TopologyConnectionResourceName(Connection{From: "a", To: "b", Latency: "10ms"})
	second := network.TopologyConnectionResourceName(Connection{From: "a", To: "b", Latency: "20ms"})
	third := network.TopologyConnectionResourceName(Connection{From: "b", To: "a", Latency: "10ms"})

	require.Equal(first, second)
	require.NotEqual(first, third)
	require.Regexp(`^tmpnet-topology-[a-f0-9]{16}$`, first)
}

func TestManagedTopologyResourceInjected(t *testing.T) {
	require := require.New(t)

	obj := &chaosv1alpha1.NetworkChaos{
		Status: chaosv1alpha1.NetworkChaosStatus{
			ChaosStatus: chaosv1alpha1.ChaosStatus{
				Conditions: []chaosv1alpha1.ChaosCondition{
					{Type: chaosv1alpha1.ConditionSelected, Status: corev1.ConditionTrue},
					{Type: chaosv1alpha1.ConditionAllInjected, Status: corev1.ConditionTrue},
				},
			},
		},
	}
	require.True(ManagedTopologyResourceInjected(obj))

	obj = &chaosv1alpha1.NetworkChaos{
		Status: chaosv1alpha1.NetworkChaosStatus{
			ChaosStatus: chaosv1alpha1.ChaosStatus{
				Conditions: []chaosv1alpha1.ChaosCondition{
					{Type: chaosv1alpha1.ConditionSelected, Status: corev1.ConditionTrue},
					{Type: chaosv1alpha1.ConditionAllInjected, Status: corev1.ConditionFalse},
				},
			},
		},
	}
	require.False(ManagedTopologyResourceInjected(obj))

	require.False(ManagedTopologyResourceInjected(&chaosv1alpha1.NetworkChaos{}))
}
