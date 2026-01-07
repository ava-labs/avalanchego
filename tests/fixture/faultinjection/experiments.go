// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package faultinjection

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Chaos Mesh GVRs for the CRDs we use
var (
	podChaosGVR = schema.GroupVersionResource{
		Group:    "chaos-mesh.org",
		Version:  "v1alpha1",
		Resource: "podchaos",
	}

	networkChaosGVR = schema.GroupVersionResource{
		Group:    "chaos-mesh.org",
		Version:  "v1alpha1",
		Resource: "networkchaos",
	}
)

// PodChaosGVR returns the GVR for PodChaos resources.
func PodChaosGVR() schema.GroupVersionResource {
	return podChaosGVR
}

// NetworkChaosGVR returns the GVR for NetworkChaos resources.
func NetworkChaosGVR() schema.GroupVersionResource {
	return networkChaosGVR
}

// ExperimentType represents the type of chaos experiment.
type ExperimentType string

const (
	ExperimentTypePodKill      ExperimentType = "pod-kill"
	ExperimentTypeNetworkDelay ExperimentType = "network-delay"
	ExperimentTypeNetworkLoss  ExperimentType = "network-loss"
)

// Experiment represents an active chaos experiment.
type Experiment struct {
	Name      string
	Namespace string
	Type      ExperimentType
	GVR       schema.GroupVersionResource
	StartTime time.Time
	Duration  time.Duration
}

// IsActive returns true if the experiment is currently active at the given time.
func (e *Experiment) IsActive(now time.Time) bool {
	return now.After(e.StartTime) && now.Before(e.StartTime.Add(e.Duration))
}

// createPodChaos creates a PodChaos CRD for pod-kill experiments.
func createPodChaos(
	ctx context.Context,
	client dynamic.Interface,
	name string,
	namespace string,
	targetNamespace string,
	labelSelectors map[string]string,
	duration time.Duration,
) (*Experiment, error) {
	podChaos := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "chaos-mesh.org/v1alpha1",
			"kind":       "PodChaos",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"action": "pod-kill",
				"mode":   "one",
				"selector": map[string]any{
					"namespaces":     []any{targetNamespace},
					"labelSelectors": toAnyMap(labelSelectors),
				},
				"duration": duration.String(),
			},
		},
	}

	_, err := client.Resource(podChaosGVR).Namespace(namespace).Create(ctx, podChaos, metav1.CreateOptions{})
	if err != nil {
		return nil, stacktrace.Errorf("failed to create PodChaos %s/%s: %w", namespace, name, err)
	}

	return &Experiment{
		Name:      name,
		Namespace: namespace,
		Type:      ExperimentTypePodKill,
		GVR:       podChaosGVR,
		StartTime: time.Now(),
		Duration:  duration,
	}, nil
}

// createNetworkDelay creates a NetworkChaos CRD for network delay experiments.
func createNetworkDelay(
	ctx context.Context,
	client dynamic.Interface,
	name string,
	namespace string,
	targetNamespace string,
	labelSelectors map[string]string,
	latency string,
	jitter string,
	duration time.Duration,
) (*Experiment, error) {
	networkChaos := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "chaos-mesh.org/v1alpha1",
			"kind":       "NetworkChaos",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"action": "delay",
				"mode":   "all",
				"selector": map[string]any{
					"namespaces":     []any{targetNamespace},
					"labelSelectors": toAnyMap(labelSelectors),
				},
				"delay": map[string]any{
					"latency": latency,
					"jitter":  jitter,
				},
				"duration": duration.String(),
			},
		},
	}

	_, err := client.Resource(networkChaosGVR).Namespace(namespace).Create(ctx, networkChaos, metav1.CreateOptions{})
	if err != nil {
		return nil, stacktrace.Errorf("failed to create NetworkChaos (delay) %s/%s: %w", namespace, name, err)
	}

	return &Experiment{
		Name:      name,
		Namespace: namespace,
		Type:      ExperimentTypeNetworkDelay,
		GVR:       networkChaosGVR,
		StartTime: time.Now(),
		Duration:  duration,
	}, nil
}

// createNetworkLoss creates a NetworkChaos CRD for network packet loss experiments.
func createNetworkLoss(
	ctx context.Context,
	client dynamic.Interface,
	name string,
	namespace string,
	targetNamespace string,
	labelSelectors map[string]string,
	lossPercent string,
	duration time.Duration,
) (*Experiment, error) {
	networkChaos := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "chaos-mesh.org/v1alpha1",
			"kind":       "NetworkChaos",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"action": "loss",
				"mode":   "all",
				"selector": map[string]any{
					"namespaces":     []any{targetNamespace},
					"labelSelectors": toAnyMap(labelSelectors),
				},
				"loss": map[string]any{
					"loss": lossPercent,
				},
				"duration": duration.String(),
			},
		},
	}

	_, err := client.Resource(networkChaosGVR).Namespace(namespace).Create(ctx, networkChaos, metav1.CreateOptions{})
	if err != nil {
		return nil, stacktrace.Errorf("failed to create NetworkChaos (loss) %s/%s: %w", namespace, name, err)
	}

	return &Experiment{
		Name:      name,
		Namespace: namespace,
		Type:      ExperimentTypeNetworkLoss,
		GVR:       networkChaosGVR,
		StartTime: time.Now(),
		Duration:  duration,
	}, nil
}

// deleteExperiment deletes a chaos experiment CRD.
func deleteExperiment(ctx context.Context, client dynamic.Interface, exp *Experiment) error {
	err := client.Resource(exp.GVR).Namespace(exp.Namespace).Delete(ctx, exp.Name, metav1.DeleteOptions{})
	if err != nil {
		return stacktrace.Errorf("failed to delete experiment %s/%s: %w", exp.Namespace, exp.Name, err)
	}
	return nil
}

// toAnyMap converts a map[string]string to map[string]any for unstructured objects.
func toAnyMap(m map[string]string) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
