// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build tools

package coreth

import (
	_ "github.com/fjl/gencodec"
	_ "golang.org/x/tools/imports" // golang.org/x/tools to satisfy requirement for go.uber.org/mock/mockgen@v0.5
)
