// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

//go:generate go run go.uber.org/mock/mockgen@v0.4 -package=${GOPACKAGE} -source=block.go -destination=mock_post_fork_block.go -exclude_interfaces=Block
