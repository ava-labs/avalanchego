// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/ava-labs/avalanchego/ids"
)

// Summary represents all the information needed for state sync processing.
// Summary must allow a VM to download, verify and rebuild its state,
// no matter whether VM is freshly created or it has previous state.
// Both Height and ID uniquely identify a Summary. However:
// Height is used to efficiently elicit network votes;
// ID must allow summaries comparison and verification as an alternative to Bytes;
// it is used to verify what summaries votes are casted for.
// Byte returns the Summary content which is defined by the VM and opaque to the engine.
// BlockID represent the ID of the block associated with summary
// Finally Accept method triggers VM to start state syncing with given summary. The returned
// boolean confirms to the engine whether state sync is actually started or skipped by the VM.
type Summary interface {
	Bytes() []byte
	Height() uint64
	ID() ids.ID
	BlockID() ids.ID

	Accept() (bool, error)
}
