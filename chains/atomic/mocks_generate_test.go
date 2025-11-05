// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

//go:generate go tool -modfile=../../tools/go.mod mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/shared_memory.go -mock_names=SharedMemory=SharedMemory . SharedMemory
