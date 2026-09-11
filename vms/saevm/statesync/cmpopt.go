// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !prod && !nocmpopts

package statesync

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// CmpOpt returns a configuration for [cmp.Diff] to compare [Summary] instances in
// tests.
func CmpOpt() cmp.Option {
	return cmp.Options{
		cmp.AllowUnexported(Summary{}),
		cmpopts.IgnoreFields(Summary{}, "canotoData"),
	}
}
