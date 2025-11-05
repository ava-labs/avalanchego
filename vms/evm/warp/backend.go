// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/metrics"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const (
	ParseErrCode = iota + 1
	VerifyErrCode
)

var (
	_ Backend         = (*backend)(nil)
	_ acp118.Verifier = (*backend)(nil)
	_ p2p.Handler     = (*Handler)(nil)

	messageCacheSize = 500

	errParsingOffChainMessage = errors.New("failed to parse off-chain message")
	ErrValidateBlock          = errors.New("failed to validate block message")
	ErrVerifyWarpMessage      = errors.New("failed to verify warp message")
)

// BlockStore provides access to accepted blocks.
type BlockStore interface {
	GetBlock(ctx context.Context, blockID ids.ID) (snowman.Block, error)
}

// DB stores and retrieves warp messages.
type DB struct {
	networkID                 uint32
	sourceChainID             ids.ID
	db                        database.Database
	messageCache              *lru.Cache[ids.ID, *warp.UnsignedMessage]
	offchainAddressedCallMsgs map[ids.ID]*warp.UnsignedMessage
}

// Add stores a warp message in the database and cache.
func (d *DB) Add(unsignedMsg *warp.UnsignedMessage) error {
	msgID := unsignedMsg.ID()
	log.Debug("Adding warp message to backend", "messageID", msgID)

	// In the case when a node restarts, and possibly changes its bls key, the cache gets emptied but the database does not.
	// So to avoid having incorrect signatures saved in the database after a bls key change, we save the full message in the database.
	// Whereas for the cache, after the node restart, the cache would be emptied so we can directly save the signatures.
	if err := d.db.Put(msgID[:], unsignedMsg.Bytes()); err != nil {
		return fmt.Errorf("failed to put warp message in db: %w", err)
	}

	return nil
}

// Get retrieves a warp message from cache, offchain messages, or database.
func (d *DB) Get(msgID ids.ID) (*warp.UnsignedMessage, error) {
	if msg, ok := d.messageCache.Get(msgID); ok {
		return msg, nil
	}
	if msg, ok := d.offchainAddressedCallMsgs[msgID]; ok {
		return msg, nil
	}

	unsignedMessageBytes, err := d.db.Get(msgID[:])
	if err != nil {
		return nil, err
	}

	unsignedMessage, err := warp.ParseUnsignedMessage(unsignedMessageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse unsigned message %s: %w", msgID.String(), err)
	}
	d.messageCache.Put(msgID, unsignedMessage)

	return unsignedMessage, nil
}

// Signer signs warp messages and caches the signatures.
type Signer struct {
	warpSigner     warp.Signer
	verifier       acp118.Verifier
	signatureCache cache.Cacher[ids.ID, []byte]
}

// Sign verifies the warp message, signs it, and caches the signature.
func (s *Signer) Sign(ctx context.Context, msg *warp.UnsignedMessage) ([]byte, error) {
	// Check cache first
	msgID := msg.ID()
	if sig, ok := s.signatureCache.Get(msgID); ok {
		return sig, nil
	}

	if err := s.verifier.Verify(ctx, msg, nil); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVerifyWarpMessage, err)
	}

	sig, err := s.warpSigner.Sign(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to sign warp message: %w", err)
	}

	s.signatureCache.Put(msgID, sig)
	return sig, nil
}

// Handler implements p2p.Handler and handles warp signature requests.
// It hides the acp118.Verifier implementation as an implementation detail.
type Handler struct {
	*acp118.Handler
}

// NewHandler creates a new p2p warp signature request handler.
func NewHandler(
	signatureCache cache.Cacher[ids.ID, []byte],
	verifier acp118.Verifier,
	signer warp.Signer,
) p2p.Handler {
	return &Handler{
		Handler: acp118.NewCachedHandler(signatureCache, verifier, signer),
	}
}

// Backend tracks signature-eligible warp messages and provides an interface to fetch them.
// The backend is used by the warp API service to retrieve messages.
type Backend interface {
	AddMessage(ctx context.Context, unsignedMessage *warp.UnsignedMessage) error
	GetMessage(messageHash ids.ID) (*warp.UnsignedMessage, error)
}

// backend implements Backend and keeps track of warp messages.
type backend struct {
	db            *DB
	signer        *Signer
	blockClient   BlockStore
	uptimeTracker *uptimetracker.UptimeTracker
	networkID     uint32
	sourceChainID ids.ID

	// Metrics
	messageParseFail            metrics.Counter
	addressedCallValidationFail metrics.Counter
	blockValidationFail         metrics.Counter
	uptimeValidationFail        metrics.Counter
}

// NewBackend creates a new Backend, and initializes the signature cache and message tracking database.
func NewBackend(
	networkID uint32,
	sourceChainID ids.ID,
	warpSigner warp.Signer,
	blockClient BlockStore,
	uptimeTracker *uptimetracker.UptimeTracker,
	db database.Database,
	signatureCache cache.Cacher[ids.ID, []byte],
	offchainMessages [][]byte,
) (Backend, *Signer, p2p.Handler, error) {
	messageDB := &DB{
		networkID:                 networkID,
		sourceChainID:             sourceChainID,
		db:                        db,
		messageCache:              lru.NewCache[ids.ID, *warp.UnsignedMessage](messageCacheSize),
		offchainAddressedCallMsgs: make(map[ids.ID]*warp.UnsignedMessage),
	}

	if err := initOffChainMessages(messageDB, networkID, sourceChainID, offchainMessages); err != nil {
		return nil, nil, nil, err
	}

	b := &backend{
		db:                          messageDB,
		blockClient:                 blockClient,
		uptimeTracker:               uptimeTracker,
		networkID:                   networkID,
		sourceChainID:               sourceChainID,
		messageParseFail:            metrics.NewRegisteredCounter("warp_backend_message_parse_fail", nil),
		addressedCallValidationFail: metrics.NewRegisteredCounter("warp_backend_addressed_call_validation_fail", nil),
		blockValidationFail:         metrics.NewRegisteredCounter("warp_backend_block_validation_fail", nil),
		uptimeValidationFail:        metrics.NewRegisteredCounter("warp_backend_uptime_validation_fail", nil),
	}

	signer := &Signer{
		warpSigner:     warpSigner,
		verifier:       b,
		signatureCache: signatureCache,
	}
	b.signer = signer

	handler := NewHandler(signatureCache, b, warpSigner)

	return b, signer, handler, nil
}

