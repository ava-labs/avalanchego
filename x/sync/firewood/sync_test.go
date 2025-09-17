package firewood

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/utils/logging"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

func Test_Firewood_Sync(t *testing.T) {
	numKeys := []int{0, 1, 1000}
	for _, n := range numKeys {
		t.Run("numKeys="+string(rune(n)), func(t *testing.T) {
			require := require.New(t)
			numKeys := 1000
			fullDB := generateDB(t, numKeys)
			db := generateDB(t, 0)

			root, err := fullDB.GetMerkleRoot(context.Background())
			require.NoError(err)

			ctx := context.Background()
			syncer, err := xsync.NewManager(
				db,
				xsync.ManagerConfig{
					RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetRangeProofHandler(fullDB)),
					ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetChangeProofHandler(fullDB)),
					SimultaneousWorkLimit: 5,
					Log:                   logging.NoLog{},
					TargetRoot:            root,
				},
				prometheus.NewRegistry(),
			)
			require.NoError(err)
			require.NotNil(syncer)
			require.NoError(syncer.Start(ctx))
			require.NoError(syncer.Wait(ctx))
		})
	}
}
