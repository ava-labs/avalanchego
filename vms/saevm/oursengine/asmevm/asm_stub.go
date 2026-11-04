//go:build !asmcore || !(arm64 || amd64)

package asmevm

// asmEVMAvailable is false unless built with -tags asmcore on arm64 or amd64. The Go
// loop is the reference path.
var asmEVMAvailable = false
var asmEVMForced = false

func (in *Interpreter) runAsm(c *Contract) ([]byte, error) {
	return in.runGo(c, nil)
}

func SetHostStats(s *[257]uint64) {}

func SetHostTiming(p *int64) int64 { return 0 }

func HostCallTiming() (calls, nanos int64) { return 0, 0 }

var HostOpNanos [256]int64

func KeccakCachePolicy() string { return "none (no native core)" }

func (in *Interpreter) resetKeccakCache() {}

// AsmKeccakImpl names the permutation the native core uses for KECCAK256.
func AsmKeccakImpl() string { return "none (no native core)" }
