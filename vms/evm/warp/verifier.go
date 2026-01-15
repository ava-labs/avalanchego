// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const (
	ParseErrCode = iota + 1
	VerifyErrCode
)

// VerifyHandler defines how to handle different an unknown [payload.Payload]
// that can be signed.
//
// TODO: separate [payload.AddressedCall] and [payload.Hash] into different p2p
// protocols to deprecate this interface.
type VerifyHandler interface {
	VerifyBlockHash(ctx context.Context, p *payload.Hash) *common.AppError
	VerifyUnknown(
		p payload.Payload,
		messageParseFail prometheus.Counter,
	) *common.AppError
}

// VM provides access to accepted blocks.
type VM interface {
	HasBlock(ctx context.Context, blockID ids.ID) error
}

// TODO naming
var _ VerifyHandler = (*BlockVerifier)(nil)

type BlockVerifier struct {
	blockClient     VM
	blockVerifyFail prometheus.Counter
}

func NewBlockVerifier(vm VM, r prometheus.Registerer) (*BlockVerifier, error) {
	b := &BlockVerifier{
		blockClient: vm,
		blockVerifyFail: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "warp_backend_block_verify_fail",
			Help: "Number of block verification failures",
		}),
	}

	if err := r.Register(b.blockVerifyFail); err != nil {
		return nil, err
	}

	return b, nil
}

func (b *BlockVerifier) VerifyBlockHash(
	ctx context.Context,
	hash *payload.Hash,
) *common.AppError {
	if err := b.blockClient.HasBlock(ctx, hash.Hash); err != nil {
		b.blockVerifyFail.Inc()
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("failed to get block %s: %s", hash.Hash, err),
		}
	}

	return nil
}

func (b *BlockVerifier) VerifyUnknown(
	p payload.Payload,
	messageParseFail prometheus.Counter,
) *common.AppError {
	messageParseFail.Inc()
	return &common.AppError{
		Code:    ParseErrCode,
		Message: fmt.Sprintf("unknown payload type: %T", p),
	}
}

// Verifier validates whether a warp message should be signed.
type Verifier struct {
	handler VerifyHandler
	db            *DB
	uptimeTracker *uptimetracker.UptimeTracker
	messageParseFail        prometheus.Counter
}

// NewVerifier creates a new warp message verifier.
func NewVerifier(
	handler VerifyHandler,
	db *DB,
	r prometheus.Registerer,
) (*Verifier, error) {
	v := &Verifier{
		handler: handler,
		db:            db,
		messageParseFail: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "warp_backend_message_parse_fail",
			Help: "Number of warp message parse failures",
		}),
	}

	if err := r.Register(v.messageParseFail); err != nil {
		return nil, err
	}

	return v, nil
}

// Verify validates whether a warp message should be signed.
func (v *Verifier) Verify(ctx context.Context, unsignedMessage *warp.UnsignedMessage) *common.AppError {
	messageID := unsignedMessage.ID()
	// Known on-chain messages should be signed
	if _, err := v.db.Get(messageID); err == nil {
		return nil
	} else if !errors.Is(err, database.ErrNotFound) {
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("failed to get message %s: %s", messageID, err),
		}
	}

	parsed, err := payload.Parse(unsignedMessage.Payload)
	if err != nil {
		v.messageParseFail.Inc()
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("failed to parse payload: %s", err),
		}
	}

	switch p := parsed.(type) {
	case *payload.Hash:
		return v.handler.VerifyBlockHash(ctx, p)
	default:
		return v.handler.VerifyUnknown(p, v.messageParseFail)
	}
}

var _ acp118.Verifier = (*acp118Handler)(nil)

// acp118Handler supports signing warp messages requested by peers.
type acp118Handler struct {
	verifier *Verifier
}

func (a *acp118Handler) Verify(ctx context.Context, message *warp.UnsignedMessage, _ []byte) *common.AppError {
	return a.verifier.Verify(ctx, message)
}

// NewHandler returns a handler for signing warp messages requested by peers.
func NewHandler(
	signatureCache cache.Cacher[ids.ID, []byte],
	verifier *Verifier,
	signer warp.Signer,
) *acp118.Handler {
	return acp118.NewCachedHandler(
		signatureCache,
		&acp118Handler{verifier: verifier},
		signer,
	)
}
