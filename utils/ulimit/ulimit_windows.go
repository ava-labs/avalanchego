// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build windows
// +build windows

package ulimit

import "github.com/chain4travel/caminogo/utils/logging"

const DefaultFDLimit = 16384

// Set is a no-op for windows and will warn if the default is not used.
func Set(max uint64, log logging.Logger) error {
	if max != DefaultFDLimit {
		log.Warn("fd-limit is not supported for windows")
	}
	return nil
}
