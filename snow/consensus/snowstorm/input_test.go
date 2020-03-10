// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"testing"
)

func TestInputParams(t *testing.T) { ParamsTest(t, InputFactory{}) }

func TestInputIssued(t *testing.T) { IssuedTest(t, InputFactory{}) }

func TestInputLeftoverInput(t *testing.T) { LeftoverInputTest(t, InputFactory{}) }

func TestInputLowerConfidence(t *testing.T) { LowerConfidenceTest(t, InputFactory{}) }

func TestInputMiddleConfidence(t *testing.T) { MiddleConfidenceTest(t, InputFactory{}) }

func TestInputIndependent(t *testing.T) { IndependentTest(t, InputFactory{}) }

func TestInputVirtuous(t *testing.T) { VirtuousTest(t, InputFactory{}) }

func TestInputIsVirtuous(t *testing.T) { IsVirtuousTest(t, InputFactory{}) }

func TestInputConflicts(t *testing.T) { ConflictsTest(t, InputFactory{}) }

func TestInputQuiesce(t *testing.T) { QuiesceTest(t, InputFactory{}) }

func TestInputAcceptingDependency(t *testing.T) { AcceptingDependencyTest(t, InputFactory{}) }

func TestInputRejectingDependency(t *testing.T) { RejectingDependencyTest(t, InputFactory{}) }

func TestInputVacuouslyAccepted(t *testing.T) { VacuouslyAcceptedTest(t, InputFactory{}) }

func TestInputVirtuousDependsOnRogue(t *testing.T) { VirtuousDependsOnRogueTest(t, InputFactory{}) }

func TestInputString(t *testing.T) { StringTest(t, InputFactory{}, "IG") }
