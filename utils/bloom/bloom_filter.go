package bloom

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/willf/bitset"

	btcsuiteWire "github.com/btcsuite/btcd/wire"
	btcsuite "github.com/btcsuite/btcutil/bloom"
	"github.com/spaolacci/murmur3"
	streakKnife "github.com/steakknife/bloomfilter"
	willfBloom "github.com/willf/bloom"
)

type FilterType int

var FilterTypeSteakKnife FilterType = 1
var FilterTypeWillf FilterType = 2
var FilterTypeBtcsuite FilterType = 3
var FilterTypeDefault = FilterTypeSteakKnife

var (
	ErrMaxBytes = fmt.Errorf("too large")
)

type Filter interface {
	// Add adds to filter, assumed thread safe
	Add(...[]byte)
	// Check checks filter, assumed thread safe
	Check([]byte) bool
	MarshalJSON() ([]byte, error)
}

func New(maxN uint64, p float64, maxBytes uint64) (Filter, error) {
	switch FilterTypeDefault {
	case FilterTypeSteakKnife:
		neededBytes := BytesSteakKnifeFilter(maxN, p)
		if neededBytes > maxBytes {
			return nil, ErrMaxBytes
		}
		return NewSteakKnifeFilter(maxN, p)
	case FilterTypeBtcsuite:
		neededBytes := BytesBtcsuiteFilter(maxN, p)
		if neededBytes > maxBytes {
			return nil, ErrMaxBytes
		}
		return NewBtcsuiteFilter(maxN, p)
	}
	neededBytes := BytesWillfFilter(maxN, p)
	if neededBytes > maxBytes {
		return nil, ErrMaxBytes
	}
	return NewWillfFilter(maxN, p)
}

type willfFilter struct {
	lock    sync.RWMutex
	bfilter *willfBloom.BloomFilter
}

func BytesWillfFilter(maxN uint64, p float64) uint64 {
	// this is pulled from bitset.
	// the calculation is the size of the bitset which would be created from this filter.
	// to ensure we don't crash memory, we would ensure the size
	m := uint(streakKnife.OptimalM(maxN, p))
	// 8 == sizeof(uint64))
	return uint64(WordsNeeded(m)) * 8
}

func NewWillfFilter(maxN uint64, p float64) (Filter, error) {
	m := uint(streakKnife.OptimalM(maxN, p))
	k := uint(streakKnife.OptimalK(uint64(m), maxN))
	return &willfFilter{bfilter: willfBloom.New(m, k)}, nil
}

func (f *willfFilter) Add(bl ...[]byte) {
	f.lock.Lock()
	defer f.lock.Unlock()
	for _, b := range bl {
		f.bfilter.Add(b)
	}
}

func (f *willfFilter) Check(b []byte) bool {
	f.lock.RLock()
	defer f.lock.RUnlock()
	return f.bfilter.Test(b)
}

func (f *willfFilter) MarshalJSON() ([]byte, error) {
	return f.bfilter.MarshalJSON()
}

type steakKnifeFilter struct {
	lock    sync.RWMutex
	bfilter *streakKnife.Filter
}

func BytesSteakKnifeFilter(maxN uint64, p float64) uint64 {
	m := streakKnife.OptimalM(maxN, p)
	k := streakKnife.OptimalK(m, maxN)

	// this is pulled from bloomFilter.newBits and bloomfilter.newRandKeys
	// the calculation is the size of the bitset which would be created from this filter.
	// to ensure we don't crash memory, we would ensure the size
	msize := (m + 63) / 64
	msize += k

	// 8 == sizeof(uint64))
	return msize * 8
}

func NewSteakKnifeFilter(maxN uint64, p float64) (Filter, error) {
	m := streakKnife.OptimalM(maxN, p)
	k := streakKnife.OptimalK(m, maxN)

	bfilter, err := streakKnife.New(m, k)
	return &steakKnifeFilter{bfilter: bfilter}, err
}

func (f *steakKnifeFilter) Add(bl ...[]byte) {
	f.lock.Lock()
	defer f.lock.Unlock()
	for _, b := range bl {
		h := murmur3.New64()
		_, errf := h.Write(b)

		// this shouldnt happen
		if errf != nil {
			continue
		}
		f.bfilter.Add(h)
	}
}

func (f *steakKnifeFilter) Check(b []byte) bool {
	f.lock.RLock()
	defer f.lock.RUnlock()
	h := murmur3.New64()
	_, err := h.Write(b)
	if err != nil {
		return false
	}
	return f.bfilter.Contains(h)
}

type SteakKnifeJSON struct {
	K    uint64   `json:"k"`
	N    uint64   `json:"n"`
	M    uint64   `json:"m"`
	Keys []uint64 `json:"keys"`
	Bits []uint64 `json:"bits"`
}

