package simplex

import (
	"context"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"simplex"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/stretchr/testify/require"
)

func TestStorage(t *testing.T) {
	s1, pk1 := createBLSSigner(t)
	s2, pk2 := createBLSSigner(t)
	s3, pk3 := createBLSSigner(t)
	s4, pk4 := createBLSSigner(t)

	var v = BLSVerifier{
		nodeID2PK: map[ids.NodeID]bls.PublicKey{
			s1.NodeID: pk1,
			s2.NodeID: pk2,
			s3.NodeID: pk3,
			s4.NodeID: pk4,
		},
	}

	vmIDStorage := make(map[uint64]ids.ID)
	vmBlockStorage := make(map[ids.ID]*snowmantest.Block)

	memDB := memdb.New()
	vm := &blocktest.VM{}
	s, err := NewStorage(memDB, QCDeserializer(v), vm, []byte{1, 2, 3})
	require.NoError(t, err)

	msg := "Begin at the beginning, and go on till you come to the end: then stop."

	sig1, err := s1.Sign([]byte(msg))
	require.NoError(t, err)

	sig2, err := s2.Sign([]byte(msg))
	require.NoError(t, err)

	sig4, err := s4.Sign([]byte(msg))
	require.NoError(t, err)

	qc, err := SignatureAggregator(v).Aggregate([]simplex.Signature{
		{Signer: s1.NodeID[:], Value: sig1},
		{Signer: s2.NodeID[:], Value: sig2},
		{Signer: s4.NodeID[:], Value: sig4},
	})

	blockID2 := ids.GenerateTestID()

	vm.GetBlockIDAtHeightF = func(ctx context.Context, height uint64) (ids.ID, error) {
		return vmIDStorage[height], nil
	}

	vm.GetBlockF = func(ctx context.Context, id ids.ID) (snowman.Block, error) {
		return vmBlockStorage[id], nil
	}

	s.Index(&VerifiedBlock{
		accept: func(ctx context.Context) error {
			vmIDStorage[2] = blockID2
			vmBlockStorage[blockID2] = &snowmantest.Block{
				BytesV: []byte{4, 5, 6},
			}
			return nil
		},
		innerBlock: []byte{4, 5, 6},
	}, simplex.FinalizationCertificate{QC: qc})
	require.Equal(t, uint64(2), s.Height())

	block0, fCert, ok := s.Retrieve(0)
	require.True(t, ok)
	require.Equal(t, simplex.FinalizationCertificate{}, fCert)

	var vb VerifiedBlock
	err = vb.FromBytes(block0.Bytes())
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2, 3}, vb.innerBlock)

	block1, fCert, ok := s.Retrieve(1)
	require.True(t, ok)

	err = vb.FromBytes(block1.Bytes())
	require.NoError(t, err)
	require.Equal(t, []byte{4, 5, 6}, vb.innerBlock)
	require.Equal(t, qc.Bytes(), fCert.QC.Bytes())

}
