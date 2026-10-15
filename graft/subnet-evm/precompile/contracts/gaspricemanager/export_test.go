// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gaspricemanager

const (
	// GetGasPriceConfigGasCost exposes getGasPriceConfigGasCost to external tests.
	GetGasPriceConfigGasCost = getGasPriceConfigGasCost
	// GetGasPriceConfigLastChangedAtGasCost exposes getGasPriceConfigLastChangedAtGasCost to external tests.
	GetGasPriceConfigLastChangedAtGasCost = getGasPriceConfigLastChangedAtGasCost
	// SetGasPriceConfigGasCost exposes setGasPriceConfigGasCost to external tests.
	SetGasPriceConfigGasCost = setGasPriceConfigGasCost
)

var (
	// ErrCannotSetGasPriceConfig exposes errCannotSetGasPriceConfig to external tests.
	ErrCannotSetGasPriceConfig = errCannotSetGasPriceConfig
	// ErrNilBlockNumber exposes errNilBlockNumber to external tests.
	ErrNilBlockNumber = errNilBlockNumber
)
