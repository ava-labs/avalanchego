// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	_                         Backend = (*backend)(nil)
	ErrValidateBlock                  = errors.New("failed to validate block message")
	ErrVerifyWarpMessage              = errors.New("failed to verify warp message")
	errParsingOffChainMessage         = errors.New("failed to parse off-chain message")

	messageCacheSize = 500
)

type BlockClient interface {
	GetAcceptedBlock(ctx context.Context, blockID ids.ID) (snowman.Block, error)
}

// Backend tracks signature-eligible warp messages and provides an interface to fetch them.
// The backend is also used to query for warp message signatures by the signature request handler.
type Backend interface {
	// AddMessage signs [unsignedMessage] and adds it to the warp backend database
	AddMessage(unsignedMessage *avalancheWarp.UnsignedMessage) error

	// GetMessageSignature validates the message and returns the signature of the requested message.
	GetMessageSignature(ctx context.Context, message *avalancheWarp.UnsignedMessage) ([]byte, error)

	// GetBlockSignature returns the signature of a hash payload containing blockID if it's the ID of an accepted block.
	GetBlockSignature(ctx context.Context, blockID ids.ID) ([]byte, error)

	// GetMessage retrieves the [unsignedMessage] from the warp backend database if available
	GetMessage(messageHash ids.ID) (*avalancheWarp.UnsignedMessage, error)

	acp118.Verifier
}

// backend implements Backend, keeps track of warp messages, and generates message signatures.
type backend struct {
	networkID                 uint32
	sourceChainID             ids.ID
	db                        database.Database
	warpSigner                avalancheWarp.Signer
	blockClient               BlockClient
	uptimeTracker             *uptimetracker.UptimeTracker
	signatureCache            cache.Cacher[ids.ID, []byte]
	messageCache              *lru.Cache[ids.ID, *avalancheWarp.UnsignedMessage]
	offchainAddressedCallMsgs map[ids.ID]*avalancheWarp.UnsignedMessage
	stats                     *verifierStats
}

// NewBackend creates a new Backend, and initializes the signature cache and message tracking database.
func NewBackend(
	networkID uint32,
	sourceChainID ids.ID,
	warpSigner avalancheWarp.Signer,
	blockClient BlockClient,
	uptimeTracker *uptimetracker.UptimeTracker,
	db database.Database,
	signatureCache cache.Cacher[ids.ID, []byte],
	offchainMessages [][]byte,
) (Backend, error) {
	b := &backend{
		networkID:                 networkID,
		sourceChainID:             sourceChainID,
		db:                        db,
		warpSigner:                warpSigner,
		blockClient:               blockClient,
		signatureCache:            signatureCache,
		uptimeTracker:             uptimeTracker,
		messageCache:              lru.NewCache[ids.ID, *avalancheWarp.UnsignedMessage](messageCacheSize),
		stats:                     newVerifierStats(),
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

func (b *backend) AddMessage(unsignedMessage *avalancheWarp.UnsignedMessage) error {
	messageID := unsignedMessage.ID()
	log.Debug("Adding warp message to backend", "messageID", messageID)

	// In the case when a node restarts, and possibly changes its bls key, the cache gets emptied but the database does not.
	// So to avoid having incorrect signatures saved in the database after a bls key change, we save the full message in the database.
	// Whereas for the cache, after the node restart, the cache would be emptied so we can directly save the signatures.
	if err := b.db.Put(messageID[:], unsignedMessage.Bytes()); err != nil {
		return fmt.Errorf("failed to put warp signature in db: %w", err)
	}

	if _, err := b.signMessage(unsignedMessage); err != nil {
		return fmt.Errorf("failed to sign warp message: %w", err)
	}
	return nil
}

func (b *backend) GetMessageSignature(ctx context.Context, unsignedMessage *avalancheWarp.UnsignedMessage) ([]byte, error) {
	messageID := unsignedMessage.ID()

	log.Debug("Getting warp message from backend", "messageID", messageID)
	if sig, ok := b.signatureCache.Get(messageID); ok {
		return sig, nil
	}

	if err := b.Verify(ctx, unsignedMessage, nil); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVerifyWarpMessage, err)
	}
	return b.signMessage(unsignedMessage)
}

func (b *backend) GetBlockSignature(ctx context.Context, blockID ids.ID) ([]byte, error) {
	log.Debug("Getting block from backend", "blockID", blockID)

	blockHashPayload, err := payload.NewHash(blockID)
	if err != nil {
		return nil, fmt.Errorf("failed to create new block hash payload: %w", err)
	}

	unsignedMessage, err := avalancheWarp.NewUnsignedMessage(b.networkID, b.sourceChainID, blockHashPayload.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to create new unsigned warp message: %w", err)
	}

	if sig, ok := b.signatureCache.Get(unsignedMessage.ID()); ok {
		return sig, nil
	}

	if err := b.verifyBlockMessage(ctx, blockHashPayload); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrValidateBlock, err)
	}

	sig, err := b.signMessage(unsignedMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to sign block message: %w", err)
	}
	return sig, nil
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
		return nil, err
	}

	unsignedMessage, err := avalancheWarp.ParseUnsignedMessage(unsignedMessageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse unsigned message %s: %w", messageID.String(), err)
	}
	b.messageCache.Put(messageID, unsignedMessage)

	return unsignedMessage, nil
}

func (b *backend) signMessage(unsignedMessage *avalancheWarp.UnsignedMessage) ([]byte, error) {
	sig, err := b.warpSigner.Sign(unsignedMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to sign warp message: %w", err)
	}

	b.signatureCache.Put(unsignedMessage.ID(), sig)
	return sig, nil
}
