package proposer


import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestRetrieverNoValidators(t *testing.T) {
	require := require.New(t)

	subnetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
			return nil, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

	delay, err := w.Delay(1, 0, nodeID)
	require.NoError(err)
	require.EqualValues(0, delay)
}
