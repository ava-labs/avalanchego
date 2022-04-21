// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import "errors"

var ErrNotStateSyncable = errors.New("block isn't state syncable")

// StateSyncableBlock is a block participating State Sync.
type StateSyncableBlock interface {
	Block

	// Once last summary block pulled from VM via StateSyncGetResult has been
	// retrieved from network,  Register allows confirming the block to the VM
	Register() error
}
