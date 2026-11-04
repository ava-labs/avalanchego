//go:build asmcore

// Copyright 2026 the libevm authors.
//
// The libevm additions to go-ethereum are free software: you can redistribute
// them and/or modify them under the terms of the GNU Lesser General Public License
// as published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The libevm additions are distributed in the hope that they will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see
// <http://www.gnu.org/licenses/>.

#include "go_asm.h"
#include "textflag.h"
#include "funcdata.h"

// One asmRun call executes a whole EVM frame. Registers that live across
// operations:
//
//	R19  *asmFrame
//	R20  code base
//	R21  pc (index into code and into the pre-decoded stream)
//	R22  gas left
//	R23  stack items (sp); item i is at R26 + 32*i, limbs little-endian
//	R24  pre-decoded handlers (asmFrame.handlers): handler address per pc
//	R25  pre-decoded PUSH immediates (asmFrame.args), per pc
//	R26  stack base (1024 words, owned by the frame)
//	R17  asmFrame.flags (fork-dependent opcodes)
//
// R0-R16 are scratch. R8 holds the address one past the top item inside a
// handler (TOP). R18 (platform), R27 (assembler temporary), R28 (g), R29 (frame
// pointer) and R30 (link, used by BL subroutines) are not used for state.
//
// Operations that need the Go host (state, environment, calls, logs, ...) and
// every failing check (gas, stack, memory beyond the pooled buffer, invalid
// jump) branch to host with the operation not yet executed. host stores pc, gas
// and sp in the frame and CALLs asmHost (ABI0), which runs the stock geth
// operation with all its checks and errors. All live registers are reloaded
// from the frame afterwards, because the call may move the goroutine stack;
// no pointer into the goroutine stack is kept across it, and the locals hold
// no pointers (NO_LOCAL_POINTERS).
//
// Gas is checked and charged per operation exactly as in geth. Native memory
// expansion zeroes the pooled buffer and charges the geth quadratic formula.

#define F_CODE asmFrame_code(R19)
#define F_CODELEN asmFrame_codeLen(R19)
#define F_STACK asmFrame_stackBase(R19)
#define F_MEM asmFrame_mem(R19)
#define F_MEMLEN asmFrame_memLen(R19)
#define F_MEMCAP asmFrame_memCap(R19)
#define F_MEMCOST asmFrame_memCost(R19)
#define F_JUMPDESTS asmFrame_jumpdests(R19)
#define F_INPUT asmFrame_input(R19)
#define F_INPUTLEN asmFrame_inputLen(R19)
#define F_FLAGS asmFrame_flags(R19)
#define F_ABORT asmFrame_abort(R19)
#define F_PC asmFrame_pc(R19)
#define F_GAS asmFrame_gas(R19)
#define F_SP asmFrame_sp(R19)
#define F_STATUS asmFrame_status(R19)
#define F_RET0 asmFrame_ret0(R19)
#define F_RET1 asmFrame_ret1(R19)
#define F_HANDLERS asmFrame_handlers(R19)
#define F_ARGS asmFrame_args(R19)

// Locals (RSP offsets). 8(RSP) is the outgoing argument of host calls.
#define L_LR 16     // saved link register of nested subroutines
#define L_PAD 32    // 136 bytes: last KECCAK256 block
#define L_SCR 176   // 64 bytes: shift and SIGNEXTEND scratch, MULMOD operands
#define L_U 240     // 9 words: dividend
#define L_UN 312    // 9 words: normalised dividend
#define L_D 384     // 4 words: divisor
#define L_Q 416     // 8 words: quotient
#define L_R 480     // 4 words: remainder

// The stream is padded with STOP handlers past the end of code, so there is no
// bounds check here.
#define NEXT(n) \
	ADD $n, R21; \
	MOVD (R24)(R21<<3), R0; \
	JMP (R0)

// EVM.Cancel makes JUMP and JUMPI stop the frame; host handles it.
#define ABORT_CHECK \
	MOVD F_ABORT, R9; \
	MOVWU (R9), R9; \
	CBNZ R9, host

#define NEED(k) \
	CMP $k, R23; \
	BLO host

#define ROOM \
	CMP $1024, R23; \
	BHS host

#define USEGAS(n, undo) \
	SUBS $n, R22; \
	BLO undo

#define TOP ADD R23<<5, R26, R8

// a (top) in R0-R3, b in R4-R7.
#define BIN_LOAD \
	TOP; \
	LDP -32(R8), (R0, R1); \
	LDP -16(R8), (R2, R3); \
	LDP -64(R8), (R4, R5); \
	LDP -48(R8), (R6, R7)

#define BIN_STORE \
	STP (R0, R1), -64(R8); \
	STP (R2, R3), -48(R8); \
	SUB $1, R23

#define BOOL_STORE \
	STP (R0, ZR), -64(R8); \
	STP (ZR, ZR), -48(R8); \
	SUB $1, R23

#define PUSH_R0 \
	TOP; \
	STP (R0, ZR), (R8); \
	STP (ZR, ZR), 16(R8); \
	ADD $1, R23

#define DUP(n) \
	NEED(n); \
	ROOM; \
	USEGAS(3, undo3); \
	TOP; \
	LDP -(32*n)(R8), (R0, R1); \
	LDP -(32*n-16)(R8), (R2, R3); \
	STP (R0, R1), (R8); \
	STP (R2, R3), 16(R8); \
	ADD $1, R23; \
	NEXT(1)

#define SWAP(n) \
	NEED(n+1); \
	USEGAS(3, undo3); \
	TOP; \
	SUB $(32*(n+1)), R8, R9; \
	LDP -32(R8), (R0, R1); \
	LDP -16(R8), (R2, R3); \
	LDP (R9), (R4, R5); \
	LDP 16(R9), (R6, R7); \
	STP (R4, R5), -32(R8); \
	STP (R6, R7), -16(R8); \
	STP (R0, R1), (R9); \
	STP (R2, R3), 16(R9); \
	NEXT(1)

// Branch to host unless the 256-bit value in (lo, h1, h2, h3) is below 2^32.
#define SMALL(lo, h1, h2, h3) \
	ORR h1, h2, R9; \
	ORR h3, R9; \
	ORR lo>>32, R9; \
	CBNZ R9, host

// q, rem = <uh, ul> / d with reciprocal v; uh < d. rem may alias uh.
// "Improved division by invariant integers", Algorithm 4, as in uint256.
#define DIV2BY1(uh, ul, d, v, q, rem, ql, tmp) \
	MUL v, uh, ql; \
	UMULH v, uh, q; \
	ADDS ul, ql; \
	ADC uh, q; \
	ADD $1, q; \
	MUL q, d, tmp; \
	SUB tmp, ul, rem; \
	CMP ql, rem; \
	SUB $1, q, tmp; \
	CSEL HI, tmp, q, q; \
	ADD d, rem, tmp; \
	CSEL HI, tmp, rem, rem; \
	CMP d, rem; \
	ADD $1, q, tmp; \
	CSEL HS, tmp, q, q; \
	SUB d, rem, tmp; \
	CSEL HS, tmp, rem, rem

// XOR one 136-byte block at P into the Keccak state V0-V16; P advances.
#define ABSORB(blk) \
	VLD1.P 16(blk), [V25.D1, V26.D1]; \
	VEOR V25.B16, V0.B16, V0.B16; \
	VEOR V26.B16, V1.B16, V1.B16; \
	VLD1.P 16(blk), [V25.D1, V26.D1]; \
	VEOR V25.B16, V2.B16, V2.B16; \
	VEOR V26.B16, V3.B16, V3.B16; \
	VLD1.P 16(blk), [V25.D1, V26.D1]; \
	VEOR V25.B16, V4.B16, V4.B16; \
	VEOR V26.B16, V5.B16, V5.B16; \
	VLD1.P 16(blk), [V25.D1, V26.D1]; \
	VEOR V25.B16, V6.B16, V6.B16; \
	VEOR V26.B16, V7.B16, V7.B16; \
	VLD1.P 16(blk), [V25.D1, V26.D1]; \
	VEOR V25.B16, V8.B16, V8.B16; \
	VEOR V26.B16, V9.B16, V9.B16; \
	VLD1.P 16(blk), [V25.D1, V26.D1]; \
	VEOR V25.B16, V10.B16, V10.B16; \
	VEOR V26.B16, V11.B16, V11.B16; \
	VLD1.P 16(blk), [V25.D1, V26.D1]; \
	VEOR V25.B16, V12.B16, V12.B16; \
	VEOR V26.B16, V13.B16, V13.B16; \
	VLD1.P 16(blk), [V25.D1, V26.D1]; \
	VEOR V25.B16, V14.B16, V14.B16; \
	VEOR V26.B16, V15.B16, V15.B16; \
	VLD1.P 8(blk), [V25.D1]; \
	VEOR V25.B16, V16.B16, V16.B16

// func asmRun(f *asmFrame)
TEXT ·asmRun(SB), 0, $512-8
	NO_LOCAL_POINTERS
	MOVD f+0(FP), R19
	MOVD F_STATUS, R0
	CMP $const_asmSlotCheck, R0
	BNE reload
	ADR slots, R1
	ADR fused_end, R2
	SUB R1, R2
	MOVD R2, F_RET0
	MOVD R1, F_RET1
	RET

host:
	MOVD R21, F_PC
	MOVD R22, F_GAS
	MOVD R23, F_SP
	MOVD R19, 8(RSP)
	CALL ·asmHost(SB)

reload:
	MOVD f+0(FP), R19
	MOVD F_STATUS, R0
	CBNZ R0, done
	MOVD F_CODE, R20
	MOVD F_PC, R21
	MOVD F_GAS, R22
	MOVD F_SP, R23
	MOVD F_ARGS, R25
	MOVD F_STACK, R26
	MOVD F_FLAGS, R17
	MOVD F_HANDLERS, R24
	MOVD (R24)(R21<<3), R0
	JMP (R0)

op_stop:
	MOVD $const_asmStop, R0

