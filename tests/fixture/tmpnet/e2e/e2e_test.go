// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"

	tmpnetflags "github.com/ava-labs/avalanchego/tests/fixture/tmpnet/flags"
	chaosv1alpha1 "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultNodeCount = 2
	defaultOwner     = "avalanchego-tmpnet-topology-e2e"

	defaultTopologyLatency = "1s"
	localHealthHost        = "127.0.0.1"

	topologyLocationLabelKey = "tmpnet.avax.network/location"

	adminDefaultContext = "kind-kind"
)

var (
	networkVars *tmpnetflags.StartNetworkVars

	topologyLatency     string
	adminKubeconfigPath string
	adminKubeconfigCtx  string
)

func TestE2E(t *testing.T) {
	ginkgo.RunSpecs(t, "tmpnet topology e2e suite")
}

func init() {
	networkVars = tmpnetflags.NewStartNetworkFlagVars(defaultOwner, defaultNodeCount)
	flag.StringVar(
		&topologyLatency,
		"topology-latency",
		defaultTopologyLatency,
		"one-way latency to apply in both directions between the first two tmpnet nodes",
	)
	flag.StringVar(
		&adminKubeconfigPath,
		"admin-kubeconfig",
		"",
		"optional kubeconfig path for verification operations; defaults to the runtime kubeconfig path",
	)
	flag.StringVar(
		&adminKubeconfigCtx,
		"admin-kubeconfig-context",
		adminDefaultContext,
		"kubeconfig context for verification operations such as listing Chaos Mesh resources and execing into pods",
	)
}

var _ = ginkgo.Describe("[tmpnet topology]", func() {
	ginkgo.It("creates, realizes, and removes kube topology resources", func() {
		tc := e2e.NewTestContext()
		require := require.New(tc)
		ctx := tc.DefaultContext()

		nodeCount, err := networkVars.GetNodeCount()
		require.NoError(err)
		require.GreaterOrEqual(nodeCount, 2, "topology validation requires at least two nodes")

		runtimeConfig, err := networkVars.GetNodeRuntimeConfig()
		require.NoError(err)
		if runtimeConfig.Kube == nil {
			ginkgo.Skip("tmpnet topology e2e requires --runtime=kube")
		}

		configuredLatency, err := time.ParseDuration(topologyLatency)
		require.NoError(err)

		verificationKubeconfigPath := adminKubeconfigPath
		if verificationKubeconfigPath == "" {
			verificationKubeconfigPath = runtimeConfig.Kube.ConfigPath
		}

		adminClientConfig, err := tmpnet.GetClientConfig(tc.Log(), verificationKubeconfigPath, adminKubeconfigCtx)
		require.NoError(err)

		clientset, err := kubernetes.NewForConfig(adminClientConfig)
		require.NoError(err)

		nodes := tmpnet.NewNodesOrPanic(nodeCount)
		network := &tmpnet.Network{
			Owner:                networkVars.NetworkOwner,
			DefaultRuntimeConfig: *runtimeConfig,
			DefaultFlags:         tmpnet.DefaultE2EFlags(),
			Nodes:                nodes,
			Topology: &tmpnet.Topology{
				Locations: []tmpnet.Location{
					{Name: "chicago", NodeIDs: []ids.NodeID{nodes[0].NodeID}},
					{Name: "new-york", NodeIDs: []ids.NodeID{nodes[1].NodeID}},
				},
				Connectivity: []tmpnet.Connection{
					{From: "chicago", To: "new-york", Latency: topologyLatency},
					{From: "new-york", To: "chicago", Latency: topologyLatency},
				},
			},
		}

		require.NoError(network.EnsureDefaultConfig(ctx, tc.Log()))
		require.NoError(network.Create(networkVars.RootNetworkDir))
		ginkgo.DeferCleanup(func() {
			_ = os.RemoveAll(network.Dir)
		})

		networkRunning := false
		ginkgo.DeferCleanup(func() {
			if !networkRunning {
				return
			}
			cleanupCtx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
			defer cancel()
			_ = network.Stop(cleanupCtx)
		})

		ginkgo.By("starting a tmpnet kube network with topology configured")
		require.NoError(network.Bootstrap(ctx, tc.Log()))
		networkRunning = true

		ginkgo.By("verifying the expected Chaos Mesh resources are created and injected")
		chaosResources, err := network.ManagedTopologyResources(ctx)
		require.NoError(err)
		require.Len(chaosResources, 2)
		requireExpectedTopologyResources(require, network, chaosResources)

		ginkgo.By("verifying pods are labeled by topology location")
		pods := listNetworkPods(ctx, require, clientset, runtimeConfig.Kube.Namespace, network.UUID)
		require.Len(pods, 2)
		sourcePodName := pods[0].Name
		sourcePodLocation := pods[0].Labels[topologyLocationLabelKey]
		destinationPodIP := pods[1].Status.PodIP
		destinationPodLocation := pods[1].Labels[topologyLocationLabelKey]
		require.NotEmpty(sourcePodLocation)
		require.NotEmpty(destinationPodLocation)
		require.NotEqual(sourcePodLocation, destinationPodLocation)

		ginkgo.By("verifying the induced latency is clearly observable relative to a local request")
		localAverage := averageLatency(runHTTPMeasurements(tc, verificationKubeconfigPath, adminKubeconfigCtx, runtimeConfig.Kube.Namespace, sourcePodName, localHealthHost))
		remoteAverage := averageLatency(runHTTPMeasurements(tc, verificationKubeconfigPath, adminKubeconfigCtx, runtimeConfig.Kube.Namespace, sourcePodName, destinationPodIP))
		delta := remoteAverage - localAverage

		tc.Log().Info(fmt.Sprintf("local average latency: %s", localAverage))
		tc.Log().Info(fmt.Sprintf("remote average latency: %s", remoteAverage))
		tc.Log().Info(fmt.Sprintf("observed latency delta: %s", delta))

		require.Less(localAverage, 250*time.Millisecond, "expected local request latency to remain near baseline")
		require.GreaterOrEqual(delta, configuredLatency*3/4, "expected induced latency to dominate measurement noise")

		ginkgo.By("stopping the network")
		stopCtx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultNetworkTimeout)
		defer cancel()
		require.NoError(network.Stop(stopCtx))
		networkRunning = false

		ginkgo.By("verifying tmpnet-owned Chaos Mesh resources are removed on teardown")
		tc.Eventually(func() bool {
			listCtx, cancel := context.WithTimeout(context.Background(), tmpnet.DefaultPollingInterval)
			defer cancel()

			resources, err := network.ManagedTopologyResources(listCtx)
			require.NoError(err)
			return len(resources) == 0
		}, tmpnet.DefaultNetworkTimeout, tmpnet.DefaultPollingInterval, "expected tmpnet-owned Chaos Mesh resources to be removed on teardown")
	})
})

