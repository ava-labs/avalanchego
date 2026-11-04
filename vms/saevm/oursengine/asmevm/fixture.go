package asmevm

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"

	"github.com/holiman/uint256"
)

type jAccount struct {
	Address string            `json:"address"`
	Balance string            `json:"balance"`
	Nonce   uint64            `json:"nonce"`
	Code    string            `json:"code"`
	Storage map[string]string `json:"storage"`
}
type jTx struct {
	Caller   string `json:"caller"`
	To       string `json:"to"`
	Data     string `json:"data"`
	Value    string `json:"value"`
	Gas      uint64 `json:"gas"`
	Nonce    uint64 `json:"nonce"`
	GasPrice string `json:"gas_price"`
}
type jBlock struct {
	Number     uint64 `json:"number"`
	Timestamp  uint64 `json:"timestamp"`
	GasLimit   uint64 `json:"gas_limit"`
	BaseFee    string `json:"base_fee"`
	Coinbase   string `json:"coinbase"`
	Prevrandao string `json:"prevrandao"`
}
type jCase struct {
	Name     string     `json:"name"`
	Accounts []jAccount `json:"accounts"`
	Tx       jTx        `json:"tx"`
	Block    jBlock     `json:"block"`
}
type jFixtures struct {
	Spec  string  `json:"spec"`
	Cases []jCase `json:"cases"`
}

func hx(s string) []byte {
	s = strings.TrimPrefix(s, "0x")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}
func addrOf(s string) Address     { return Address(hx(s)) }
func u256Of(s string) uint256.Int { var v uint256.Int; v.SetBytes(hx(s)); return v }
func hashOf(s string) Hash        { var h Hash; b := hx(s); copy(h[32-len(b):], b); return h }

func buildState(c jCase) *State {
	st := NewState()
	for _, a := range c.Accounts {
		addr := addrOf(a.Address)
		acc := st.getOrNew(addr)
		acc.balance = u256Of(a.Balance)
		acc.nonce = a.Nonce
		code := hx(a.Code)
		acc.code = code
		if len(code) > 0 {
			acc.jd = codeBitmap(code)
			if predecodeHook != nil {
				acc.dec = predecodeHook(code) // setup-time analysis, like revm
			}
		}
		if len(code) > 0 {
			acc.codeHash = Keccak256(code)
		}
		for k, v := range a.Storage {
			acc.storage[hashOf(k)] = u256Of(v)
		}
	}
	st.journal = st.journal[:0]
	st.IndexStorage()
	return st
}

// Case is one decoded Uniswap V3 fixture: a warm in-memory pre-state template,
// the block and the transaction.
type Case struct {
	Name     string
	Template *State
	Block    BlockContext
	Tx       Tx
}

// LoadFixtures decodes fixtures.json (revm transaction probe format).
func LoadFixtures(path string) ([]Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fx jFixtures
	if err := json.Unmarshal(raw, &fx); err != nil {
		return nil, err
	}
	var out []Case
	for _, c := range fx.Cases {
		to := addrOf(c.Tx.To)
		out = append(out, Case{
			Name:     c.Name,
			Template: buildState(c),
			Block: BlockContext{
				Coinbase:    addrOf(c.Block.Coinbase),
				GasLimit:    c.Block.GasLimit,
				BlockNumber: c.Block.Number,
				Time:        c.Block.Timestamp,
				BaseFee:     u256Of(c.Block.BaseFee),
				Random:      hashOf(c.Block.Prevrandao),
			},
			Tx: Tx{
				Caller:   addrOf(c.Tx.Caller),
				To:       &to,
				Data:     hx(c.Tx.Data),
				Value:    u256Of(c.Tx.Value),
				Gas:      c.Tx.Gas,
				Nonce:    c.Tx.Nonce,
				GasPrice: u256Of(c.Tx.GasPrice),
			},
		})
	}
	return out, nil
}
