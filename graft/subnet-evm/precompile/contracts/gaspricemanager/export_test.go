// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gaspricemanager

// Export identifiers for tests compiled as a separate package.

const (
	GetGasPriceConfigGasCost              = getGasPriceConfigGasCost
	GetGasPriceConfigLastChangedAtGasCost = getGasPriceConfigLastChangedAtGasCost
	SetGasPriceConfigGasCost              = setGasPriceConfigGasCost
)

var (
	ErrCannotSetGasPriceConfig = errCannotSetGasPriceConfig
	ErrNilBlockNumber          = errNilBlockNumber
)
