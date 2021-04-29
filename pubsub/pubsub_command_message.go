package pubsub

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

// CommandMessage command message
type CommandMessage struct {
	Command string `json:"command"`
	// AddressIds array of ids.ShortID
	AddressIds [][]byte `json:"addressIds,omitempty"`
	// Addresses array of avax addresses i.e. fuji123....
	Addresses []string `json:"addresses,omitempty"`
	// FilterMax size of bloom filter
	FilterMax uint64 `json:"filterMax,omitempty"`
	// FilterError expected error rate of filter
	FilterError float64 `json:"filterError,omitempty"`
	// subscription to this kind of messages
	EventType EventType `json:"eventType"`
	// Unsubscribe unsubscribe channel remove address or reset filter
	Unsubscribe bool `json:"unsubscribe"`
}

func NewCommandMessage(r io.Reader, hrp string) (*CommandMessage, error) {
	c := CommandMessage{}
	err := c.Load(r)
	if err != nil {
		return nil, err
	}
	c.TransposeAddress(hrp)
	return &c, nil
}

func (c *CommandMessage) IsNewFilter() bool {
	return c.FilterMax > 0 && c.FilterError > 0
}

func (c *CommandMessage) FilterOrDefault() {
	if c.IsNewFilter() {
		return
	}
	c.FilterMax = DefaultFilterMax
	c.FilterError = DefaultFilterError
}

// TransposeAddress converts any b32 address to their byte equiv ids.ShortID.
func (c *CommandMessage) TransposeAddress(hrp string) {
	if c.AddressIds == nil {
		c.AddressIds = make([][]byte, 0, len(c.Addresses))
	}
	for _, astr := range c.Addresses {
		// remove chain prefix if found..  X-fuji....
		addressParts := strings.SplitN(astr, "-", 2)
		if len(addressParts) >= 2 {
			astr = addressParts[1]
		}
		if !strings.HasPrefix(astr, hrp) {
			continue
		}
		_, _, abytes, err := formatting.ParseAddress("X-" + astr)
		if err != nil {
			continue
		}
		c.AddressIds = append(c.AddressIds, abytes)
	}
}

func (c *CommandMessage) Load(r io.Reader) error {
	return json.NewDecoder(r).Decode(c)
}

func (c *CommandMessage) ParseQuery(q map[string][]string) {
	if len(q) == 0 {
		return
	}
	if c.AddressIds == nil {
		c.AddressIds = make([][]byte, 0, 10)
	}
	for valuesk, valuesv := range q {
		switch valuesk {
		case ParamAddress:
			for _, value := range valuesv {
				// 0x or 0X followed by enough bytes for a ids.ShortID
				if (strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X")) &&
					len(value) == (len(ids.ShortEmpty)+1)*2 {
					sid, err := AddressToID(value[2:])
					if err == nil {
						c.AddressIds = append(c.AddressIds, sid[:])
						continue
					}
				}
				//  enough bytes for a ids.ShortID
				if len(value) == len(ids.ShortEmpty)*2 {
					sid, err := AddressToID(value)
					if err == nil {
						c.AddressIds = append(c.AddressIds, sid[:])
						continue
					}
				}
				c.AddressIds = append(c.AddressIds, []byte(value))
			}
		default:
		}
	}
}

func AddressToID(address string) (ids.ShortID, error) {
	addrBytes, err := hex.DecodeString(address)
	if err != nil {
		return ids.ShortEmpty, err
	}
	return ByteToID(addrBytes), nil
}
