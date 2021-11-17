// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"
)

func UnarySnowballStateTest(t *testing.T, sb *unarySnowball, expectedNumSuccessfulPolls, expectedConfidence int, expectedFinalized bool) {
	if numSuccessfulPolls := sb.numSuccessfulPolls; numSuccessfulPolls != expectedNumSuccessfulPolls {
		t.Fatalf("Wrong numSuccessfulPolls. Expected %d got %d", expectedNumSuccessfulPolls, numSuccessfulPolls)
	} else if confidence := sb.confidence; confidence != expectedConfidence {
		t.Fatalf("Wrong confidence. Expected %d got %d", expectedConfidence, confidence)
	} else if finalized := sb.Finalized(); finalized != expectedFinalized {
		t.Fatalf("Wrong finalized status. Expected %v got %v", expectedFinalized, finalized)
	}
}

func TestUnarySnowball(t *testing.T) {
	beta := 2

	sb := &unarySnowball{}
	sb.Initialize(beta)

	sb.RecordSuccessfulPoll()
	UnarySnowballStateTest(t, sb, 1, 1, false)

	sb.RecordUnsuccessfulPoll()
	UnarySnowballStateTest(t, sb, 1, 0, false)

	sb.RecordSuccessfulPoll()
	UnarySnowballStateTest(t, sb, 2, 1, false)

	sbCloneIntf := sb.Clone()
	sbClone, ok := sbCloneIntf.(*unarySnowball)
	if !ok {
		t.Fatalf("Unexpected clone type")
	}

	UnarySnowballStateTest(t, sbClone, 2, 1, false)

	binarySnowball := sbClone.Extend(beta, 0)

	expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0)))"
	if result := binarySnowball.String(); result != expected {
		t.Fatalf("Expected:\n%s\nReturned:\n%s", expected, result)
	}

	binarySnowball.RecordUnsuccessfulPoll()
	for i := 0; i < 3; i++ {
		if binarySnowball.Preference() != 0 {
			t.Fatalf("Wrong preference")
		} else if binarySnowball.Finalized() {
			t.Fatalf("Should not have finalized")
		}
		binarySnowball.RecordSuccessfulPoll(1)
		binarySnowball.RecordUnsuccessfulPoll()
	}

	if binarySnowball.Preference() != 1 {
		t.Fatalf("Wrong preference")
	} else if binarySnowball.Finalized() {
		t.Fatalf("Should not have finalized")
	}

	binarySnowball.RecordSuccessfulPoll(1)
	if binarySnowball.Preference() != 1 {
		t.Fatalf("Wrong preference")
	} else if binarySnowball.Finalized() {
		t.Fatalf("Should not have finalized")
	}

	binarySnowball.RecordSuccessfulPoll(1)

	if binarySnowball.Preference() != 1 {
		t.Fatalf("Wrong preference")
	} else if !binarySnowball.Finalized() {
		t.Fatalf("Should have finalized")
	}

	expected = "SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = false))"
	if str := sb.String(); str != expected {
		t.Fatalf("Wrong state. Expected:\n%s\nGot:\n%s", expected, str)
	}
}
