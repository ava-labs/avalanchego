// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

// The following function signatures are common across flag and
// spf13/pflag. They can be used to define a single registration method that
// supports both flag libraries.

type stringVarFunc func(p *string, name string, value string, usage string)

type boolVarFunc func(p *bool, name string, value bool, usage string)
