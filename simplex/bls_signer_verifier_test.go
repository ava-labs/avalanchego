package simplex

import (
	"encoding/base64"
	"fmt"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/stretchr/testify/require"
	blst "github.com/supranational/blst/bindings/go"
	"simplex"
	"testing"
)

func TestBLSSignVerify(t *testing.T) {
	ls, err := localsigner.New()
	require.NoError(t, err)

	pkBytes := bls.PublicKeyToCompressedBytes(ls.PublicKey())
	nodeID, err := ids.ToShortID(pkBytes[:ids.NodeIDLen])
	require.NoError(t, err)

	signer := BLSSigner{
		NodeID:  ids.NodeID(nodeID),
		SignBLS: ls.Sign,
	}

	msg := "Begin at the beginning, and go on till you come to the end: then stop"
	sig, err := signer.Sign([]byte(msg))
	require.NoError(t, err)

	var v = BLSVerifier{
		nodeID2PK: map[ids.NodeID]bls.PublicKey{
			signer.NodeID: *ls.PublicKey(),
		},
	}

	err = v.Verify([]byte(msg), sig, signer.NodeID[:])
	require.NoError(t, err)
}

func TestVerifyChainHalt(t *testing.T) {
	s := `R59myL6JWDBUfnC0spjK/UM9um6qGNOZHPY3qmwWL16VzxY/ac2Ckd4xtNiyKZHVGqaqH8cz8jqFGoyU6QlPc2mAAv1SyQgZtFe5+8hmq4Dym85fNKdDAesN5xbVGU5KSupdeoW+Z/kTGcuwEn9xJbkEqP0ohiM9PnrxoI9obiH3pG36W+gyJZTVYYsm4HqrbrFQzxZttFrdryz1KKnwaTwzbPrN/WplcV1v5TxUsFy3IzTXUsodjantybon/zakCA3FMA==`
	bytes, err := base64.StdEncoding.DecodeString(s)
	require.NoError(t, err)

	pos := ids.NodeIDLen * 4

	fmt.Println(blst.BLST_P2_COMPRESS_BYTES, len(bytes[pos:]))

	sig, err := bls.SignatureFromBytes(bytes[pos:])
	require.NoError(t, err)
	require.NotNil(t, sig)
}

func TestBLSVerifyQC(t *testing.T) {
	s1, pk1 := createBLSSigner(t)
	s2, pk2 := createBLSSigner(t)
	s3, pk3 := createBLSSigner(t)
	s4, pk4 := createBLSSigner(t)

	msg := "If you don't know where you are going any road can take you there"

	sig1, err := s1.Sign([]byte(msg))
	require.NoError(t, err)

	sig2, err := s2.Sign([]byte(msg))
	require.NoError(t, err)

	sig4, err := s4.Sign([]byte(msg))
	require.NoError(t, err)

	var v = BLSVerifier{
		nodeID2PK: map[ids.NodeID]bls.PublicKey{
			s1.NodeID: pk1,
			s2.NodeID: pk2,
			s3.NodeID: pk3,
			s4.NodeID: pk4,
		},
	}

	err = v.Verify([]byte(msg), sig1, s1.NodeID[:])
	require.NoError(t, err)

	err = v.Verify([]byte(msg), sig2, s2.NodeID[:])
	require.NoError(t, err)

	err = v.Verify([]byte(msg), sig4, s4.NodeID[:])
	require.NoError(t, err)

	sa := SignatureAggregator(v)
	qc, err := sa.Aggregate([]simplex.Signature{
		{Signer: s1.NodeID[:], Value: sig1},
		{Signer: s2.NodeID[:], Value: sig2},
		{Signer: s4.NodeID[:], Value: sig4},
	})
	require.NoError(t, err)

	err = qc.Verify([]byte(msg))
	require.NoError(t, err)

	qcd := QCDeserializer(v)
	qc, err = qcd.DeserializeQuorumCertificate(qc.Bytes())
	require.NoError(t, err)

	err = qc.Verify([]byte(msg))
	require.NoError(t, err)

	msg = msg + "J"
	err = qc.Verify([]byte(msg))
	require.Error(t, err)
}

func createBLSSigner(t *testing.T) (BLSSigner, bls.PublicKey) {
	ls, err := localsigner.New()
	require.NoError(t, err)

	pkBytes := bls.PublicKeyToCompressedBytes(ls.PublicKey())
	nodeID, err := ids.ToShortID(pkBytes[:ids.NodeIDLen])
	require.NoError(t, err)
	return BLSSigner{
		SignBLS: ls.Sign,
		NodeID:  ids.NodeID(nodeID),
	}, *ls.PublicKey()
}
