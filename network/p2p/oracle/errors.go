// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package oracle

import "errors"

// ErrSourceUnavailable is wrapped by SidecarClient implementations when the
// source chain's RPC cannot be reached. The oracle sidecar server maps this to
// codes.Unavailable; all other errors map to codes.InvalidArgument.
var ErrSourceUnavailable = errors.New("source chain unavailable")
