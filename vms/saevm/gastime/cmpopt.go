// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !prod && !nocmpopts

package gastime

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/proxytime"
)

// CmpOpt returns a configuration for [cmp.Diff] to compare [Time] instances in
// tests.
func CmpOpt() cmp.Option {
	return cmp.Options{
		cmp.AllowUnexported(Time{}, GasPriceConfig{}),
		cmpopts.IgnoreTypes(canotoData_Time{}, canotoData_GasPriceConfig{}),
		proxytime.CmpOpt[gas.Gas](),
	}
}
