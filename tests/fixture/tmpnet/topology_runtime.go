// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"

	chaosv1alpha1 "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	topologyManagedByLabelKey   = "tmpnet.topology"
	topologyManagedByLabelValue = "true"
	topologyLocationLabelKey    = "tmpnet.avax.network/location"
	topologyPollInterval        = 250 * time.Millisecond
)

var (
	errTopologyRequiresKubeRuntime          = errors.New("topology requires kube runtime for all nodes")
	errTopologyDuplicateConnection          = errors.New("duplicate topology connection")
	errTopologyNodeAssignedMultipleLocation = errors.New("topology node assigned to multiple locations")

	topologyClientLoggerOnce sync.Once
)

type topologyApplicability int

const (
	topologyApplicabilityNotConfigured topologyApplicability = iota
	topologyApplicabilitySupported
	topologyApplicabilitySkipBestEffort
)

func (n *Network) ApplyTopology(ctx context.Context) error {
	applicability, reason, err := n.topologyApplicability()
	if err != nil {
		return stacktrace.Wrap(err)
	}
	switch applicability {
	case topologyApplicabilityNotConfigured:
		return nil
	case topologyApplicabilitySkipBestEffort:
		n.log.Warn("skipping topology application",
			zap.String("networkUUID", n.UUID),
			zap.String("reason", reason),
			zap.String("mode", string(n.topologyMode())),
		)
		return nil
	}

	chaosClient, err := n.getTopologyClient()
	if err != nil {
		return stacktrace.Wrap(err)
	}

	objects, err := n.topologyNetworkChaosObjects()
	if err != nil {
		return stacktrace.Wrap(err)
	}

	if err := n.removeManagedTopology(ctx, chaosClient); err != nil {
		return stacktrace.Wrap(err)
	}

	if len(objects) == 0 {
		n.log.Info("no topology connections to apply",
			zap.String("networkUUID", n.UUID),
		)
		return nil
	}

	n.log.Info("applying topology connections",
		zap.String("networkUUID", n.UUID),
		zap.Int("connections", len(objects)),
	)

	for _, obj := range objects {
		n.log.Debug("creating topology connection",
			zap.String("name", obj.GetName()),
		)
		if err := chaosClient.Create(ctx, obj); err != nil {
			return stacktrace.Errorf("failed to create topology connection %s: %w", obj.GetName(), err)
		}
	}

	return stacktrace.Wrap(wait.PollUntilContextTimeout(ctx, topologyPollInterval, DefaultNetworkTimeout, true, func(ctx context.Context) (bool, error) {
		for _, desired := range objects {
			current := &chaosv1alpha1.NetworkChaos{}
			err := chaosClient.Get(ctx, types.NamespacedName{Namespace: desired.GetNamespace(), Name: desired.GetName()}, current)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			if !ManagedTopologyResourceInjected(current) {
				return false, nil
			}
		}
		return true, nil
	}))
}

func (n *Network) RemoveTopology(ctx context.Context) error {
	applicability, reason, err := n.topologyApplicability()
	if err != nil {
		return stacktrace.Wrap(err)
	}
	switch applicability {
	case topologyApplicabilityNotConfigured:
		return nil
	case topologyApplicabilitySkipBestEffort:
		n.log.Warn("skipping topology removal",
			zap.String("networkUUID", n.UUID),
			zap.String("reason", reason),
			zap.String("mode", string(n.topologyMode())),
		)
		return nil
	}

	chaosClient, err := n.getTopologyClient()
	if err != nil {
		return stacktrace.Wrap(err)
	}
	return stacktrace.Wrap(n.removeManagedTopology(ctx, chaosClient))
}

func (n *Network) removeManagedTopology(ctx context.Context, chaosClient ctrlclient.Client) error {
	list, err := n.managedTopologyResources(ctx, chaosClient)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	if len(list.Items) > 0 {
		n.log.Info("removing topology connections",
			zap.String("networkUUID", n.UUID),
			zap.Int("connections", len(list.Items)),
		)
	}
	for i := range list.Items {
		item := &list.Items[i]
		n.log.Debug("deleting topology connection",
			zap.String("name", item.GetName()),
		)
		if err := chaosClient.Delete(ctx, item); err != nil && !apierrors.IsNotFound(err) {
			return stacktrace.Errorf("failed to delete topology resource %s: %w", item.GetName(), err)
		}
	}

	return stacktrace.Wrap(wait.PollUntilContextTimeout(ctx, topologyPollInterval, DefaultNetworkTimeout, true, func(ctx context.Context) (bool, error) {
		list, err := n.managedTopologyResources(ctx, chaosClient)
		if err != nil {
			return false, err
		}
		return len(list.Items) == 0, nil
	}))
}

