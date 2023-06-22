package podman

import (
	"fmt"
	"testing"

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
	if err != nil {
		fmt.Printf("%#v", err)
	}

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

	// TODO: need a socket and a client

	// kube.PlayWithBody() fix me :)


}
