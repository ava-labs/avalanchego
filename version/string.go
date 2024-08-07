// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
)

// String is displayed when CLI arg --version is used
var String string

func init() {
	format := "caminogo: %s, commit: %s\ncompat: %s [database: %s]\n"
	args := []interface{}{
		GitVersion,
		GitCommit,
		Current,
		CurrentDatabase,
	}
	String = fmt.Sprintf(format, args...)
}
