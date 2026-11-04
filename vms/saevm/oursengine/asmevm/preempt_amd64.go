//go:build asmcore && asmevm_preempt

package asmevm

// The asmevm_preempt tag needs the matching assembler define, which a build
// tag cannot set:
//
//	go test -tags asmcore,asmevm_preempt -asmflags=all=-D=ASMEVM_PREEMPT .
func init() {
	if !asmPreempt() {
		panic("asmevm: asmevm_preempt needs -asmflags=all=-D=ASMEVM_PREEMPT")
	}
}