// R0 is the exit status.
exit:
	MOVD R21, F_PC
	MOVD R22, F_GAS
	MOVD R23, F_SP
	MOVD R0, F_STATUS

done:
	RET

// The constant gas was charged before a check failed; give it back.
undo1:
	ADD $1, R22
	B host
undo2:
	ADD $2, R22
	B host
undo3:
	ADD $3, R22
	B host
undo5:
	ADD $5, R22
	B host
undo8:
	ADD $8, R22
	B host

// BEGIN SLOTS (generated)
	PCALIGN $128
slots:
// 0x00
	MOVD $const_asmStop, R0
	B exit
	PCALIGN $128
// 0x01
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	ADDS R4, R0
	ADCS R5, R1
	ADCS R6, R2
	ADC R7, R3
	BIN_STORE
	NEXT(1)
	PCALIGN $128
// 0x02
	NEED(2)
	USEGAS(5, undo5)
	BIN_LOAD
	BL mul256
	BIN_STORE
	NEXT(1)
	PCALIGN $128
// 0x03
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	SUBS R4, R0
	SBCS R5, R1
	SBCS R6, R2
	SBC R7, R3
	BIN_STORE
	NEXT(1)
	PCALIGN $128
// 0x04
	B op_div
	PCALIGN $128
// 0x05
	B op_sdiv
	PCALIGN $128
// 0x06
	B op_mod
	PCALIGN $128
// 0x07
	B op_smod
	PCALIGN $128
// 0x08
	B op_addmod
	PCALIGN $128
// 0x09
	B op_mulmod
	PCALIGN $128
// 0x0a
	B op_exp
	PCALIGN $128
// 0x0b
	B op_signextend
	PCALIGN $128
// 0x0c
	B host
	PCALIGN $128
// 0x0d
	B host
	PCALIGN $128
// 0x0e
	B host
	PCALIGN $128
// 0x0f
	B host
	PCALIGN $128
// 0x10
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	SUBS R4, R0, R9
	SBCS R5, R1, R9
	SBCS R6, R2, R9
	SBCS R7, R3, R9
	CSET LO, R0
	BOOL_STORE
	NEXT(1)
	PCALIGN $128
// 0x11
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	SUBS R0, R4, R9
	SBCS R1, R5, R9
	SBCS R2, R6, R9
	SBCS R3, R7, R9
	CSET LO, R0
	BOOL_STORE
	NEXT(1)
	PCALIGN $128
// 0x12
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	SUBS R4, R0, R9
	SBCS R5, R1, R9
	SBCS R6, R2, R9
	SBCS R7, R3, R9
	CSET LT, R0
	BOOL_STORE
	NEXT(1)
	PCALIGN $128
// 0x13
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	SUBS R0, R4, R9
	SBCS R1, R5, R9
	SBCS R2, R6, R9
	SBCS R3, R7, R9
	CSET LT, R0
	BOOL_STORE
	NEXT(1)
	PCALIGN $128
// 0x14
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	EOR R4, R0
	EOR R5, R1
	EOR R6, R2
	EOR R7, R3
	ORR R1, R0
	ORR R2, R0
	ORR R3, R0
	CMP $0, R0
	CSET EQ, R0
	BOOL_STORE
	NEXT(1)
	PCALIGN $128
// 0x15
	NEED(1)
	USEGAS(3, undo3)
	TOP
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	ORR R1, R0
	ORR R2, R0
	ORR R3, R0
	CMP $0, R0
	CSET EQ, R0
	STP (R0, ZR), -32(R8)
	STP (ZR, ZR), -16(R8)
	NEXT(1)
	PCALIGN $128
// 0x16
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	AND R4, R0
	AND R5, R1
	AND R6, R2
	AND R7, R3
	BIN_STORE
	NEXT(1)
	PCALIGN $128
// 0x17
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	ORR R4, R0
	ORR R5, R1
	ORR R6, R2
	ORR R7, R3
	BIN_STORE
	NEXT(1)
	PCALIGN $128
// 0x18
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	EOR R4, R0
	EOR R5, R1
	EOR R6, R2
	EOR R7, R3
	BIN_STORE
	NEXT(1)
	PCALIGN $128
// 0x19
	NEED(1)
	USEGAS(3, undo3)
	TOP
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	MVN R0, R0
	MVN R1, R1
	MVN R2, R2
	MVN R3, R3
	STP (R0, R1), -32(R8)
	STP (R2, R3), -16(R8)
	NEXT(1)
	PCALIGN $128
// 0x1a
	B op_byte
	PCALIGN $128
// 0x1b
	B op_shl
	PCALIGN $128
// 0x1c
	B op_shr
	PCALIGN $128
// 0x1d
	B op_sar
	PCALIGN $128
// 0x1e
	B host
	PCALIGN $128
// 0x1f
	B host
	PCALIGN $128
// 0x20
	B op_keccak
	PCALIGN $128
// 0x21
	B host
	PCALIGN $128
// 0x22
	B host
	PCALIGN $128
// 0x23
	B host
	PCALIGN $128
// 0x24
	B host
	PCALIGN $128
// 0x25
	B host
	PCALIGN $128
// 0x26
	B host
	PCALIGN $128
// 0x27
	B host
	PCALIGN $128
// 0x28
	B host
	PCALIGN $128
// 0x29
	B host
	PCALIGN $128
// 0x2a
	B host
	PCALIGN $128
// 0x2b
	B host
	PCALIGN $128
// 0x2c
	B host
	PCALIGN $128
// 0x2d
	B host
	PCALIGN $128
// 0x2e
	B host
	PCALIGN $128
// 0x2f
	B host
	PCALIGN $128
// 0x30
	B host
	PCALIGN $128
// 0x31
	B host
	PCALIGN $128
// 0x32
	B host
	PCALIGN $128
// 0x33
	B host
	PCALIGN $128
// 0x34
	B host
	PCALIGN $128
// 0x35
	B op_calldataload
	PCALIGN $128
// 0x36
	ROOM
	USEGAS(2, undo2)
	MOVD F_INPUTLEN, R0
	PUSH_R0
	NEXT(1)
	PCALIGN $128
// 0x37
	B op_calldatacopy
	PCALIGN $128
// 0x38
	ROOM
	USEGAS(2, undo2)
	MOVD F_CODELEN, R0
	PUSH_R0
	NEXT(1)
	PCALIGN $128
// 0x39
	B op_codecopy
	PCALIGN $128
// 0x3a
	B host
	PCALIGN $128
// 0x3b
	B host
	PCALIGN $128
// 0x3c
	B host
	PCALIGN $128
// 0x3d
	B host
	PCALIGN $128
// 0x3e
	B host
	PCALIGN $128
// 0x3f
	B host
	PCALIGN $128
// 0x40
	B host
	PCALIGN $128
// 0x41
	B host
	PCALIGN $128
// 0x42
	B host
	PCALIGN $128
// 0x43
	B host
	PCALIGN $128
// 0x44
	B host
	PCALIGN $128
// 0x45
	B host
	PCALIGN $128
// 0x46
	B host
	PCALIGN $128
// 0x47
	B host
	PCALIGN $128
// 0x48
	B host
	PCALIGN $128
// 0x49
	B host
	PCALIGN $128
// 0x4a
	B host
	PCALIGN $128
// 0x4b
	B host
	PCALIGN $128
// 0x4c
	B host
	PCALIGN $128
// 0x4d
	B host
	PCALIGN $128
// 0x4e
	B host
	PCALIGN $128
// 0x4f
	B host
	PCALIGN $128
// 0x50
	NEED(1)
	USEGAS(2, undo2)
	SUB $1, R23
	NEXT(1)
	PCALIGN $128
// 0x51
	B op_mload
	PCALIGN $128
// 0x52
	B op_mstore
	PCALIGN $128
// 0x53
	B op_mstore8
	PCALIGN $128
// 0x54
	B host
	PCALIGN $128
// 0x55
	B host
	PCALIGN $128
// 0x56
	B op_jump
	PCALIGN $128
// 0x57
	B op_jumpi
	PCALIGN $128
// 0x58
	ROOM
	USEGAS(2, undo2)
	MOVD R21, R0
	PUSH_R0
	NEXT(1)
	PCALIGN $128
// 0x59
	ROOM
	USEGAS(2, undo2)
	MOVD F_MEMLEN, R0
	PUSH_R0
	NEXT(1)
	PCALIGN $128
// 0x5a
	ROOM
	USEGAS(2, undo2)
	MOVD R22, R0
	PUSH_R0
	NEXT(1)
	PCALIGN $128
// 0x5b
jumpdest_slot:
	USEGAS(1, undo1)
	NEXT(1)
	PCALIGN $128
// 0x5c
	B host
	PCALIGN $128
// 0x5d
	B host
	PCALIGN $128
// 0x5e
	B op_mcopy
	PCALIGN $128
// 0x5f
	AND $const_asmFlagPush0, R17, R9
	CBZ R9, host
	ROOM
	USEGAS(2, undo2)
	MOVD ZR, R0
	PUSH_R0
	NEXT(1)
	PCALIGN $128
// 0x60
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R0
	STP (R0, ZR), (R8)
	STP (ZR, ZR), 16(R8)
	ADD $1, R23
	NEXT(2)
	PCALIGN $128
// 0x61
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R0
	STP (R0, ZR), (R8)
	STP (ZR, ZR), 16(R8)
	ADD $1, R23
	NEXT(3)
	PCALIGN $128
// 0x62
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R0
	STP (R0, ZR), (R8)
	STP (ZR, ZR), 16(R8)
	ADD $1, R23
	NEXT(4)
	PCALIGN $128
// 0x63
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R0
	STP (R0, ZR), (R8)
	STP (ZR, ZR), 16(R8)
	ADD $1, R23
	NEXT(5)
	PCALIGN $128
// 0x64
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R0
	STP (R0, ZR), (R8)
	STP (ZR, ZR), 16(R8)
	ADD $1, R23
	NEXT(6)
	PCALIGN $128
