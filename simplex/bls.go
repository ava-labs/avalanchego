package simplex

import (
	"encoding/binary"
	"fmt"
	"simplex"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	simplexLabel = []byte("<simplex>")
)

// BLSSigner signs messages encoded with the provided ChainID and NetworkID
// using the SignBLS function.
type BLSSigner struct {
	chainID  ids.ID
	subnetID ids.ID
	signBLS  func(msg []byte) (*bls.Signature, error) // should we pass in a verifier function into BLSVerifier?
	nodeID   ids.NodeID
}

type BLSVerifier struct {
	nodeID2PK map[ids.NodeID]bls.PublicKey
	subnetID  ids.ID
	chainID   ids.ID
}

func NewBLSAuth(config *Config) (BLSSigner, BLSVerifier) {
	return BLSSigner{
			chainID:  config.Ctx.ChainID,
			subnetID: config.Ctx.SubnetID,
			nodeID:   config.Ctx.NodeID,
			signBLS:  config.SignBLS,
		}, BLSVerifier{
			nodeID2PK: make(map[ids.NodeID]bls.PublicKey),
			subnetID:  ids.Empty,
			chainID:   ids.Empty,
		}
}

// Sign signs the given message using BLS signature scheme.
// It encodes the message to sign with the simplex label, chain ID, and subnet ID,
// Returns the signature as a byte slice prefixed with the node ID.
func (s *BLSSigner) Sign(message []byte) ([]byte, error) {
	message2Sign := encodeMessageToSign(message, s.chainID, s.subnetID)
	sig, err := s.signBLS(message2Sign)
	if err != nil {
		return nil, err
	}

	sigBytes := bls.SignatureToBytes(sig)

	buff := make([]byte, ids.NodeIDLen+len(sigBytes))
	copy(buff, s.nodeID[:])
	copy(buff[len(s.nodeID):], sigBytes)

	return sigBytes, nil
}

// encodesMessageToSign returns a byte slice [simplexLabel][chainID][networkID][message length][message].
func encodeMessageToSign(message []byte, chainID ids.ID, networkID ids.ID) []byte {
	message2Sign := make([]byte, len(message)+len(simplexLabel)+ids.IDLen+ids.IDLen+4)
	msgLenBuff := make([]byte, 4)
	binary.BigEndian.PutUint32(msgLenBuff, uint32(len(message)))

	copy(message2Sign, simplexLabel)
	copy(message2Sign[len(simplexLabel):], chainID[:])
	copy(message2Sign[len(simplexLabel)+ids.IDLen:], networkID[:])
	copy(message2Sign[len(simplexLabel)+ids.IDLen+ids.IDLen:], msgLenBuff)
	copy(message2Sign[len(simplexLabel)+ids.IDLen+ids.IDLen+4:], message)
	return message2Sign
}

func (v BLSVerifier) Verify(message []byte, signature []byte, signer simplex.NodeID) error {
	if len(signer) != ids.NodeIDLen {
		return fmt.Errorf("expected signer to be %d bytes but got %d bytes", ids.NodeIDLen, len(signer))
	}

	key := ids.NodeID(signer)
	pk, exists := v.nodeID2PK[key]
	if !exists {
		return fmt.Errorf("signer %x is not found in the membership set", signer)
	}

	sig, err := bls.SignatureFromBytes(signature)
	if err != nil {
		return fmt.Errorf("failed to parse signature: %w", err)
	}

	message2Verify := encodeMessageToSign(message, v.chainID, v.subnetID)

	if !bls.Verify(&pk, sig, message2Verify) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

func createVerifier(config *Config) BLSVerifier {
	verifier := BLSVerifier{
		nodeID2PK: make(map[ids.NodeID]bls.PublicKey),
		subnetID:  config.Ctx.SubnetID,
		chainID:   config.Ctx.ChainID,
	}

	nodes := config.Validators.GetValidatorIDs(config.Ctx.SubnetID)
	for _, node := range nodes {
		validator, ok := config.Validators.GetValidator(config.Ctx.SubnetID, node)
		if !ok {
			continue
		}

		verifier.nodeID2PK[node] = *validator.PublicKey
	}
	return verifier
}
