// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/chain.go -mock_names=Chain=Chain . Chain
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/diff.go -mock_names=Diff=Diff . Diff
//go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/state.go -mock_names=State=State . State
