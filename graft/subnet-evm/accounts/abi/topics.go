// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2018 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package abi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"reflect"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/crypto"
)

// packTopic packs rule into the corresponding hash value for a log's topic
// according to the Solidity documentation:
// https://docs.soliditylang.org/en/v0.8.17/abi-spec.html#indexed-event-encoding.
func packTopic(rule interface{}) (common.Hash, error) {
	var topic common.Hash

	// Try to generate the topic based on simple types
	switch rule := rule.(type) {
	case common.Hash:
		copy(topic[:], rule[:])
	case common.Address:
		copy(topic[common.HashLength-common.AddressLength:], rule[:])
	case *big.Int:
		copy(topic[:], math.U256Bytes(rule))
	case bool:
		if rule {
			topic[common.HashLength-1] = 1
		}
	case int8:
		copy(topic[:], genIntType(int64(rule), 1))
	case int16:
		copy(topic[:], genIntType(int64(rule), 2))
	case int32:
		copy(topic[:], genIntType(int64(rule), 4))
	case int64:
		copy(topic[:], genIntType(rule, 8))
	case uint8:
		blob := new(big.Int).SetUint64(uint64(rule)).Bytes()
		copy(topic[common.HashLength-len(blob):], blob)
	case uint16:
		blob := new(big.Int).SetUint64(uint64(rule)).Bytes()
		copy(topic[common.HashLength-len(blob):], blob)
	case uint32:
		blob := new(big.Int).SetUint64(uint64(rule)).Bytes()
		copy(topic[common.HashLength-len(blob):], blob)
	case uint64:
		blob := new(big.Int).SetUint64(rule).Bytes()
		copy(topic[common.HashLength-len(blob):], blob)
	case string:
		hash := crypto.Keccak256Hash([]byte(rule))
		copy(topic[:], hash[:])
	case []byte:
		hash := crypto.Keccak256Hash(rule)
		copy(topic[:], hash[:])

	default:
		// todo(rjl493456442) according to solidity documentation, indexed event
		// parameters that are not value types i.e. arrays and structs are not
		// stored directly but instead a keccak256-hash of an encoding is stored.
		//
		// We only convert strings and bytes to hash, still need to deal with
		// array(both fixed-size and dynamic-size) and struct.

		// Attempt to generate the topic from funky types
		val := reflect.ValueOf(rule)
		switch {
		// static byte array
		case val.Kind() == reflect.Array && reflect.TypeOf(rule).Elem().Kind() == reflect.Uint8:
			reflect.Copy(reflect.ValueOf(topic[:val.Len()]), val)
		default:
			return common.Hash{}, fmt.Errorf("unsupported indexed type: %T", rule)
		}
	}
	return topic, nil
}

// PackTopics packs the array of filters into an array of corresponding topics
// according to the Solidity documentation.
// Note: PackTopics does not support array (fixed or dynamic-size) or struct types.
func PackTopics(filter []interface{}) ([]common.Hash, error) {
	topics := make([]common.Hash, len(filter))
	for i, rule := range filter {
		topic, err := packTopic(rule)
		if err != nil {
			return nil, err
		}
		topics[i] = topic
	}

	return topics, nil
}

// MakeTopics converts a filter query argument list into a filter topic set.
func MakeTopics(query ...[]interface{}) ([][]common.Hash, error) {
	topics := make([][]common.Hash, len(query))
	for i, filter := range query {
		for _, rule := range filter {
			topic, err := packTopic(rule)
			if err != nil {
				return nil, err
			}
			topics[i] = append(topics[i], topic)
		}
	}
	return topics, nil
}

func genIntType(rule int64, size uint) []byte {
	var topic [common.HashLength]byte
	if rule < 0 {
		// if a rule is negative, we need to put it into two's complement.
		// extended to common.HashLength bytes.
		topic = [common.HashLength]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}
	}
	for i := uint(0); i < size; i++ {
		topic[common.HashLength-i-1] = byte(rule >> (i * 8))
	}
	return topic[:]
}

// ParseTopics converts the indexed topic fields into actual log field values.
func ParseTopics(out interface{}, fields Arguments, topics []common.Hash) error {
	return parseTopicWithSetter(fields, topics,
		func(arg Argument, reconstr interface{}) {
			field := reflect.ValueOf(out).Elem().FieldByName(ToCamelCase(arg.Name))
			field.Set(reflect.ValueOf(reconstr))
		})
}

// ParseTopicsIntoMap converts the indexed topic field-value pairs into map key-value pairs.
func ParseTopicsIntoMap(out map[string]interface{}, fields Arguments, topics []common.Hash) error {
	return parseTopicWithSetter(fields, topics,
		func(arg Argument, reconstr interface{}) {
			out[arg.Name] = reconstr
		})
}

// parseTopicWithSetter converts the indexed topic field-value pairs and stores them using the
// provided set function.
//
// Note, dynamic types cannot be reconstructed since they get mapped to Keccak256
// hashes as the topic value!
func parseTopicWithSetter(fields Arguments, topics []common.Hash, setter func(Argument, interface{})) error {
	// Sanity check that the fields and topics match up
	if len(fields) != len(topics) {
		return errors.New("topic/field count mismatch")
	}
	// Iterate over all the fields and reconstruct them from topics
	for i, arg := range fields {
		if !arg.Indexed {
			return errors.New("non-indexed field in topic reconstruction")
		}
		var reconstr interface{}
		switch arg.Type.T {
		case TupleTy:
			return errors.New("tuple type in topic reconstruction")
		case StringTy, BytesTy, SliceTy, ArrayTy:
			// Array types (including strings and bytes) have their keccak256 hashes stored in the topic- not a hash
			// whose bytes can be decoded to the actual value- so the best we can do is retrieve that hash
			reconstr = topics[i]
		case FunctionTy:
			if garbage := binary.BigEndian.Uint64(topics[i][0:8]); garbage != 0 {
				return fmt.Errorf("bind: got improperly encoded function type, got %v", topics[i].Bytes())
			}
			var tmp [24]byte
			copy(tmp[:], topics[i][8:32])
			reconstr = tmp
		default:
			var err error
			reconstr, err = toGoType(0, arg.Type, topics[i].Bytes())
			if err != nil {
				return err
			}
		}
		// Use the setter function to store the value
		setter(arg, reconstr)
	}

	return nil
}