// 0x65
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R0
	STP (R0, ZR), (R8)
	STP (ZR, ZR), 16(R8)
	ADD $1, R23
	NEXT(7)
	PCALIGN $128
// 0x66
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R0
	STP (R0, ZR), (R8)
	STP (ZR, ZR), 16(R8)
	ADD $1, R23
	NEXT(8)
	PCALIGN $128
// 0x67
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R0
	STP (R0, ZR), (R8)
	STP (ZR, ZR), 16(R8)
	ADD $1, R23
	NEXT(9)
	PCALIGN $128
// 0x68
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(10)
	PCALIGN $128
// 0x69
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(11)
	PCALIGN $128
// 0x6a
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(12)
	PCALIGN $128
// 0x6b
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(13)
	PCALIGN $128
// 0x6c
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(14)
	PCALIGN $128
// 0x6d
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(15)
	PCALIGN $128
// 0x6e
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(16)
	PCALIGN $128
// 0x6f
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(17)
	PCALIGN $128
// 0x70
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(18)
	PCALIGN $128
// 0x71
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(19)
	PCALIGN $128
// 0x72
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(20)
	PCALIGN $128
// 0x73
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(21)
	PCALIGN $128
// 0x74
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(22)
	PCALIGN $128
// 0x75
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(23)
	PCALIGN $128
// 0x76
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(24)
	PCALIGN $128
// 0x77
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(25)
	PCALIGN $128
// 0x78
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(26)
	PCALIGN $128
// 0x79
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(27)
	PCALIGN $128
// 0x7a
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(28)
	PCALIGN $128
// 0x7b
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(29)
	PCALIGN $128
// 0x7c
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(30)
	PCALIGN $128
// 0x7d
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(31)
	PCALIGN $128
// 0x7e
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(32)
	PCALIGN $128
// 0x7f
	ROOM
	USEGAS(3, undo3)
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(33)
	PCALIGN $128
// 0x80
	DUP(1)
	PCALIGN $128
// 0x81
	DUP(2)
	PCALIGN $128
// 0x82
	DUP(3)
	PCALIGN $128
// 0x83
	DUP(4)
	PCALIGN $128
// 0x84
	DUP(5)
	PCALIGN $128
// 0x85
	DUP(6)
	PCALIGN $128
// 0x86
	DUP(7)
	PCALIGN $128
// 0x87
	DUP(8)
	PCALIGN $128
// 0x88
	DUP(9)
	PCALIGN $128
// 0x89
	DUP(10)
	PCALIGN $128
// 0x8a
	DUP(11)
	PCALIGN $128
// 0x8b
	DUP(12)
	PCALIGN $128
// 0x8c
	DUP(13)
	PCALIGN $128
// 0x8d
	DUP(14)
	PCALIGN $128
// 0x8e
	DUP(15)
	PCALIGN $128
// 0x8f
	DUP(16)
	PCALIGN $128
// 0x90
	SWAP(1)
	PCALIGN $128
// 0x91
	SWAP(2)
	PCALIGN $128
// 0x92
	SWAP(3)
	PCALIGN $128
// 0x93
	SWAP(4)
	PCALIGN $128
// 0x94
	SWAP(5)
	PCALIGN $128
// 0x95
	SWAP(6)
	PCALIGN $128
// 0x96
	SWAP(7)
	PCALIGN $128
// 0x97
	SWAP(8)
	PCALIGN $128
// 0x98
	SWAP(9)
	PCALIGN $128
// 0x99
	SWAP(10)
	PCALIGN $128
// 0x9a
	SWAP(11)
	PCALIGN $128
// 0x9b
	SWAP(12)
	PCALIGN $128
// 0x9c
	SWAP(13)
	PCALIGN $128
// 0x9d
	SWAP(14)
	PCALIGN $128
// 0x9e
	SWAP(15)
	PCALIGN $128
// 0x9f
	SWAP(16)
	PCALIGN $128
// 0xa0
	B host
	PCALIGN $128
// 0xa1
	B host
	PCALIGN $128
// 0xa2
	B host
	PCALIGN $128
// 0xa3
	B host
	PCALIGN $128
// 0xa4
	B host
	PCALIGN $128
// 0xa5
	B host
	PCALIGN $128
// 0xa6
	B host
	PCALIGN $128
// 0xa7
	B host
	PCALIGN $128
// 0xa8
	B host
	PCALIGN $128
// 0xa9
	B host
	PCALIGN $128
// 0xaa
	B host
	PCALIGN $128
// 0xab
	B host
	PCALIGN $128
// 0xac
	B host
	PCALIGN $128
// 0xad
	B host
	PCALIGN $128
// 0xae
	B host
	PCALIGN $128
// 0xaf
	B host
	PCALIGN $128
// 0xb0
	B host
	PCALIGN $128
// 0xb1
	B host
	PCALIGN $128
// 0xb2
	B host
	PCALIGN $128
// 0xb3
	B host
	PCALIGN $128
// 0xb4
	B host
	PCALIGN $128
// 0xb5
	B host
	PCALIGN $128
// 0xb6
	B host
	PCALIGN $128
// 0xb7
	B host
	PCALIGN $128
// 0xb8
	B host
	PCALIGN $128
// 0xb9
	B host
	PCALIGN $128
// 0xba
	B host
	PCALIGN $128
// 0xbb
	B host
	PCALIGN $128
// 0xbc
	B host
	PCALIGN $128
// 0xbd
	B host
	PCALIGN $128
// 0xbe
	B host
	PCALIGN $128
// 0xbf
	B host
	PCALIGN $128
// 0xc0
	B host
	PCALIGN $128
// 0xc1
	B host
	PCALIGN $128
// 0xc2
	B host
	PCALIGN $128
// 0xc3
	B host
	PCALIGN $128
// 0xc4
	B host
	PCALIGN $128
// 0xc5
	B host
	PCALIGN $128
// 0xc6
	B host
	PCALIGN $128
// 0xc7
	B host
	PCALIGN $128
// 0xc8
	B host
	PCALIGN $128
// 0xc9
	B host
	PCALIGN $128
// 0xca
	B host
	PCALIGN $128
// 0xcb
	B host
	PCALIGN $128
// 0xcc
	B host
	PCALIGN $128
// 0xcd
	B host
	PCALIGN $128
// 0xce
	B host
	PCALIGN $128
// 0xcf
	B host
	PCALIGN $128
// 0xd0
	B host
	PCALIGN $128
// 0xd1
	B host
	PCALIGN $128
// 0xd2
	B host
	PCALIGN $128
// 0xd3
	B host
	PCALIGN $128
// 0xd4
	B host
	PCALIGN $128
// 0xd5
	B host
	PCALIGN $128
// 0xd6
	B host
	PCALIGN $128
// 0xd7
	B host
	PCALIGN $128
// 0xd8
	B host
	PCALIGN $128
// 0xd9
	B host
	PCALIGN $128
// 0xda
	B host
	PCALIGN $128
// 0xdb
	B host
	PCALIGN $128
// 0xdc
	B host
	PCALIGN $128
// 0xdd
	B host
	PCALIGN $128
// 0xde
	B host
	PCALIGN $128
// 0xdf
	B host
	PCALIGN $128
// 0xe0
	B host
	PCALIGN $128
// 0xe1
	B host
	PCALIGN $128
// 0xe2
	B host
	PCALIGN $128
// 0xe3
	B host
	PCALIGN $128
// 0xe4
	B host
	PCALIGN $128
// 0xe5
	B host
	PCALIGN $128
// 0xe6
	B host
	PCALIGN $128
// 0xe7
	B host
	PCALIGN $128
// 0xe8
	B host
	PCALIGN $128
// 0xe9
	B host
	PCALIGN $128
// 0xea
	B host
	PCALIGN $128
// 0xeb
	B host
	PCALIGN $128
// 0xec
	B host
	PCALIGN $128
// 0xed
	B host
	PCALIGN $128
// 0xee
	B host
	PCALIGN $128
// 0xef
	B host
	PCALIGN $128
// 0xf0
	B host
	PCALIGN $128
// 0xf1
	B host
	PCALIGN $128
// 0xf2
	B host
	PCALIGN $128
// 0xf3
	B op_return
	PCALIGN $128
// 0xf4
	B host
	PCALIGN $128
// 0xf5
	B host
	PCALIGN $128
// 0xf6
	B host
	PCALIGN $128
// 0xf7
	B host
	PCALIGN $128
// 0xf8
	B host
	PCALIGN $128
// 0xf9
	B host
	PCALIGN $128
// 0xfa
	B host
	PCALIGN $128
// 0xfb
	B host
	PCALIGN $128
// 0xfc
	B host
	PCALIGN $128
// 0xfd
	B op_revert
	PCALIGN $128
// 0xfe
	MOVD $const_asmInvalid, R0
	B exit
	PCALIGN $128
// 0xff
	B host
	PCALIGN $128
slots_end:
// fused 0
	CMP $1024, R23
	BHS host
	CBZ R23, host
	CMP $13, R22
	BLO host
	ABORT_CHECK
	TOP
	LDP -32(R8), (R4, R5)
	LDP -16(R8), (R6, R7)
	SUB $13, R22
	SUB $1, R23
	ORR R4, R5, R9
	ORR R6, R9
	ORR R7, R9
	CBZ R9, fused_jumpi_not
	MOVD (R25)(R21<<3), R0
	B jump_to
fused_jumpi_not:
	NEXT(4)
	PCALIGN $128
// fused 1
	CMP $1024, R23
	BHS host
	CMP $11, R22
	BLO host
	ABORT_CHECK
	SUB $11, R22
	MOVD (R25)(R21<<3), R0
	B jump_to
	PCALIGN $128
// fused 2
	CBZ R23, host
	CMP $1024, R23
	BHS host
	CMP $16, R22
	BLO host
	ABORT_CHECK
	TOP
	LDP -32(R8), (R4, R5)
	LDP -16(R8), (R6, R7)
	SUB $16, R22
	SUB $1, R23
	ORR R4, R5, R9
	ORR R6, R9
	ORR R7, R9
	CBNZ R9, fused_izjumpi_not
	MOVD (R25)(R21<<3), R0
	B jump_to