func (n *Network) getTopologyClient() (ctrlclient.Client, error) {
	if len(n.Nodes) == 0 {
		return nil, stacktrace.New("topology requires at least one node")
	}

	topologyClientLoggerOnce.Do(func() {
		ctrllog.SetLogger(logr.Discard())
	})

	kubeRuntime := &KubeRuntime{node: n.Nodes[0]}
	kubeConfig, err := kubeRuntime.getKubeconfig()
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}

	scheme := runtime.NewScheme()
	if err := chaosv1alpha1.AddToScheme(scheme); err != nil {
		return nil, stacktrace.Wrap(err)
	}

	client, err := ctrlclient.New(kubeConfig, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return client, nil
}

func (n *Network) topologyMode() TopologyMode {
	if n.Topology == nil || n.Topology.Mode == "" {
		return TopologyModeRequired
	}
	return n.Topology.Mode
}

func (n *Network) topologyApplicability() (topologyApplicability, string, error) {
	if n.Topology == nil {
		return topologyApplicabilityNotConfigured, "", nil
	}
	if len(n.Nodes) == 0 {
		return topologyApplicabilityNotConfigured, "", stacktrace.New("topology requires at least one node")
	}
	for _, node := range n.Nodes {
		if node.getRuntimeConfig().Kube == nil {
			reason := fmt.Sprintf("node %s does not use the kube runtime", node.NodeID)
			if n.topologyMode() == TopologyModeBestEffort {
				return topologyApplicabilitySkipBestEffort, reason, nil
			}
			return topologyApplicabilityNotConfigured, "", stacktrace.Errorf("%w: %s", errTopologyRequiresKubeRuntime, reason)
		}
	}
	return topologyApplicabilitySupported, "", nil
}

func (n *Network) topologyNetworkChaosObjects() ([]*chaosv1alpha1.NetworkChaos, error) {
	if err := n.validateTopologyConnectivity(); err != nil {
		return nil, stacktrace.Wrap(err)
	}

	objects := make([]*chaosv1alpha1.NetworkChaos, 0, len(n.Topology.Connectivity))
	namespace := n.Nodes[0].getRuntimeConfig().Kube.Namespace
	for _, connection := range n.Topology.Connectivity {
		objects = append(objects, &chaosv1alpha1.NetworkChaos{
			TypeMeta: metav1.TypeMeta{
				APIVersion: chaosv1alpha1.GroupVersion.String(),
				Kind:       "NetworkChaos",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      n.TopologyConnectionResourceName(connection),
				Namespace: chaosMeshNamespace,
				Labels: map[string]string{
					topologyManagedByLabelKey: topologyManagedByLabelValue,
					"network_uuid":            n.UUID,
					"topology_from_location":  connection.From,
					"topology_to_location":    connection.To,
				},
			},
			Spec: chaosv1alpha1.NetworkChaosSpec{
				PodSelector: chaosv1alpha1.PodSelector{
					Selector: chaosv1alpha1.PodSelectorSpec{
						GenericSelectorSpec: chaosv1alpha1.GenericSelectorSpec{
							Namespaces: []string{namespace},
							LabelSelectors: map[string]string{
								"network_uuid":           n.UUID,
								topologyLocationLabelKey: connection.From,
							},
						},
					},
					Mode: chaosv1alpha1.AllMode,
				},
				Action:    chaosv1alpha1.DelayAction,
				Direction: chaosv1alpha1.To,
				Target: &chaosv1alpha1.PodSelector{
					Selector: chaosv1alpha1.PodSelectorSpec{
						GenericSelectorSpec: chaosv1alpha1.GenericSelectorSpec{
							Namespaces: []string{namespace},
							LabelSelectors: map[string]string{
								"network_uuid":           n.UUID,
								topologyLocationLabelKey: connection.To,
							},
						},
					},
					Mode: chaosv1alpha1.AllMode,
				},
				TcParameter: chaosv1alpha1.TcParameter{
					Delay: &chaosv1alpha1.DelaySpec{Latency: connection.Latency},
				},
			},
		})
	}
	return objects, nil
}

