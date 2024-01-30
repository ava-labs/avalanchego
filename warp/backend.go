// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

var (
	_                         Backend = &backend{}
	errParsingOffChainMessage         = errors.New("failed to parse off-chain message")
)

const batchSize = ethdb.IdealBatchSize

type BlockClient interface {
	GetBlock(ctx context.Context, blockID ids.ID) (snowman.Block, error)
}

// Backend tracks signature-eligible warp messages and provides an interface to fetch them.
// The backend is also used to query for warp message signatures by the signature request handler.
type Backend interface {
	// AddMessage signs [unsignedMessage] and adds it to the warp backend database
	AddMessage(unsignedMessage *avalancheWarp.UnsignedMessage) error

	// GetMessageSignature returns the signature of the requested message hash.
	GetMessageSignature(messageID ids.ID) ([bls.SignatureLen]byte, error)

	// GetBlockSignature returns the signature of the requested message hash.
	GetBlockSignature(blockID ids.ID) ([bls.SignatureLen]byte, error)

	// GetMessage retrieves the [unsignedMessage] from the warp backend database if available
	GetMessage(messageHash ids.ID) (*avalancheWarp.UnsignedMessage, error)

	// Clear clears the entire db
	Clear() error
}

// backend implements Backend, keeps track of warp messages, and generates message signatures.
type backend struct {
	networkID                 uint32
	sourceChainID             ids.ID
	db                        database.Database
	warpSigner                avalancheWarp.Signer
	blockClient               BlockClient
	messageSignatureCache     *cache.LRU[ids.ID, [bls.SignatureLen]byte]
	blockSignatureCache       *cache.LRU[ids.ID, [bls.SignatureLen]byte]
	messageCache              *cache.LRU[ids.ID, *avalancheWarp.UnsignedMessage]
	offchainAddressedCallMsgs map[ids.ID]*avalancheWarp.UnsignedMessage
}

// NewBackend creates a new Backend, and initializes the signature cache and message tracking database.
func NewBackend(
	networkID uint32,
	sourceChainID ids.ID,
	warpSigner avalancheWarp.Signer,
	blockClient BlockClient,
	db database.Database,
	cacheSize int,
	offchainMessages [][]byte,
) (Backend, error) {
	b := &backend{
		networkID:                 networkID,
		sourceChainID:             sourceChainID,
		db:                        db,
		warpSigner:                warpSigner,
		blockClient:               blockClient,
		messageSignatureCache:     &cache.LRU[ids.ID, [bls.SignatureLen]byte]{Size: cacheSize},
		blockSignatureCache:       &cache.LRU[ids.ID, [bls.SignatureLen]byte]{Size: cacheSize},
		messageCache:              &cache.LRU[ids.ID, *avalancheWarp.UnsignedMessage]{Size: cacheSize},
		offchainAddressedCallMsgs: make(map[ids.ID]*avalancheWarp.UnsignedMessage),
	}
	return b, b.initOffChainMessages(offchainMessages)
}

func (b *backend) initOffChainMessages(offchainMessages [][]byte) error {
	for i, offchainMsg := range offchainMessages {
		unsignedMsg, err := avalancheWarp.ParseUnsignedMessage(offchainMsg)
		if err != nil {
			return fmt.Errorf("%w at index %d: %w", errParsingOffChainMessage, i, err)
		}

		if unsignedMsg.NetworkID != b.networkID {
			return fmt.Errorf("%w at index %d", avalancheWarp.ErrWrongNetworkID, i)
		}

		if unsignedMsg.SourceChainID != b.sourceChainID {
			return fmt.Errorf("%w at index %d", avalancheWarp.ErrWrongSourceChainID, i)
		}

		_, err = payload.ParseAddressedCall(unsignedMsg.Payload)
		if err != nil {
			return fmt.Errorf("%w at index %d as AddressedCall: %w", errParsingOffChainMessage, i, err)
		}
		b.offchainAddressedCallMsgs[unsignedMsg.ID()] = unsignedMsg
	}

	return nil
}

func (b *backend) Clear() error {
	b.messageSignatureCache.Flush()
	b.blockSignatureCache.Flush()
	b.messageCache.Flush()
	return database.Clear(b.db, batchSize)
}

