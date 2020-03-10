// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"testing"
)

func TestDirectedParams(t *testing.T) { ParamsTest(t, DirectedFactory{}) }

func TestDirectedIssued(t *testing.T) { IssuedTest(t, DirectedFactory{}) }

func TestDirectedLeftoverInput(t *testing.T) { LeftoverInputTest(t, DirectedFactory{}) }

func TestDirectedLowerConfidence(t *testing.T) { LowerConfidenceTest(t, DirectedFactory{}) }

func TestDirectedMiddleConfidence(t *testing.T) { MiddleConfidenceTest(t, DirectedFactory{}) }

func TestDirectedIndependent(t *testing.T) { IndependentTest(t, DirectedFactory{}) }

func TestDirectedVirtuous(t *testing.T) { VirtuousTest(t, DirectedFactory{}) }

func TestDirectedIsVirtuous(t *testing.T) { IsVirtuousTest(t, DirectedFactory{}) }

func TestDirectedConflicts(t *testing.T) { ConflictsTest(t, DirectedFactory{}) }

func TestDirectedQuiesce(t *testing.T) { QuiesceTest(t, DirectedFactory{}) }

func TestDirectedAcceptingDependency(t *testing.T) { AcceptingDependencyTest(t, DirectedFactory{}) }

func TestDirectedRejectingDependency(t *testing.T) { RejectingDependencyTest(t, DirectedFactory{}) }

func TestDirectedVacuouslyAccepted(t *testing.T) { VacuouslyAcceptedTest(t, DirectedFactory{}) }

func TestDirectedVirtuousDependsOnRogue(t *testing.T) {
	VirtuousDependsOnRogueTest(t, DirectedFactory{})
}

func TestDirectedString(t *testing.T) { StringTest(t, DirectedFactory{}, "DG") }
