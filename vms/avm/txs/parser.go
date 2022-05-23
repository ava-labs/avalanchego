// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"fmt"
	"math"
	"reflect"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
)

const codecVersion = 0

var _ Parser = &parser{}

type Parser interface {
	Codec() codec.Manager
	GenesisCodec() codec.Manager

	Parse(bytes []byte) (*Tx, error)
	ParseGenesis(bytes []byte) (*Tx, error)
}

type parser struct {
	cm  codec.Manager
	gcm codec.Manager
}

func NewParser(fxs []fxs.Fx) (Parser, error) {
	return NewCustomParser(
		make(map[reflect.Type]int),
		&mockable.Clock{},
		logging.NoLog{},
		fxs,
	)
}

func NewCustomParser(
	typeToFxIndex map[reflect.Type]int,
	clock *mockable.Clock,
	log logging.Logger,
	fxs []fxs.Fx,
) (Parser, error) {
	gc := linearcodec.New([]string{reflectcodec.DefaultTagName}, 1<<20)
	c := linearcodec.NewDefault()

	gcm := codec.NewManager(math.MaxInt32)
	cm := codec.NewDefaultManager()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&BaseTx{}),
		c.RegisterType(&CreateAssetTx{}),
		c.RegisterType(&OperationTx{}),
		c.RegisterType(&ImportTx{}),
		c.RegisterType(&ExportTx{}),
		cm.RegisterCodec(codecVersion, c),

		gc.RegisterType(&BaseTx{}),
		gc.RegisterType(&CreateAssetTx{}),
		gc.RegisterType(&OperationTx{}),
		gc.RegisterType(&ImportTx{}),
		gc.RegisterType(&ExportTx{}),
		gcm.RegisterCodec(codecVersion, gc),
	)
	if errs.Errored() {
		return nil, errs.Err
	}

	vm := &fxVM{
		typeToFxIndex: typeToFxIndex,
		clock:         clock,
		log:           log,
	}
	for i, fx := range fxs {
		vm.codecRegistry = &codecRegistry{
			codecs:      []codec.Registry{gc, c},
			index:       i,
			typeToIndex: vm.typeToFxIndex,
		}
		if err := fx.Initialize(vm); err != nil {
			return nil, err
		}
	}
	return &parser{
		cm:  cm,
		gcm: gcm,
	}, nil
}

func (p *parser) Codec() codec.Manager                   { return p.cm }
func (p *parser) GenesisCodec() codec.Manager            { return p.gcm }
func (p *parser) Parse(bytes []byte) (*Tx, error)        { return parse(p.cm, bytes) }
func (p *parser) ParseGenesis(bytes []byte) (*Tx, error) { return parse(p.gcm, bytes) }

func parse(cm codec.Manager, bytes []byte) (*Tx, error) {
	tx := &Tx{}
	parsedVersion, err := cm.Unmarshal(bytes, tx)
	if err != nil {
		return nil, err
	}
	if parsedVersion != codecVersion {
		return nil, fmt.Errorf("expected codec version %d but got %d", codecVersion, parsedVersion)
	}

	unsignedBytes, err := cm.Marshal(codecVersion, tx)
	if err != nil {
		return nil, err
	}
	tx.UnsignedTx.Initialize(unsignedBytes, bytes)

	return tx, nil
}
