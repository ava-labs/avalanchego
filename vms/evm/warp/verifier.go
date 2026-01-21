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
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var errVerify = errors.New("failed verification")

// VerifyHandler defines how to handle different an unknown [payload.Payload]
// that can be signed.
//
// TODO: separate [payload.AddressedCall] and [payload.Hash] into different p2p
// protocols to deprecate this interface.
type VerifyHandler interface {
	// Verify returns a non-nil [common.AppError] if we do not want to sign `p`.
	// `p` is guaranteed to not be of type [payload.Hash].
	Verify(
		p payload.Payload,
		messageParseFail prometheus.Counter,
	) error
}

// NoVerifier does not verify any message
type NoVerifier struct{}

func (NoVerifier) Verify(
	p payload.Payload,
	messageParseFail prometheus.Counter,
) error {
	messageParseFail.Inc()
	return fmt.Errorf("%w: unknown message type", errVerify)
}

// VM provides access to accepted blocks.
type VM interface {
	// HasBlock returns a non-nil error if `blkID` is accepted.
	HasBlock(ctx context.Context, blkID ids.ID) error
}

// Verifier validates whether a warp message should be signed.
type Verifier struct {
	vm               VM
	handler          VerifyHandler
	db               *DB
	offChainMsgs     OffChainMessages
	messageParseFail prometheus.Counter
	blockVerifyFail  prometheus.Counter
}

// NewVerifier creates a new warp message verifier.
func NewVerifier(
	handler VerifyHandler,
	vm VM,
	db *DB,
	offChainMsgs OffChainMessages,
	r prometheus.Registerer,
) (*Verifier, error) {
	messageParseFail := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "warp_backend_message_parse_fail",
		Help: "Number of warp message parse failures",
	})

	blockVerifyFail := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "warp_backend_block_verify_fail",
		Help: "Number of block verification failures",
	})

	if err := r.Register(blockVerifyFail); err != nil {
		return nil, err
	}

	if err := r.Register(messageParseFail); err != nil {
		return nil, err
	}

	return &Verifier{
		vm:               vm,
		handler:          handler,
		db:               db,
		offChainMsgs: offChainMsgs,
		messageParseFail: messageParseFail,
		blockVerifyFail:  blockVerifyFail,
	}, nil
}

// Verify validates whether a warp message should be signed.
func (v *Verifier) Verify(
	ctx context.Context,
	msg *warp.UnsignedMessage,
) error {
	_, ok := v.offChainMsgs.Get(msg.ID())
	if ok {
		return nil
	}

	if _, err := v.db.Get(msg.ID()); err == nil {
		return nil
	} else if !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("failed to get message %s: %s", msg.ID(), err)
	}

	parsed, err := payload.Parse(msg.Payload)
	if err != nil {
		v.messageParseFail.Inc()
		return fmt.Errorf("%w: failed to parse payload: %w", errVerify, err)
	}

	hash, ok := parsed.(*payload.Hash)
	if !ok {
		return v.handler.Verify(parsed, v.messageParseFail)
	}

	if err := v.vm.HasBlock(ctx, hash.Hash); err != nil {
		v.blockVerifyFail.Inc()
		return fmt.Errorf("%w: failed to get block %s: %w", errVerify, hash.Hash, err)
	}

	return nil
}

// acp118Handler supports signing warp messages requested by peers.
type acp118Handler struct {
	verifier *Verifier
}

var _ acp118.Verifier = (*acp118Handler)(nil)

func (a *acp118Handler) Verify(
	ctx context.Context,
	message *warp.UnsignedMessage,
	_ []byte,
) *common.AppError {
	if err := a.verifier.Verify(ctx, message); err != nil {
		return &common.AppError{
			Message: err.Error(),
		}
	}

	return nil
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
