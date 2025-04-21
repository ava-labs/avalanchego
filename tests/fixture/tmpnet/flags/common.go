// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

// The following function signature is common across flag and
// spf13/pflag. It can be used to define a single registration method
// that supports both flag libraries.
type varFunc[T any] func(p *T, name string, value T, usage string)