fused_izjumpi_not:
	NEXT(5)
	PCALIGN $128
// fused 3
	CMP $1023, R23
	BHS host
	CMP $9, R22
	BLO host
	SUB $9, R22
	TOP
	MOVD (R25)(R21<<3), R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	STP (R0, R1), (R8)
	STP (R2, R3), 16(R8)
	ADD $1, R23
	NEXT(5)
	PCALIGN $128
// fused 4
	CMP $2, R23
	BLO host
	CMP $16, R22
	BLO host
	ABORT_CHECK
	BIN_LOAD
	SUB $16, R22
	SUB $2, R23
	EOR R4, R0
	EOR R5, R1
	EOR R6, R2
	EOR R7, R3
	ORR R1, R0
	ORR R2, R0
	ORR R3, R0
	CBZ R0, fused_4_taken
	NEXT(5)
fused_4_taken:
	MOVD (R25)(R21<<3), R0
	B jump_to
	PCALIGN $128
// fused 5
	CMP $2, R23
	BLO host
	CMP $16, R22
	BLO host
	ABORT_CHECK
	BIN_LOAD
	SUB $16, R22
	SUB $2, R23
	SUBS R4, R0, R9
	SBCS R5, R1, R9
	SBCS R6, R2, R9
	SBCS R7, R3, R9
	BLO fused_5_taken
	NEXT(5)
fused_5_taken:
	MOVD (R25)(R21<<3), R0
	B jump_to
	PCALIGN $128
// fused 6
	CMP $2, R23
	BLO host
	CMP $16, R22
	BLO host
	ABORT_CHECK
	BIN_LOAD
	SUB $16, R22
	SUB $2, R23
	SUBS R0, R4, R9
	SBCS R1, R5, R9
	SBCS R2, R6, R9
	SBCS R3, R7, R9
	BLO fused_6_taken
	NEXT(5)
fused_6_taken:
	MOVD (R25)(R21<<3), R0
	B jump_to
	PCALIGN $128
// fused 7
	CMP $2, R23
	BLO host
	CMP $16, R22
	BLO host
	ABORT_CHECK
	BIN_LOAD
	SUB $16, R22
	SUB $2, R23
	AND R4, R0
	AND R5, R1
	AND R6, R2
	AND R7, R3
	ORR R1, R0
	ORR R2, R0
	ORR R3, R0
	CBNZ R0, fused_7_taken
	NEXT(5)
fused_7_taken:
	MOVD (R25)(R21<<3), R0
	B jump_to
	PCALIGN $128
// fused 8
	CBZ R23, host
	CMP $1024, R23
	BHS host
	CMP $19, R22
	BLO host
	ABORT_CHECK
	TOP
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	SUB $19, R22
	SUB $1, R23
	MOVD (R25)(R21<<3), R4
	EOR R4, R0
	ORR R1, R0
	ORR R2, R0
	ORR R3, R0
	CBZ R0, fused_8_taken
	NEXT(10)
fused_8_taken:
	ADD $6, R21, R9
	MOVD (R25)(R9<<3), R0
	B jump_to
	PCALIGN $128
fused_end:
// END SLOTS

// Pops a, sets the new top to zero.
bin_zero:
	TOP
	STP (ZR, ZR), -64(R8)
	STP (ZR, ZR), -48(R8)
	SUB $1, R23
	NEXT(1)

op_div:
	NEED(2)
	USEGAS(5, undo5)
	BIN_LOAD
	ORR R4, R5, R9
	ORR R6, R9
	ORR R7, R9
	CBZ R9, bin_zero
	BL udiv4
	TOP
	BIN_STORE
	NEXT(1)

op_mod:
	NEED(2)
	USEGAS(5, undo5)
	BIN_LOAD
	ORR R4, R5, R9
	ORR R6, R9
	ORR R7, R9
	CBZ R9, bin_zero
	BL udiv4
	TOP
	MOVD R4, R0
	MOVD R5, R1
	MOVD R6, R2
	MOVD R7, R3
	BIN_STORE
	NEXT(1)

// Negates (r0..r3) when the mask s is -1; s is 0 or -1.
#define NEGIF(r0, r1, r2, r3, s) \
	EOR s, r0; \
	EOR s, r1; \
	EOR s, r2; \
	EOR s, r3; \
	SUBS s, r0; \
	SBCS s, r1; \
	SBCS s, r2; \
	SBC s, r3

// |x| of a two's complement value in (r0..r3); its sign mask in s.
#define ABS(r0, r1, r2, r3, s) \
	ASR $63, r3, s; \
	NEGIF(r0, r1, r2, r3, s)

op_sdiv:
	NEED(2)
	USEGAS(5, undo5)
	BIN_LOAD
	ORR R4, R5, R9
	ORR R6, R9
	ORR R7, R9
	CBZ R9, bin_zero
	ABS(R0, R1, R2, R3, R10)
	ABS(R4, R5, R6, R7, R11)
	EOR R10, R11
	MOVD R11, L_SCR(RSP)
	BL udiv4
	MOVD L_SCR(RSP), R11
	NEGIF(R0, R1, R2, R3, R11)
	TOP
	BIN_STORE
	NEXT(1)

op_smod:
	NEED(2)
	USEGAS(5, undo5)
	BIN_LOAD
	ORR R4, R5, R9
	ORR R6, R9
	ORR R7, R9
	CBZ R9, bin_zero
	ABS(R0, R1, R2, R3, R10)
	ABS(R4, R5, R6, R7, R11)
	MOVD R10, L_SCR(RSP)
	BL udiv4
	MOVD L_SCR(RSP), R11
	NEGIF(R4, R5, R6, R7, R11)
	TOP
	MOVD R4, R0
	MOVD R5, R1
	MOVD R6, R2
	MOVD R7, R3
	BIN_STORE
	NEXT(1)

op_addmod:
	NEED(3)
	USEGAS(8, undo8)
	BIN_LOAD
	LDP -96(R8), (R9, R10)
	LDP -80(R8), (R11, R12)
	ORR R9, R10, R13
	ORR R11, R13
	ORR R12, R13
	CBZ R13, addmod_zero
	ADD $L_D, RSP, R13
	STP (R9, R10), (R13)
	STP (R11, R12), 16(R13)
	ADDS R4, R0
	ADCS R5, R1
	ADCS R6, R2
	ADCS R7, R3
	CSET HS, R4
	ADD $L_U, RSP, R13
	STP (R0, R1), (R13)
	STP (R2, R3), 16(R13)
	MOVD R4, 32(R13)
	MOVD $5, R0
	BL udivrem
	ADD $L_R, RSP, R13
	LDP (R13), (R0, R1)
	LDP 16(R13), (R2, R3)
	TOP
	STP (R0, R1), -96(R8)
	STP (R2, R3), -80(R8)
	SUB $2, R23
	NEXT(1)

addmod_zero:
	STP (ZR, ZR), -96(R8)
	STP (ZR, ZR), -80(R8)
	SUB $2, R23
	NEXT(1)

op_mulmod:
	NEED(3)
	USEGAS(8, undo8)
	BIN_LOAD
	LDP -96(R8), (R9, R10)
	LDP -80(R8), (R11, R12)
	ORR R9, R10, R13
	ORR R11, R13
	ORR R12, R13
	CBZ R13, addmod_zero
	ADD $L_D, RSP, R13
	STP (R9, R10), (R13)
	STP (R11, R12), 16(R13)
	ADD $L_SCR, RSP, R13
	STP (R0, R1), (R13)
	STP (R2, R3), 16(R13)
	STP (R4, R5), 32(R13)
	STP (R6, R7), 48(R13)
	// U[0..7] = a * b, schoolbook.
	ADD $L_U, RSP, R14
	STP (ZR, ZR), (R14)
	STP (ZR, ZR), 16(R14)
	STP (ZR, ZR), 32(R14)
	STP (ZR, ZR), 48(R14)
	MOVD ZR, R0 // i
mulmod_i:
	MOVD (R13)(R0<<3), R1 // a[i]
	MOVD ZR, R2           // j
	MOVD ZR, R3           // carry
	ADD R0<<3, R14, R4    // &U[i]
mulmod_j:
	ADD $32, R13, R5
	MOVD (R5)(R2<<3), R5 // b[j]
	MUL R1, R5, R6
	UMULH R1, R5, R7
	MOVD (R4)(R2<<3), R9
	ADDS R9, R6
	ADC ZR, R7
	ADDS R3, R6
	ADC ZR, R7, R3
	MOVD R6, (R4)(R2<<3)
	ADD $1, R2
	CMP $4, R2
	BLO mulmod_j
	MOVD R3, 32(R4)
	ADD $1, R0
	CMP $4, R0
	BLO mulmod_i
	MOVD $8, R0
	BL udivrem
	ADD $L_R, RSP, R13
	LDP (R13), (R0, R1)
	LDP 16(R13), (R2, R3)
	TOP
	STP (R0, R1), -96(R8)
	STP (R2, R3), -80(R8)
	SUB $2, R23
	NEXT(1)

op_exp:
	NEED(2)
	TOP
	LDP -64(R8), (R4, R5)
	LDP -48(R8), (R6, R7)
	// R9 = bit length of the exponent.
	MOVD $256, R9
	CLZ R7, R10
	CBNZ R7, exp_len
	MOVD $192, R9
	CLZ R6, R10
	CBNZ R6, exp_len
	MOVD $128, R9
	CLZ R5, R10
	CBNZ R5, exp_len
	MOVD $64, R9
	CLZ R4, R10
