// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rapidtest provides helpers for tests using [pgregory.net/rapid].
package rapidtest

import (
	"flag"
	"testing"
)

// FixedSeed is the seed used when neither -rapid.seed nor -test.shuffle is set.
const FixedSeed = "1"

const (
	seedFlag    = "rapid.seed"
	shuffleFlag = "test.shuffle"
)

// Main runs the tests with a fixed rapid seed unless -rapid.seed is passed or
// -test.shuffle is on, in which case the seed is left to rapid. Call it from
// TestMain.
func Main(m *testing.M) int {
	flag.Parse()
	if err := maybeSetRapidSeed(flag.CommandLine); err != nil {
		panic(err)
	}
	return m.Run()
}

func maybeSetRapidSeed(fs *flag.FlagSet) error {
	seedSet := false
	fs.Visit(func(f *flag.Flag) { seedSet = seedSet || f.Name == seedFlag })
	if seedSet || fs.Lookup(shuffleFlag).Value.String() != "off" {
		return nil
	}
	return fs.Set(seedFlag, FixedSeed)
}
