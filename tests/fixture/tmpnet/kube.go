// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/utils/pointer"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/rest"
)

// DefaultPodFlags defines common flags for avalanchego nodes running in a pod.
func DefaultPodFlags(networkName string, dataDir string, sybilProtectionEnabled bool) FlagsMap {
	return FlagsMap{
		config.DataDirKey:                dataDir,
		config.NetworkNameKey:            networkName,
		config.SybilProtectionEnabledKey: sybilProtectionEnabled,
		config.HealthCheckFreqKey:        "500ms", // Ensure rapid detection of a healthy state
		config.LogDisplayLevelKey:        logging.Info.String(),
		config.LogLevelKey:               logging.Off.String(),
		config.HTTPHostKey:               "0.0.0.0", // Need to bind to pod IP to ensure kubelet can access the http port for the readiness check
	}
}

// NewNodeStatefulSet returns a statefulset for an avalanchego node.
func NewNodeStatefulSet(
	name string,
	generateName bool,
	imageName string,
	containerName string,
	volumeName string,
	volumeSize string,
	volumeMountPath string,
	labels map[string]string,
	flags FlagsMap,
) *appsv1.StatefulSet {
	var objectMeta metav1.ObjectMeta
	if generateName {
		objectMeta = metav1.ObjectMeta{
			GenerateName: name + "-",
		}
	} else {
		objectMeta = metav1.ObjectMeta{
			Name: name,
		}
	}

	podLabels := map[string]string{
		"app": name,
	}
	var (
		ghRepo     string
		ghWorkflow string
	)
	for label, value := range labels {
		// gh_repo contains a slash so it is not a valid label. Set it as an annotation instead.
		if label == "gh_repo" {
			ghRepo = value
			continue
		}
		// gh_workflow can contain spaces which is not valid in labels. Set it as an annotation instead.
		if label == "gh_workflow" {
			ghWorkflow = value
			continue
		}
		podLabels[label] = value
	}
	podAnnotations := map[string]string{
		// TODO(marun) Specify the port prometheus should target
		"prometheus.io/scrape": "true",
		"prometheus.io/path":   "/ext/metrics",
		"promtail/collect":     "true",
		"gh_repo":              ghRepo,
		"gh_workflow":          ghWorkflow,
	}

	return &appsv1.StatefulSet{
		ObjectMeta: objectMeta,
		Spec: appsv1.StatefulSetSpec{
			Replicas:    pointer.Int32(1),
			ServiceName: name,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: volumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(volumeSize),
							},
						},
					},
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  containerName,
							Image: imageName,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: config.DefaultHTTPPort,
								},
								{
									Name:          "staker",
									ContainerPort: config.DefaultStakingPort,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      volumeName,
									MountPath: volumeMountPath,
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ext/health/liveness",
										Port: intstr.FromInt(config.DefaultHTTPPort),
									},
								},
								PeriodSeconds:    1,
								SuccessThreshold: 1,
							},
							Env: flagsToEnvVarSlice(flags),
						},
					},
				},
			},
		},
	}
}

// stringMapToEnvVarSlice converts a string map to a kube EnvVar slice.
func flagsToEnvVarSlice(flags FlagsMap) []corev1.EnvVar {
	envVars := make([]corev1.EnvVar, len(flags))
	var i int
	for k, v := range flags {
		envVars[i] = corev1.EnvVar{
			Name:  config.EnvVarName(config.EnvPrefix, k),
			Value: fmt.Sprintf("%v", v),
		}
		i++
	}
	sortEnvVars(envVars)
	return envVars
}

func envVarsToJSONValue(envVars []corev1.EnvVar) []map[string]string {
	jsonValue := make([]map[string]string, len(envVars))
	for i, envVar := range envVars {
		jsonValue[i] = map[string]string{
			"name":  envVar.Name,
			"value": envVar.Value,
		}
	}
	return jsonValue
}

func sortEnvVars(envVars []corev1.EnvVar) {
	sort.Slice(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})
}

// WaitForNodeHealthy waits for the node running in the specified pod to report healthy.
func WaitForNodeHealthy(
	ctx context.Context,
	log logging.Logger,
	kubeconfig *restclient.Config,
	namespace string,
	podName string,
	healthCheckInterval time.Duration,
	out io.Writer,
	outErr io.Writer,
) (ids.NodeID, error) {
	// A forwarded connection enables connectivity without exposing the node external to the kube cluster
	localPort, localPortStopChan, err := enableLocalForwardForPod(
		kubeconfig,
		namespace,
		podName,
		config.DefaultHTTPPort,
		out,
		outErr,
	)
	if err != nil {
		return ids.NodeID{}, fmt.Errorf("failed to enable local forward for pod: %w", err)
	}
	defer close(localPortStopChan)
	localNodeURI := fmt.Sprintf("http://127.0.0.1:%d", localPort)

	// TODO(marun) A node started with tmpnet should know the node ID before start
	infoClient := info.NewClient(localNodeURI)
	bootstrapNodeID, _, err := infoClient.GetNodeID(ctx)
	if err != nil {
		return ids.NodeID{}, fmt.Errorf("failed to retrieve node bootstrap ID: %w", err)
	}
	if err := wait.PollImmediateInfinite(healthCheckInterval, func() (bool, error) {
		healthReply, err := CheckNodeHealth(ctx, localNodeURI)
		if errors.Is(ErrUnrecoverableNodeHealthCheck, err) {
			return false, err
		} else if err != nil {
			// Error is potentially recoverable - log and continue
			log.Debug("failed to check node health",
				zap.Error(err),
			)
			return false, nil
		}
		return healthReply.Healthy, nil
	}); err != nil {
		return ids.NodeID{}, fmt.Errorf("failed to wait for node to report healthy: %w", err)
	}

	return bootstrapNodeID, nil
}

