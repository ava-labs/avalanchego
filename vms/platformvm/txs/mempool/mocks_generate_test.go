// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

//go:generate go run go.uber.org/mock/mockgen@v0.4 -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/mempool.go -mock_names=Mempool=Mempool . Mempool
