package podman

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/kube"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

var podYamlBytes = `
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: demo-pod
  name: demo-pod
spec:
  containers:
  - image: quay.io/libpod/alpine_nginx:latest
    name: demo
    ports:
    - containerPort: 80
      hostPort: 8000`

// go test -v -timeout 30s -run ^TestSchedulePod$ github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime/podman
func TestSchedulePod(t *testing.T) {
	require := require.New(t)
	obj, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(podYamlBytes), nil, nil)
	require.NoError(err)

	// ensure valid pod spec from bytes
	pod, ok := obj.(*v1.Pod)
	require.NotNil(ok)

	//now we can inject stuff we want to enforce
	pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env,
		v1.EnvVar{
			Name:  "INJECTED_IP",
			Value: "IP",
		},
	)

	fmt.Printf("%#v\n", pod)

	socket, err := getSocketPath()
	require.NoError(err)

	ctx, err := bindings.NewConnection(context.Background(), socket)
	require.NoError(err)

	//  reader := bytes.NewReader(pod.)

	kube.PlayWithBody(ctx)

}
