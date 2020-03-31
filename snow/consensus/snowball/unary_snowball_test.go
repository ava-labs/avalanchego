// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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

	binarySnowball.RecordUnsuccessfulPoll()

	binarySnowball.RecordSuccessfulPoll(1)

	if binarySnowball.Finalized() {
		t.Fatalf("Should not have finalized")
	}

	binarySnowball.RecordSuccessfulPoll(1)

	if binarySnowball.Preference() != 1 {
		t.Fatalf("Wrong preference")
	} else if !binarySnowball.Finalized() {
		t.Fatalf("Should have finalized")
	}
}
