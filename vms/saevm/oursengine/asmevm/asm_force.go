//go:build asmcore && (arm64 || amd64)

package asmevm

import (
	"os"
	"strconv"
	"strings"
)

func envForceAsm() bool { return os.Getenv("ASMEVM") == "1" }

// fuseEnabled turns decode-time superinstructions on; ASMEVM_NOFUSE=1 turns
// them off for A/B measurements.
var fuseEnabled = os.Getenv("ASMEVM_NOFUSE") == ""

// The KECCAK256 result cache (keccakCache) policy, ASMEVM_KCACHE (exact input
// match on a pure function, so any hit is the right hash):
//   - unset or "persistent4096" (default): a direct-mapped memo of 4096
//     64-byte inputs owned by the State, kept across transactions
//     (State.keccakMemo);
//   - "ring8": the 8-entry ring of the interpreter (one interpreter per EVM:
//     it survives across the transactions of a reused EVM);
//   - "tx": the ring, emptied at the start of every transaction (ExecTx);
//   - "0": off.
var (
	keccakCacheOn    = os.Getenv("ASMEVM_KCACHE") != "0"
	keccakCachePerTx = os.Getenv("ASMEVM_KCACHE") == "tx"
	keccakMemoOn     = os.Getenv("ASMEVM_KCACHE") == "" || os.Getenv("ASMEVM_KCACHE") == "persistent4096"
)

// KeccakCachePolicy names the KECCAK256 cache policy in use.
func KeccakCachePolicy() string {
	switch {
	case !keccakCacheOn:
		return "off"
	case keccakCachePerTx:
		return "per-tx (8 x 64-byte inputs, emptied by ExecTx)"
	case keccakMemoOn:
		return "persistent4096 (direct-mapped 4096 x 64-byte inputs per State, kept across txs)"
	}
	return "ring8 (8 x 64-byte inputs per interpreter, kept across the txs of a reused EVM)"
}

// resetKeccakCache empties the cache when the policy is per-tx.
func (in *Interpreter) resetKeccakCache() {
	if keccakCachePerTx {
		in.kcache.n, in.kcache.next = 0, 0
	}
}

// nativeSload enables the native warm SLOAD (slotTable); ASMEVM_SLOAD=0 turns
// it off for A/B measurements.
var nativeSload = os.Getenv("ASMEVM_SLOAD") != "0"

// nativeCalls enables the native CALL, STATICCALL and DELEGATECALL (amd64);
// ASMEVM_CALLS=0 turns them off for A/B measurements.
var nativeCalls = os.Getenv("ASMEVM_CALLS") != "0"

// nativeSstore enables the native warm SSTORE (nativeJournal); ASMEVM_SSTORE=0
// turns it off for A/B measurements.
var nativeSstore = os.Getenv("ASMEVM_SSTORE") != "0"

// hostOps lists opcodes routed to the host at decode time (ASMEVM_HOSTOPS,
// comma-separated hex, e.g. "0x01,0x50"); diagnosis for the callback cost.
var hostOps = func() (ops [256]bool) {
	for _, s := range strings.Split(os.Getenv("ASMEVM_HOSTOPS"), ",") {
		if v, err := strconv.ParseUint(strings.TrimSpace(s), 0, 8); err == nil {
			ops[v] = true
		}
	}
	return
}()

// fuseMask selects fused patterns by index (ASMEVM_FUSEMASK, hex; default all).
var fuseMask = func() uint64 {
	if v, err := strconv.ParseUint(os.Getenv("ASMEVM_FUSEMASK"), 0, 64); err == nil {
		return v
	}
	return ^uint64(0)
}()
