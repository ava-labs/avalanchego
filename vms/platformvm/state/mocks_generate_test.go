// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

//go:generate go run go.uber.org/mock/mockgen@v0.5 -package=${GOPACKAGE} -source=diff.go -destination=mock_diff.go
//go:generate go run go.uber.org/mock/mockgen@v0.5 -package=${GOPACKAGE} -source=state.go -destination=mock_state.go -exclude_interfaces=Chain
//go:generate go run go.uber.org/mock/mockgen@v0.5 -package=${GOPACKAGE} -source=state.go -destination=mock_chain.go -exclude_interfaces=State
