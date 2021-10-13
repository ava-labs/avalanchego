// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"
	"fmt"
)

type jsonFormatter struct {
	obj interface{}
}

func (f jsonFormatter) String() string {
	jsonBytes, err := json.Marshal(f.obj)
	if err != nil {
		return fmt.Sprintf("error marshalling: %s", err)
	}
	return string(jsonBytes)
}
