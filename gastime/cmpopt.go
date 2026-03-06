// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !prod && !nocmpopts

package gastime

import (
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/ava-labs/strevm/proxytime"
)

// CmpOpt returns a configuration for [cmp.Diff] to compare [Time] instances in
// tests.
func CmpOpt() cmp.Option {
	return cmp.Options{
		cmp.AllowUnexported(TimeMarshaler{}, config{}),
		cmpopts.IgnoreTypes(canotoData_TimeMarshaler{}, canotoData_config{}),
		proxytime.CmpOpt[gas.Gas](proxytime.CmpRateInvariantsByValue),
	}
}
