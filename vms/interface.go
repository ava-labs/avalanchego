package vms

import (
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// A Factory creates new instances of a VM
type Factory[T common.VM] interface {
	New(logging.Logger) (T, error)
}