func (b *backend) AddMessage(unsignedMessage *avalancheWarp.UnsignedMessage) error {
	messageID := unsignedMessage.ID()

	// In the case when a node restarts, and possibly changes its bls key, the cache gets emptied but the database does not.
	// So to avoid having incorrect signatures saved in the database after a bls key change, we save the full message in the database.
	// Whereas for the cache, after the node restart, the cache would be emptied so we can directly save the signatures.
	if err := b.db.Put(messageID[:], unsignedMessage.Bytes()); err != nil {
		return fmt.Errorf("failed to put warp signature in db: %w", err)
	}

	var signature [bls.SignatureLen]byte
	sig, err := b.warpSigner.Sign(unsignedMessage)
	if err != nil {
		return fmt.Errorf("failed to sign warp message: %w", err)
	}

	copy(signature[:], sig)
	b.messageSignatureCache.Put(messageID, signature)
	log.Debug("Adding warp message to backend", "messageID", messageID)
	return nil
}

func (b *backend) GetMessageSignature(messageID ids.ID) ([bls.SignatureLen]byte, error) {
	log.Debug("Getting warp message from backend", "messageID", messageID)
	if sig, ok := b.messageSignatureCache.Get(messageID); ok {
		return sig, nil
	}

	unsignedMessage, err := b.GetMessage(messageID)
	if err != nil {
		return [bls.SignatureLen]byte{}, fmt.Errorf("failed to get warp message %s from db: %w", messageID.String(), err)
	}

	var signature [bls.SignatureLen]byte
	sig, err := b.warpSigner.Sign(unsignedMessage)
	if err != nil {
		return [bls.SignatureLen]byte{}, fmt.Errorf("failed to sign warp message: %w", err)
	}

	copy(signature[:], sig)
	b.messageSignatureCache.Put(messageID, signature)
	return signature, nil
}

func (b *backend) GetBlockSignature(blockID ids.ID) ([bls.SignatureLen]byte, error) {
	log.Debug("Getting block from backend", "blockID", blockID)
	if sig, ok := b.blockSignatureCache.Get(blockID); ok {
		return sig, nil
	}

	block, err := b.blockClient.GetBlock(context.TODO(), blockID)
	if err != nil {
		return [bls.SignatureLen]byte{}, fmt.Errorf("failed to get block %s: %w", blockID, err)
	}
	if block.Status() != choices.Accepted {
		return [bls.SignatureLen]byte{}, fmt.Errorf("block %s was not accepted", blockID)
	}

	var signature [bls.SignatureLen]byte
	blockHashPayload, err := payload.NewHash(blockID)
	if err != nil {
		return [bls.SignatureLen]byte{}, fmt.Errorf("failed to create new block hash payload: %w", err)
	}
	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(b.networkID, b.sourceChainID, blockHashPayload.Bytes())
	if err != nil {
		return [bls.SignatureLen]byte{}, fmt.Errorf("failed to create new unsigned warp message: %w", err)
	}
	sig, err := b.warpSigner.Sign(unsignedMessage)
	if err != nil {
		return [bls.SignatureLen]byte{}, fmt.Errorf("failed to sign warp message: %w", err)
	}

	copy(signature[:], sig)
	b.blockSignatureCache.Put(blockID, signature)
	return signature, nil
}

func (b *backend) GetMessage(messageID ids.ID) (*avalancheWarp.UnsignedMessage, error) {
	if message, ok := b.messageCache.Get(messageID); ok {
		return message, nil
	}
	if message, ok := b.offchainAddressedCallMsgs[messageID]; ok {
		return message, nil
	}

	unsignedMessageBytes, err := b.db.Get(messageID[:])
	if err != nil {
		return nil, fmt.Errorf("failed to get warp message %s from db: %w", messageID.String(), err)
	}

	unsignedMessage, err := avalancheWarp.ParseUnsignedMessage(unsignedMessageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse unsigned message %s: %w", messageID.String(), err)
	}
	b.messageCache.Put(messageID, unsignedMessage)

	return unsignedMessage, nil
}
