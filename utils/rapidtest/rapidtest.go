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

// Main runs the tests with a fixed rapid seed unless -rapid.seed is passed or
// -test.shuffle is on, in which case the seed is left to rapid. Call it from
// TestMain.
func Main(m *testing.M) int {
	flag.Parse()
	seedSet := false
	flag.Visit(func(f *flag.Flag) { seedSet = seedSet || f.Name == "rapid.seed" })
	if seed, pin := pinSeed(seedSet, flag.Lookup("test.shuffle").Value.String()); pin {
		if err := flag.Set("rapid.seed", seed); err != nil {
			panic(err)
		}
	}
	return m.Run()
}

func pinSeed(seedSet bool, shuffle string) (seed string, pin bool) {
	if seedSet || shuffle != "off" {
		return "", false
	}
	return FixedSeed, true
}