func (n *Network) topologyLocationNames() (map[string]struct{}, error) {
	byID := make(map[string]*Node, len(n.Nodes))
	for _, node := range n.Nodes {
		byID[node.NodeID.String()] = node
	}

	locations := make(map[string]struct{}, len(n.Topology.Locations))
	assignedNodeLocations := make(map[string]string, len(n.Nodes))
	for _, location := range n.Topology.Locations {
		if len(location.Name) == 0 {
			return nil, stacktrace.New("topology location missing name")
		}
		if _, exists := locations[location.Name]; exists {
			return nil, stacktrace.Errorf("duplicate topology location %q", location.Name)
		}
		locations[location.Name] = struct{}{}
		for _, nodeID := range location.NodeIDs {
			if _, ok := byID[nodeID.String()]; !ok {
				return nil, stacktrace.Errorf("topology location %q references unknown node %s", location.Name, nodeID)
			}
			if existingLocation, exists := assignedNodeLocations[nodeID.String()]; exists {
				return nil, stacktrace.Errorf("%w: %s assigned to multiple locations: %q and %q", errTopologyNodeAssignedMultipleLocation, nodeID, existingLocation, location.Name)
			}
			assignedNodeLocations[nodeID.String()] = location.Name
		}
	}
	return locations, nil
}

func (n *Network) topologyLocationForNode(node *Node) (string, bool) {
	if n.Topology == nil {
		return "", false
	}
	for _, location := range n.Topology.Locations {
		for _, nodeID := range location.NodeIDs {
			if nodeID == node.NodeID {
				return location.Name, true
			}
		}
	}
	return "", false
}

func topologyConnectionKey(connection Connection) string {
	return fmt.Sprintf("%s\x00%s", connection.From, connection.To)
}

func (n *Network) TopologyConnectionResourceName(connection Connection) string {
	hash := sha256.Sum256([]byte(n.UUID + "\x00" + topologyConnectionKey(connection)))
	return "tmpnet-topology-" + hex.EncodeToString(hash[:8])
}

func (n *Network) TopologyConnectionResourceNames() map[string]Connection {
	names := make(map[string]Connection, len(n.Topology.Connectivity))
	if n.Topology == nil {
		return names
	}
	for _, connection := range n.Topology.Connectivity {
		names[n.TopologyConnectionResourceName(connection)] = connection
	}
	return names
}

func (n *Network) ManagedTopologyResources(ctx context.Context) ([]chaosv1alpha1.NetworkChaos, error) {
	chaosClient, err := n.getTopologyClient()
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}
	list, err := n.managedTopologyResources(ctx, chaosClient)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return list.Items, nil
}

func (n *Network) managedTopologyResources(ctx context.Context, chaosClient ctrlclient.Client) (*chaosv1alpha1.NetworkChaosList, error) {
	list := &chaosv1alpha1.NetworkChaosList{}
	if err := chaosClient.List(
		ctx,
		list,
		ctrlclient.InNamespace(chaosMeshNamespace),
		ctrlclient.MatchingLabels{
			topologyManagedByLabelKey: topologyManagedByLabelValue,
			"network_uuid":            n.UUID,
		},
	); err != nil {
		return nil, stacktrace.Errorf("failed to list managed topology resources: %w", err)
	}
	return list, nil
}

func (n *Network) validateTopologyConnectivity() error {
	locationNames, err := n.topologyLocationNames()
	if err != nil {
		return stacktrace.Wrap(err)
	}

	seenConnections := make(map[string]Connection, len(n.Topology.Connectivity))
	for _, connection := range n.Topology.Connectivity {
		if _, ok := locationNames[connection.From]; !ok {
			return stacktrace.Errorf("unknown topology source location %q", connection.From)
		}
		if _, ok := locationNames[connection.To]; !ok {
			return stacktrace.Errorf("unknown topology target location %q", connection.To)
		}
		if len(connection.Latency) == 0 {
			return stacktrace.Errorf("missing latency for topology connection %q -> %q", connection.From, connection.To)
		}

		key := topologyConnectionKey(connection)
		if previous, exists := seenConnections[key]; exists {
			return stacktrace.Errorf(
				"%w: %q -> %q (previous latency %q, duplicate latency %q)",
				errTopologyDuplicateConnection,
				connection.From,
				connection.To,
				previous.Latency,
				connection.Latency,
			)
		}
		seenConnections[key] = connection
	}
	return nil
}

func ManagedTopologyResourceInjected(obj *chaosv1alpha1.NetworkChaos) bool {
	for _, condition := range obj.Status.Conditions {
		if condition.Type == chaosv1alpha1.ConditionAllInjected && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
