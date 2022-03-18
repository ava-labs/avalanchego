// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import "testing"

func TestLog(t *testing.T) {
	log, err := NewTestLog(DefaultConfig)
	if err != nil {
		t.Fatalf("Error creating log: %s", err)
	}

	recovered := new(bool)
	panicFunc := func() {
		panic("DON'T PANIC!")
	}
	exitFunc := func() {
		*recovered = true
	}
	log.RecoverAndExit(panicFunc, exitFunc)

	if !*recovered {
		t.Fatalf("Exit function was never called")
	}
}