exp_len:
	SUB R10, R9
	// gas = 10 + bytes * (50 after EIP-158, else 10)
	ADD $7, R9, R10
	LSR $3, R10
	MOVD $10, R11
	MOVD $50, R12
	AND $const_asmFlagExpEIP158, R17, R13
	CMP $0, R13
	CSEL NE, R12, R11, R12
	MUL R12, R10, R10
	ADD $10, R10
	CMP R10, R22
	BLO host
	SUB R10, R22
	// Square and multiply over the exponent bits, LSB first.
	ADD $L_SCR, RSP, R13
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	STP (R0, R1), 32(R13) // base
	STP (R2, R3), 48(R13)
	MOVD $1, R0
	STP (R0, ZR), (R13)   // result
	STP (ZR, ZR), 16(R13)
	ADD $L_D, RSP, R0
	STP (R4, R5), (R0)    // exponent
	STP (R6, R7), 16(R0)
	MOVD R9, L_U(RSP)     // bit length
	MOVD ZR, R16          // bit index
exp_loop:
	MOVD L_U(RSP), R9
	CMP R9, R16
	BHS exp_done
	ADD $L_D, RSP, R0
	LSR $6, R16, R1
	MOVD (R0)(R1<<3), R1
	LSR R16, R1
	TBZ $0, R1, exp_square
	ADD $L_SCR, RSP, R13
	LDP (R13), (R0, R1)
	LDP 16(R13), (R2, R3)
	LDP 32(R13), (R4, R5)
	LDP 48(R13), (R6, R7)
	BL mul256
	ADD $L_SCR, RSP, R13
	STP (R0, R1), (R13)
	STP (R2, R3), 16(R13)
exp_square:
	ADD $1, R16
	MOVD L_U(RSP), R9
	CMP R9, R16
	BHS exp_done
	ADD $L_SCR, RSP, R13
	LDP 32(R13), (R0, R1)
	LDP 48(R13), (R2, R3)
	MOVD R0, R4
	MOVD R1, R5
	MOVD R2, R6
	MOVD R3, R7
	BL mul256
	ADD $L_SCR, RSP, R13
	STP (R0, R1), 32(R13)
	STP (R2, R3), 48(R13)
	B exp_loop
exp_done:
	ADD $L_SCR, RSP, R13
	LDP (R13), (R0, R1)
	LDP 16(R13), (R2, R3)
	TOP
	BIN_STORE
	NEXT(1)

op_signextend:
	NEED(2)
	USEGAS(5, undo5)
	BIN_LOAD
	ORR R1, R2, R9
	ORR R3, R9
	CBNZ R9, signext_keep
	CMP $31, R0
	BHS signext_keep
	LSL $3, R0, R9
	ADD $7, R9           // sign bit index
	LSR $6, R9, R10      // its limb
	AND $63, R9, R11     // its position
	ADD $L_SCR, RSP, R12
	STP (R4, R5), (R12)
	STP (R6, R7), 16(R12)
	MOVD (R12)(R10<<3), R13
	LSR R11, R13, R14
	AND $1, R14
	NEG R14, R14         // sign mask
	MOVD $2, R15
	LSL R11, R15
	SUB $1, R15          // bits at and below the sign bit
	AND R15, R13
	BIC R15, R14, R9
	ORR R9, R13
	MOVD R13, (R12)(R10<<3)
	ADD $1, R10
signext_fill:
	CMP $4, R10
	BHS signext_done
	MOVD R14, (R12)(R10<<3)
	ADD $1, R10
	B signext_fill
signext_done:
	LDP (R12), (R0, R1)
	LDP 16(R12), (R2, R3)
	BIN_STORE
	NEXT(1)
signext_keep:
	SUB $1, R23
	NEXT(1)

op_byte:
	NEED(2)
	USEGAS(3, undo3)
	TOP
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	ORR R1, R2, R9
	ORR R3, R9
	CBNZ R9, bin_zero
	CMP $32, R0
	BHS bin_zero
	SUB $64, R8, R9
	MOVD $31, R10
	SUB R0, R10
	MOVBU (R9)(R10), R0
	BOOL_STORE
	NEXT(1)

// res[j] = (p[j+1] << b) | (p[j] >> (64-b)) for p = &s[3-n].
#define SHLP(hi, lo, dst) \
	LSL R11, hi, dst; \
	LSR $1, lo, R15; \
	LSR R13, R15; \
	ORR R15, dst

// res[j] = (p[j] >> b) | (p[j+1] << (64-b)) for p = &s[n].
#define SHRP(lo, hi, dst) \
	LSR R11, lo, dst; \
	LSL $1, hi, R15; \
	LSL R13, R15; \
	ORR R15, dst

op_shl:
	AND $const_asmFlagShifts, R17, R9
	CBZ R9, host
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	ORR R1, R2, R9
	ORR R3, R9
	CBNZ R9, bin_zero
	CMP $256, R0
	BHS bin_zero
	ADD $L_SCR, RSP, R12
	STP (ZR, ZR), (R12)
	STP (ZR, ZR), 16(R12)
	STP (R4, R5), 32(R12)
	STP (R6, R7), 48(R12)
	LSR $6, R0, R10
	AND $63, R0, R11
	MOVD $63, R13
	SUB R11, R13
	MOVD $3, R14
	SUB R10, R14
	ADD R14<<3, R12, R14
	LDP (R14), (R4, R5)
	LDP 16(R14), (R6, R7)
	MOVD 32(R14), R9
	SHLP(R5, R4, R0)
	SHLP(R6, R5, R1)
	SHLP(R7, R6, R2)
	SHLP(R9, R7, R3)
	BIN_STORE
	NEXT(1)

op_shr:
	AND $const_asmFlagShifts, R17, R9
	CBZ R9, host
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	ORR R1, R2, R9
	ORR R3, R9
	CBNZ R9, bin_zero
	CMP $256, R0
	BHS bin_zero
	MOVD ZR, R12
	B shr_common

op_sar:
	AND $const_asmFlagShifts, R17, R9
	CBZ R9, host
	NEED(2)
	USEGAS(3, undo3)
	BIN_LOAD
	ASR $63, R7, R12 // fill
	ORR R1, R2, R9
	ORR R3, R9
	CBNZ R9, sar_fill
	CMP $256, R0
	BHS sar_fill

shr_common:
	ADD $L_SCR, RSP, R14
	STP (R4, R5), (R14)
	STP (R6, R7), 16(R14)
	STP (R12, R12), 32(R14)
	STP (R12, R12), 48(R14)
	LSR $6, R0, R10
	AND $63, R0, R11
	MOVD $63, R13
	SUB R11, R13
	ADD R10<<3, R14, R14
	LDP (R14), (R4, R5)
	LDP 16(R14), (R6, R7)
	MOVD 32(R14), R9
	SHRP(R4, R5, R0)
	SHRP(R5, R6, R1)
	SHRP(R6, R7, R2)
	SHRP(R7, R9, R3)
	BIN_STORE
	NEXT(1)

sar_fill:
	STP (R12, R12), -64(R8)
	STP (R12, R12), -48(R8)
	SUB $1, R23
	NEXT(1)

op_calldataload:
	NEED(1)
	USEGAS(3, undo3)
	TOP
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	STP (ZR, ZR), -32(R8)
	STP (ZR, ZR), -16(R8)
	MOVD F_INPUTLEN, R9
	ORR R1, R2, R10
	ORR R3, R10
	CBNZ R10, cdl_done
	CMP R9, R0
	BHS cdl_done
	MOVD F_INPUT, R10
	ADD R0, R10
	ADD $32, R0, R11
	CMP R9, R11
	BHI cdl_partial
	LDP (R10), (R0, R1)
	LDP 16(R10), (R2, R3)
	REV R0, R0
	REV R1, R1
	REV R2, R2
	REV R3, R3
	STP (R3, R2), -32(R8)
	STP (R1, R0), -16(R8)
	NEXT(1)
cdl_partial:
	// Byte i of the data is byte 31-i of the little-endian word.
	SUB R0, R9, R11
	SUB $1, R8, R12
cdl_loop:
	MOVBU.P 1(R10), R13
	MOVB.P R13, -1(R12)
	SUBS $1, R11
	BNE cdl_loop
cdl_done:
	NEXT(1)

op_mload:
	NEED(1)
	TOP
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	SMALL(R0, R1, R2, R3)
	ADD $32, R0, R10
	MOVD $3, R11
	BL memexp
	MOVD F_MEM, R9
	ADD R0, R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	REV R0, R0
	REV R1, R1
	REV R2, R2
	REV R3, R3
	STP (R3, R2), -32(R8)
	STP (R1, R0), -16(R8)
	NEXT(1)

op_mstore:
	NEED(2)
	TOP
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	SMALL(R0, R1, R2, R3)
	ADD $32, R0, R10
	MOVD $3, R11
	BL memexp
	LDP -64(R8), (R4, R5)
	LDP -48(R8), (R6, R7)
	REV R4, R4
	REV R5, R5
	REV R6, R6
	REV R7, R7
	MOVD F_MEM, R9
	ADD R0, R9
	STP (R7, R6), (R9)
	STP (R5, R4), 16(R9)
	SUB $2, R23
	NEXT(1)

op_mstore8:
	NEED(2)
	TOP
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	SMALL(R0, R1, R2, R3)
	ADD $1, R0, R10
	MOVD $3, R11
	BL memexp
	MOVD -64(R8), R4
	MOVD F_MEM, R9
	MOVB R4, (R9)(R0)
	SUB $2, R23
	NEXT(1)

// Loads the copy operands: R0 = destination offset (a), R1 = source offset
// low word (b), R4 = b high words or-ed, R2 = length (c). Sets R10 = memory
// end (0 for an empty copy) and R11 = 3 + 3*words(length).
#define COPY_ARGS \
	TOP; \
	LDP -96(R8), (R2, R5); \
	LDP -80(R8), (R6, R7); \
	SMALL(R2, R5, R6, R7); \
	ADD $31, R2, R11; \
	LSR $5, R11; \
	ADD R11<<1, R11; \
	ADD $3, R11; \
	LDP -64(R8), (R1, R5); \
	LDP -48(R8), (R6, R7); \
	ORR R5, R6, R4; \
	ORR R7, R4; \
	MOVD ZR, R10; \
	LDP -32(R8), (R0, R5); \
	LDP -16(R8), (R6, R7)

