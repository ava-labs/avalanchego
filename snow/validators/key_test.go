package validators

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestPublicKey_GetKey(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk := bls.PublicFromSecretKey(sk)
	pkBytes := pk.Serialize()

	type expected struct {
		key *bls.PublicKey
		err error
	}

	tests := []struct {
		name     string
		Key      PublicKey
		expected expected
	}{
		{
			name: "nil key, populated Bytes",
			Key:  NewPublicKeyFromBytes(pkBytes),
			expected: expected{
				key: pk,
			},
		},
		{
			name: "populated key, nil Bytes",
			Key:  NewPublicKey(pk),
			expected: expected{
				key: pk,
			},
		},
		{
			name: "invalid key",
			Key:  NewPublicKeyFromBytes([]byte("foobar")),
			expected: expected{
				err: errFailedPublicKeyDeserialize,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			actual, err := test.Key.Key()
			r.Equal(test.expected.key, actual)
			r.Equal(test.expected.err, err)
		})
	}
}
func TestPublicKey_GetBytes(t *testing.T) {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk := bls.PublicFromSecretKey(sk)
	pkBytes := pk.Serialize()

	tests := []struct {
		name     string
		key      PublicKey
		expected []byte
	}{
		{
			name:     "nil key, populated Bytes",
			key:      NewPublicKeyFromBytes(pkBytes),
			expected: pkBytes,
		},
		{
			name:     "populated key, nil Bytes",
			key:      NewPublicKey(pk),
			expected: pkBytes,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			actual := test.key.Bytes()
			r.Equal(test.expected, actual)
		})
	}
}
