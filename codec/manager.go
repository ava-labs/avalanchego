// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	VersionSize = wrappers.ShortLen

	// default max size, in bytes, of something being marshaled by Marshal()
	defaultMaxSize = 256 * units.KiB

	// initial capacity of byte slice that values are marshaled into.
	// Larger value --> need less memory allocations but possibly have allocated but unused memory
	// Smaller value --> need more memory allocations but more efficient use of allocated memory
	initialSliceCap = 128
)

var (
	ErrUnknownVersion    = errors.New("unknown codec version")
	ErrMarshalNil        = errors.New("can't marshal nil pointer or interface")
	ErrUnmarshalNil      = errors.New("can't unmarshal nil")
	ErrUnmarshalTooBig   = errors.New("byte array exceeds maximum length")
	ErrCantPackVersion   = errors.New("couldn't pack codec version")
	ErrCantUnpackVersion = errors.New("couldn't unpack codec version")
	ErrDuplicatedVersion = errors.New("duplicated codec version")
	ErrExtraSpace        = errors.New("trailing buffer space")
)

var _ Manager = (*manager)(nil)

// Manager describes the functionality for managing codec versions.
type Manager interface {
	// Associate the given codec with the given version ID
	RegisterCodec(version uint16, codec Codec) error

	// Size returns the size, in bytes, of [value] when it's marshaled
	// using the codec with the given version.
	// RegisterCodec must have been called with that version.
	// If [value] is nil, returns [ErrMarshalNil]
	Size(version uint16, value interface{}) (int, error)

	// Marshal the given value using the codec with the given version.
	// RegisterCodec must have been called with that version.
	Marshal(version uint16, source interface{}) (destination []byte, err error)

	// Unmarshal the given bytes into the given destination. [destination] must
	// be a pointer or an interface. Returns the version of the codec that
	// produces the given bytes.
	Unmarshal(source []byte, destination interface{}) (version uint16, err error)
}

// NewManager returns a new codec manager.
func NewManager(maxSize int) Manager {
	return &manager{
		maxSize: maxSize,
		codecs:  map[uint16]Codec{},
	}
}

// NewDefaultManager returns a new codec manager.
func NewDefaultManager() Manager {
	return NewManager(defaultMaxSize)
}

type manager struct {
	lock    sync.RWMutex
	maxSize int
	codecs  map[uint16]Codec
}

// RegisterCodec is used to register a new codec version that can be used to
// (un)marshal with.
func (m *manager) RegisterCodec(version uint16, codec Codec) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, exists := m.codecs[version]; exists {
		return ErrDuplicatedVersion
	}
	m.codecs[version] = codec
	return nil
}

func (m *manager) Size(version uint16, value interface{}) (int, error) {
	if value == nil {
		return 0, ErrMarshalNil // can't marshal nil
	}

	m.lock.RLock()
	c, exists := m.codecs[version]
	m.lock.RUnlock()

	if !exists {
		return 0, ErrUnknownVersion
	}

	res, err := c.Size(value)

	// Add [VersionSize] for the codec version
	return VersionSize + res, err
}

// To marshal an interface, [value] must be a pointer to the interface.
func (m *manager) Marshal(version uint16, value interface{}) ([]byte, error) {
	if value == nil {
		return nil, ErrMarshalNil // can't marshal nil
	}

	m.lock.RLock()
	c, exists := m.codecs[version]
	m.lock.RUnlock()
	if !exists {
		return nil, ErrUnknownVersion
	}

	p := wrappers.Packer{
		MaxSize: m.maxSize,
		Bytes:   make([]byte, 0, initialSliceCap),
	}
	p.PackShort(version)
	if p.Errored() {
		return nil, ErrCantPackVersion // Should never happen
	}
	return p.Bytes, c.MarshalInto(value, &p)
}

// Unmarshal unmarshals [bytes] into [dest], where [dest] must be a pointer or
// interface.
func (m *manager) Unmarshal(bytes []byte, dest interface{}) (uint16, error) {
	if dest == nil {
		return 0, ErrUnmarshalNil
	}

	if byteLen := len(bytes); byteLen > m.maxSize {
		return 0, fmt.Errorf("%w: %d > %d", ErrUnmarshalTooBig, byteLen, m.maxSize)
	}

	p := wrappers.Packer{
		Bytes: bytes,
	}
	version := p.UnpackShort()
	if p.Errored() { // Make sure the codec version is correct
		return 0, ErrCantUnpackVersion
	}

	m.lock.RLock()
	c, exists := m.codecs[version]
	m.lock.RUnlock()
	if !exists {
		return version, ErrUnknownVersion
	}

	if err := c.UnmarshalFrom(&p, dest); err != nil {
		return version, err
	}
	if p.Offset != len(bytes) {
		return version, fmt.Errorf("%w: read %d provided %d",
			ErrExtraSpace,
			p.Offset,
			len(bytes),
		)
	}

	return version, nil
}