// WaitForPodCondition watches the specified pod until the status includes the specified condition.
func WaitForPodCondition(ctx context.Context, clientset *kubernetes.Clientset, namespace string, podName string, conditionType corev1.PodConditionType) error {
	return WaitForPodStatus(
		ctx,
		clientset,
		namespace,
		podName,
		func(status *corev1.PodStatus) bool {
			for _, condition := range status.Conditions {
				if condition.Type == conditionType && condition.Status == corev1.ConditionTrue {
					return true
				}
			}
			return false
		},
	)
}

// WaitForPodStatus watches the specified pod until the status is deemed acceptable by the provided test function.
func WaitForPodStatus(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	name string,
	acceptable func(*corev1.PodStatus) bool,
) error {
	watch, err := clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.SingleObject(metav1.ObjectMeta{Name: name}))
	if err != nil {
		return fmt.Errorf("failed to initiate watch of pod %s/%s: %w", namespace, name, err)
	}

	for {
		select {
		case event := <-watch.ResultChan():
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}

			if acceptable(&pod.Status) {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for pod readiness: %w", ctx.Err())
		}
	}
}

// enableLocalForwardForPod enables traffic forwarding from a local port to the specified pod with client-go. The returned
// stop channel should be closed to stop the port forwarding.
func enableLocalForwardForPod(
	kubeconfig *restclient.Config,
	namespace string,
	name string,
	port int,
	out, errOut io.Writer,
) (uint16, chan struct{}, error) {
	transport, upgrader, err := spdy.RoundTripperFor(kubeconfig)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create round tripper: %w", err)
	}

	dialer := spdy.NewDialer(
		upgrader,
		&http.Client{
			Transport: transport,
		},
		http.MethodPost,
		&url.URL{
			Scheme: "https",
			Path:   fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, name),
			Host:   strings.TrimPrefix(kubeconfig.Host, "https://"),
		},
	)
	addresses := []string{"localhost"}
	ports := []string{fmt.Sprintf("0:%d", port)}
	stopChan, readyChan := make(chan struct{}, 1), make(chan struct{}, 1)
	forwarder, err := portforward.NewOnAddresses(dialer, addresses, ports, stopChan, readyChan, out, errOut)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create forwarder: %w", err)
	}

	go func() {
		if err := forwarder.ForwardPorts(); err != nil {
			// TODO(marun) Need better error handling here
			panic(err)
		}
	}()

	<-readyChan // Wait for port forwarding to be ready

	// Retrieve the dynamically allocated local port
	forwardedPorts, err := forwarder.GetPorts()
	if err != nil {
		close(stopChan)
		return 0, nil, fmt.Errorf("failed to get forwarded ports: %w", err)
	}
	if len(forwardedPorts) == 0 {
		close(stopChan)
		return 0, nil, fmt.Errorf("failed to find at least one forwarded port: %w", err)
	}
	return forwardedPorts[0].Local, stopChan, nil
}

// GetClientConfig replicates the behavior of clientcmd.BuildConfigFromFlags with zap logging and
// support for an optional config context. If path is not provided, use of in-cluster config will
// be attempted.
func GetClientConfig(log logging.Logger, path string, context string) (*restclient.Config, error) {
	if len(path) == 0 {
		log.Warn("--kubeconfig not set.  Using the inClusterConfig.  This might not work.")
		kubeconfig, err := restclient.InClusterConfig()
		if err == nil {
			return kubeconfig, nil
		}
		log.Warn("failed to create inClusterConfig, falling back to default config",
			zap.Error(err),
		)
	}
	overrides := &clientcmd.ConfigOverrides{}
	if len(context) > 0 {
		overrides.CurrentContext = context
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: path,
		},
		overrides,
	).ClientConfig()
}

// GetClientset returns a kubernetes clientset for the provided kubeconfig path and context.
func GetClientset(log logging.Logger, path string, context string) (*kubernetes.Clientset, error) {
	clientConfig, err := GetClientConfig(log, path, context)
	if err != nil {
		return nil, fmt.Errorf("failed to get client config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	return clientset, nil
}
