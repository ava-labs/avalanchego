// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"reflect"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

// CodecVersion is the current default codec version
const CodecVersion = txs.CodecVersion

var _ Parser = (*parser)(nil)

type Parser interface {
	txs.Parser

	ParseBlock(bytes []byte) (Block, error)
	ParseGenesisBlock(bytes []byte) (Block, error)
}

type parser struct {
	txs.Parser
}

func NewParser(fxs []fxs.Fx) (Parser, error) {
	p, err := txs.NewParser(fxs)
	if err != nil {
		return nil, err
	}
	c := p.CodecRegistry()
	gc := p.GenesisCodecRegistry()

	err = errors.Join(
		c.RegisterType(&StandardBlock{}),
		gc.RegisterType(&StandardBlock{}),
	)
	return &parser{
		Parser: p,
	}, err
}

func NewCustomParser(
	typeToFxIndex map[reflect.Type]int,
	clock *mockable.Clock,
	log logging.Logger,
	fxs []fxs.Fx,
) (Parser, error) {
	p, err := txs.NewCustomParser(typeToFxIndex, clock, log, fxs)
	if err != nil {
		return nil, err
	}
	c := p.CodecRegistry()
	gc := p.GenesisCodecRegistry()

	err = errors.Join(
		c.RegisterType(&StandardBlock{}),
		gc.RegisterType(&StandardBlock{}),
	)
	return &parser{
		Parser: p,
	}, err
}

func (p *parser) ParseBlock(bytes []byte) (Block, error) {
	return parse(p.Codec(), bytes)
}

func (p *parser) ParseGenesisBlock(bytes []byte) (Block, error) {
	return parse(p.GenesisCodec(), bytes)
}

func parse(cm codec.Manager, bytes []byte) (Block, error) {
	var blk Block
	if _, err := cm.Unmarshal(bytes, &blk); err != nil {
		return nil, err
	}
	return blk, blk.initialize(bytes, cm)
}