func (b *backend) AddMessage(ctx context.Context, unsignedMessage *warp.UnsignedMessage) error {
	if err := b.db.Add(unsignedMessage); err != nil {
		return err
	}

	// Fill the signature cache now so subsequent requests can serve the
	// signature without repeating verification or signing work.
	if _, err := b.signer.Sign(ctx, unsignedMessage); err != nil {
		return err
	}
	return nil
}

func (b *backend) GetMessage(messageHash ids.ID) (*warp.UnsignedMessage, error) {
	return b.db.Get(messageHash)
}

// Verify implements acp118.Verifier and validates whether a warp message should be signed.
func (b *backend) Verify(ctx context.Context, unsignedMessage *warp.UnsignedMessage, _ []byte) *common.AppError {
	messageID := unsignedMessage.ID()
	// Known on-chain messages should be signed
	if _, err := b.db.Get(messageID); err == nil {
		return nil
	} else if err != database.ErrNotFound {
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("failed to get message %s: %s", messageID, err.Error()),
		}
	}

	parsed, err := payload.Parse(unsignedMessage.Payload)
	if err != nil {
		b.messageParseFail.Inc(1)
		return &common.AppError{
			Code:    ParseErrCode,
			Message: "failed to parse payload: " + err.Error(),
		}
	}

	switch p := parsed.(type) {
	case *payload.AddressedCall:
		return b.verifyOffchainAddressedCall(p)
	case *payload.Hash:
		return b.verifyBlockMessage(ctx, p)
	default:
		b.messageParseFail.Inc(1)
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("unknown payload type: %T", p),
		}
	}
}

// verifyBlockMessage returns nil if blockHashPayload contains the ID
// of an accepted block indicating it should be signed by the VM.
func (b *backend) verifyBlockMessage(ctx context.Context, blockHashPayload *payload.Hash) *common.AppError {
	blockID := blockHashPayload.Hash
	_, err := b.blockClient.GetBlock(ctx, blockID)
	if err != nil {
		b.blockValidationFail.Inc(1)
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("failed to get block %s: %s", blockID, err.Error()),
		}
	}

	return nil
}

// verifyOffchainAddressedCall verifies the addressed call message
func (b *backend) verifyOffchainAddressedCall(addressedCall *payload.AddressedCall) *common.AppError {
	// Further, parse the payload to see if it is a known type.
	parsed, err := message.Parse(addressedCall.Payload)
	if err != nil {
		b.messageParseFail.Inc(1)
		return &common.AppError{
			Code:    ParseErrCode,
			Message: "failed to parse addressed call message: " + err.Error(),
		}
	}

	if len(addressedCall.SourceAddress) != 0 {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: "source address should be empty for offchain addressed messages",
		}
	}

	switch p := parsed.(type) {
	case *message.ValidatorUptime:
		if err := b.verifyUptimeMessage(p); err != nil {
			b.uptimeValidationFail.Inc(1)
			return err
		}
	default:
		b.messageParseFail.Inc(1)
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("unknown message type: %T", p),
		}
	}

	return nil
}

func (b *backend) verifyUptimeMessage(uptimeMsg *message.ValidatorUptime) *common.AppError {
	currentUptime, _, err := b.uptimeTracker.GetUptime(uptimeMsg.ValidationID)
	if err != nil {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: "failed to get uptime: " + err.Error(),
		}
	}

	currentUptimeSeconds := uint64(currentUptime.Seconds())
	// verify the current uptime against the total uptime in the message
	if currentUptimeSeconds < uptimeMsg.TotalUptime {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("current uptime %d is less than queried uptime %d for validationID %s", currentUptimeSeconds, uptimeMsg.TotalUptime, uptimeMsg.ValidationID),
		}
	}

	return nil
}

func initOffChainMessages(db *DB, networkID uint32, sourceChainID ids.ID, offchainMessages [][]byte) error {
	for i, offchainMsg := range offchainMessages {
		unsignedMsg, err := warp.ParseUnsignedMessage(offchainMsg)
		if err != nil {
			return fmt.Errorf("%w at index %d: %w", errParsingOffChainMessage, i, err)
		}

		if unsignedMsg.NetworkID != networkID {
			return fmt.Errorf("%w at index %d", warp.ErrWrongNetworkID, i)
		}

		if unsignedMsg.SourceChainID != sourceChainID {
			return fmt.Errorf("%w at index %d", warp.ErrWrongSourceChainID, i)
		}

		_, err = payload.ParseAddressedCall(unsignedMsg.Payload)
		if err != nil {
			return fmt.Errorf("%w at index %d as AddressedCall: %w", errParsingOffChainMessage, i, err)
		}
		db.offchainAddressedCallMsgs[unsignedMsg.ID()] = unsignedMsg
	}

	return nil
}
