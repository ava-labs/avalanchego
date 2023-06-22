//go:build (amd64 && !windows && amd64 && !darwin) || (arm64 && !windows && arm64 && !darwin) || (amd64 && darwin)

package podman

import (
	"github.com/containers/podman/v4/pkg/machine"
)

// TODO :)
func GetSystemProvider() (machine.VirtProvider, error) {
	return nil, nil
}
