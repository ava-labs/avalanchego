// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"testing"
)

func TestTopological(t *testing.T) { ConsensusTest(t, TopologicalFactory{}) }

func TestTopologicalCollect(t *testing.T) { CollectTest(t, TopologicalFactory{}) }

func TestTopologicalCollectNothing(t *testing.T) { CollectNothingTest(t, TopologicalFactory{}) }

func TestTopologicalCollectTransReject(t *testing.T) { CollectTransRejectTest(t, TopologicalFactory{}) }

func TestTopologicalCollectTransResetTest(t *testing.T) {
	CollectTransResetTest(t, TopologicalFactory{})
}

func TestTopologicalCollectTransVote(t *testing.T) { CollectTransVoteTest(t, TopologicalFactory{}) }

func TestTopologicalDivergedVoting(t *testing.T) { DivergedVotingTest(t, TopologicalFactory{}) }

func TestTopologicalMetricsError(t *testing.T) { MetricsErrorTest(t, TopologicalFactory{}) }

func TestTopologicalConsistent(t *testing.T) { ConsistentTest(t, TopologicalFactory{}) }
