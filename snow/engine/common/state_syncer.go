// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

// StateSyncer controls selection and network verification of
// state summaries driving VM state syncing. It collects
// the latest state summaries and elicit votes on them, making sure
// that a qualified majority of nodes support the state summaries.
type StateSyncer interface {
	Engine
	IsEnabled() (bool, error)
}
