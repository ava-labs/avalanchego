//go:build asmcore && arm64 && !darwin

package asmevm

import "golang.org/x/sys/cpu"

func hasSHA3() bool { return cpu.ARM64.HasSHA3 }
