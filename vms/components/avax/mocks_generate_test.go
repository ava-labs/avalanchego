// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

//go:generate go tool -modfile=../../../tools/go.mod mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/transferable_in.go -mock_names=TransferableIn=TransferableIn . TransferableIn
