package crypto

import (
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	_ BLSSigner = (*BLSKeySigner)(nil)
	_ BLSSigner = (*NoOpBLSSigner)(nil)
)

// BLSSigner creates BLS signatures for messages.
type BLSSigner interface {
	// Sign returns the signed representation of [msg].
	Sign(msg []byte) []byte
}

// BLSKeySigner signs ips with a BLS key.
type BLSKeySigner struct {
	secretKey *bls.SecretKey
}

func NewBLSSigner(sk *bls.SecretKey) BLSSigner {
	return BLSKeySigner{
		secretKey: sk,
	}
}

func (b BLSKeySigner) Sign(msg []byte) []byte {
	return bls.SignatureToBytes(bls.Sign(b.secretKey, msg))
}

// NoOpBLSSigner is a signer that always returns an empty signature.
type NoOpBLSSigner struct{}

func (NoOpBLSSigner) Sign([]byte) []byte {
	return nil
}
