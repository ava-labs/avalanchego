// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cubesigner

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ava-labs/libevm/common"
	"github.com/cubist-labs/cubesigner-go-sdk/client"
	"github.com/cubist-labs/cubesigner-go-sdk/models"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/wallet/chain/c"

	avasecp256k1 "github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
)

var (
	_ keychain.Keychain = (*Keychain)(nil)
	_ c.EthKeychain     = (*Keychain)(nil)
	_ keychain.Signer   = (*cubesignerSigner)(nil)
	_ CubeSignerClient  = (*client.ApiClient)(nil)

	ErrNoKeysProvided           = errors.New("you need to provide at least one key to create a server keychain")
	ErrEmptySignatureFromServer = errors.New("empty signature obtained from server")
	ErrChainAliasMissing        = errors.New("chainAlias must be specified in options for CubeSigner")
	ErrInvalidChainAlias        = errors.New("chainAlias must be 'P', 'X' or 'C' for CubeSigner")
	ErrNetworkIDMissing         = errors.New("network ID must be specified in options for CubeSigner")
	ErrUnsupportedKeyType       = errors.New("unsupported key type")
	ErrInvalidPublicKey         = errors.New("invalid public key format")
)

const (
	// UncompressedPublicKeyLength is the expected length of an uncompressed secp256k1 public key in bytes.
	// This includes the prefix byte (1 byte) plus the X and Y coordinates (32 bytes each).
	UncompressedPublicKeyLength = 65
	// UncompressedPublicKeyPrefix is the prefix byte for uncompressed secp256k1 public keys.
	// This byte indicates that the key is in uncompressed format.
	UncompressedPublicKeyPrefix = 0x04
)

// keyInfo holds both the public key and keyID for a CubeSigner key.
type keyInfo struct {
	pubKey *avasecp256k1.PublicKey // The Avalanche public key derived from CubeSigner
	keyID  string                  // The CubeSigner key identifier
}

// Keychain provides an abstraction over CubeSigner remote signing capabilities.
type Keychain struct {
	cubesignerClient CubeSignerClient            // Client for CubeSigner API operations
	avaAddrToKeyInfo map[ids.ShortID]*keyInfo    // Maps Avalanche addresses to key info
	ethAddrToKeyInfo map[common.Address]*keyInfo // Maps Ethereum addresses to key info
}

// processKey obtains and processes key information from CubeSigner.
// It validates that the key exists in the CubeSigner organization, verifies
// that the key type is supported (secp256k1 for Avalanche/Ethereum), and
// converts the public key from hex format to an Avalanche public key.
func processKey(
	cubesignerClient CubeSignerClient,
	keyID string,
) (*avasecp256k1.PublicKey, error) {
	// Validate key exists
	keyInfo, err := cubesignerClient.GetKeyInOrg(keyID)
	if err != nil {
		return nil, fmt.Errorf("could not find server key %s: %w", keyID, err)
	}

	// Validate key type
	switch keyInfo.KeyType {
	case models.SecpAvaAddr, models.SecpAvaTestAddr, models.SecpEthAddr:
		// Supported key types
	default:
		return nil, fmt.Errorf("keytype %s of server key %s: %w", keyInfo.KeyType, keyID, ErrUnsupportedKeyType)
	}

	// get public key
	pubKeyHex := strings.TrimPrefix(keyInfo.PublicKey, "0x")
	pubKeyBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to decode public key for server key %s: %w", ErrInvalidPublicKey, keyID, err)
	}
	if len(pubKeyBytes) != UncompressedPublicKeyLength {
		return nil, fmt.Errorf("invalid public key length for server key %s: expected %d bytes, got %d", keyID, UncompressedPublicKeyLength, len(pubKeyBytes))
	}
	if pubKeyBytes[0] != UncompressedPublicKeyPrefix {
		return nil, fmt.Errorf("invalid public key format for server key %s: expected uncompressed format (0x%02x prefix), got 0x%02x", keyID, UncompressedPublicKeyPrefix, pubKeyBytes[0])
	}
	pubKey, err := secp256k1.ParsePubKey(pubKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid public key format for server key %s: %w", keyID, err)
	}
	avaPubKey, err := avasecp256k1.ToPublicKey(pubKey.SerializeCompressed())
	if err != nil {
		return nil, fmt.Errorf("invalid public key format for server key %s: %w", keyID, err)
	}

	return avaPubKey, nil
}

// NewKeychain creates a new keychain abstraction over a CubeSigner connection.
// It validates that all provided keyIDs exist in the CubeSigner organization and returns
// a keychain that can be used to sign transactions using those keys.
func NewKeychain(
	cubesignerClient CubeSignerClient,
	keyIDs []string,
) (*Keychain, error) {
	if len(keyIDs) == 0 {
		return nil, ErrNoKeysProvided
	}

	avaAddrToKeyInfo := map[ids.ShortID]*keyInfo{}
	ethAddrToKeyInfo := map[common.Address]*keyInfo{}

	for _, keyID := range keyIDs {
		avaPubKey, err := processKey(cubesignerClient, keyID)
		if err != nil {
			return nil, err
		}

		keyInf := &keyInfo{
			pubKey: avaPubKey,
			keyID:  keyID,
		}

		avaAddrToKeyInfo[avaPubKey.Address()] = keyInf
		ethAddrToKeyInfo[avaPubKey.EthAddress()] = keyInf
	}

	return &Keychain{
		cubesignerClient: cubesignerClient,
		avaAddrToKeyInfo: avaAddrToKeyInfo,
		ethAddrToKeyInfo: ethAddrToKeyInfo,
	}, nil
}

