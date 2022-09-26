// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

type TxFees struct {
	Base        uint64 `json:"base"`
	CreateAsset uint64 `json:"createAsset"`
	Operation   uint64 `json:"operation"`
	Import      uint64 `json:"import"`
	Export      uint64 `json:"export"`
}
