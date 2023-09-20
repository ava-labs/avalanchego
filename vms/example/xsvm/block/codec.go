// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import "github.com/ava-labs/avalanchego/vms/example/xsvm/tx"

// Version is the current default codec version
const Version = tx.Version

var Codec = tx.Codec
