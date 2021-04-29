package bloom

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/spaolacci/murmur3"
	streakKnife "github.com/steakknife/bloomfilter"
)

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
	neededBytes := BytesSteakKnifeFilter(maxN, p)
	if neededBytes > maxBytes {
		return nil, ErrMaxBytes
	}
	return NewSteakKnifeFilter(maxN, p)
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
