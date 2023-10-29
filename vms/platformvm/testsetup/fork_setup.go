// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

type ActiveFork uint8

const (
	ApricotPhase3Fork ActiveFork = 0
	ApricotPhase5Fork ActiveFork = 1
	BanffFork         ActiveFork = 2
	CortinaFork       ActiveFork = 3
	DFork             ActiveFork = 4
	LatestFork        ActiveFork = DFork
)
