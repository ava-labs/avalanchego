// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

//go:generate go tool mockgen -package=${GOPACKAGE} -destination=mock_diff.go . Diff
//go:generate go tool mockgen -package=${GOPACKAGE} -destination=mock_state.go . State
//go:generate go tool mockgen -package=${GOPACKAGE} -destination=mock_chain.go . Chain
