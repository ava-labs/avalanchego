// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"
)

func UnarySnowflakeStateTest(t *testing.T, sf *unarySnowflake, expectedConfidence int, expectedFinalized bool) {
	if confidence := sf.confidence; confidence != expectedConfidence {
		t.Fatalf("Wrong confidence. Expected %d got %d", expectedConfidence, confidence)
	} else if finalized := sf.Finalized(); finalized != expectedFinalized {
		t.Fatalf("Wrong finalized status. Expected %v got %v", expectedFinalized, finalized)
	}
}

func TestUnarySnowflake(t *testing.T) {
	beta := 2

	sf := &unarySnowflake{}
	sf.Initialize(beta)

	sf.RecordSuccessfulPoll()
	UnarySnowflakeStateTest(t, sf, 1, false)

	sf.RecordUnsuccessfulPoll()
	UnarySnowflakeStateTest(t, sf, 0, false)

	sf.RecordSuccessfulPoll()
	UnarySnowflakeStateTest(t, sf, 1, false)

	sfCloneIntf := sf.Clone()
	sfClone, ok := sfCloneIntf.(*unarySnowflake)
	if !ok {
		t.Fatalf("Unexpected clone type")
	}

	UnarySnowflakeStateTest(t, sfClone, 1, false)

	binarySnowflake := sfClone.Extend(beta, 0)

	binarySnowflake.RecordUnsuccessfulPoll()

	binarySnowflake.RecordSuccessfulPoll(1)

	if binarySnowflake.Finalized() {
		t.Fatalf("Should not have finalized")
	}

	binarySnowflake.RecordSuccessfulPoll(1)

	if binarySnowflake.Preference() != 1 {
		t.Fatalf("Wrong preference")
	} else if !binarySnowflake.Finalized() {
		t.Fatalf("Should have finalized")
	}

	sf.RecordSuccessfulPoll()
	UnarySnowflakeStateTest(t, sf, 2, true)

	sf.RecordUnsuccessfulPoll()
	UnarySnowflakeStateTest(t, sf, 0, true)

	sf.RecordSuccessfulPoll()
	UnarySnowflakeStateTest(t, sf, 1, true)
}
