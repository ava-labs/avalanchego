// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"context"
	"flag"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/faultinjection"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet/flags"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestE2E(t *testing.T) {
	ginkgo.RunSpecs(t, "fault injection e2e test suite")
}

const avalanchegoImage = "localhost:5001/avalanchego"

var (
	kubeconfigVars *flags.KubeconfigVars
	kubeImage      string
)

func init() {
	kubeconfigVars = flags.NewKubeconfigFlagVars()
	flag.StringVar(
		&kubeImage,
		"kube-image",
		avalanchegoImage+":master",
		"the avalanchego image to use for kube deployment",
	)
}

var _ = ginkgo.Describe("[Fault Injection]", func() {
	ginkgo.It("should inject pod-kill and recover", func() {
		tc := e2e.NewTestContext()
		require := require.New(tc)

		ginkgo.By("Configuring kubernetes clients")
		kubeconfig, err := tmpnet.GetClientConfig(tc.Log(), kubeconfigVars.Path, kubeconfigVars.Context)
		require.NoError(err)
		dynamicClient, err := dynamic.NewForConfig(kubeconfig)
		require.NoError(err)

		// Use the existing tmpnet namespace which has the required ConfigMap
		namespace := "tmpnet"
		tc.Log().Info("using existing namespace", zap.String("namespace", namespace))

		ginkgo.By("Starting a 2-node tmpnet network in the test namespace")
		network := &tmpnet.Network{
			Owner: "faultinjection-e2e",
			Nodes: tmpnet.NewNodesOrPanic(2),
			DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
				Kube: &tmpnet.KubeRuntimeConfig{
					ConfigPath:    kubeconfigVars.Path,
					ConfigContext: kubeconfigVars.Context,
					Namespace:     namespace,
					Image:         kubeImage,
					VolumeSizeGB:  tmpnet.MinimumVolumeSizeGB,
				},
			},
		}

		timeout, err := network.DefaultRuntimeConfig.GetNetworkStartTimeout(len(network.Nodes))
		require.NoError(err)
		ctx := tc.ContextWithTimeout(timeout)

		err = tmpnet.BootstrapNewNetwork(ctx, tc.Log(), network, "" /* use default ~/.tmpnet */)
		require.NoError(err, "failed to bootstrap network")

		tc.Log().Info("network started successfully",
			zap.String("networkDir", network.Dir),
			zap.String("networkUUID", network.UUID),
		)

		tc.DeferCleanup(func() {
			tc.Log().Info("shutting down network")
			// Use context.Background() instead of tc.DefaultContext() to avoid
			// calling DeferCleanup from within a DeferCleanup callback
			ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
			defer cancel()
			err := network.Stop(ctx)
			if err != nil {
				tc.Log().Warn("failed to stop network", zap.Error(err))
			}
		})

		ginkgo.By("Verifying all nodes are healthy")
		for _, node := range network.Nodes {
			healthy, err := node.IsHealthy(tc.DefaultContext())
			require.NoError(err)
			require.True(healthy, "node %s is not healthy", node.NodeID)
		}

		ginkgo.By("Creating a ChaosInjector")
		chaosConfig := faultinjection.DefaultConfig()
		chaosConfig.MinDuration = 10 * time.Second
		chaosConfig.MaxDuration = 15 * time.Second

		injector, err := faultinjection.NewInjector(
			tc.Log(),
			kubeconfig,
			chaosConfig,
			namespace, // chaos experiments created in same namespace
			namespace, // target namespace (where pods are)
			network.UUID,
		)
		require.NoError(err)

		tc.DeferCleanup(func() {
			tc.Log().Info("stopping chaos injector")
			injector.Stop()
		})

		ginkgo.By("Injecting a pod-kill experiment")
		targetNode := network.Nodes[0]
		tc.Log().Info("targeting node for pod-kill",
			zap.Stringer("nodeID", targetNode.NodeID),
		)

		exp, err := injector.InjectOnce(tc.DefaultContext(), faultinjection.ExperimentTypePodKill, 30*time.Second)
		require.NoError(err)
		tc.Log().Info("created chaos experiment",
			zap.String("name", exp.Name),
			zap.String("type", string(exp.Type)),
			zap.Duration("duration", exp.Duration),
		)

		ginkgo.By("Waiting for chaos experiment to be applied")
		// Give chaos mesh time to process the experiment
		time.Sleep(10 * time.Second)

		ginkgo.By("Verifying Chaos Mesh experiment was created and injected")
		// Verify experiment exists in k8s and was successfully injected
		podChaosList, err := dynamicClient.Resource(faultinjection.PodChaosGVR()).Namespace(namespace).List(tc.DefaultContext(), metav1.ListOptions{})
		require.NoError(err)
		tc.Log().Info("found PodChaos experiments",
			zap.Int("count", len(podChaosList.Items)),
		)
		require.GreaterOrEqual(len(podChaosList.Items), 1, "expected at least 1 PodChaos experiment")

		// Check that the experiment was successfully injected
		for _, item := range podChaosList.Items {
			status, found, err := unstructured.NestedMap(item.Object, "status")
			require.NoError(err)
			if !found {
				continue
			}
			conditions, found, err := unstructured.NestedSlice(status, "conditions")
			require.NoError(err)
			if !found {
				continue
			}
			for _, cond := range conditions {
				condMap, ok := cond.(map[string]any)
				if !ok {
					continue
				}
				condType, _ := condMap["type"].(string)
				condStatus, _ := condMap["status"].(string)
				tc.Log().Info("chaos experiment condition",
					zap.String("name", item.GetName()),
					zap.String("conditionType", condType),
					zap.String("conditionStatus", condStatus),
				)
				if condType == "AllInjected" {
					require.Equal("True", condStatus, "expected AllInjected condition to be True")
				}
			}
		}

		tc.Log().Info("chaos injection verified successfully")
	})
})