op_mcopy:
	AND $const_asmFlagMcopy, R17, R9
	CBZ R9, host
	NEED(3)
	COPY_ARGS
	CBZ R2, copy_empty
	SMALL(R0, R5, R6, R7)
	ORR R1>>32, R4, R9
	CBNZ R9, host
	CMP R1, R0
	CSEL HI, R0, R1, R10
	ADD R2, R10
	BL memexp
	MOVD F_MEM, R9
	ADD R9, R0
	ADD R9, R1
	BL memmove
	SUB $3, R23
	NEXT(1)

copy_empty:
	BL memexp
	SUB $3, R23
	NEXT(1)

op_calldatacopy:
	NEED(3)
	COPY_ARGS
	CBZ R2, copy_empty
	SMALL(R0, R5, R6, R7)
	ADD R2, R0, R10
	BL memexp
	MOVD F_INPUT, R12
	MOVD F_INPUTLEN, R13
	B copy_padded

op_codecopy:
	NEED(3)
	COPY_ARGS
	CBZ R2, copy_empty
	SMALL(R0, R5, R6, R7)
	ADD R2, R0, R10
	BL memexp
	MOVD R20, R12
	MOVD F_CODELEN, R13

// Copies R2 bytes from source R12 (length R13) at offset R1 (R4 non-zero if
// it overflows) to memory offset R0, padding with zeros like getData.
copy_padded:
	MOVD F_MEM, R9
	ADD R9, R0
	MOVD R2, R14       // total
	MOVD ZR, R2        // available
	CBNZ R4, copy_zero
	CMP R13, R1
	BHS copy_zero
	SUB R1, R13, R2
	CMP R14, R2
	CSEL HI, R14, R2, R2
	ADD R12, R1
	MOVD R0, R15
	BL memmove
	ADD R2, R15, R0
copy_zero:
	SUB R2, R14, R2
	BL memzero
	SUB $3, R23
	NEXT(1)

op_keccak:
	NEED(2)
	TOP
	LDP -64(R8), (R4, R5)
	LDP -48(R8), (R6, R7)
	SMALL(R4, R5, R6, R7)
	ADD $31, R4, R11
	LSR $5, R11
	MOVD $6, R12
	MUL R12, R11
	ADD $30, R11
	MOVD ZR, R10
	CBZ R4, keccak_mem
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	SMALL(R0, R1, R2, R3)
	ADD R4, R0, R10
keccak_mem:
	BL memexp
	MOVD F_MEM, R9
	ADD R0, R9
	VEOR V0.B16, V0.B16, V0.B16
	VEOR V1.B16, V1.B16, V1.B16
	VEOR V2.B16, V2.B16, V2.B16
	VEOR V3.B16, V3.B16, V3.B16
	VEOR V4.B16, V4.B16, V4.B16
	VEOR V5.B16, V5.B16, V5.B16
	VEOR V6.B16, V6.B16, V6.B16
	VEOR V7.B16, V7.B16, V7.B16
	VEOR V8.B16, V8.B16, V8.B16
	VEOR V9.B16, V9.B16, V9.B16
	VEOR V10.B16, V10.B16, V10.B16
	VEOR V11.B16, V11.B16, V11.B16
	VEOR V12.B16, V12.B16, V12.B16
	VEOR V13.B16, V13.B16, V13.B16
	VEOR V14.B16, V14.B16, V14.B16
	VEOR V15.B16, V15.B16, V15.B16
	VEOR V16.B16, V16.B16, V16.B16
	VEOR V17.B16, V17.B16, V17.B16
	VEOR V18.B16, V18.B16, V18.B16
	VEOR V19.B16, V19.B16, V19.B16
	VEOR V20.B16, V20.B16, V20.B16
	VEOR V21.B16, V21.B16, V21.B16
	VEOR V22.B16, V22.B16, V22.B16
	VEOR V23.B16, V23.B16, V23.B16
	VEOR V24.B16, V24.B16, V24.B16
keccak_block:
	CMP $136, R4
	BLO keccak_last
	ABSORB(R9)
	SUB $136, R4
	BL keccakf
	B keccak_block
keccak_last:
	// Legacy Keccak padding: 0x01 after the data, 0x80 in the last byte.
	ADD $L_PAD, RSP, R0
	STP (ZR, ZR), (R0)
	STP (ZR, ZR), 16(R0)
	STP (ZR, ZR), 32(R0)
	STP (ZR, ZR), 48(R0)
	STP (ZR, ZR), 64(R0)
	STP (ZR, ZR), 80(R0)
	STP (ZR, ZR), 96(R0)
	STP (ZR, ZR), 112(R0)
	MOVD ZR, 128(R0)
	MOVD R9, R1
	MOVD R4, R2
	BL memmove
	ADD $L_PAD, RSP, R0
	MOVBU (R0)(R4), R1
	EOR $0x01, R1
	MOVB R1, (R0)(R4)
	MOVBU 135(R0), R1
	EOR $0x80, R1
	MOVB R1, 135(R0)
	ABSORB(R0)
	BL keccakf
	VMOV V0.D[0], R0
	VMOV V1.D[0], R1
	VMOV V2.D[0], R2
	VMOV V3.D[0], R3
	REV R0, R0
	REV R1, R1
	REV R2, R2
	REV R3, R3
	STP (R3, R2), -64(R8)
	STP (R1, R0), -48(R8)
	SUB $1, R23
	NEXT(1)

// Validates the jump destination in R0-R3, or branches to host.
// A valid destination is a JUMPDEST instruction: its pre-decoded handler is the
// JUMPDEST slot (PUSH data bytes carry the INVALID handler).
#define JUMPDEST_CHECK \
	ORR R1, R2, R9; \
	ORR R3, R9; \
	CBNZ R9, host; \
	MOVD F_CODELEN, R9; \
	CMP R9, R0; \
	BHS host; \
	MOVD (R24)(R0<<3), R9; \
	ADR jumpdest_slot, R10; \
	CMP R10, R9; \
	BNE host


op_jump:
	NEED(1)
	CMP $8, R22
	BLO host
	ABORT_CHECK
	TOP
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	JUMPDEST_CHECK
	SUB $8, R22
	SUB $1, R23
	B jump_to

op_jumpi:
	NEED(2)
	CMP $10, R22
	BLO host
	ABORT_CHECK
	TOP
	LDP -64(R8), (R4, R5)
	LDP -48(R8), (R6, R7)
	ORR R4, R5, R9
	ORR R6, R9
	ORR R7, R9
	CBNZ R9, jumpi_taken
	SUB $10, R22
	SUB $2, R23
	NEXT(1)
jumpi_taken:
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	JUMPDEST_CHECK
	SUB $10, R22
	SUB $2, R23

// R0 is a valid JUMPDEST. Assembly is not asynchronously preemptible, so
// taken jumps yield when the runtime asks for preemption (stackguard0 is
// stackPreempt, the check every Go prologue makes).
jump_to:
	MOVD R0, R21
	MOVD 16(g), R9
	CMN $1314, R9
	BEQ jump_yield
	// The destination is a JUMPDEST: run it here (1 gas) and dispatch the op
	// after it. Out of gas goes to the host at the JUMPDEST, as before.
	SUBS $1, R22
	BLO undo1
	NEXT(1)
jump_yield:
	MOVD R21, F_PC
	MOVD R22, F_GAS
	MOVD R23, F_SP
	CALL ·asmYield(SB)
	B reload

op_return:
	NEED(2)
	MOVD $const_asmReturn, R9
	B return_common

op_revert:
	AND $const_asmFlagRevert, R17, R9
	CBZ R9, host
	NEED(2)
	MOVD $const_asmRevert, R9

return_common:
	MOVD R9, L_LR(RSP) // memexp clobbers registers
	TOP
	LDP -64(R8), (R4, R5)
	LDP -48(R8), (R6, R7)
	SMALL(R4, R5, R6, R7)
	MOVD ZR, R10
	MOVD ZR, R11
	CBZ R4, return_mem
	LDP -32(R8), (R0, R1)
	LDP -16(R8), (R2, R3)
	SMALL(R0, R1, R2, R3)
	ADD R4, R0, R10
return_mem:
	BL memexp
	MOVD R0, F_RET0
	MOVD R4, F_RET1
	SUB $2, R23
	MOVD L_LR(RSP), R0
	B exit

// memexp charges R11 gas plus the expansion of memory to R10 bytes (0 for
// none), expanding and zeroing it. It branches to host, with nothing changed,
// when gas is short or the pooled buffer is too small. Clobbers R12-R16.
memexp:
	MOVD F_MEMLEN, R12
	CMP R12, R10
	BLS memexp_charge
	ADD $31, R10, R13
	LSR $5, R13           // words
	LSL $5, R13, R14      // new length
	MOVD F_MEMCAP, R15
	CMP R15, R14
	BHI host
	MUL R13, R13, R15
	LSR $9, R15
	ADD R13<<1, R15
	ADD R13, R15          // words*3 + words*words/512
	MOVD F_MEMCOST, R16
	SUB R16, R15, R16
	ADD R11, R16
	CMP R16, R22
	BLO host
	SUB R16, R22
	MOVD R15, F_MEMCOST
	MOVD R14, F_MEMLEN
	MOVD F_MEM, R16
	ADD R16, R14
	ADD R16, R12
memexp_zero:
	STP.P (ZR, ZR), 16(R12)
	STP.P (ZR, ZR), 16(R12)
	CMP R14, R12
	BLO memexp_zero
	JMP (R30)
memexp_charge:
	CMP R11, R22
	BLO host
	SUB R11, R22
	JMP (R30)

// memmove copies R2 bytes from R1 to R0; the ranges may overlap.
// Clobbers R3, R5-R7 and advances nothing visible to the caller.
memmove:
	CBZ R2, memmove_ret
	MOVD R0, R5
	MOVD R1, R6
	MOVD R2, R7
	CMP R1, R0
	BLS memmove_fwd
	ADD R2, R1, R3
	CMP R3, R0
	BHS memmove_fwd
	ADD R7, R5
	ADD R7, R6
