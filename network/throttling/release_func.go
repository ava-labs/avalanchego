// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

type ReleaseFunc func()

func noopRelease() {}