// Addresses returns the set of Avalanche addresses that this keychain can sign for.
func (kc *Keychain) Addresses() set.Set[ids.ShortID] {
	return set.Of(maps.Keys(kc.avaAddrToKeyInfo)...)
}

// Get returns a signer for the given Avalanche address, if it exists in this keychain.
func (kc *Keychain) Get(addr ids.ShortID) (keychain.Signer, bool) {
	keyInf, found := kc.avaAddrToKeyInfo[addr]
	if !found {
		return nil, false
	}
	return &cubesignerSigner{
		cubesignerClient: kc.cubesignerClient,
		pubKey:           keyInf.pubKey,
		keyID:            keyInf.keyID,
	}, true
}

// EthAddresses returns the set of Ethereum addresses that this keychain can sign for.
func (kc *Keychain) EthAddresses() set.Set[common.Address] {
	return set.Of(maps.Keys(kc.ethAddrToKeyInfo)...)
}

// GetEth returns a signer for the given Ethereum address, if it exists in this keychain.
func (kc *Keychain) GetEth(addr common.Address) (keychain.Signer, bool) {
	keyInf, found := kc.ethAddrToKeyInfo[addr]
	if !found {
		return nil, false
	}
	return &cubesignerSigner{
		cubesignerClient: kc.cubesignerClient,
		pubKey:           keyInf.pubKey,
		keyID:            keyInf.keyID,
	}, true
}

// cubesignerAvagoSigner is an abstraction of the underlying cubesigner connection,
// to be able sign for a specific address
type cubesignerSigner struct {
	cubesignerClient CubeSignerClient
	pubKey           *avasecp256k1.PublicKey
	keyID            string
}

// processSignatureResponse is a helper function that processes the common response
// pattern from CubeSigner signing operations. It decodes the hex signature and validates its length.
func processSignatureResponse(signatureHex string) ([]byte, error) {
	signatureBytes, err := hex.DecodeString(strings.TrimPrefix(signatureHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("failed to decode server's signature: %w", err)
	}
	if len(signatureBytes) != avasecp256k1.SignatureLen {
		return nil, fmt.Errorf("invalid server's signature length: expected %d bytes, got %d", avasecp256k1.SignatureLen, len(signatureBytes))
	}
	return signatureBytes, nil
}

// SignHash signs the given hash using CubeSigner's BlobSign API.
// It expects to receive a hash of the unsigned transaction bytes.
func (s *cubesignerSigner) SignHash(b []byte) ([]byte, error) {
	response, err := s.cubesignerClient.BlobSign(
		s.keyID,
		models.BlobSignRequest{
			MessageBase64: base64.StdEncoding.EncodeToString(b),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("server signing err: %w", err)
	}
	if response.ResponseData == nil {
		return nil, ErrEmptySignatureFromServer
	}
	return processSignatureResponse(response.ResponseData.Signature)
}

// Sign signs the given payload according to the given signing options.
// It expects to receive the unsigned transaction bytes and requires ChainAlias and NetworkID
// to be specified in the signing options for CubeSigner's AvaSerializedTxSign API.
func (s *cubesignerSigner) Sign(b []byte, opts ...keychain.SigningOption) ([]byte, error) {
	options := &keychain.SigningOptions{}
	for _, opt := range opts {
		opt(options)
	}
	// Require chainAlias and network from options
	if options.ChainAlias == "" {
		return nil, ErrChainAliasMissing
	}
	if options.ChainAlias != "P" && options.ChainAlias != "X" && options.ChainAlias != "C" {
		return nil, fmt.Errorf("%w, got %q", ErrInvalidChainAlias, options.ChainAlias)
	}
	if options.NetworkID == 0 {
		return nil, ErrNetworkIDMissing
	}
	var materialID string
	if options.ChainAlias == "C" {
		materialID = s.pubKey.EthAddress().Hex()
	} else {
		hrp := constants.GetHRP(options.NetworkID)
		addr := s.pubKey.Address()
		var err error
		materialID, err = address.FormatBech32(hrp, addr.Bytes())
		if err != nil {
			return nil, fmt.Errorf("failed to format %s address %v as Bech32: %w", hrp, addr, err)
		}
	}

	response, err := s.cubesignerClient.AvaSerializedTxSign(
		options.ChainAlias,
		materialID,
		models.AvaSerializedTxSignRequest{
			Tx: "0x" + hex.EncodeToString(b),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("server signing err: %w", err)
	}
	if response.ResponseData == nil {
		return nil, ErrEmptySignatureFromServer
	}
	return processSignatureResponse(response.ResponseData.Signature)
}

// Address returns the Avalanche address associated with this signer.
func (s *cubesignerSigner) Address() ids.ShortID {
	return s.pubKey.Address()
}
