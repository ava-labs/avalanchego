// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build tools

package strevm

// Protects indirect dependencies of tools from being pruned by `go mod tidy`.
import (
	_ "github.com/StephenButtolph/canoto/canoto"
)
