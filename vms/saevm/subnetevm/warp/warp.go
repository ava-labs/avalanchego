// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package warp provides the Subnet-EVM-specific glue around the shared SAE
// warp implementation ([saewarp]): predicate verification against
// subnet-evm's precompile registry, precompile-accept handling, and
// validator-uptime attestation signing.
package warp

import (
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"

	saewarp "github.com/ava-labs/avalanchego/vms/saevm/warp"
)

// The shared storage doubles as the warp message sink of subnet-evm's
// precompile accept context.
var _ precompileconfig.WarpMessageWriter = (*saewarp.Storage)(nil)
