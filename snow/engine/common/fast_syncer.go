// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

type FastSyncer interface {
	Engine
	IsEnabled() bool
}
