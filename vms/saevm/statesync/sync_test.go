// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import "github.com/ava-labs/avalanchego/vms/saevm/adaptor"

var _ adaptor.SyncableVM[adaptor.BlockProperties, *Summary] = (*vm)(nil)

type vm struct {
	adaptor.ChainVM[adaptor.BlockProperties]
	*SummaryHandler
}
