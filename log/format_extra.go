// (c) 2024 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package log

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum/log"
)

func CorethTermFormat(alias string) log.Format {
	prefix := fmt.Sprintf("<%s Chain>", alias)
	return log.FormatFunc(func(r *log.Record) []byte {
		msg := escapeMessage(r.Msg)
		b := &bytes.Buffer{}
		lvl := r.Lvl.AlignedString()

		location := fmt.Sprintf("%+v", r.Call)
		for _, prefix := range locationTrims {
			location = strings.TrimPrefix(location, prefix)
		}

		fmt.Fprintf(b, "[%s] %s %s %s %s ", r.Time.Format(termTimeFormat), lvl, prefix, location, msg)
		// try to justify the log output for short messages
		length := utf8.RuneCountInString(msg)
		if len(r.Ctx) > 0 && length < termMsgJust {
			b.Write(bytes.Repeat([]byte{' '}, termMsgJust-length))
		}
		// print the keys logfmt style
		logfmt(b, r.Ctx, 0, true)
		return b.Bytes()
	})
}

func CorethJSONFormat(alias string) log.Format {
	prefix := fmt.Sprintf("%s Chain", alias)
	return log.FormatFunc(func(r *log.Record) []byte {
		props := make(map[string]interface{}, 5+len(r.Ctx)/2)
		props["timestamp"] = r.Time
		props["level"] = r.Lvl.String()
		props[r.KeyNames.Msg] = r.Msg
		props["logger"] = prefix
		props["caller"] = fmt.Sprintf("%+v", r.Call)
		for i := 0; i < len(r.Ctx); i += 2 {
			k, ok := r.Ctx[i].(string)
			if !ok {
				props[errorKey] = fmt.Sprintf("%+v is not a string key", r.Ctx[i])
			} else {
				// The number of arguments is normalized from the geth logger
				// to ensure that this will not cause an index out of bounds error
				props[k] = formatJSONValue(r.Ctx[i+1])
			}
		}

		b, err := json.Marshal(props)
		if err != nil {
			b, _ = json.Marshal(map[string]string{
				errorKey: err.Error(),
			})
			return b
		}

		b = append(b, '\n')
		return b
	})
}