func parseSteakKnifeText(byts []byte) (*SteakKnifeJSON, error) {
	/*
		k
		4
		n
		0
		m
		48
		keys
		0000000000000000
		0000000000000001
		0000000000000002
		0000000000000003
		bits
		0000000000000000
		sha384
		000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f
	*/
	res := &SteakKnifeJSON{}
	scanner := bufio.NewScanner(bytes.NewReader(byts))
	scanner.Buffer(make([]byte, 10e8), 10e8)
	var b string
	if scanner.Scan() {
		b = string(scanner.Bytes())
		if b != "k" {
			return nil, fmt.Errorf("expect k")
		}
		if scanner.Scan() {
			b = string(scanner.Bytes())
			res.K, _ = strconv.ParseUint(b, 10, 64)
		}
	}
	if scanner.Scan() {
		b = string(scanner.Bytes())
		if b != "n" {
			return nil, fmt.Errorf("expect m")
		}
		if scanner.Scan() {
			b = string(scanner.Bytes())
			res.N, _ = strconv.ParseUint(b, 10, 64)
		}
	}
	if scanner.Scan() {
		b = string(scanner.Bytes())
		if b != "m" {
			return nil, fmt.Errorf("expect m")
		}
		if scanner.Scan() {
			b = string(scanner.Bytes())
			res.M, _ = strconv.ParseUint(b, 10, 64)
		}
	}
	if scanner.Scan() {
		b = string(scanner.Bytes())
		if b != "keys" {
			return nil, fmt.Errorf("expect keys")
		}
		for scanner.Scan() {
			b = string(scanner.Bytes())
			if b == "bits" {
				break
			}
			hbits, _ := hex.DecodeString(b)
			if len(hbits) != 8 {
				return nil, fmt.Errorf("invalid hkeys sz")
			}
			num := binary.BigEndian.Uint64(hbits)
			res.Keys = append(res.Keys, num)
		}
	}
	if b != "bits" {
		return nil, fmt.Errorf("expect bits")
	}
	for scanner.Scan() {
		b = string(scanner.Bytes())
		if b == "sha384" {
			break
		}
		hbits, _ := hex.DecodeString(b)
		if len(hbits) != 8 {
			return nil, fmt.Errorf("invalid hbits sz")
		}
		num := binary.BigEndian.Uint64(hbits)
		res.Bits = append(res.Bits, num)
	}
	if b != "sha384" {
		return nil, fmt.Errorf("expect sha384")
	}
	return res, nil
}

func (f *steakKnifeFilter) MarshalJSON() ([]byte, error) {
	bits, err := f.bfilter.MarshalText()
	if err != nil {
		return nil, err
	}
	j, err := parseSteakKnifeText(bits)
	if err != nil {
		return nil, err
	}
	return json.Marshal(j)
}

type btcsuiteFilter struct {
	lock    sync.RWMutex
	bfilter *btcsuite.Filter
}

func BytesBtcsuiteFilter(maxN uint64, p float64) uint64 {
	// ths is pulled from the btcsuite filter logic
	dataLen := uint32(-1 * float64(maxN) * math.Log(p) / Ln2Squared)
	dataLen = MinUint32(dataLen, btcsuiteWire.MaxFilterLoadFilterSize*8) / 8
	return uint64(dataLen)
}

func NewBtcsuiteFilter(maxN uint64, p float64) (Filter, error) {
	tweak := uint32(time.Now().UnixNano())

	bfilter := btcsuite.NewFilter(uint32(maxN), tweak, p, btcsuiteWire.BloomUpdateNone)
	return &btcsuiteFilter{bfilter: bfilter}, nil
}

func (f *btcsuiteFilter) Add(bl ...[]byte) {
	f.lock.Lock()
	defer f.lock.Unlock()
	for _, b := range bl {
		f.bfilter.Add(b)
	}
}

func (f *btcsuiteFilter) Check(b []byte) bool {
	f.lock.RLock()
	defer f.lock.RUnlock()
	return f.bfilter.Matches(b)
}

type MsgFilterLoadJSON struct {
	HashFuncs uint32                       `json:"hashFuncs"`
	Tweak     uint32                       `json:"tweak"`
	Flags     btcsuiteWire.BloomUpdateType `json:"updateType"`
	Filter    []byte                       `json:"filter"`
}

func (f *btcsuiteFilter) MarshalJSON() ([]byte, error) {
	filterLoad := f.bfilter.MsgFilterLoad()
	j := &MsgFilterLoadJSON{
		Filter:    filterLoad.Filter,
		HashFuncs: filterLoad.HashFuncs,
		Tweak:     filterLoad.Tweak,
		Flags:     filterLoad.Flags,
	}
	return json.Marshal(j)
}

// the wordSize of a bit set
const wordSize = uint(64)

// log2WordSize is lg(wordSize)
const log2WordSize = uint(6)

func WordsNeeded(i uint) int {
	if i > (bitset.Cap() - wordSize + 1) {
		return int(bitset.Cap() >> log2WordSize)
	}
	return int((i + (wordSize - 1)) >> log2WordSize)
}

const Ln2Squared = math.Ln2 * math.Ln2

func MinUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
