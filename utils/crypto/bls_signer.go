package crypto

import "github.com/ava-labs/avalanchego/utils/crypto/bls"

var (
	_ BLSSigner = (*blsSigner)(nil)
	_ BLSSigner = (*noOpBLSSigner)(nil)
)

// BLSSigner creates BLS signatures for messages.
type BLSSigner interface {
	// Sign returns the signed representation of [msg].
	Sign(msg []byte) []byte
}

// blsSigner signs ips with a BLS key.
type blsSigner struct {
	secretKey *bls.SecretKey
}

// NewBLSSigner returns a BLSSigner
func NewBLSSigner(secretKey *bls.SecretKey) BLSSigner {
	if secretKey == nil {
		return &noOpBLSSigner{}
	}
	return &blsSigner{
		secretKey: secretKey,
	}
}

func (b blsSigner) Sign(msg []byte) []byte {
	return bls.SignatureToBytes(bls.Sign(b.secretKey, msg))
}

// noOpBLSSigner is a signer that always returns an empty signature.
type noOpBLSSigner struct{}

func (noOpBLSSigner) Sign([]byte) []byte {
	return nil
}
