// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
)

const Version = blocks.Version

var Codec = blocks.GenesisCodec