memmove_back8:
	CMP $8, R7
	BLO memmove_back1
	MOVD.W -8(R6), R3
	MOVD.W R3, -8(R5)
	SUB $8, R7
	B memmove_back8
memmove_back1:
	CBZ R7, memmove_ret
	MOVBU.W -1(R6), R3
	MOVB.W R3, -1(R5)
	SUB $1, R7
	B memmove_back1
memmove_fwd:
	CMP $8, R7
	BLO memmove_fwd1
	MOVD.P 8(R6), R3
	MOVD.P R3, 8(R5)
	SUB $8, R7
	B memmove_fwd
memmove_fwd1:
	CBZ R7, memmove_ret
	MOVBU.P 1(R6), R3
	MOVB.P R3, 1(R5)
	SUB $1, R7
	B memmove_fwd1
memmove_ret:
	JMP (R30)

// memzero clears R2 bytes at R0. Clobbers R5, R7.
memzero:
	MOVD R0, R5
	MOVD R2, R7
memzero8:
	CMP $8, R7
	BLO memzero1
	MOVD.P ZR, 8(R5)
	SUB $8, R7
	B memzero8
memzero1:
	CBZ R7, memzero_ret
	MOVB.P ZR, 1(R5)
	SUB $1, R7
	B memzero1
memzero_ret:
	JMP (R30)

// mul256 sets R0-R3 to the low 256 bits of R0-R3 * R4-R7, like
// uint256.Int.Mul. Clobbers R9-R15.
mul256:
	MUL R0, R4, R9
	UMULH R0, R4, R13
	MUL R1, R4, R10
	UMULH R1, R4, R15
	ADDS R13, R10
	ADC ZR, R15, R13
	MUL R2, R4, R11
	UMULH R2, R4, R15
	ADDS R13, R11
	ADC ZR, R15, R13
	MUL R3, R4, R12
	ADD R13, R12
	MUL R0, R5, R14
	UMULH R0, R5, R15
	ADDS R14, R10
	ADC ZR, R15, R13
	MUL R1, R5, R14
	UMULH R1, R5, R15
	ADDS R13, R14
	ADC ZR, R15
	ADDS R14, R11
	ADC ZR, R15, R13
	MUL R2, R5, R14
	ADD R14, R12
	ADD R13, R12
	MUL R0, R6, R14
	UMULH R0, R6, R15
	ADDS R14, R11
	ADC ZR, R15, R13
	MUL R1, R6, R14
	ADD R14, R12
	ADD R13, R12
	MUL R0, R7, R14
	ADD R14, R12
	MOVD R9, R0
	MOVD R10, R1
	MOVD R11, R2
	MOVD R12, R3
	JMP (R30)

// udiv4 sets R0-R3 to R0-R3 / R4-R7 and R4-R7 to the remainder; the divisor
// is not zero. Clobbers R0-R16.
udiv4:
	ORR R1, R2, R9
	ORR R3, R9
	ORR R5, R9
	ORR R6, R9
	ORR R7, R9
	CBNZ R9, udiv4_wide
	UDIV R4, R0, R9
	MUL R9, R4, R10
	SUB R10, R0, R4
	MOVD R9, R0
	MOVD ZR, R1
	MOVD ZR, R2
	MOVD ZR, R3
	MOVD ZR, R5
	MOVD ZR, R6
	MOVD ZR, R7
	JMP (R30)
udiv4_wide:
	MOVD R30, L_LR(RSP)
	ADD $L_U, RSP, R9
	STP (R0, R1), (R9)
	STP (R2, R3), 16(R9)
	ADD $L_D, RSP, R9
	STP (R4, R5), (R9)
	STP (R6, R7), 16(R9)
	MOVD $4, R0
	BL udivrem
	ADD $L_Q, RSP, R9
	LDP (R9), (R0, R1)
	LDP 16(R9), (R2, R3)
	ADD $L_R, RSP, R9
	LDP (R9), (R4, R5)
	LDP 16(R9), (R6, R7)
	MOVD L_LR(RSP), R30
	JMP (R30)

// udivrem divides the R0-word number at L_U by the non-zero 4-word number at
// L_D (which it normalises in place), leaving 8 quotient words at L_Q and 4
// remainder words at L_R. Knuth algorithm D as in uint256 udivrem.
// Clobbers R0-R16.
udivrem:
	ADD $L_U, RSP, R1
	ADD $L_D, RSP, R2
	ADD $L_Q, RSP, R3
	STP (ZR, ZR), (R3)
	STP (ZR, ZR), 16(R3)
	STP (ZR, ZR), 32(R3)
	STP (ZR, ZR), 48(R3)
	ADD $L_R, RSP, R4
	STP (ZR, ZR), (R4)
	STP (ZR, ZR), 16(R4)
	MOVD $4, R5
udivrem_dlen:
	SUB $1, R5, R6
	MOVD (R2)(R6<<3), R7
	CBNZ R7, udivrem_dlen_done
	MOVD R6, R5
	B udivrem_dlen
udivrem_dlen_done:
	CLZ R7, R6            // shift
udivrem_ulen:
	CBZ R0, udivrem_ulen_done
	SUB $1, R0, R8
	MOVD (R1)(R8<<3), R9
	CBNZ R9, udivrem_ulen_done
	MOVD R8, R0
	B udivrem_ulen
udivrem_ulen_done:
	CMP R5, R0
	BHS udivrem_norm
	// u < d: the remainder is u, which has at most 4 words here.
	LDP (R1), (R7, R8)
	STP (R7, R8), (R4)
	LDP 16(R1), (R7, R8)
	STP (R7, R8), 16(R4)
	JMP (R30)
udivrem_norm:
	MOVD $63, R10
	SUB R6, R10           // 63-shift, so (x>>1)>>R10 is x>>(64-shift) or 0
	SUB $1, R5, R7
udivrem_dn:
	CBZ R7, udivrem_dn_last
	MOVD (R2)(R7<<3), R8
	LSL R6, R8
	SUB $1, R7, R9
	MOVD (R2)(R9<<3), R13
	LSR $1, R13
	LSR R10, R13
	ORR R13, R8
	MOVD R8, (R2)(R7<<3)
	MOVD R9, R7
	B udivrem_dn
udivrem_dn_last:
	MOVD (R2), R8
	LSL R6, R8
	MOVD R8, (R2)
	ADD $L_UN, RSP, R14
	SUB $1, R0, R7
	MOVD (R1)(R7<<3), R8
	LSR $1, R8
	LSR R10, R8
	MOVD R8, (R14)(R0<<3)
udivrem_un:
	CBZ R7, udivrem_un_last
	MOVD (R1)(R7<<3), R8
	LSL R6, R8
	SUB $1, R7, R9
	MOVD (R1)(R9<<3), R13
	LSR $1, R13
	LSR R10, R13
	ORR R13, R8
	MOVD R8, (R14)(R7<<3)
	MOVD R9, R7
	B udivrem_un
udivrem_un_last:
	MOVD (R1), R8
	LSL R6, R8
	MOVD R8, (R14)
	MOVD R14, R1          // R1 = un from here on
	SUB $1, R5, R7
	MOVD (R2)(R7<<3), R12 // dh
	MOVD R10, L_LR+8(RSP)  // spill 63-shift; the reciprocal needs registers

	// R11 = <^dh, ^0> / dh (Hacker's Delight divlu with a normalised divisor,
	// as math/bits.Div64 computes it).
	LSR $32, R12, R13     // yn1
	MOVWU R12, R14        // yn0
	MVN R12, R15          // un32
	UDIV R13, R15, R7     // q1
	MUL R7, R13, R8
	SUB R8, R15, R8       // rhat
udivrem_q1:
	LSR $32, R7, R9
	CBNZ R9, udivrem_q1_dec
	MUL R7, R14, R9
	LSL $32, R8, R10
	ORR $0xffffffff, R10
	CMP R10, R9
	BLS udivrem_q1_done
udivrem_q1_dec:
	SUB $1, R7
	ADD R13, R8
	LSR $32, R8, R9
	CBZ R9, udivrem_q1
udivrem_q1_done:
	LSL $32, R15, R10
	ORR $0xffffffff, R10
	MUL R7, R12, R9
	SUB R9, R10           // un21
	UDIV R13, R10, R9     // q0
	MUL R9, R13, R8
	SUB R8, R10, R8       // rhat
udivrem_q0:
	LSR $32, R9, R15
	CBNZ R15, udivrem_q0_dec
	MUL R9, R14, R15
	LSL $32, R8, R16
	ORR $0xffffffff, R16
	CMP R16, R15
	BLS udivrem_q0_done
udivrem_q0_dec:
	SUB $1, R9
	ADD R13, R8
	LSR $32, R8, R15
	CBZ R15, udivrem_q0
udivrem_q0_done:
	LSL $32, R7
	ADD R9, R7, R11       // reciprocal

	CMP $1, R5
	BNE udivrem_knuth
	// One-word divisor.
	MOVD (R1)(R0<<3), R13
	SUB $1, R0, R14
udivrem_by1:
	MOVD (R1)(R14<<3), R15
	DIV2BY1(R13, R15, R12, R11, R7, R13, R8, R9)
	MOVD R7, (R3)(R14<<3)
	SUBS $1, R14
	BGE udivrem_by1
	LSR R6, R13
	MOVD R13, (R4)
	JMP (R30)

udivrem_knuth:
	SUB $2, R5, R7
	MOVD (R2)(R7<<3), R4  // dl
	SUB R5, R0            // j
udivrem_j:
	ADD R0, R5, R7
	MOVD (R1)(R7<<3), R13 // u2
	SUB $1, R7, R8
	MOVD (R1)(R8<<3), R14 // u1
	SUB $1, R8
	MOVD (R1)(R8<<3), R15 // u0
	MOVD $-1, R9
	CMP R12, R13
	BHS udivrem_mulsub
	DIV2BY1(R13, R14, R12, R11, R9, R10, R7, R8)
	UMULH R9, R4, R7
	MUL R9, R4, R8
	CMP R10, R7
	BHI udivrem_dec
	BNE udivrem_mulsub
	CMP R15, R8
	BLS udivrem_mulsub
