// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package json

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"
)

const (
	// Null is the string representation of a null value
	Null = "null"
)

var (
	errUppercaseMethod = errors.New("method must start with a non-uppercase letter")
	errInvalidArg      = errors.New("couldn't unmarshal an argument. Ensure arguments are valid and properly formatted. See documentation for example calls")
)

// NewCodec returns a new json codec that will convert the first character of
// the method to uppercase
func NewCodec() rpc.Codec {
	return lowercase{json2.NewCodec()}
}

type lowercase struct{ *json2.Codec }

func (lc lowercase) NewRequest(r *http.Request) rpc.CodecRequest {
	return &request{lc.Codec.NewRequest(r).(*json2.CodecRequest)}
}

type request struct{ *json2.CodecRequest }

func (r *request) Method() (string, error) {
	method, err := r.CodecRequest.Method()
	methodSections := strings.SplitN(method, ".", 2)
	if len(methodSections) != 2 || err != nil {
		return method, err
	}
	class, function := methodSections[0], methodSections[1]
	firstRune, runeLen := utf8.DecodeRuneInString(function)
	if firstRune == utf8.RuneError {
		return method, nil
	}
	if unicode.IsUpper(firstRune) {
		return method, errUppercaseMethod
	}
	uppercaseRune := string(unicode.ToUpper(firstRune))
	return fmt.Sprintf("%s.%s%s", class, uppercaseRune, function[runeLen:]), nil
}

func (r *request) ReadRequest(args interface{}) error {
	if err := r.CodecRequest.ReadRequest(args); err != nil {
		return errInvalidArg
	}
	return nil
}
