// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

type ActiveFork uint8

const (
	ApricotPhase3Fork ActiveFork = iota
	ApricotPhase5Fork
	BanffFork
	CortinaFork
	DurangoFork

	LatestFork ActiveFork = DurangoFork
)