udivrem_dec:
	SUB $1, R9
udivrem_mulsub:
	// un[j:j+dLen] -= qhat * dn; R10 = borrow.
	MOVD ZR, R10
	MOVD ZR, R7
	ADD R0<<3, R1, R8
udivrem_ms:
	MOVD (R8)(R7<<3), R14
	SUBS R10, R14
	CSET LO, R15
	MOVD (R2)(R7<<3), R16
	MUL R9, R16, R13
	UMULH R9, R16, R16
	SUBS R13, R14
	CSET LO, R13
	MOVD R14, (R8)(R7<<3)
	ADD R15, R16
	ADD R13, R16, R10
	ADD $1, R7
	CMP R5, R7
	BLO udivrem_ms
	ADD R0, R5, R7
	MOVD (R1)(R7<<3), R13
	SUBS R10, R13, R14
	MOVD R14, (R1)(R7<<3)
	BHS udivrem_store
	// Too much subtracted: add dn back.
	SUB $1, R9
	MOVD ZR, R10
	MOVD ZR, R7
udivrem_add:
	MOVD (R8)(R7<<3), R14
	MOVD (R2)(R7<<3), R15
	ADDS R10, R14
	CSET HS, R13
	ADDS R15, R14
	ADC ZR, R13, R10
	MOVD R14, (R8)(R7<<3)
	ADD $1, R7
	CMP R5, R7
	BLO udivrem_add
	ADD R0, R5, R7
	MOVD (R1)(R7<<3), R14
	ADD R10, R14
	MOVD R14, (R1)(R7<<3)
udivrem_store:
	MOVD R9, (R3)(R0<<3)
	SUBS $1, R0
	BGE udivrem_j
	// rem[i] = un[i]>>shift | un[i+1]<<(64-shift)
	MOVD L_LR+8(RSP), R10
	ADD $L_R, RSP, R4
	MOVD ZR, R7
	SUB $1, R5, R9
udivrem_rem:
	CMP R9, R7
	BHS udivrem_rem_last
	MOVD (R1)(R7<<3), R13
	LSR R6, R13
	ADD $1, R7, R8
	MOVD (R1)(R8<<3), R14
	LSL $1, R14
	LSL R10, R14
	ORR R14, R13
	MOVD R13, (R4)(R7<<3)
	MOVD R8, R7
	B udivrem_rem
udivrem_rem_last:
	MOVD (R1)(R7<<3), R13
	LSR R6, R13
	MOVD R13, (R4)(R7<<3)
	JMP (R30)

// keccakf permutes the state in V0-V24 (lane i in Vi.D[0]). The round body is
// the FEAT_SHA3 sequence of crypto/internal/fips140/sha3/sha3_arm64.s.
// Clobbers R1, R2, V25-V31.
keccakf:
	MOVD $asmevm_rc<>(SB), R1
	MOVD $24, R2
keccakf_round:
	// theta
	VEOR3 V20.B16, V15.B16, V10.B16, V25.B16
	VEOR3 V21.B16, V16.B16, V11.B16, V26.B16
	VEOR3 V22.B16, V17.B16, V12.B16, V27.B16
	VEOR3 V23.B16, V18.B16, V13.B16, V28.B16
	VEOR3 V24.B16, V19.B16, V14.B16, V29.B16
	VEOR3 V25.B16, V5.B16, V0.B16, V25.B16
	VEOR3 V26.B16, V6.B16, V1.B16, V26.B16
	VEOR3 V27.B16, V7.B16, V2.B16, V27.B16
	VEOR3 V28.B16, V8.B16, V3.B16, V28.B16
	VEOR3 V29.B16, V9.B16, V4.B16, V29.B16

	VRAX1 V27.D2, V25.D2, V30.D2
	VRAX1 V28.D2, V26.D2, V31.D2
	VRAX1 V29.D2, V27.D2, V27.D2
	VRAX1 V25.D2, V28.D2, V28.D2
	VRAX1 V26.D2, V29.D2, V29.D2

	// theta, rho and pi
	VEOR V29.B16, V0.B16, V0.B16

	VXAR $63, V30.D2, V1.D2, V25.D2

	VXAR $20, V30.D2, V6.D2, V1.D2
	VXAR $44, V28.D2, V9.D2, V6.D2
	VXAR $3, V31.D2, V22.D2, V9.D2
	VXAR $25, V28.D2, V14.D2, V22.D2
	VXAR $46, V29.D2, V20.D2, V14.D2

	VXAR $2, V31.D2, V2.D2, V26.D2

	VXAR $21, V31.D2, V12.D2, V2.D2
	VXAR $39, V27.D2, V13.D2, V12.D2
	VXAR $56, V28.D2, V19.D2, V13.D2
	VXAR $8, V27.D2, V23.D2, V19.D2
	VXAR $23, V29.D2, V15.D2, V23.D2

	VXAR $37, V28.D2, V4.D2, V15.D2

	VXAR $50, V28.D2, V24.D2, V28.D2
	VXAR $62, V30.D2, V21.D2, V24.D2
	VXAR $9, V27.D2, V8.D2, V8.D2
	VXAR $19, V30.D2, V16.D2, V4.D2
	VXAR $28, V29.D2, V5.D2, V16.D2

	VXAR $36, V27.D2, V3.D2, V5.D2

	VXAR $43, V27.D2, V18.D2, V27.D2
	VXAR $49, V31.D2, V17.D2, V3.D2
	VXAR $54, V30.D2, V11.D2, V30.D2
	VXAR $58, V31.D2, V7.D2, V31.D2
	VXAR $61, V29.D2, V10.D2, V29.D2

	// chi and iota
	VBCAX V8.B16, V22.B16, V26.B16, V20.B16
	VBCAX V22.B16, V23.B16, V8.B16, V21.B16
	VBCAX V23.B16, V24.B16, V22.B16, V22.B16
	VBCAX V24.B16, V26.B16, V23.B16, V23.B16
	VBCAX V26.B16, V8.B16, V24.B16, V24.B16

	VLD1R.P 8(R1), [V26.D2]

	VBCAX V3.B16, V19.B16, V30.B16, V17.B16
	VBCAX V19.B16, V15.B16, V3.B16, V18.B16
	VBCAX V15.B16, V16.B16, V19.B16, V19.B16
	VBCAX V16.B16, V30.B16, V15.B16, V15.B16
	VBCAX V30.B16, V3.B16, V16.B16, V16.B16

	VBCAX V31.B16, V12.B16, V25.B16, V10.B16
	VBCAX V12.B16, V13.B16, V31.B16, V11.B16
	VBCAX V13.B16, V14.B16, V12.B16, V12.B16
	VBCAX V14.B16, V25.B16, V13.B16, V13.B16
	VBCAX V25.B16, V31.B16, V14.B16, V14.B16

	VBCAX V4.B16, V9.B16, V29.B16, V7.B16
	VBCAX V9.B16, V5.B16, V4.B16, V8.B16
	VBCAX V5.B16, V6.B16, V9.B16, V9.B16
	VBCAX V6.B16, V29.B16, V5.B16, V5.B16
	VBCAX V29.B16, V4.B16, V6.B16, V6.B16

	VBCAX V28.B16, V0.B16, V27.B16, V3.B16
	VBCAX V0.B16, V1.B16, V28.B16, V4.B16

	VBCAX V1.B16, V2.B16, V0.B16, V0.B16

	VBCAX V2.B16, V27.B16, V1.B16, V1.B16
	VBCAX V27.B16, V28.B16, V2.B16, V2.B16

	VEOR V26.B16, V0.B16, V0.B16

	SUB $1, R2
	CBNZ R2, keccakf_round
	JMP (R30)

DATA asmevm_rc<>+0x00(SB)/8, $0x0000000000000001
DATA asmevm_rc<>+0x08(SB)/8, $0x0000000000008082
DATA asmevm_rc<>+0x10(SB)/8, $0x800000000000808a
DATA asmevm_rc<>+0x18(SB)/8, $0x8000000080008000
DATA asmevm_rc<>+0x20(SB)/8, $0x000000000000808b
DATA asmevm_rc<>+0x28(SB)/8, $0x0000000080000001
DATA asmevm_rc<>+0x30(SB)/8, $0x8000000080008081
DATA asmevm_rc<>+0x38(SB)/8, $0x8000000000008009
DATA asmevm_rc<>+0x40(SB)/8, $0x000000000000008a
DATA asmevm_rc<>+0x48(SB)/8, $0x0000000000000088
DATA asmevm_rc<>+0x50(SB)/8, $0x0000000080008009
DATA asmevm_rc<>+0x58(SB)/8, $0x000000008000000a
DATA asmevm_rc<>+0x60(SB)/8, $0x000000008000808b
DATA asmevm_rc<>+0x68(SB)/8, $0x800000000000008b
DATA asmevm_rc<>+0x70(SB)/8, $0x8000000000008089
DATA asmevm_rc<>+0x78(SB)/8, $0x8000000000008003
DATA asmevm_rc<>+0x80(SB)/8, $0x8000000000008002
DATA asmevm_rc<>+0x88(SB)/8, $0x8000000000000080
DATA asmevm_rc<>+0x90(SB)/8, $0x000000000000800a
DATA asmevm_rc<>+0x98(SB)/8, $0x800000008000000a
DATA asmevm_rc<>+0xA0(SB)/8, $0x8000000080008081
DATA asmevm_rc<>+0xA8(SB)/8, $0x8000000000008080
DATA asmevm_rc<>+0xB0(SB)/8, $0x0000000080000001
DATA asmevm_rc<>+0xB8(SB)/8, $0x8000000080008008
GLOBL asmevm_rc<>(SB), NOPTR|RODATA, $192