func requireExpectedTopologyResources(require *require.Assertions, network *tmpnet.Network, resources []chaosv1alpha1.NetworkChaos) {
	ginkgo.GinkgoHelper()

	expected := network.TopologyConnectionResourceNames()
	require.Len(resources, len(expected))
	for _, resource := range resources {
		connection, ok := expected[resource.GetName()]
		require.True(ok, "unexpected topology resource %s", resource.GetName())
		labels := resource.GetLabels()
		require.Equal(network.UUID, labels["network_uuid"])
		require.Equal(connection.From, labels["topology_from_location"])
		require.Equal(connection.To, labels["topology_to_location"])
		require.True(tmpnet.ManagedTopologyResourceInjected(&resource), "expected NetworkChaos resource %s to report injected", resource.GetName())
	}
}

func listNetworkPods(
	ctx context.Context,
	require *require.Assertions,
	clientset *kubernetes.Clientset,
	namespace string,
	networkUUID string,
) []corev1.Pod {
	ginkgo.GinkgoHelper()

	podList, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "network_uuid=" + networkUUID,
	})
	require.NoError(err)
	slices.SortFunc(podList.Items, func(a, b corev1.Pod) int {
		return strings.Compare(a.Name, b.Name)
	})
	return podList.Items
}

func runHTTPMeasurements(
	tc tests.TestContext,
	kubeconfigPath string,
	kubeconfigContext string,
	namespace string,
	sourcePod string,
	destinationHost string,
) []time.Duration {
	ginkgo.GinkgoHelper()

	script := `set -euo pipefail
peer="$1"
for i in 1 2 3 4 5; do
  start=$(date +%s%3N)
  exec 3<>/dev/tcp/$peer/9650
  printf "GET /ext/health HTTP/1.1\r\nHost: $peer\r\nConnection: close\r\n\r\n" >&3
  IFS= read -r _ <&3
  end=$(date +%s%3N)
  echo $((end-start))
  exec 3<&-
  exec 3>&-
done
`

	args := []string{"-n", namespace}
	if kubeconfigPath != "" {
		args = append(args, "--kubeconfig", kubeconfigPath)
	}
	if kubeconfigContext != "" {
		args = append(args, "--context", kubeconfigContext)
	}
	args = append(args, "exec", "-i", sourcePod, "--", "bash", "-s", "--", destinationHost)
	cmd := exec.CommandContext(tc.DefaultContext(), "kubectl", args...) // #nosec G204
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	require.NoError(tc, err, "kubectl exec failed: %s", output)

	lines := strings.Fields(string(bytes.TrimSpace(output)))
	measurements := make([]time.Duration, 0, len(lines))
	for _, line := range lines {
		ms, err := strconv.Atoi(line)
		require.NoError(tc, err, "failed to parse latency measurement %q from output %q", line, output)
		measurements = append(measurements, time.Duration(ms)*time.Millisecond)
	}
	require.NotEmpty(tc, measurements)
	return measurements
}

func averageLatency(measurements []time.Duration) time.Duration {
	var total time.Duration
	for _, measurement := range measurements {
		total += measurement
	}
	return total / time.Duration(len(measurements))
}
