// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build tools

package subnetevm

import (
	_ "github.com/fjl/gencodec"
	_ "golang.org/x/mod/modfile" // golang.org/x/mod to satisfy requirement for go.uber.org/mock/mockgen@v0.4
)
