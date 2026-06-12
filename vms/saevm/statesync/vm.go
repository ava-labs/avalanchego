// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
)

// VM is a wrapper around [sae.VM] that implements all of [adaptor.SyncableVM],
// except Initialize, which needs to be done by the harness.
type VM[T hook.Transaction] struct {
	*sae.VM
}
