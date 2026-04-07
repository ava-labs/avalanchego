// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package backend

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const (
	dbPrefix           = "warp"
	signatureCacheSize = 500
)

var (
	_                         precompileconfig.WarpMessageWriter = (*Backend)(nil)
	errParsingOffChainMessage                                    = errors.New("failed to parse off-chain message")

	messageCacheSize = 500
)

type BlockClient interface {
	IsAccepted(ctx context.Context, blockID ids.ID) error
}

// Backend implements Backend, keeps track of warp messages, and generates message signatures.
type Backend struct {
	networkID                 uint32
	sourceChainID             ids.ID
	db                        database.Database
	warpSigner                avalancheWarp.Signer
	blockClient               BlockClient
	signatureCache            cache.Cacher[ids.ID, []byte]
	messageCache              *lru.Cache[ids.ID, *avalancheWarp.UnsignedMessage]
	offchainAddressedCallMsgs map[ids.ID]*avalancheWarp.UnsignedMessage
	stats                     *verifierStats
}

// New creates a new Backend with a prefixed database and signature cache.
// It returns the backend and the signature cache (for use with
// acp118.NewCachedHandler).
func New(
	networkID uint32,
	sourceChainID ids.ID,
	warpSigner avalancheWarp.Signer,
	blockClient BlockClient,
	parentDB database.Database,
	offchainMessages [][]byte,
) (*Backend, cache.Cacher[ids.ID, []byte], error) {
	signatureCache := lru.NewCache[ids.ID, []byte](signatureCacheSize)
	b := &Backend{
		networkID:                 networkID,
		sourceChainID:             sourceChainID,
		db:                        prefixdb.New([]byte(dbPrefix), parentDB),
		warpSigner:                warpSigner,
		blockClient:               blockClient,
		signatureCache:            signatureCache,
		messageCache:              lru.NewCache[ids.ID, *avalancheWarp.UnsignedMessage](messageCacheSize),
		stats:                     newVerifierStats(),
		offchainAddressedCallMsgs: make(map[ids.ID]*avalancheWarp.UnsignedMessage),
	}
	return b, signatureCache, b.initOffChainMessages(offchainMessages)
}

func (b *Backend) initOffChainMessages(offchainMessages [][]byte) error {
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

func (b *Backend) AddMessage(unsignedMessage *avalancheWarp.UnsignedMessage) error {
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

func (b *Backend) getMessage(messageID ids.ID) (*avalancheWarp.UnsignedMessage, error) {
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

func (b *Backend) signMessage(unsignedMessage *avalancheWarp.UnsignedMessage) ([]byte, error) {
	sig, err := b.warpSigner.Sign(unsignedMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to sign warp message: %w", err)
	}

	b.signatureCache.Put(unsignedMessage.ID(), sig)
	return sig, nil
}
