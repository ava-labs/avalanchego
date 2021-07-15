// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/proposervm/option"
)

const (
	optionCacheSize = 4096
)

var (
	errOptionWrongVersion = errors.New("wrong version")

	_ OptionState = &optionState{}
)

type OptionState interface {
	GetOption(blkID ids.ID) (option.Option, choices.Status, error)
	PutOption(opt option.Option, status choices.Status) error
	DeleteOption(blkID ids.ID) error

	WipeCache() // useful for UTs
}

type optionState struct {
	// Caches OptionID -> Option. If the Option is nil, that means the Option is not
	// in storage.
	optCache cache.Cacher

	db database.Database
}

func (s *optionState) WipeCache() {
	s.optCache.Flush()
}

type optionWrapper struct {
	Option []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`

	option option.Option
}

func NewOptionState(db database.Database) OptionState {
	return &optionState{
		optCache: &cache.LRU{Size: optionCacheSize},
		db:       db,
	}
}

func NewMeteredOptionState(db database.Database, namespace string, metrics prometheus.Registerer) (OptionState, error) {
	optCache, err := metercacher.New(
		fmt.Sprintf("%s_option_cache", namespace),
		metrics,
		&cache.LRU{Size: optionCacheSize},
	)

	return &optionState{
		optCache: optCache,
		db:       db,
	}, err
}

func (s *optionState) GetOption(optID ids.ID) (option.Option, choices.Status, error) {
	if optIntf, found := s.optCache.Get(optID); found {
		if optIntf == nil {
			return nil, choices.Unknown, database.ErrNotFound
		}
		opt, ok := optIntf.(*optionWrapper)
		if !ok {
			return nil, choices.Unknown, database.ErrNotFound
		}
		return opt.option, opt.Status, nil
	}

	optWrapperBytes, err := s.db.Get(optID[:])
	if err == database.ErrNotFound {
		s.optCache.Put(optID, nil)
		return nil, choices.Unknown, database.ErrNotFound
	}
	if err != nil {
		return nil, choices.Unknown, err
	}

	optWrapper := optionWrapper{}
	parsedVersion, err := c.Unmarshal(optWrapperBytes, &optWrapper)
	if err != nil {
		return nil, choices.Unknown, err
	}
	if parsedVersion != version {
		return nil, choices.Unknown, errOptionWrongVersion
	}

	// The key was in the database
	opt, err := option.Parse(optWrapper.Option)
	if err != nil {
		return nil, choices.Unknown, err
	}
	optWrapper.option = opt

	s.optCache.Put(optID, &optWrapper)
	return opt, optWrapper.Status, nil
}

func (s *optionState) PutOption(opt option.Option, status choices.Status) error {
	optWrapper := optionWrapper{
		Option: opt.Bytes(),
		Status: status,
		option: opt,
	}

	bytes, err := c.Marshal(version, &optWrapper)
	if err != nil {
		return err
	}

	optID := opt.ID()
	s.optCache.Put(optID, &optWrapper)
	return s.db.Put(optID[:], bytes)
}

func (s *optionState) DeleteOption(optID ids.ID) error {
	s.optCache.Put(optID, nil)
	return s.db.Delete(optID[:])
}
