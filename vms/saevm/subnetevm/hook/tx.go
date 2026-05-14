// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import saehook "github.com/ava-labs/avalanchego/vms/saevm/hook"

// Tx is the user-defined transaction type carried by SAE end-of-block ops.
//
// Subnet-EVM has no end-of-block ops. [Tx] therefore exists solely as an inert placeholder so the
// [saehook.PointsG[T]] generic constraint can be satisfied. It is never
// constructed at runtime and is not expected to gain a body.
type Tx struct{}

func (*Tx) AsOp() saehook.Op { return saehook.Op{} }
