// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

type caminoStateChanges struct{}

func (*caminoStateChanges) Apply(_ state.Diff) {
}

func (*caminoStateChanges) Len() int {
	return 0
}

func caminoAdvanceTimeTo(
	_ *Backend,
	_ state.Chain,
	_ time.Time,
	_ *stateChanges,
) error {
	return nil
}
