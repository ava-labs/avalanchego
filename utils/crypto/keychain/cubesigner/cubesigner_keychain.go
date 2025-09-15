// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package cubesigner

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	avasecp256k1 "github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/libevm/common"

	"github.com/cubist-labs/cubesigner-go-sdk/client"
	"github.com/cubist-labs/cubesigner-go-sdk/models"
	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/exp/maps"
)

var (
	_ keychain.Keychain    = (*CubesignerKeychain)(nil)
	_ keychain.EthKeychain = (*CubesignerKeychain)(nil)
	_ keychain.Signer      = (*cubesignerSigner)(nil)
	_ CubeSignerClient     = (*client.ApiClient)(nil)
)

// keyInfo holds both the public key and keyID for a cubesigner key
type keyInfo struct {
	pubKey *avasecp256k1.PublicKey
	keyID  string
}

// cubesignerKeychain is an abstraction of the underlying cubesigner connection,
// to be able to get a signer from a specific address
type CubesignerKeychain struct {
	cubesignerClient CubeSignerClient
	avaAddrToKeyInfo map[ids.ShortID]*keyInfo
	ethAddrToKeyInfo map[common.Address]*keyInfo
}

// processKey obtains and processes key information from cubesigner
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
		return nil, fmt.Errorf("keytype %s of server key %s is not supported", keyInfo.KeyType, keyID)
	}

	// get public key
	pubKeyHex := strings.TrimPrefix(keyInfo.PublicKey, "0x")
	pubKeyBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode public key for server key %s: %w", keyID, err)
	}
	if len(pubKeyBytes) != 65 {
		return nil, fmt.Errorf("invalid public key length for server key %s: expected 65 bytes, got %d", keyID, len(pubKeyBytes))
	}
	if pubKeyBytes[0] != 0x04 {
		return nil, fmt.Errorf("invalid public key format for server key %s: expected uncompressed format (0x04 prefix), got 0x%02x", keyID, pubKeyBytes[0])
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

// NewCubeSignerKeychain creates a new keychain abstraction over a cubesigner connection
func NewCubesignerKeychain(
	cubesignerClient CubeSignerClient,
	keyIDs []string,
) (*CubesignerKeychain, error) {
	if len(keyIDs) == 0 {
		return nil, fmt.Errorf("you need to provide at least one key to create a server keychain")
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

	return &CubesignerKeychain{
		cubesignerClient: cubesignerClient,
		avaAddrToKeyInfo: avaAddrToKeyInfo,
		ethAddrToKeyInfo: ethAddrToKeyInfo,
	}, nil
}

func (kc *CubesignerKeychain) Addresses() set.Set[ids.ShortID] {
	return set.Of(maps.Keys(kc.avaAddrToKeyInfo)...)
}

func (kc *CubesignerKeychain) Get(addr ids.ShortID) (keychain.Signer, bool) {
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

func (kc *CubesignerKeychain) EthAddresses() set.Set[common.Address] {
	return set.Of(maps.Keys(kc.ethAddrToKeyInfo)...)
}

func (kc *CubesignerKeychain) GetEth(addr common.Address) (keychain.Signer, bool) {
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
// pattern from CubeSigner signing operations
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

// expects to receive a hash of the unsigned tx bytes
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
		return nil, fmt.Errorf("empty signature obtained from server")
	}
	return processSignatureResponse(response.ResponseData.Signature)
}

// expects to receive the unsigned tx bytes
func (s *cubesignerSigner) Sign(b []byte, opts ...keychain.SigningOption) ([]byte, error) {
	options := &keychain.SigningOptions{}
	for _, opt := range opts {
		opt(options)
	}
	
	// Require chainAlias and network from options
	if options.ChainAlias == "" {
		return nil, fmt.Errorf("chainAlias must be specified in options for CubeSigner")
	}
	if options.ChainAlias != "P" && options.ChainAlias != "X" && options.ChainAlias != "C" {
		return nil, fmt.Errorf("chainAlias must be 'P', 'X' or 'C' for CubeSigner, got %q", options.ChainAlias)
	}
	if options.NetworkID == 0 {
		return nil, fmt.Errorf("network ID must be specified in options for CubeSigner")
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
		return nil, fmt.Errorf("empty signature obtained from server")
	}
	return processSignatureResponse(response.ResponseData.Signature)
}

func (s *cubesignerSigner) Address() ids.ShortID {
	return s.pubKey.Address()
}
