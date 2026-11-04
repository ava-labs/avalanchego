//go:build asmcore

#include "go_asm.h"
#include "textflag.h"
#include "funcdata.h"

// amd64 port of core_arm64.s. One asmRun call executes a whole EVM frame.
// Registers that live across operations:
//
//	DI   *asmFrame
//	SI   pc (index into code and into the pre-decoded stream)
//	BX   gas left
//	R13  stack top: address one past the top item; item i is at R10 + 32*i,
//	     limbs little-endian, so the top item is at -32(R13)
//	R12  pre-decoded handlers (asmFrame.handlers): handler address per pc
//	R11  pre-decoded PUSH immediates (asmFrame.args), per pc
//	R10  stack base (1024 words, owned by the frame)
//
// AX, CX, DX, R8, R9, R15 are scratch. R14 is Go's g and is never written: the
// preemption check reads g.stackguard0 through it. BP (frame pointer) and SP
// are not used for state. udivrem and MULMOD also use R10-R12 as scratch and
// reload them from the frame before they return to a handler.
//
// Operations that need the Go host, and every failing check (gas, stack,
// memory beyond the pooled buffer, invalid jump), jump to host with the
// operation not yet executed: pc unchanged, no gas charged, stack unchanged.
// host stores pc, gas and sp in the frame and CALLs asmHost (ABI0), which runs
// the Go operation with all its checks. All live registers are reloaded from
// the frame afterwards; no pointer into the goroutine stack is kept across a
// call, and the locals hold no pointers (NO_LOCAL_POINTERS).
//
// There are no internal subroutines (a CALL to a label would move SP under
// the assembler's frame bookkeeping, and RET is the function epilogue):
// memory expansion and 256-bit multiplication are macros, udivrem is a shared
// block that returns through a small continuation switch (L_RET).

#define F_CODE asmFrame_code(DI)
#define F_CODELEN asmFrame_codeLen(DI)
#define F_STACK asmFrame_stackBase(DI)
#define F_MEM asmFrame_mem(DI)
#define F_MEMLEN asmFrame_memLen(DI)
#define F_MEMCAP asmFrame_memCap(DI)
#define F_MEMCOST asmFrame_memCost(DI)
#define F_INPUT asmFrame_input(DI)
#define F_INPUTLEN asmFrame_inputLen(DI)
#define F_FLAGS asmFrame_flags(DI)
#define F_ABORT asmFrame_abort(DI)
#define F_PC asmFrame_pc(DI)
#define F_GAS asmFrame_gas(DI)
#define F_SP asmFrame_sp(DI)
#define F_STATUS asmFrame_status(DI)
#define F_RET0 asmFrame_ret0(DI)
#define F_RET1 asmFrame_ret1(DI)
#define F_HANDLERS asmFrame_handlers(DI)
#define F_ARGS asmFrame_args(DI)
#define F_KSTATE asmFrame_kstate(DI)
#define F_KPAD asmFrame_kpad(DI)
#define F_RETLEN asmFrame_retLen(DI)
#define F_TIME asmFrame_time(DI)
#define F_KCACHE asmFrame_kcache(DI)
#define F_STAB asmFrame_stab(DI)
#define F_SELFHASH asmFrame_selfHash(DI)

// Locals (hardware SP offsets). 0(SP) is the outgoing argument of calls.
#define L_RET 8     // udivrem continuation
#define L_T1 16     // temporaries that live across a block
#define L_T2 24
#define L_SCR 32    // 8 words: shift buffer
#define L_U 96      // 9 words: dividend
#define L_UN 168    // 9 words: normalised dividend
#define L_D 240     // 4 words: divisor
#define L_Q 272     // 8 words: quotient
#define L_R 336     // 4 words: remainder
#define L_E 368     // 16 words: EXP result, base, square, exponent
#define L_F 496     // the current frame (*asmFrame): native CALL switches it

// call +0; pop R9: R9 = address of the pop. BYTE-encoded so the assembler
// does not track the SP change (it rejects CALL to a label).
#define GETPC_R9 BYTE $0xE8; LONG $0; BYTE $0x41; BYTE $0x59

// The stream is padded with STOP handlers past the end of code, so there is no
// bounds check here.
#define NEXT(n) \
	ADDQ $n, SI; \
	JMP (R12)(SI*8)

// AX is a valid JUMPDEST. The destination is a JUMPDEST: run it here (1 gas)
// and dispatch the op after it; out of gas goes to the host at the JUMPDEST.
// Every jumping handler expands its own copy, so each has its own dispatch
// branch.
//
// Assembly is not asynchronously preemptible. By default (trading target) a
// native frame runs until it exits or calls the host, which delays GC and the
// scheduler for that long, and EVM.Cancel is not checked. Built with the
// asmevm_preempt tag (see preempt_amd64.go), taken jumps yield when the
// runtime asks for preemption (stackguard0 is stackPreempt, the check every Go
// prologue makes) and stop at EVM.Cancel.
#ifdef ASMEVM_PREEMPT
#define JUMP_TO(next) \
	MOVQ AX, SI; \
	CMPQ 16(R14), $-1314; \
	JEQ jump_yield; \
	SUBQ $1, BX; \
	JCS undo1; \
	next(1)
#else
#define JUMP_TO(next) \
	MOVQ AX, SI; \
	SUBQ $1, BX; \
	JCS undo1; \
	next(1)
#endif

// Per-site dispatch HNEXT_<site>(n) (generated): like NEXT, through the site's
// out-of-line tail that first tests the most frequent targets of that site
// with compares and direct jumps (well predicted conditional branches), then
// falls back to the indirect jump.
// BEGIN HINTS (generated)
#define HNEXT_00(n) NEXT(n)
#define HNEXT_01(n) ADDQ $n, SI; JMP disp_01
#define HNEXT_02(n) ADDQ $n, SI; JMP disp_02
#define HNEXT_03(n) ADDQ $n, SI; JMP disp_03
#define HNEXT_04(n) ADDQ $n, SI; JMP disp_04
#define HNEXT_05(n) ADDQ $n, SI; JMP disp_05
#define HNEXT_06(n) ADDQ $n, SI; JMP disp_06
#define HNEXT_07(n) ADDQ $n, SI; JMP disp_07
#define HNEXT_08(n) ADDQ $n, SI; JMP disp_08
#define HNEXT_09(n) ADDQ $n, SI; JMP disp_09
#define HNEXT_0a(n) ADDQ $n, SI; JMP disp_0a
#define HNEXT_0b(n) ADDQ $n, SI; JMP disp_0b
#define HNEXT_0c(n) NEXT(n)
#define HNEXT_0d(n) NEXT(n)
#define HNEXT_0e(n) NEXT(n)
#define HNEXT_0f(n) NEXT(n)
#define HNEXT_10(n) ADDQ $n, SI; JMP disp_10
#define HNEXT_11(n) ADDQ $n, SI; JMP disp_11
#define HNEXT_12(n) ADDQ $n, SI; JMP disp_12
#define HNEXT_13(n) ADDQ $n, SI; JMP disp_13
#define HNEXT_14(n) ADDQ $n, SI; JMP disp_14
#define HNEXT_15(n) ADDQ $n, SI; JMP disp_15
#define HNEXT_16(n) ADDQ $n, SI; JMP disp_16
#define HNEXT_17(n) ADDQ $n, SI; JMP disp_17
#define HNEXT_18(n) ADDQ $n, SI; JMP disp_18
#define HNEXT_19(n) ADDQ $n, SI; JMP disp_19
#define HNEXT_1a(n) ADDQ $n, SI; JMP disp_1a
#define HNEXT_1b(n) ADDQ $n, SI; JMP disp_1b
#define HNEXT_1c(n) ADDQ $n, SI; JMP disp_1c
#define HNEXT_1d(n) ADDQ $n, SI; JMP disp_1d
#define HNEXT_1e(n) NEXT(n)
#define HNEXT_1f(n) NEXT(n)
#define HNEXT_20(n) ADDQ $n, SI; JMP disp_20
#define HNEXT_21(n) NEXT(n)
#define HNEXT_22(n) NEXT(n)
#define HNEXT_23(n) NEXT(n)
#define HNEXT_24(n) NEXT(n)
#define HNEXT_25(n) NEXT(n)
#define HNEXT_26(n) NEXT(n)
#define HNEXT_27(n) NEXT(n)
#define HNEXT_28(n) NEXT(n)
#define HNEXT_29(n) NEXT(n)
#define HNEXT_2a(n) NEXT(n)
#define HNEXT_2b(n) NEXT(n)
#define HNEXT_2c(n) NEXT(n)
#define HNEXT_2d(n) NEXT(n)
#define HNEXT_2e(n) NEXT(n)
#define HNEXT_2f(n) NEXT(n)
#define HNEXT_30(n) ADDQ $n, SI; JMP disp_30
#define HNEXT_31(n) ADDQ $n, SI; JMP disp_31
#define HNEXT_32(n) ADDQ $n, SI; JMP disp_32
#define HNEXT_33(n) ADDQ $n, SI; JMP disp_33
#define HNEXT_34(n) ADDQ $n, SI; JMP disp_34
#define HNEXT_35(n) ADDQ $n, SI; JMP disp_35
#define HNEXT_36(n) ADDQ $n, SI; JMP disp_36
#define HNEXT_37(n) ADDQ $n, SI; JMP disp_37
#define HNEXT_38(n) ADDQ $n, SI; JMP disp_38
#define HNEXT_39(n) ADDQ $n, SI; JMP disp_39
#define HNEXT_3a(n) ADDQ $n, SI; JMP disp_3a
#define HNEXT_3b(n) ADDQ $n, SI; JMP disp_3b
#define HNEXT_3c(n) ADDQ $n, SI; JMP disp_3c
#define HNEXT_3d(n) ADDQ $n, SI; JMP disp_3d
#define HNEXT_3e(n) ADDQ $n, SI; JMP disp_3e
#define HNEXT_3f(n) ADDQ $n, SI; JMP disp_3f
#define HNEXT_40(n) ADDQ $n, SI; JMP disp_40
#define HNEXT_41(n) NEXT(n)
#define HNEXT_42(n) ADDQ $n, SI; JMP disp_42
#define HNEXT_43(n) ADDQ $n, SI; JMP disp_43
#define HNEXT_44(n) ADDQ $n, SI; JMP disp_44
#define HNEXT_45(n) NEXT(n)
#define HNEXT_46(n) ADDQ $n, SI; JMP disp_46
#define HNEXT_47(n) ADDQ $n, SI; JMP disp_47
#define HNEXT_48(n) ADDQ $n, SI; JMP disp_48
#define HNEXT_49(n) NEXT(n)
#define HNEXT_4a(n) NEXT(n)
#define HNEXT_4b(n) NEXT(n)
#define HNEXT_4c(n) NEXT(n)
#define HNEXT_4d(n) NEXT(n)
#define HNEXT_4e(n) NEXT(n)
#define HNEXT_4f(n) NEXT(n)
#define HNEXT_50(n) ADDQ $n, SI; JMP disp_50
#define HNEXT_51(n) ADDQ $n, SI; JMP disp_51
#define HNEXT_52(n) ADDQ $n, SI; JMP disp_52
#define HNEXT_53(n) ADDQ $n, SI; JMP disp_53
#define HNEXT_54(n) ADDQ $n, SI; JMP disp_54
#define HNEXT_55(n) ADDQ $n, SI; JMP disp_55
#define HNEXT_56(n) NEXT(n)
#define HNEXT_57(n) ADDQ $n, SI; JMP disp_57
#define HNEXT_58(n) ADDQ $n, SI; JMP disp_58
#define HNEXT_59(n) ADDQ $n, SI; JMP disp_59
#define HNEXT_5a(n) ADDQ $n, SI; JMP disp_5a
#define HNEXT_5b(n) ADDQ $n, SI; JMP disp_5b
#define HNEXT_5c(n) ADDQ $n, SI; JMP disp_5c
#define HNEXT_5d(n) ADDQ $n, SI; JMP disp_5d
#define HNEXT_5e(n) ADDQ $n, SI; JMP disp_5e
#define HNEXT_5f(n) ADDQ $n, SI; JMP disp_5f
#define HNEXT_60(n) ADDQ $n, SI; JMP disp_60
#define HNEXT_61(n) ADDQ $n, SI; JMP disp_61
#define HNEXT_62(n) ADDQ $n, SI; JMP disp_62
#define HNEXT_63(n) ADDQ $n, SI; JMP disp_63
#define HNEXT_64(n) ADDQ $n, SI; JMP disp_64
#define HNEXT_65(n) ADDQ $n, SI; JMP disp_65
#define HNEXT_66(n) ADDQ $n, SI; JMP disp_66
#define HNEXT_67(n) ADDQ $n, SI; JMP disp_67
#define HNEXT_68(n) ADDQ $n, SI; JMP disp_68
#define HNEXT_69(n) ADDQ $n, SI; JMP disp_69
#define HNEXT_6a(n) ADDQ $n, SI; JMP disp_6a
#define HNEXT_6b(n) ADDQ $n, SI; JMP disp_6b
#define HNEXT_6c(n) ADDQ $n, SI; JMP disp_6c
#define HNEXT_6d(n) ADDQ $n, SI; JMP disp_6d
#define HNEXT_6e(n) ADDQ $n, SI; JMP disp_6e
#define HNEXT_6f(n) ADDQ $n, SI; JMP disp_6f
#define HNEXT_70(n) ADDQ $n, SI; JMP disp_70
#define HNEXT_71(n) ADDQ $n, SI; JMP disp_71
#define HNEXT_72(n) ADDQ $n, SI; JMP disp_72
#define HNEXT_73(n) ADDQ $n, SI; JMP disp_73
#define HNEXT_74(n) ADDQ $n, SI; JMP disp_74
#define HNEXT_75(n) ADDQ $n, SI; JMP disp_75
#define HNEXT_76(n) ADDQ $n, SI; JMP disp_76
#define HNEXT_77(n) ADDQ $n, SI; JMP disp_77
#define HNEXT_78(n) ADDQ $n, SI; JMP disp_78
#define HNEXT_79(n) ADDQ $n, SI; JMP disp_79
#define HNEXT_7a(n) ADDQ $n, SI; JMP disp_7a
#define HNEXT_7b(n) ADDQ $n, SI; JMP disp_7b
#define HNEXT_7c(n) ADDQ $n, SI; JMP disp_7c
#define HNEXT_7d(n) ADDQ $n, SI; JMP disp_7d
#define HNEXT_7e(n) ADDQ $n, SI; JMP disp_7e
#define HNEXT_7f(n) ADDQ $n, SI; JMP disp_7f
#define HNEXT_80(n) ADDQ $n, SI; JMP disp_80
#define HNEXT_81(n) ADDQ $n, SI; JMP disp_81
#define HNEXT_82(n) ADDQ $n, SI; JMP disp_82
#define HNEXT_83(n) ADDQ $n, SI; JMP disp_83
#define HNEXT_84(n) ADDQ $n, SI; JMP disp_84
#define HNEXT_85(n) ADDQ $n, SI; JMP disp_85
#define HNEXT_86(n) ADDQ $n, SI; JMP disp_86
#define HNEXT_87(n) ADDQ $n, SI; JMP disp_87
#define HNEXT_88(n) ADDQ $n, SI; JMP disp_88
#define HNEXT_89(n) ADDQ $n, SI; JMP disp_89
#define HNEXT_8a(n) ADDQ $n, SI; JMP disp_8a
#define HNEXT_8b(n) ADDQ $n, SI; JMP disp_8b
#define HNEXT_8c(n) ADDQ $n, SI; JMP disp_8c
#define HNEXT_8d(n) ADDQ $n, SI; JMP disp_8d
#define HNEXT_8e(n) ADDQ $n, SI; JMP disp_8e
#define HNEXT_8f(n) ADDQ $n, SI; JMP disp_8f
#define HNEXT_90(n) ADDQ $n, SI; JMP disp_90
#define HNEXT_91(n) ADDQ $n, SI; JMP disp_91
#define HNEXT_92(n) ADDQ $n, SI; JMP disp_92
#define HNEXT_93(n) ADDQ $n, SI; JMP disp_93
#define HNEXT_94(n) ADDQ $n, SI; JMP disp_94
#define HNEXT_95(n) ADDQ $n, SI; JMP disp_95
#define HNEXT_96(n) ADDQ $n, SI; JMP disp_96
#define HNEXT_97(n) ADDQ $n, SI; JMP disp_97
#define HNEXT_98(n) ADDQ $n, SI; JMP disp_98
#define HNEXT_99(n) ADDQ $n, SI; JMP disp_99
#define HNEXT_9a(n) ADDQ $n, SI; JMP disp_9a
#define HNEXT_9b(n) ADDQ $n, SI; JMP disp_9b
#define HNEXT_9c(n) ADDQ $n, SI; JMP disp_9c
#define HNEXT_9d(n) ADDQ $n, SI; JMP disp_9d
#define HNEXT_9e(n) ADDQ $n, SI; JMP disp_9e
#define HNEXT_9f(n) ADDQ $n, SI; JMP disp_9f
#define HNEXT_a0(n) NEXT(n)
#define HNEXT_a1(n) ADDQ $n, SI; JMP disp_a1
#define HNEXT_a2(n) ADDQ $n, SI; JMP disp_a2
#define HNEXT_a3(n) ADDQ $n, SI; JMP disp_a3
#define HNEXT_a4(n) ADDQ $n, SI; JMP disp_a4
#define HNEXT_a5(n) NEXT(n)
#define HNEXT_a6(n) NEXT(n)
#define HNEXT_a7(n) NEXT(n)
#define HNEXT_a8(n) NEXT(n)
#define HNEXT_a9(n) NEXT(n)
#define HNEXT_aa(n) NEXT(n)
#define HNEXT_ab(n) NEXT(n)
#define HNEXT_ac(n) NEXT(n)
#define HNEXT_ad(n) NEXT(n)
#define HNEXT_ae(n) NEXT(n)
#define HNEXT_af(n) NEXT(n)
#define HNEXT_b0(n) NEXT(n)
#define HNEXT_b1(n) NEXT(n)
#define HNEXT_b2(n) NEXT(n)
#define HNEXT_b3(n) NEXT(n)
#define HNEXT_b4(n) NEXT(n)
#define HNEXT_b5(n) NEXT(n)
#define HNEXT_b6(n) NEXT(n)
#define HNEXT_b7(n) NEXT(n)
#define HNEXT_b8(n) NEXT(n)
#define HNEXT_b9(n) NEXT(n)
#define HNEXT_ba(n) NEXT(n)
#define HNEXT_bb(n) NEXT(n)
#define HNEXT_bc(n) NEXT(n)
#define HNEXT_bd(n) NEXT(n)
#define HNEXT_be(n) NEXT(n)
#define HNEXT_bf(n) NEXT(n)
#define HNEXT_c0(n) NEXT(n)
#define HNEXT_c1(n) NEXT(n)
#define HNEXT_c2(n) NEXT(n)
#define HNEXT_c3(n) NEXT(n)
#define HNEXT_c4(n) NEXT(n)
#define HNEXT_c5(n) NEXT(n)
#define HNEXT_c6(n) NEXT(n)
#define HNEXT_c7(n) NEXT(n)
#define HNEXT_c8(n) NEXT(n)
#define HNEXT_c9(n) NEXT(n)
#define HNEXT_ca(n) NEXT(n)
#define HNEXT_cb(n) NEXT(n)
#define HNEXT_cc(n) NEXT(n)
#define HNEXT_cd(n) NEXT(n)
#define HNEXT_ce(n) NEXT(n)
#define HNEXT_cf(n) NEXT(n)
#define HNEXT_d0(n) NEXT(n)
#define HNEXT_d1(n) NEXT(n)
#define HNEXT_d2(n) NEXT(n)
#define HNEXT_d3(n) NEXT(n)
#define HNEXT_d4(n) NEXT(n)
#define HNEXT_d5(n) NEXT(n)
#define HNEXT_d6(n) NEXT(n)
#define HNEXT_d7(n) NEXT(n)
#define HNEXT_d8(n) NEXT(n)
#define HNEXT_d9(n) NEXT(n)
#define HNEXT_da(n) NEXT(n)
#define HNEXT_db(n) NEXT(n)
#define HNEXT_dc(n) NEXT(n)
#define HNEXT_dd(n) NEXT(n)
#define HNEXT_de(n) NEXT(n)
#define HNEXT_df(n) NEXT(n)
#define HNEXT_e0(n) NEXT(n)
#define HNEXT_e1(n) NEXT(n)
#define HNEXT_e2(n) NEXT(n)
#define HNEXT_e3(n) NEXT(n)
#define HNEXT_e4(n) NEXT(n)
#define HNEXT_e5(n) NEXT(n)
#define HNEXT_e6(n) NEXT(n)
#define HNEXT_e7(n) NEXT(n)
#define HNEXT_e8(n) NEXT(n)
#define HNEXT_e9(n) NEXT(n)
#define HNEXT_ea(n) NEXT(n)
#define HNEXT_eb(n) NEXT(n)
#define HNEXT_ec(n) NEXT(n)
#define HNEXT_ed(n) NEXT(n)
#define HNEXT_ee(n) NEXT(n)
#define HNEXT_ef(n) NEXT(n)
#define HNEXT_f0(n) ADDQ $n, SI; JMP disp_f0
#define HNEXT_f1(n) ADDQ $n, SI; JMP disp_f1
#define HNEXT_f2(n) NEXT(n)
#define HNEXT_f3(n) NEXT(n)
#define HNEXT_f4(n) ADDQ $n, SI; JMP disp_f4
#define HNEXT_f5(n) ADDQ $n, SI; JMP disp_f5
#define HNEXT_f6(n) NEXT(n)
#define HNEXT_f7(n) NEXT(n)
#define HNEXT_f8(n) NEXT(n)
#define HNEXT_f9(n) NEXT(n)
#define HNEXT_fa(n) ADDQ $n, SI; JMP disp_fa
#define HNEXT_fb(n) NEXT(n)
#define HNEXT_fc(n) NEXT(n)
#define HNEXT_fd(n) NEXT(n)
#define HNEXT_fe(n) NEXT(n)
#define HNEXT_ff(n) NEXT(n)
#define HNEXT_F0(n) ADDQ $n, SI; JMP disp_F0
#define HNEXT_F1(n) NEXT(n)
#define HNEXT_F2(n) ADDQ $n, SI; JMP disp_F2
#define HNEXT_F3(n) ADDQ $n, SI; JMP disp_F3
#define HNEXT_F4(n) ADDQ $n, SI; JMP disp_F4
#define HNEXT_F5(n) ADDQ $n, SI; JMP disp_F5
#define HNEXT_F6(n) ADDQ $n, SI; JMP disp_F6
#define HNEXT_F7(n) ADDQ $n, SI; JMP disp_F7
#define HNEXT_F8(n) ADDQ $n, SI; JMP disp_F8
#define HNEXT_F9(n) NEXT(n)
#define HNEXT_F10(n) ADDQ $n, SI; JMP disp_F10
#define HNEXT_F11(n) NEXT(n)
#define HNEXT_F12(n) ADDQ $n, SI; JMP disp_F12
#define HNEXT_F13(n) NEXT(n)
#define HNEXT_F14(n) ADDQ $n, SI; JMP disp_F14
#define HNEXT_F15(n) ADDQ $n, SI; JMP disp_F15
#define HNEXT_F16(n) ADDQ $n, SI; JMP disp_F16
#define HNEXT_F17(n) ADDQ $n, SI; JMP disp_F17
#define HNEXT_F18(n) ADDQ $n, SI; JMP disp_F18
#define HNEXT_F19(n) ADDQ $n, SI; JMP disp_F19
#define HNEXT_F20(n) ADDQ $n, SI; JMP disp_F20
#define HNEXT_F21(n) ADDQ $n, SI; JMP disp_F21
#define HNEXT_F22(n) ADDQ $n, SI; JMP disp_F22
#define HNEXT_J_56(n) ADDQ $n, SI; JMP disp_J_56
#define HNEXT_J_57(n) ADDQ $n, SI; JMP disp_J_57
#define HNEXT_J_F0(n) ADDQ $n, SI; JMP disp_J_F0
#define HNEXT_J_F1(n) ADDQ $n, SI; JMP disp_J_F1
#define HNEXT_J_F2(n) ADDQ $n, SI; JMP disp_J_F2
#define HNEXT_J_F4(n) ADDQ $n, SI; JMP disp_J_F4
#define HNEXT_J_F5(n) ADDQ $n, SI; JMP disp_J_F5
#define HNEXT_J_F6(n) ADDQ $n, SI; JMP disp_J_F6
#define HNEXT_J_F7(n) ADDQ $n, SI; JMP disp_J_F7
#define HNEXT_J_F8(n) ADDQ $n, SI; JMP disp_J_F8
// END HINTS

// EVM.Cancel makes JUMP and JUMPI stop the frame; host handles it.
#ifdef ASMEVM_PREEMPT
#define ABORT_CHECK \
	MOVQ F_ABORT, R9; \
	CMPL (R9), $0; \
	JNE host
#else
#define ABORT_CHECK
#endif

// At least k items.
#define NEED(k) \
	LEAQ -(32*k)(R13), R9; \
	CMPQ R9, R10; \
	JCS host

// At most 1023 items.
#define ROOM \
	LEAQ -32768(R13), R9; \
	CMPQ R9, R10; \
	JCC host

#define USEGAS(n, undo) \
	SUBQ $n, BX; \
	JCS undo

// Stores the 0/1 (or any 64-bit) value in AX as the word at off(R13).
#define WORD_STORE(off) \
	MOVQ AX, off(R13); \
	XORL AX, AX; \
	MOVQ AX, off+8(R13); \
	MOVQ AX, off+16(R13); \
	MOVQ AX, off+24(R13)

#define ZERO_STORE(off) \
	XORL AX, AX; \
	MOVQ AX, off(R13); \
	MOVQ AX, off+8(R13); \
	MOVQ AX, off+16(R13); \
	MOVQ AX, off+24(R13)

// reg = the word at off(R13), or host unless it is below 2^32. Clobbers R9.
#define SMALL(off, reg) \
	MOVQ off+8(R13), R9; \
	ORQ off+16(R13), R9; \
	ORQ off+24(R13), R9; \
	JNE host; \
	MOVQ off(R13), reg; \
	MOVQ reg, R9; \
	SHRQ $32, R9; \
	JNE host

// SMALL with the word at off(base).
#define SMALLB(off, base, reg) \
	MOVQ off+8(base), R9; \
	ORQ off+16(base), R9; \
	ORQ off+24(base), R9; \
	JNE host; \
	MOVQ off(base), reg; \
	MOVQ reg, R9; \
	SHRQ $32, R9; \
	JNE host

#define DUP(n) \
	NEED(n); \
	ROOM; \
	USEGAS(3, undo3); \
	MOVQ -(32*n)(R13), AX; \
	MOVQ AX, (R13); \
	MOVQ -(32*n-8)(R13), AX; \
	MOVQ AX, 8(R13); \
	MOVQ -(32*n-16)(R13), AX; \
	MOVQ AX, 16(R13); \
	MOVQ -(32*n-24)(R13), AX; \
	MOVQ AX, 24(R13); \
	ADDQ $32, R13

#define SWAP(n) \
	NEED(n+1); \
	USEGAS(3, undo3); \
	LEAQ -(32*(n+1))(R13), R9; \
	MOVQ -32(R13), AX; \
	MOVQ (R9), CX; \
	MOVQ CX, -32(R13); \
	MOVQ AX, (R9); \
	MOVQ -24(R13), AX; \
	MOVQ 8(R9), CX; \
	MOVQ CX, -24(R13); \
	MOVQ AX, 8(R9); \
	MOVQ -16(R13), AX; \
	MOVQ 16(R9), CX; \
	MOVQ CX, -16(R13); \
	MOVQ AX, 16(R9); \
	MOVQ -8(R13), AX; \
	MOVQ 24(R9), CX; \
	MOVQ CX, -8(R13); \
	MOVQ AX, 24(R9)

// MEMEXP charges DX gas plus the expansion of memory to CX bytes (0 for
// none), expanding and zeroing it. It jumps to host, with nothing changed,
// when gas is short or the pooled buffer is too small. Clobbers AX, CX, DX,
// R8, R9, R15. The three labels must be unique per use.
#define MEMEXP(lcharge, lzero, ldone) \
	MOVQ F_MEMLEN, R8; \
	CMPQ CX, R8; \
	JLS lcharge; \
	ADDQ $31, CX; \
	SHRQ $5, CX; \
	MOVQ CX, R9; \
	SHLQ $5, R9; \
	CMPQ R9, F_MEMCAP; \
	JHI host; \
	MOVQ CX, AX; \
	IMULQ CX, AX; \
	SHRQ $9, AX; \
	LEAQ (CX)(CX*2), R15; \
	ADDQ R15, AX; \
	MOVQ AX, R15; \
	SUBQ F_MEMCOST, R15; \
	ADDQ DX, R15; \
	CMPQ BX, R15; \
	JCS host; \
	SUBQ R15, BX; \
	MOVQ AX, F_MEMCOST; \
	MOVQ R9, F_MEMLEN; \
	MOVQ F_MEM, AX; \
	ADDQ AX, R9; \
	ADDQ AX, R8; \
	XORL AX, AX; \
lzero: \
	MOVQ AX, (R8); \
	MOVQ AX, 8(R8); \
	MOVQ AX, 16(R8); \
	MOVQ AX, 24(R8); \
	ADDQ $32, R8; \
	CMPQ R8, R9; \
	JCS lzero; \
	JMP ldone; \
lcharge: \
	CMPQ BX, DX; \
	JCS host; \
	SUBQ DX, BX; \
ldone: \
	NOP

// d = low 256 bits of x * y, all three 4-word little-endian values at
// offsets from base. d may alias y but not x. Clobbers AX, CX, DX, R8, R9, R15.
#define MUL256(x, y, d, base) \
	MOVQ x(base), AX; \
	MULQ y(base); \
	MOVQ AX, R15; \
	MOVQ DX, CX; \
	MOVQ x+8(base), AX; \
	MULQ y(base); \
	ADDQ AX, CX; \
	ADCQ $0, DX; \
	MOVQ DX, R8; \
	MOVQ x+16(base), AX; \
	MULQ y(base); \
	ADDQ AX, R8; \
	ADCQ $0, DX; \
	MOVQ DX, R9; \
	MOVQ x+24(base), AX; \
	IMULQ y(base), AX; \
	ADDQ AX, R9; \
	MOVQ R15, d(base); \
	MOVQ x(base), AX; \
	MULQ y+8(base); \
	ADDQ AX, CX; \
	ADCQ $0, DX; \
	MOVQ DX, R15; \
	MOVQ x+8(base), AX; \
	MULQ y+8(base); \
	ADDQ R15, AX; \
	ADCQ $0, DX; \
	ADDQ AX, R8; \
	ADCQ $0, DX; \
	MOVQ DX, R15; \
	MOVQ x+16(base), AX; \
	IMULQ y+8(base), AX; \
	ADDQ AX, R9; \
	ADDQ R15, R9; \
	MOVQ CX, d+8(base); \
	MOVQ x(base), AX; \
	MULQ y+16(base); \
	ADDQ AX, R8; \
	ADCQ $0, DX; \
	MOVQ DX, R15; \
	MOVQ x+8(base), AX; \
	IMULQ y+16(base), AX; \
	ADDQ AX, R9; \
	ADDQ R15, R9; \
	MOVQ R8, d+16(base); \
	MOVQ x(base), AX; \
	IMULQ y+24(base), AX; \
	ADDQ AX, R9; \
	MOVQ R9, d+24(base)

// Copies the 4-word value at so(sb) to do(db) through AX.
#define COPY4(so, sb, do, db) \
	MOVQ so(sb), AX; \
	MOVQ AX, do(db); \
	MOVQ so+8(sb), AX; \
	MOVQ AX, do+8(db); \
	MOVQ so+16(sb), AX; \
	MOVQ AX, do+16(db); \
	MOVQ so+24(sb), AX; \
	MOVQ AX, do+24(db)

// x = -x when the mask s is -1 (s is 0 or -1), for the 4-word local at off.
#define NEGIF(off, s) \
	XORQ s, off(SP); \
	XORQ s, off+8(SP); \
	XORQ s, off+16(SP); \
	XORQ s, off+24(SP); \
	SUBQ s, off(SP); \
	SBBQ s, off+8(SP); \
	SBBQ s, off+16(SP); \
	SBBQ s, off+24(SP)

// Reloads the registers that live across operations after a call.
#define RELOAD \
	MOVQ L_F(SP), DI; \
	MOVQ F_PC, SI; \
	MOVQ F_GAS, BX; \
	MOVQ F_STACK, R10; \
	MOVQ F_SP, R13; \
	SHLQ $5, R13; \
	ADDQ R10, R13; \
	MOVQ F_ARGS, R11; \
	MOVQ F_HANDLERS, R12

#define SAVE \
	MOVQ SI, F_PC; \
	MOVQ BX, F_GAS; \
	MOVQ R13, R9; \
	SUBQ R10, R9; \
	SHRQ $5, R9; \
	MOVQ R9, F_SP

// Restores R10-R12 after a block that used them as scratch.
#define RESTORE_BASES \
	MOVQ F_STACK, R10; \
	MOVQ F_ARGS, R11; \
	MOVQ F_HANDLERS, R12

// XORs the 136-byte block at p into the Keccak state in the frame.
#define ABSORB(p) \
	MOVQ (p), AX; XORQ AX, asmFrame_kstate+0(DI); \
	MOVQ 8(p), AX; XORQ AX, asmFrame_kstate+8(DI); \
	MOVQ 16(p), AX; XORQ AX, asmFrame_kstate+16(DI); \
	MOVQ 24(p), AX; XORQ AX, asmFrame_kstate+24(DI); \
	MOVQ 32(p), AX; XORQ AX, asmFrame_kstate+32(DI); \
	MOVQ 40(p), AX; XORQ AX, asmFrame_kstate+40(DI); \
	MOVQ 48(p), AX; XORQ AX, asmFrame_kstate+48(DI); \
	MOVQ 56(p), AX; XORQ AX, asmFrame_kstate+56(DI); \
	MOVQ 64(p), AX; XORQ AX, asmFrame_kstate+64(DI); \
	MOVQ 72(p), AX; XORQ AX, asmFrame_kstate+72(DI); \
	MOVQ 80(p), AX; XORQ AX, asmFrame_kstate+80(DI); \
	MOVQ 88(p), AX; XORQ AX, asmFrame_kstate+88(DI); \
	MOVQ 96(p), AX; XORQ AX, asmFrame_kstate+96(DI); \
	MOVQ 104(p), AX; XORQ AX, asmFrame_kstate+104(DI); \
	MOVQ 112(p), AX; XORQ AX, asmFrame_kstate+112(DI); \
	MOVQ 120(p), AX; XORQ AX, asmFrame_kstate+120(DI); \
	MOVQ 128(p), AX; XORQ AX, asmFrame_kstate+128(DI)

// func asmRun(f *asmFrame)
TEXT ·asmRun(SB), 0, $512-8
	NO_LOCAL_POINTERS
	MOVQ f+0(FP), DI
	MOVQ DI, L_F(SP)
	CMPQ F_STATUS, $const_asmSlotCheck
	JNE reload
	JMP probe_a

host:
	SAVE
	MOVQ DI, 0(SP)
	CALL ·asmHost(SB)

reload:
	MOVQ L_F(SP), DI
	CMPQ F_STATUS, $0
	JNE done
	RELOAD
	JMP (R12)(SI*8)

op_stop:
	MOVQ $const_asmStop, AX

// AX is the exit status.
exit:
	SAVE
	MOVQ AX, F_STATUS

done:
	CMPQ asmFrame_parent(DI), $0
	JNE child_done
	RET

// A natively entered frame ended (DI). STOP and RETURN go back to the caller
// here; anything else (REVERT, INVALID, an exit through the host) is finished
// by asmChildExit in Go, which also reverts the state when needed.
child_done:
	CMPQ F_STATUS, $const_asmStop
	JEQ child_ok
	CMPQ F_STATUS, $const_asmReturn
	JEQ child_ok
	MOVQ DI, 0(SP)
	CALL ·asmChildExit(SB)
	MOVQ L_F(SP), DI
	MOVQ asmFrame_parent(DI), DI
	JMP child_resume
child_ok:
	// Return data: RETURN's region of the child's memory, none for STOP.
	XORL AX, AX               // pointer
	XORL CX, CX               // length
	CMPQ F_STATUS, $const_asmStop
	JEQ child_ret
	MOVQ F_MEM, AX
	ADDQ F_RET0, AX
	MOVQ F_RET1, CX
child_ret:
	MOVQ asmFrame_parent(DI), R8
	MOVQ AX, asmFrame_retPtr(R8)
	MOVQ CX, asmFrame_retLen(R8)
	MOVQ F_GAS, DX
	ADDQ DX, asmFrame_gas(R8)
	// Copy min(length, callRetSize) bytes to the caller's return region.
	CMPQ CX, asmFrame_callRetSize(R8)
	JLS child_copy
	MOVQ asmFrame_callRetSize(R8), CX
child_copy:
	MOVQ asmFrame_mem(R8), DX
	ADDQ asmFrame_callRetOff(R8), DX
child_copy8:
	CMPQ CX, $8
	JCS child_copy1
	MOVQ (AX), R9
	MOVQ R9, (DX)
	ADDQ $8, AX
	ADDQ $8, DX
	SUBQ $8, CX
	JMP child_copy8
child_copy1:
	TESTQ CX, CX
	JEQ child_push
	MOVB (AX), R9
	MOVB R9, (DX)
	ADDQ $1, AX
	ADDQ $1, DX
	SUBQ $1, CX
	JMP child_copy1
child_push:
	// Push 1 (success) on the caller's stack, step past its CALL.
	MOVQ asmFrame_sp(R8), AX
	SHLQ $5, AX
	ADDQ asmFrame_stackBase(R8), AX
	MOVQ $1, 0(AX)
	MOVQ $0, 8(AX)
	MOVQ $0, 16(AX)
	MOVQ $0, 24(AX)
	ADDQ $1, asmFrame_sp(R8)
	ADDQ $1, asmFrame_pc(R8)
	MOVQ R8, DI
child_resume:
	MOVQ DI, L_F(SP)
	RELOAD
	JMP (R12)(SI*8)

// The constant gas was charged before a check failed; give it back.
undo1:
	ADDQ $1, BX
	JMP host
undo2:
	ADDQ $2, BX
	JMP host
undo3:
	ADDQ $3, BX
	JMP host
undo5:
	ADDQ $5, BX
	JMP host
undo8:
	ADDQ $8, BX
	JMP host

// Slot self-check: ret0 = address just before the slots, ret1 = slot 0
// (derived from the probe slot after the fused slots). asm_amd64.go checks
// that they agree, which fails if any slot overflows its 128 bytes.
probe_a:
	GETPC_R9
	MOVQ R9, F_RET0
	JMP probe_slot

// BEGIN SLOTS (generated)
	PCALIGN $128
slots:
// 0x00
slot_00:
	MOVQ $const_asmStop, AX
	JMP exit
	PCALIGN $128
// 0x01
slot_01:
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -32(R13), AX
	ADDQ -64(R13), AX
	MOVQ AX, -64(R13)
	MOVQ -24(R13), AX
	ADCQ -56(R13), AX
	MOVQ AX, -56(R13)
	MOVQ -16(R13), AX
	ADCQ -48(R13), AX
	MOVQ AX, -48(R13)
	MOVQ -8(R13), AX
	ADCQ -40(R13), AX
	MOVQ AX, -40(R13)
	SUBQ $32, R13
	HNEXT_01(1)
	PCALIGN $128
// 0x02
slot_02:
	JMP op_mul
	PCALIGN $128
// 0x03
slot_03:
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -32(R13), AX
	SUBQ -64(R13), AX
	MOVQ AX, -64(R13)
	MOVQ -24(R13), AX
	SBBQ -56(R13), AX
	MOVQ AX, -56(R13)
	MOVQ -16(R13), AX
	SBBQ -48(R13), AX
	MOVQ AX, -48(R13)
	MOVQ -8(R13), AX
	SBBQ -40(R13), AX
	MOVQ AX, -40(R13)
	SUBQ $32, R13
	HNEXT_03(1)
	PCALIGN $128
// 0x04
slot_04:
	JMP op_div
	PCALIGN $128
// 0x05
slot_05:
	JMP op_sdiv
	PCALIGN $128
// 0x06
slot_06:
	JMP op_mod
	PCALIGN $128
// 0x07
slot_07:
	JMP op_smod
	PCALIGN $128
// 0x08
slot_08:
	JMP op_addmod
	PCALIGN $128
// 0x09
slot_09:
	JMP op_mulmod
	PCALIGN $128
// 0x0a
slot_0a:
	JMP op_exp
	PCALIGN $128
// 0x0b
slot_0b:
	JMP op_signextend
	PCALIGN $128
// 0x0c
slot_0c:
	JMP host
	PCALIGN $128
// 0x0d
slot_0d:
	JMP host
	PCALIGN $128
// 0x0e
slot_0e:
	JMP host
	PCALIGN $128
// 0x0f
slot_0f:
	JMP host
	PCALIGN $128
// 0x10
slot_10:
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -32(R13), AX
	SUBQ -64(R13), AX
	MOVQ -24(R13), AX
	SBBQ -56(R13), AX
	MOVQ -16(R13), AX
	SBBQ -48(R13), AX
	MOVQ -8(R13), AX
	SBBQ -40(R13), AX
	SETCS AX
	MOVBQZX AX, AX
	WORD_STORE(-64)
	SUBQ $32, R13
	HNEXT_10(1)
	PCALIGN $128
// 0x11
slot_11:
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -64(R13), AX
	SUBQ -32(R13), AX
	MOVQ -56(R13), AX
	SBBQ -24(R13), AX
	MOVQ -48(R13), AX
	SBBQ -16(R13), AX
	MOVQ -40(R13), AX
	SBBQ -8(R13), AX
	SETCS AX
	MOVBQZX AX, AX
	WORD_STORE(-64)
	SUBQ $32, R13
	HNEXT_11(1)
	PCALIGN $128
// 0x12
slot_12:
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -32(R13), AX
	SUBQ -64(R13), AX
	MOVQ -24(R13), AX
	SBBQ -56(R13), AX
	MOVQ -16(R13), AX
	SBBQ -48(R13), AX
	MOVQ -8(R13), AX
	SBBQ -40(R13), AX
	SETLT AX
	MOVBQZX AX, AX
	WORD_STORE(-64)
	SUBQ $32, R13
	HNEXT_12(1)
	PCALIGN $128
// 0x13
slot_13:
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -64(R13), AX
	SUBQ -32(R13), AX
	MOVQ -56(R13), AX
	SBBQ -24(R13), AX
	MOVQ -48(R13), AX
	SBBQ -16(R13), AX
	MOVQ -40(R13), AX
	SBBQ -8(R13), AX
	SETLT AX
	MOVBQZX AX, AX
	WORD_STORE(-64)
	SUBQ $32, R13
	HNEXT_13(1)
	PCALIGN $128
// 0x14
slot_14:
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -32(R13), AX
	XORQ -64(R13), AX
	MOVQ -24(R13), CX
	XORQ -56(R13), CX
	ORQ CX, AX
	MOVQ -16(R13), CX
	XORQ -48(R13), CX
	ORQ CX, AX
	MOVQ -8(R13), CX
	XORQ -40(R13), CX
	ORQ CX, AX
	SETEQ AX
	MOVBQZX AX, AX
	WORD_STORE(-64)
	SUBQ $32, R13
	HNEXT_14(1)
	PCALIGN $128
// 0x15
slot_15:
	NEED(1)
	USEGAS(3, undo3)
	MOVQ -32(R13), AX
	ORQ -24(R13), AX
	ORQ -16(R13), AX
	ORQ -8(R13), AX
	SETEQ AX
	MOVBQZX AX, AX
	WORD_STORE(-32)
	HNEXT_15(1)
	PCALIGN $128
// 0x16
slot_16:
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -32(R13), AX
	ANDQ AX, -64(R13)
	MOVQ -24(R13), AX
	ANDQ AX, -56(R13)
	MOVQ -16(R13), AX
	ANDQ AX, -48(R13)
	MOVQ -8(R13), AX
	ANDQ AX, -40(R13)
	SUBQ $32, R13
	HNEXT_16(1)
	PCALIGN $128
// 0x17
slot_17:
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -32(R13), AX
	ORQ AX, -64(R13)
	MOVQ -24(R13), AX
	ORQ AX, -56(R13)
	MOVQ -16(R13), AX
	ORQ AX, -48(R13)
	MOVQ -8(R13), AX
	ORQ AX, -40(R13)
	SUBQ $32, R13
	HNEXT_17(1)
	PCALIGN $128
// 0x18
slot_18:
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -32(R13), AX
	XORQ AX, -64(R13)
	MOVQ -24(R13), AX
	XORQ AX, -56(R13)
	MOVQ -16(R13), AX
	XORQ AX, -48(R13)
	MOVQ -8(R13), AX
	XORQ AX, -40(R13)
	SUBQ $32, R13
	HNEXT_18(1)
	PCALIGN $128
// 0x19
slot_19:
	NEED(1)
	USEGAS(3, undo3)
	NOTQ -32(R13)
	NOTQ -24(R13)
	NOTQ -16(R13)
	NOTQ -8(R13)
	HNEXT_19(1)
	PCALIGN $128
// 0x1a
slot_1a:
	JMP op_byte
	PCALIGN $128
// 0x1b
slot_1b:
	JMP op_shl
	PCALIGN $128
// 0x1c
slot_1c:
	JMP op_shr
	PCALIGN $128
// 0x1d
slot_1d:
	JMP op_sar
	PCALIGN $128
// 0x1e
slot_1e:
	JMP host
	PCALIGN $128
// 0x1f
slot_1f:
	JMP host
	PCALIGN $128
// 0x20
slot_20:
	JMP op_keccak
	PCALIGN $128
// 0x21
slot_21:
	JMP host
	PCALIGN $128
// 0x22
slot_22:
	JMP host
	PCALIGN $128
// 0x23
slot_23:
	JMP host
	PCALIGN $128
// 0x24
slot_24:
	JMP host
	PCALIGN $128
// 0x25
slot_25:
	JMP host
	PCALIGN $128
// 0x26
slot_26:
	JMP host
	PCALIGN $128
// 0x27
slot_27:
	JMP host
	PCALIGN $128
// 0x28
slot_28:
	JMP host
	PCALIGN $128
// 0x29
slot_29:
	JMP host
	PCALIGN $128
// 0x2a
slot_2a:
	JMP host
	PCALIGN $128
// 0x2b
slot_2b:
	JMP host
	PCALIGN $128
// 0x2c
slot_2c:
	JMP host
	PCALIGN $128
// 0x2d
slot_2d:
	JMP host
	PCALIGN $128
// 0x2e
slot_2e:
	JMP host
	PCALIGN $128
// 0x2f
slot_2f:
	JMP host
	PCALIGN $128
// 0x30
slot_30:
	ROOM
	USEGAS(2, undo2)
	MOVQ asmFrame_env+0(DI), AX
	MOVQ AX, 0(R13)
	MOVQ asmFrame_env+8(DI), AX
	MOVQ AX, 8(R13)
	MOVQ asmFrame_env+16(DI), AX
	MOVQ AX, 16(R13)
	MOVQ asmFrame_env+24(DI), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_30(1)
	PCALIGN $128
// 0x31
slot_31:
	JMP host
	PCALIGN $128
// 0x32
slot_32:
	JMP host
	PCALIGN $128
// 0x33
slot_33:
	ROOM
	USEGAS(2, undo2)
	MOVQ asmFrame_env+32(DI), AX
	MOVQ AX, 0(R13)
	MOVQ asmFrame_env+40(DI), AX
	MOVQ AX, 8(R13)
	MOVQ asmFrame_env+48(DI), AX
	MOVQ AX, 16(R13)
	MOVQ asmFrame_env+56(DI), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_33(1)
	PCALIGN $128
// 0x34
slot_34:
	ROOM
	USEGAS(2, undo2)
	MOVQ asmFrame_env+64(DI), AX
	MOVQ AX, 0(R13)
	MOVQ asmFrame_env+72(DI), AX
	MOVQ AX, 8(R13)
	MOVQ asmFrame_env+80(DI), AX
	MOVQ AX, 16(R13)
	MOVQ asmFrame_env+88(DI), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_34(1)
	PCALIGN $128
// 0x35
slot_35:
	JMP op_calldataload
	PCALIGN $128
// 0x36
slot_36:
	ROOM
	USEGAS(2, undo2)
	MOVQ F_INPUTLEN, AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_36(1)
	PCALIGN $128
// 0x37
slot_37:
	JMP op_calldatacopy
	PCALIGN $128
// 0x38
slot_38:
	ROOM
	USEGAS(2, undo2)
	MOVQ F_CODELEN, AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_38(1)
	PCALIGN $128
// 0x39
slot_39:
	JMP op_codecopy
	PCALIGN $128
// 0x3a
slot_3a:
	JMP host
	PCALIGN $128
// 0x3b
slot_3b:
	JMP op_extcodesize
	PCALIGN $128
// 0x3c
slot_3c:
	JMP host
	PCALIGN $128
// 0x3d
slot_3d:
	TESTB $const_asmFlagReturnData, F_FLAGS
	JEQ host
	ROOM
	USEGAS(2, undo2)
	MOVQ F_RETLEN, AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_3d(1)
	PCALIGN $128
// 0x3e
slot_3e:
	JMP op_returndatacopy
	PCALIGN $128
// 0x3f
slot_3f:
	JMP host
	PCALIGN $128
// 0x40
slot_40:
	JMP host
	PCALIGN $128
// 0x41
slot_41:
	JMP host
	PCALIGN $128
// 0x42
slot_42:
	ROOM
	USEGAS(2, undo2)
	MOVQ F_TIME, AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_42(1)
	PCALIGN $128
// 0x43
slot_43:
	JMP host
	PCALIGN $128
// 0x44
slot_44:
	JMP host
	PCALIGN $128
// 0x45
slot_45:
	JMP host
	PCALIGN $128
// 0x46
slot_46:
	JMP host
	PCALIGN $128
// 0x47
slot_47:
	JMP host
	PCALIGN $128
// 0x48
slot_48:
	JMP host
	PCALIGN $128
// 0x49
slot_49:
	JMP host
	PCALIGN $128
// 0x4a
slot_4a:
	JMP host
	PCALIGN $128
// 0x4b
slot_4b:
	JMP host
	PCALIGN $128
// 0x4c
slot_4c:
	JMP host
	PCALIGN $128
// 0x4d
slot_4d:
	JMP host
	PCALIGN $128
// 0x4e
slot_4e:
	JMP host
	PCALIGN $128
// 0x4f
slot_4f:
	JMP host
	PCALIGN $128
// 0x50
slot_50:
	NEED(1)
	USEGAS(2, undo2)
	SUBQ $32, R13
	HNEXT_50(1)
	PCALIGN $128
// 0x51
slot_51:
	JMP op_mload
	PCALIGN $128
// 0x52
slot_52:
	JMP op_mstore
	PCALIGN $128
// 0x53
slot_53:
	JMP op_mstore8
	PCALIGN $128
// 0x54
slot_54:
	JMP op_sload
	PCALIGN $128
// 0x55
slot_55:
	JMP op_sstore
	PCALIGN $128
// 0x56
slot_56:
	JMP op_jump
	PCALIGN $128
// 0x57
slot_57:
	JMP op_jumpi
	PCALIGN $128
// 0x58
slot_58:
	ROOM
	USEGAS(2, undo2)
	MOVQ SI, AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_58(1)
	PCALIGN $128
// 0x59
slot_59:
	ROOM
	USEGAS(2, undo2)
	MOVQ F_MEMLEN, AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_59(1)
	PCALIGN $128
// 0x5a
slot_5a:
	ROOM
	USEGAS(2, undo2)
	MOVQ BX, AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_5a(1)
	PCALIGN $128
// 0x5b
slot_5b:
jumpdest_slot:
	USEGAS(1, undo1)
	HNEXT_5b(1)
	PCALIGN $128
// 0x5c
slot_5c:
	JMP host
	PCALIGN $128
// 0x5d
slot_5d:
	JMP host
	PCALIGN $128
// 0x5e
slot_5e:
	JMP op_mcopy
	PCALIGN $128
// 0x5f
slot_5f:
	TESTB $const_asmFlagPush0, F_FLAGS
	JEQ host
	ROOM
	USEGAS(2, undo2)
	ZERO_STORE(0)
	ADDQ $32, R13
	HNEXT_5f(1)
	PCALIGN $128
// 0x60
slot_60:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_60(2)
	PCALIGN $128
// 0x61
slot_61:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_61(3)
	PCALIGN $128
// 0x62
slot_62:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_62(4)
	PCALIGN $128
// 0x63
slot_63:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_63(5)
	PCALIGN $128
// 0x64
slot_64:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_64(6)
	PCALIGN $128
// 0x65
slot_65:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_65(7)
	PCALIGN $128
// 0x66
slot_66:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_66(8)
	PCALIGN $128
// 0x67
slot_67:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), AX
	WORD_STORE(0)
	ADDQ $32, R13
	HNEXT_67(9)
	PCALIGN $128
// 0x68
slot_68:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_68(10)
	PCALIGN $128
// 0x69
slot_69:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_69(11)
	PCALIGN $128
// 0x6a
slot_6a:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_6a(12)
	PCALIGN $128
// 0x6b
slot_6b:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_6b(13)
	PCALIGN $128
// 0x6c
slot_6c:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_6c(14)
	PCALIGN $128
// 0x6d
slot_6d:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_6d(15)
	PCALIGN $128
// 0x6e
slot_6e:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_6e(16)
	PCALIGN $128
// 0x6f
slot_6f:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_6f(17)
	PCALIGN $128
// 0x70
slot_70:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_70(18)
	PCALIGN $128
// 0x71
slot_71:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_71(19)
	PCALIGN $128
// 0x72
slot_72:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_72(20)
	PCALIGN $128
// 0x73
slot_73:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_73(21)
	PCALIGN $128
// 0x74
slot_74:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_74(22)
	PCALIGN $128
// 0x75
slot_75:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_75(23)
	PCALIGN $128
// 0x76
slot_76:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_76(24)
	PCALIGN $128
// 0x77
slot_77:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_77(25)
	PCALIGN $128
// 0x78
slot_78:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_78(26)
	PCALIGN $128
// 0x79
slot_79:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_79(27)
	PCALIGN $128
// 0x7a
slot_7a:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_7a(28)
	PCALIGN $128
// 0x7b
slot_7b:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_7b(29)
	PCALIGN $128
// 0x7c
slot_7c:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_7c(30)
	PCALIGN $128
// 0x7d
slot_7d:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_7d(31)
	PCALIGN $128
// 0x7e
slot_7e:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_7e(32)
	PCALIGN $128
// 0x7f
slot_7f:
	ROOM
	USEGAS(3, undo3)
	MOVQ (R11)(SI*8), R9
	MOVQ 0(R9), AX
	MOVQ AX, 0(R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_7f(33)
	PCALIGN $128
// 0x80
slot_80:
	DUP(1)
	HNEXT_80(1)
	PCALIGN $128
// 0x81
slot_81:
	DUP(2)
	HNEXT_81(1)
	PCALIGN $128
// 0x82
slot_82:
	DUP(3)
	HNEXT_82(1)
	PCALIGN $128
// 0x83
slot_83:
	DUP(4)
	HNEXT_83(1)
	PCALIGN $128
// 0x84
slot_84:
	DUP(5)
	HNEXT_84(1)
	PCALIGN $128
// 0x85
slot_85:
	DUP(6)
	HNEXT_85(1)
	PCALIGN $128
// 0x86
slot_86:
	DUP(7)
	HNEXT_86(1)
	PCALIGN $128
// 0x87
slot_87:
	DUP(8)
	HNEXT_87(1)
	PCALIGN $128
// 0x88
slot_88:
	DUP(9)
	HNEXT_88(1)
	PCALIGN $128
// 0x89
slot_89:
	DUP(10)
	HNEXT_89(1)
	PCALIGN $128
// 0x8a
slot_8a:
	DUP(11)
	HNEXT_8a(1)
	PCALIGN $128
// 0x8b
slot_8b:
	DUP(12)
	HNEXT_8b(1)
	PCALIGN $128
// 0x8c
slot_8c:
	DUP(13)
	HNEXT_8c(1)
	PCALIGN $128
// 0x8d
slot_8d:
	DUP(14)
	HNEXT_8d(1)
	PCALIGN $128
// 0x8e
slot_8e:
	DUP(15)
	HNEXT_8e(1)
	PCALIGN $128
// 0x8f
slot_8f:
	DUP(16)
	HNEXT_8f(1)
	PCALIGN $128
// 0x90
slot_90:
	SWAP(1)
	HNEXT_90(1)
	PCALIGN $128
// 0x91
slot_91:
	SWAP(2)
	HNEXT_91(1)
	PCALIGN $128
// 0x92
slot_92:
	SWAP(3)
	HNEXT_92(1)
	PCALIGN $128
// 0x93
slot_93:
	SWAP(4)
	HNEXT_93(1)
	PCALIGN $128
// 0x94
slot_94:
	SWAP(5)
	HNEXT_94(1)
	PCALIGN $128
// 0x95
slot_95:
	SWAP(6)
	HNEXT_95(1)
	PCALIGN $128
// 0x96
slot_96:
	SWAP(7)
	HNEXT_96(1)
	PCALIGN $128
// 0x97
slot_97:
	SWAP(8)
	HNEXT_97(1)
	PCALIGN $128
// 0x98
slot_98:
	SWAP(9)
	HNEXT_98(1)
	PCALIGN $128
// 0x99
slot_99:
	SWAP(10)
	HNEXT_99(1)
	PCALIGN $128
// 0x9a
slot_9a:
	SWAP(11)
	HNEXT_9a(1)
	PCALIGN $128
// 0x9b
slot_9b:
	SWAP(12)
	HNEXT_9b(1)
	PCALIGN $128
// 0x9c
slot_9c:
	SWAP(13)
	HNEXT_9c(1)
	PCALIGN $128
// 0x9d
slot_9d:
	SWAP(14)
	HNEXT_9d(1)
	PCALIGN $128
// 0x9e
slot_9e:
	SWAP(15)
	HNEXT_9e(1)
	PCALIGN $128
// 0x9f
slot_9f:
	SWAP(16)
	HNEXT_9f(1)
	PCALIGN $128
// 0xa0
slot_a0:
	NEED(2)
	MOVQ $0, R15
	JMP op_log
	PCALIGN $128
// 0xa1
slot_a1:
	NEED(3)
	MOVQ $1, R15
	JMP op_log
	PCALIGN $128
// 0xa2
slot_a2:
	NEED(4)
	MOVQ $2, R15
	JMP op_log
	PCALIGN $128
// 0xa3
slot_a3:
	NEED(5)
	MOVQ $3, R15
	JMP op_log
	PCALIGN $128
// 0xa4
slot_a4:
	NEED(6)
	MOVQ $4, R15
	JMP op_log
	PCALIGN $128
// 0xa5
slot_a5:
	JMP host
	PCALIGN $128
// 0xa6
slot_a6:
	JMP host
	PCALIGN $128
// 0xa7
slot_a7:
	JMP host
	PCALIGN $128
// 0xa8
slot_a8:
	JMP host
	PCALIGN $128
// 0xa9
slot_a9:
	JMP host
	PCALIGN $128
// 0xaa
slot_aa:
	JMP host
	PCALIGN $128
// 0xab
slot_ab:
	JMP host
	PCALIGN $128
// 0xac
slot_ac:
	JMP host
	PCALIGN $128
// 0xad
slot_ad:
	JMP host
	PCALIGN $128
// 0xae
slot_ae:
	JMP host
	PCALIGN $128
// 0xaf
slot_af:
	JMP host
	PCALIGN $128
// 0xb0
slot_b0:
	JMP host
	PCALIGN $128
// 0xb1
slot_b1:
	JMP host
	PCALIGN $128
// 0xb2
slot_b2:
	JMP host
	PCALIGN $128
// 0xb3
slot_b3:
	JMP host
	PCALIGN $128
// 0xb4
slot_b4:
	JMP host
	PCALIGN $128
// 0xb5
slot_b5:
	JMP host
	PCALIGN $128
// 0xb6
slot_b6:
	JMP host
	PCALIGN $128
// 0xb7
slot_b7:
	JMP host
	PCALIGN $128
// 0xb8
slot_b8:
	JMP host
	PCALIGN $128
// 0xb9
slot_b9:
	JMP host
	PCALIGN $128
// 0xba
slot_ba:
	JMP host
	PCALIGN $128
// 0xbb
slot_bb:
	JMP host
	PCALIGN $128
// 0xbc
slot_bc:
	JMP host
	PCALIGN $128
// 0xbd
slot_bd:
	JMP host
	PCALIGN $128
// 0xbe
slot_be:
	JMP host
	PCALIGN $128
// 0xbf
slot_bf:
	JMP host
	PCALIGN $128
// 0xc0
slot_c0:
	JMP host
	PCALIGN $128
// 0xc1
slot_c1:
	JMP host
	PCALIGN $128
// 0xc2
slot_c2:
	JMP host
	PCALIGN $128
// 0xc3
slot_c3:
	JMP host
	PCALIGN $128
// 0xc4
slot_c4:
	JMP host
	PCALIGN $128
// 0xc5
slot_c5:
	JMP host
	PCALIGN $128
// 0xc6
slot_c6:
	JMP host
	PCALIGN $128
// 0xc7
slot_c7:
	JMP host
	PCALIGN $128
// 0xc8
slot_c8:
	JMP host
	PCALIGN $128
// 0xc9
slot_c9:
	JMP host
	PCALIGN $128
// 0xca
slot_ca:
	JMP host
	PCALIGN $128
// 0xcb
slot_cb:
	JMP host
	PCALIGN $128
// 0xcc
slot_cc:
	JMP host
	PCALIGN $128
// 0xcd
slot_cd:
	JMP host
	PCALIGN $128
// 0xce
slot_ce:
	JMP host
	PCALIGN $128
// 0xcf
slot_cf:
	JMP host
	PCALIGN $128
// 0xd0
slot_d0:
	JMP host
	PCALIGN $128
// 0xd1
slot_d1:
	JMP host
	PCALIGN $128
// 0xd2
slot_d2:
	JMP host
	PCALIGN $128
// 0xd3
slot_d3:
	JMP host
	PCALIGN $128
// 0xd4
slot_d4:
	JMP host
	PCALIGN $128
// 0xd5
slot_d5:
	JMP host
	PCALIGN $128
// 0xd6
slot_d6:
	JMP host
	PCALIGN $128
// 0xd7
slot_d7:
	JMP host
	PCALIGN $128
// 0xd8
slot_d8:
	JMP host
	PCALIGN $128
// 0xd9
slot_d9:
	JMP host
	PCALIGN $128
// 0xda
slot_da:
	JMP host
	PCALIGN $128
// 0xdb
slot_db:
	JMP host
	PCALIGN $128
// 0xdc
slot_dc:
	JMP host
	PCALIGN $128
// 0xdd
slot_dd:
	JMP host
	PCALIGN $128
// 0xde
slot_de:
	JMP host
	PCALIGN $128
// 0xdf
slot_df:
	JMP host
	PCALIGN $128
// 0xe0
slot_e0:
	JMP host
	PCALIGN $128
// 0xe1
slot_e1:
	JMP host
	PCALIGN $128
// 0xe2
slot_e2:
	JMP host
	PCALIGN $128
// 0xe3
slot_e3:
	JMP host
	PCALIGN $128
// 0xe4
slot_e4:
	JMP host
	PCALIGN $128
// 0xe5
slot_e5:
	JMP host
	PCALIGN $128
// 0xe6
slot_e6:
	JMP host
	PCALIGN $128
// 0xe7
slot_e7:
	JMP host
	PCALIGN $128
// 0xe8
slot_e8:
	JMP host
	PCALIGN $128
// 0xe9
slot_e9:
	JMP host
	PCALIGN $128
// 0xea
slot_ea:
	JMP host
	PCALIGN $128
// 0xeb
slot_eb:
	JMP host
	PCALIGN $128
// 0xec
slot_ec:
	JMP host
	PCALIGN $128
// 0xed
slot_ed:
	JMP host
	PCALIGN $128
// 0xee
slot_ee:
	JMP host
	PCALIGN $128
// 0xef
slot_ef:
	JMP host
	PCALIGN $128
// 0xf0
slot_f0:
	JMP host
	PCALIGN $128
// 0xf1
slot_f1:
	JMP op_call
	PCALIGN $128
// 0xf2
slot_f2:
	JMP host
	PCALIGN $128
// 0xf3
slot_f3:
	JMP op_return
	PCALIGN $128
// 0xf4
slot_f4:
	JMP op_delegatecall
	PCALIGN $128
// 0xf5
slot_f5:
	JMP host
	PCALIGN $128
// 0xf6
slot_f6:
	JMP host
	PCALIGN $128
// 0xf7
slot_f7:
	JMP host
	PCALIGN $128
// 0xf8
slot_f8:
	JMP host
	PCALIGN $128
// 0xf9
slot_f9:
	JMP host
	PCALIGN $128
// 0xfa
slot_fa:
	JMP op_staticcall
	PCALIGN $128
// 0xfb
slot_fb:
	JMP host
	PCALIGN $128
// 0xfc
slot_fc:
	JMP host
	PCALIGN $128
// 0xfd
slot_fd:
	JMP op_revert
	PCALIGN $128
// 0xfe
slot_fe:
	MOVQ $const_asmInvalid, AX
	JMP exit
	PCALIGN $128
// 0xff
slot_ff:
	JMP host
	PCALIGN $128
slots_end:
// fused 0
slot_fused0:
	ROOM
	NEED(1)
	CMPQ BX, $13
	JCS host
	ABORT_CHECK
	SUBQ $13, BX
	SUBQ $32, R13
	MOVQ (R13), R9
	ORQ 8(R13), R9
	ORQ 16(R13), R9
	ORQ 24(R13), R9
	JEQ fused_jumpi_not
	MOVQ (R11)(SI*8), AX
	JMP fused_0_jump
fused_jumpi_not:
	HNEXT_F0(4)
	PCALIGN $128
// fused 1
slot_fused1:
	ROOM
	CMPQ BX, $11
	JCS host
	ABORT_CHECK
	SUBQ $11, BX
	MOVQ (R11)(SI*8), AX
	JMP fused_1_jump
	PCALIGN $128
// fused 2
slot_fused2:
	NEED(1)
	ROOM
	CMPQ BX, $16
	JCS host
	ABORT_CHECK
	SUBQ $16, BX
	SUBQ $32, R13
	MOVQ (R13), R9
	ORQ 8(R13), R9
	ORQ 16(R13), R9
	ORQ 24(R13), R9
	JNE fused_izjumpi_not
	MOVQ (R11)(SI*8), AX
	JMP fused_2_jump
fused_izjumpi_not:
	HNEXT_F2(5)
	PCALIGN $128
// fused 3
slot_fused3:
	LEAQ -32736(R13), R9
	CMPQ R9, R10
	JCC host
	CMPQ BX, $9
	JCS host
	SUBQ $9, BX
	MOVQ (R11)(SI*8), R9
	MOVQ (R9), AX
	MOVQ AX, (R13)
	MOVQ 8(R9), AX
	MOVQ AX, 8(R13)
	MOVQ 16(R9), AX
	MOVQ AX, 16(R13)
	MOVQ 24(R9), AX
	MOVQ AX, 24(R13)
	ADDQ $32, R13
	HNEXT_F3(5)
	PCALIGN $128
// fused 4
slot_fused4:
	NEED(2)
	CMPQ BX, $16
	JCS host
	ABORT_CHECK
	MOVQ -32(R13), AX
	XORQ -64(R13), AX
	MOVQ -24(R13), CX
	XORQ -56(R13), CX
	ORQ CX, AX
	MOVQ -16(R13), CX
	XORQ -48(R13), CX
	ORQ CX, AX
	MOVQ -8(R13), CX
	XORQ -40(R13), CX
	ORQ CX, AX
	JEQ fused_4_taken
	SUBQ $16, BX
	SUBQ $64, R13
	HNEXT_F4(5)
fused_4_taken:
	SUBQ $16, BX
	SUBQ $64, R13
	MOVQ (R11)(SI*8), AX
	JMP fused_4_jump
	PCALIGN $128
// fused 5
slot_fused5:
	NEED(2)
	CMPQ BX, $16
	JCS host
	ABORT_CHECK
	MOVQ -32(R13), AX
	SUBQ -64(R13), AX
	MOVQ -24(R13), AX
	SBBQ -56(R13), AX
	MOVQ -16(R13), AX
	SBBQ -48(R13), AX
	MOVQ -8(R13), AX
	SBBQ -40(R13), AX
	JCS fused_5_taken
	SUBQ $16, BX
	SUBQ $64, R13
	HNEXT_F5(5)
fused_5_taken:
	SUBQ $16, BX
	SUBQ $64, R13
	MOVQ (R11)(SI*8), AX
	JMP fused_5_jump
	PCALIGN $128
// fused 6
slot_fused6:
	NEED(2)
	CMPQ BX, $16
	JCS host
	ABORT_CHECK
	MOVQ -64(R13), AX
	SUBQ -32(R13), AX
	MOVQ -56(R13), AX
	SBBQ -24(R13), AX
	MOVQ -48(R13), AX
	SBBQ -16(R13), AX
	MOVQ -40(R13), AX
	SBBQ -8(R13), AX
	JCS fused_6_taken
	SUBQ $16, BX
	SUBQ $64, R13
	HNEXT_F6(5)
fused_6_taken:
	SUBQ $16, BX
	SUBQ $64, R13
	MOVQ (R11)(SI*8), AX
	JMP fused_6_jump
	PCALIGN $128
// fused 7
slot_fused7:
	NEED(2)
	CMPQ BX, $16
	JCS host
	ABORT_CHECK
	MOVQ -32(R13), AX
	ANDQ -64(R13), AX
	MOVQ -24(R13), CX
	ANDQ -56(R13), CX
	ORQ CX, AX
	MOVQ -16(R13), CX
	ANDQ -48(R13), CX
	ORQ CX, AX
	MOVQ -8(R13), CX
	ANDQ -40(R13), CX
	ORQ CX, AX
	JNE fused_7_taken
	SUBQ $16, BX
	SUBQ $64, R13
	HNEXT_F7(5)
fused_7_taken:
	SUBQ $16, BX
	SUBQ $64, R13
	MOVQ (R11)(SI*8), AX
	JMP fused_7_jump
	PCALIGN $128
// fused 8
slot_fused8:
	NEED(1)
	ROOM
	CMPQ BX, $19
	JCS host
	ABORT_CHECK
	MOVQ (R11)(SI*8), AX
	XORQ -32(R13), AX
	ORQ -24(R13), AX
	ORQ -16(R13), AX
	ORQ -8(R13), AX
	JEQ fused_8_taken
	SUBQ $19, BX
	SUBQ $32, R13
	HNEXT_F8(10)
fused_8_taken:
	SUBQ $19, BX
	SUBQ $32, R13
	MOVQ 48(R11)(SI*8), AX
	JMP fused_8_jump
	PCALIGN $128
// fused 9
slot_fused9:
	NEED(2)
	CMPQ BX, $5
	JCS host
	SUBQ $5, BX
	MOVQ -32(R13), AX
	MOVQ AX, -64(R13)
	MOVQ -24(R13), AX
	MOVQ AX, -56(R13)
	MOVQ -16(R13), AX
	MOVQ AX, -48(R13)
	MOVQ -8(R13), AX
	MOVQ AX, -40(R13)
	SUBQ $32, R13
	ADDQ $2, SI
	JMP op_jump
	PCALIGN $128
// fused 10
slot_fused10:
	NEED(2)
	CMPQ BX, $5
	JCS host
	SUBQ $5, BX
	MOVQ -32(R13), AX
	MOVQ AX, -64(R13)
	MOVQ -24(R13), AX
	MOVQ AX, -56(R13)
	MOVQ -16(R13), AX
	MOVQ AX, -48(R13)
	MOVQ -8(R13), AX
	MOVQ AX, -40(R13)
	SUBQ $32, R13
	HNEXT_F10(2)
	PCALIGN $128
// fused 11
slot_fused11:
	NEED(3)
	CMPQ BX, $4
	JCS host
	SUBQ $4, BX
	SUBQ $64, R13
	ADDQ $2, SI
	JMP op_jump
	PCALIGN $128
// fused 12
slot_fused12:
	NEED(2)
	CMPQ BX, $4
	JCS host
	SUBQ $4, BX
	SUBQ $64, R13
	HNEXT_F12(2)
	PCALIGN $128
// fused 13
slot_fused13:
	NEED(2)
	CMPQ BX, $2
	JCS host
	SUBQ $2, BX
	SUBQ $32, R13
	ADDQ $1, SI
	JMP op_jump
	PCALIGN $128
// fused 14
slot_fused14:
	NEED(3)
	CMPQ BX, $8
	JCS host
	SUBQ $8, BX
	MOVQ -96(R13), AX
	MOVQ AX, -64(R13)
	MOVQ -88(R13), AX
	MOVQ AX, -56(R13)
	MOVQ -80(R13), AX
	MOVQ AX, -48(R13)
	MOVQ -72(R13), AX
	MOVQ AX, -40(R13)
	MOVQ -32(R13), AX
	MOVQ AX, -96(R13)
	MOVQ -24(R13), AX
	MOVQ AX, -88(R13)
	MOVQ -16(R13), AX
	MOVQ AX, -80(R13)
	MOVQ -8(R13), AX
	MOVQ AX, -72(R13)
	SUBQ $32, R13
	HNEXT_F14(3)
	PCALIGN $128
// fused 15
slot_fused15:
	JMP fusedbody_15
	PCALIGN $128
// fused 16
slot_fused16:
	NEED(3)
	CMPQ BX, $5
	JCS host
	SUBQ $5, BX
	MOVQ -32(R13), AX
	MOVQ AX, -96(R13)
	MOVQ -24(R13), AX
	MOVQ AX, -88(R13)
	MOVQ -16(R13), AX
	MOVQ AX, -80(R13)
	MOVQ -8(R13), AX
	MOVQ AX, -72(R13)
	SUBQ $32, R13
	HNEXT_F16(2)
	PCALIGN $128
// fused 17
slot_fused17:
	JMP fusedbody_17
	PCALIGN $128
// fused 18
slot_fused18:
	NEED(2)
	ROOM
	CMPQ BX, $8
	JCS host
	SUBQ $8, BX
	MOVQ -64(R13), AX
	MOVQ AX, -32(R13)
	MOVQ -56(R13), AX
	MOVQ AX, -24(R13)
	MOVQ -48(R13), AX
	MOVQ AX, -16(R13)
	MOVQ -40(R13), AX
	MOVQ AX, -8(R13)
	HNEXT_F18(3)
	PCALIGN $128
// fused 19
slot_fused19:
	TESTB $const_asmFlagPush0, F_FLAGS
	JEQ host
	NEED(1)
	LEAQ -32736(R13), R9
	CMPQ R9, R10
	JCC host
	CMPQ BX, $8
	JCS host
	SUBQ $8, BX
	MOVQ -32(R13), AX
	MOVQ AX, 0(R13)
	MOVQ -24(R13), AX
	MOVQ AX, 8(R13)
	MOVQ -16(R13), AX
	MOVQ AX, 16(R13)
	MOVQ -8(R13), AX
	MOVQ AX, 24(R13)
	XORL AX, AX
	MOVQ AX, 32(R13)
	MOVQ AX, 40(R13)
	MOVQ AX, 48(R13)
	MOVQ AX, 56(R13)
	ADDQ $64, R13
	HNEXT_F19(3)
	PCALIGN $128
// fused 20
slot_fused20:
	NEED(4)
	CMPQ BX, $5
	JCS host
	SUBQ $5, BX
	MOVQ -32(R13), AX
	MOVQ AX, -128(R13)
	MOVQ -24(R13), AX
	MOVQ AX, -120(R13)
	MOVQ -16(R13), AX
	MOVQ AX, -112(R13)
	MOVQ -8(R13), AX
	MOVQ AX, -104(R13)
	SUBQ $32, R13
	HNEXT_F20(2)
	PCALIGN $128
// fused 21
slot_fused21:
	NEED(2)
	ROOM
	CMPQ BX, $6
	JCS host
	SUBQ $6, BX
	MOVQ -32(R13), AX
	MOVQ -64(R13), CX
	MOVQ AX, 0(R13)
	MOVQ CX, -32(R13)
	MOVQ -24(R13), AX
	MOVQ -56(R13), CX
	MOVQ AX, 8(R13)
	MOVQ CX, -24(R13)
	MOVQ -16(R13), AX
	MOVQ -48(R13), CX
	MOVQ AX, 16(R13)
	MOVQ CX, -16(R13)
	MOVQ -8(R13), AX
	MOVQ -40(R13), CX
	MOVQ AX, 24(R13)
	MOVQ CX, -8(R13)
	ADDQ $32, R13
	HNEXT_F21(2)
	PCALIGN $128
// fused 22
slot_fused22:
	NEED(1)
	ROOM
	CMPQ BX, $6
	JCS host
	SUBQ $6, BX
	MOVQ (R11)(SI*8), AX
	ADDQ AX, -32(R13)
	ADCQ $0, -24(R13)
	ADCQ $0, -16(R13)
	ADCQ $0, -8(R13)
	HNEXT_F22(3)
	PCALIGN $128
probe_slot:
	GETPC_R9
	LEAQ -(5+(256+const_fusedCount)*128)(R9), R9
	MOVQ R9, F_RET1
	RET
fusedbody_15:
	NEED(3)
	CMPQ BX, $6
	JCS host
	SUBQ $6, BX
	MOVQ -32(R13), AX
	MOVQ -64(R13), CX
	MOVQ -96(R13), DX
	MOVQ AX, -96(R13)
	MOVQ DX, -64(R13)
	MOVQ CX, -32(R13)
	MOVQ -24(R13), AX
	MOVQ -56(R13), CX
	MOVQ -88(R13), DX
	MOVQ AX, -88(R13)
	MOVQ DX, -56(R13)
	MOVQ CX, -24(R13)
	MOVQ -16(R13), AX
	MOVQ -48(R13), CX
	MOVQ -80(R13), DX
	MOVQ AX, -80(R13)
	MOVQ DX, -48(R13)
	MOVQ CX, -16(R13)
	MOVQ -8(R13), AX
	MOVQ -40(R13), CX
	MOVQ -72(R13), DX
	MOVQ AX, -72(R13)
	MOVQ DX, -40(R13)
	MOVQ CX, -8(R13)
	HNEXT_F15(2)
fusedbody_17:
	NEED(4)
	CMPQ BX, $8
	JCS host
	SUBQ $8, BX
	MOVQ -64(R13), AX
	MOVQ -96(R13), CX
	MOVQ -128(R13), DX
	MOVQ AX, -128(R13)
	MOVQ DX, -96(R13)
	MOVQ CX, -64(R13)
	MOVQ -56(R13), AX
	MOVQ -88(R13), CX
	MOVQ -120(R13), DX
	MOVQ AX, -120(R13)
	MOVQ DX, -88(R13)
	MOVQ CX, -56(R13)
	MOVQ -48(R13), AX
	MOVQ -80(R13), CX
	MOVQ -112(R13), DX
	MOVQ AX, -112(R13)
	MOVQ DX, -80(R13)
	MOVQ CX, -48(R13)
	MOVQ -40(R13), AX
	MOVQ -72(R13), CX
	MOVQ -104(R13), DX
	MOVQ AX, -104(R13)
	MOVQ DX, -72(R13)
	MOVQ CX, -40(R13)
	SUBQ $32, R13
	HNEXT_F17(3)
fused_0_jump:
	JUMP_TO(HNEXT_J_F0)
fused_1_jump:
	JUMP_TO(HNEXT_J_F1)
fused_2_jump:
	JUMP_TO(HNEXT_J_F2)
fused_4_jump:
	JUMP_TO(HNEXT_J_F4)
fused_5_jump:
	JUMP_TO(HNEXT_J_F5)
fused_6_jump:
	JUMP_TO(HNEXT_J_F6)
fused_7_jump:
	JUMP_TO(HNEXT_J_F7)
fused_8_jump:
	JUMP_TO(HNEXT_J_F8)
disp_01:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	CMPQ AX, ·slotAddr+648(SB)
	JEQ slot_51
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	JMP AX
disp_02:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+2224(SB)
	JEQ slot_fused22
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	JMP AX
disp_03:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+144(SB)
	JEQ slot_12
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	JMP AX
disp_04:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+792(SB)
	JEQ slot_63
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+2208(SB)
	JEQ slot_fused20
	JMP AX
disp_05:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1080(SB)
	JEQ slot_87
	CMPQ AX, ·slotAddr+1232(SB)
	JEQ slot_9a
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	JMP AX
disp_06:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+2208(SB)
	JEQ slot_fused20
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	JMP AX
disp_07:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+144(SB)
	JEQ slot_12
	CMPQ AX, ·slotAddr+184(SB)
	JEQ slot_17
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+2176(SB)
	JEQ slot_fused16
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	JMP AX
disp_08:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+1176(SB)
	JEQ slot_93
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	JMP AX
disp_09:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+1072(SB)
	JEQ slot_86
	CMPQ AX, ·slotAddr+1176(SB)
	JEQ slot_93
	CMPQ AX, ·slotAddr+136(SB)
	JEQ slot_11
	JMP AX
disp_0a:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+1128(SB)
	JEQ slot_8d
	JMP AX
disp_0b:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+144(SB)
	JEQ slot_12
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	JMP AX
disp_10:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+824(SB)
	JEQ slot_67
	CMPQ AX, ·slotAddr+184(SB)
	JEQ slot_17
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+1176(SB)
	JEQ slot_93
	JMP AX
disp_11:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+184(SB)
	JEQ slot_17
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	JMP AX
disp_12:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	JMP AX
disp_13:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+184(SB)
	JEQ slot_17
	JMP AX
disp_14:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+184(SB)
	JEQ slot_17
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	JMP AX
disp_15:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+184(SB)
	JEQ slot_17
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	JMP AX
disp_16:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+184(SB)
	JEQ slot_17
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	JMP AX
disp_17:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+184(SB)
	JEQ slot_17
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	JMP AX
disp_18:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+784(SB)
	JEQ slot_62
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1208(SB)
	JEQ slot_97
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	JMP AX
disp_19:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1064(SB)
	JEQ slot_85
	JMP AX
disp_1a:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+184(SB)
	JEQ slot_17
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1088(SB)
	JEQ slot_88
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	JMP AX
disp_1b:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2224(SB)
	JEQ slot_fused22
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+856(SB)
	JEQ slot_6b
	JMP AX
disp_1c:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+224(SB)
	JEQ slot_1c
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_1d:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+784(SB)
	JEQ slot_62
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+1184(SB)
	JEQ slot_94
	CMPQ AX, ·slotAddr+1200(SB)
	JEQ slot_96
	CMPQ AX, ·slotAddr+1096(SB)
	JEQ slot_89
	JMP AX
disp_20:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+672(SB)
	JEQ slot_54
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+680(SB)
	JEQ slot_55
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+1064(SB)
	JEQ slot_85
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	JMP AX
disp_30:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+160(SB)
	JEQ slot_14
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+2080(SB)
	JEQ slot_fused4
	CMPQ AX, ·slotAddr+920(SB)
	JEQ slot_73
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	JMP AX
disp_31:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+128(SB)
	JEQ slot_10
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2176(SB)
	JEQ slot_fused16
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	JMP AX
disp_32:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+920(SB)
	JEQ slot_73
	CMPQ AX, ·slotAddr+408(SB)
	JEQ slot_33
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	JMP AX
disp_33:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+920(SB)
	JEQ slot_73
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1072(SB)
	JEQ slot_86
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	JMP AX
disp_34:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+784(SB)
	JEQ slot_62
	CMPQ AX, ·slotAddr+2080(SB)
	JEQ slot_fused4
	CMPQ AX, ·slotAddr+128(SB)
	JEQ slot_10
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_35:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+920(SB)
	JEQ slot_73
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+792(SB)
	JEQ slot_63
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	JMP AX
disp_36:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2088(SB)
	JEQ slot_fused5
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+128(SB)
	JEQ slot_10
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	JMP AX
disp_37:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+488(SB)
	JEQ slot_3d
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	JMP AX
disp_38:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+784(SB)
	JEQ slot_62
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_39:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+432(SB)
	JEQ slot_36
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1944(SB)
	JEQ slot_f3
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	JMP AX
disp_3a:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+32(SB)
	JEQ slot_04
	CMPQ AX, ·slotAddr+2096(SB)
	JEQ slot_fused6
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+720(SB)
	JEQ slot_5a
	CMPQ AX, ·slotAddr+160(SB)
	JEQ slot_14
	CMPQ AX, ·slotAddr+136(SB)
	JEQ slot_11
	JMP AX
disp_3b:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+2096(SB)
	JEQ slot_fused6
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+136(SB)
	JEQ slot_11
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_3c:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	JMP AX
disp_3d:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+488(SB)
	JEQ slot_3d
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	JMP AX
disp_3e:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	JMP AX
disp_3f:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	CMPQ AX, ·slotAddr+160(SB)
	JEQ slot_14
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_40:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	JMP AX
disp_42:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+32(SB)
	JEQ slot_04
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+2096(SB)
	JEQ slot_fused6
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+128(SB)
	JEQ slot_10
	JMP AX
disp_43:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+136(SB)
	JEQ slot_11
	CMPQ AX, ·slotAddr+160(SB)
	JEQ slot_14
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+2096(SB)
	JEQ slot_fused6
	JMP AX
disp_44:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	JMP AX
disp_46:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+160(SB)
	JEQ slot_14
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2224(SB)
	JEQ slot_fused22
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	JMP AX
disp_47:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+128(SB)
	JEQ slot_10
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+2200(SB)
	JEQ slot_fused19
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+2088(SB)
	JEQ slot_fused5
	JMP AX
disp_48:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2096(SB)
	JEQ slot_fused6
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+464(SB)
	JEQ slot_3a
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	JMP AX
disp_50:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	JMP AX
disp_51:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	JMP AX
disp_52:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2224(SB)
	JEQ slot_fused22
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	JMP AX
disp_53:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	JMP AX
disp_54:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	JMP AX
disp_55:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+1064(SB)
	JEQ slot_85
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	JMP AX
disp_57:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+488(SB)
	JEQ slot_3d
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+784(SB)
	JEQ slot_62
	JMP AX
disp_58:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	JMP AX
disp_59:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	JMP AX
disp_5a:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2000(SB)
	JEQ slot_fa
	CMPQ AX, ·slotAddr+1952(SB)
	JEQ slot_f4
	CMPQ AX, ·slotAddr+1928(SB)
	JEQ slot_f1
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1064(SB)
	JEQ slot_85
	JMP AX
disp_5b:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	JMP AX
disp_5c:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2104(SB)
	JEQ slot_fused7
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	JMP AX
disp_5d:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+2136(SB)
	JEQ slot_fused11
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+0(SB)
	JEQ slot_00
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	JMP AX
disp_5e:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+1080(SB)
	JEQ slot_87
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	JMP AX
disp_5f:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	CMPQ AX, ·slotAddr+256(SB)
	JEQ slot_20
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	JMP AX
disp_60:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+2072(SB)
	JEQ slot_fused3
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	CMPQ AX, ·slotAddr+648(SB)
	JEQ slot_51
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	JMP AX
disp_61:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+2168(SB)
	JEQ slot_fused15
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_62:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+696(SB)
	JEQ slot_57
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+2080(SB)
	JEQ slot_fused4
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_63:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2096(SB)
	JEQ slot_fused6
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1176(SB)
	JEQ slot_93
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	JMP AX
disp_64:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	JMP AX
disp_65:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	JMP AX
disp_66:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+832(SB)
	JEQ slot_68
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	JMP AX
disp_67:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	JMP AX
disp_68:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+800(SB)
	JEQ slot_64
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+136(SB)
	JEQ slot_11
	JMP AX
disp_69:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+1200(SB)
	JEQ slot_96
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	JMP AX
disp_6a:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	CMPQ AX, ·slotAddr+1144(SB)
	JEQ slot_8f
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	JMP AX
disp_6b:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+736(SB)
	JEQ slot_5c
	JMP AX
disp_6c:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+136(SB)
	JEQ slot_11
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+848(SB)
	JEQ slot_6a
	JMP AX
disp_6d:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1072(SB)
	JEQ slot_86
	JMP AX
disp_6e:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	JMP AX
disp_6f:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	JMP AX
disp_70:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+192(SB)
	JEQ slot_18
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1088(SB)
	JEQ slot_88
	JMP AX
disp_71:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1064(SB)
	JEQ slot_85
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1224(SB)
	JEQ slot_99
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+1216(SB)
	JEQ slot_98
	JMP AX
disp_72:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+848(SB)
	JEQ slot_6a
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	CMPQ AX, ·slotAddr+1096(SB)
	JEQ slot_89
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	JMP AX
disp_73:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+720(SB)
	JEQ slot_5a
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+160(SB)
	JEQ slot_14
	JMP AX
disp_74:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1064(SB)
	JEQ slot_85
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_75:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+184(SB)
	JEQ slot_17
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	JMP AX
disp_76:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+784(SB)
	JEQ slot_62
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+184(SB)
	JEQ slot_17
	JMP AX
disp_77:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	CMPQ AX, ·slotAddr+1064(SB)
	JEQ slot_85
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+1096(SB)
	JEQ slot_89
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	JMP AX
disp_78:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+2168(SB)
	JEQ slot_fused15
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	JMP AX
disp_79:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+528(SB)
	JEQ slot_42
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1184(SB)
	JEQ slot_94
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1080(SB)
	JEQ slot_87
	JMP AX
disp_7a:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1176(SB)
	JEQ slot_93
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	JMP AX
disp_7b:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+920(SB)
	JEQ slot_73
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	JMP AX
disp_7c:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	CMPQ AX, ·slotAddr+792(SB)
	JEQ slot_63
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	JMP AX
disp_7d:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+1224(SB)
	JEQ slot_99
	CMPQ AX, ·slotAddr+1192(SB)
	JEQ slot_95
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	JMP AX
disp_7e:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+200(SB)
	JEQ slot_19
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+672(SB)
	JEQ slot_54
	CMPQ AX, ·slotAddr+1064(SB)
	JEQ slot_85
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+16(SB)
	JEQ slot_02
	JMP AX
disp_7f:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+672(SB)
	JEQ slot_54
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	JMP AX
disp_80:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2112(SB)
	JEQ slot_fused8
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+792(SB)
	JEQ slot_63
	CMPQ AX, ·slotAddr+648(SB)
	JEQ slot_51
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	JMP AX
disp_81:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+128(SB)
	JEQ slot_10
	CMPQ AX, ·slotAddr+136(SB)
	JEQ slot_11
	CMPQ AX, ·slotAddr+2080(SB)
	JEQ slot_fused4
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2088(SB)
	JEQ slot_fused5
	JMP AX
disp_82:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+32(SB)
	JEQ slot_04
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+136(SB)
	JEQ slot_11
	JMP AX
disp_83:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+128(SB)
	JEQ slot_10
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+2096(SB)
	JEQ slot_fused6
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	JMP AX
disp_84:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+128(SB)
	JEQ slot_10
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	JMP AX
disp_85:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1080(SB)
	JEQ slot_87
	JMP AX
disp_86:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+720(SB)
	JEQ slot_5a
	CMPQ AX, ·slotAddr+1072(SB)
	JEQ slot_86
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	JMP AX
disp_87:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1080(SB)
	JEQ slot_87
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	JMP AX
disp_88:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1072(SB)
	JEQ slot_86
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1088(SB)
	JEQ slot_88
	CMPQ AX, ·slotAddr+136(SB)
	JEQ slot_11
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	JMP AX
disp_89:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+1096(SB)
	JEQ slot_89
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+1072(SB)
	JEQ slot_86
	JMP AX
disp_8a:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+1104(SB)
	JEQ slot_8a
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1088(SB)
	JEQ slot_88
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	JMP AX
disp_8b:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1112(SB)
	JEQ slot_8b
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	JMP AX
disp_8c:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1120(SB)
	JEQ slot_8c
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+224(SB)
	JEQ slot_1c
	JMP AX
disp_8d:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1128(SB)
	JEQ slot_8d
	CMPQ AX, ·slotAddr+1072(SB)
	JEQ slot_86
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+224(SB)
	JEQ slot_1c
	CMPQ AX, ·slotAddr+648(SB)
	JEQ slot_51
	JMP AX
disp_8e:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+1136(SB)
	JEQ slot_8e
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1088(SB)
	JEQ slot_88
	CMPQ AX, ·slotAddr+1080(SB)
	JEQ slot_87
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+224(SB)
	JEQ slot_1c
	JMP AX
disp_8f:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1120(SB)
	JEQ slot_8c
	CMPQ AX, ·slotAddr+1144(SB)
	JEQ slot_8f
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+1072(SB)
	JEQ slot_86
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	JMP AX
disp_90:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+256(SB)
	JEQ slot_20
	JMP AX
disp_91:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	JMP AX
disp_92:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2176(SB)
	JEQ slot_fused16
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+2168(SB)
	JEQ slot_fused15
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	JMP AX
disp_93:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2208(SB)
	JEQ slot_fused20
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	JMP AX
disp_94:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1176(SB)
	JEQ slot_93
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1064(SB)
	JEQ slot_85
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1200(SB)
	JEQ slot_96
	JMP AX
disp_95:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1184(SB)
	JEQ slot_94
	CMPQ AX, ·slotAddr+1072(SB)
	JEQ slot_86
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+1176(SB)
	JEQ slot_93
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1208(SB)
	JEQ slot_97
	CMPQ AX, ·slotAddr+1216(SB)
	JEQ slot_98
	JMP AX
disp_96:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1192(SB)
	JEQ slot_95
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1216(SB)
	JEQ slot_98
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	JMP AX
disp_97:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1200(SB)
	JEQ slot_96
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+1224(SB)
	JEQ slot_99
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1088(SB)
	JEQ slot_88
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_98:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+2152(SB)
	JEQ slot_fused13
	CMPQ AX, ·slotAddr+1200(SB)
	JEQ slot_96
	CMPQ AX, ·slotAddr+1208(SB)
	JEQ slot_97
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+792(SB)
	JEQ slot_63
	JMP AX
disp_99:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1216(SB)
	JEQ slot_98
	CMPQ AX, ·slotAddr+1104(SB)
	JEQ slot_8a
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1080(SB)
	JEQ slot_87
	JMP AX
disp_9a:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1216(SB)
	JEQ slot_98
	CMPQ AX, ·slotAddr+1112(SB)
	JEQ slot_8b
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+1224(SB)
	JEQ slot_99
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	JMP AX
disp_9b:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1232(SB)
	JEQ slot_9a
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1120(SB)
	JEQ slot_8c
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+224(SB)
	JEQ slot_1c
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1224(SB)
	JEQ slot_99
	JMP AX
disp_9c:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1128(SB)
	JEQ slot_8d
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+224(SB)
	JEQ slot_1c
	CMPQ AX, ·slotAddr+2216(SB)
	JEQ slot_fused21
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+784(SB)
	JEQ slot_62
	JMP AX
disp_9d:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1136(SB)
	JEQ slot_8e
	CMPQ AX, ·slotAddr+1216(SB)
	JEQ slot_98
	CMPQ AX, ·slotAddr+1240(SB)
	JEQ slot_9b
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1248(SB)
	JEQ slot_9c
	CMPQ AX, ·slotAddr+1064(SB)
	JEQ slot_85
	CMPQ AX, ·slotAddr+2048(SB)
	JEQ slot_fused0
	JMP AX
disp_9e:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1256(SB)
	JEQ slot_9d
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1192(SB)
	JEQ slot_95
	CMPQ AX, ·slotAddr+1144(SB)
	JEQ slot_8f
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	JMP AX
disp_9f:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+920(SB)
	JEQ slot_73
	CMPQ AX, ·slotAddr+1208(SB)
	JEQ slot_97
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+1264(SB)
	JEQ slot_9e
	CMPQ AX, ·slotAddr+1256(SB)
	JEQ slot_9d
	JMP AX
disp_a1:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+0(SB)
	JEQ slot_00
	CMPQ AX, ·slotAddr+720(SB)
	JEQ slot_5a
	JMP AX
disp_a2:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+2152(SB)
	JEQ slot_fused13
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+0(SB)
	JEQ slot_00
	JMP AX
disp_a3:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+1072(SB)
	JEQ slot_86
	JMP AX
disp_a4:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+2136(SB)
	JEQ slot_fused11
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+648(SB)
	JEQ slot_51
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	JMP AX
disp_f0:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	JMP AX
disp_f1:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+2176(SB)
	JEQ slot_fused16
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+2208(SB)
	JEQ slot_fused20
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+488(SB)
	JEQ slot_3d
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_f4:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+488(SB)
	JEQ slot_3d
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+2176(SB)
	JEQ slot_fused16
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	JMP AX
disp_f5:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+1208(SB)
	JEQ slot_97
	CMPQ AX, ·slotAddr+1176(SB)
	JEQ slot_93
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+1184(SB)
	JEQ slot_94
	JMP AX
disp_fa:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+168(SB)
	JEQ slot_15
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2208(SB)
	JEQ slot_fused20
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+2176(SB)
	JEQ slot_fused16
	CMPQ AX, ·slotAddr+2064(SB)
	JEQ slot_fused2
	JMP AX
disp_F0:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+648(SB)
	JEQ slot_51
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	JMP AX
disp_F2:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+488(SB)
	JEQ slot_3d
	JMP AX
disp_F3:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+176(SB)
	JEQ slot_16
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+2072(SB)
	JEQ slot_fused3
	JMP AX
disp_F4:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+2224(SB)
	JEQ slot_fused22
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	JMP AX
disp_F5:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	JMP AX
disp_F6:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+2224(SB)
	JEQ slot_fused22
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	JMP AX
disp_F7:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+2072(SB)
	JEQ slot_fused3
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	JMP AX
disp_F8:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+2112(SB)
	JEQ slot_fused8
	CMPQ AX, ·slotAddr+792(SB)
	JEQ slot_63
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	JMP AX
disp_F10:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+2160(SB)
	JEQ slot_fused14
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	JMP AX
disp_F12:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+2152(SB)
	JEQ slot_fused13
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2136(SB)
	JEQ slot_fused11
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_F14:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+680(SB)
	JEQ slot_55
	CMPQ AX, ·slotAddr+160(SB)
	JEQ slot_14
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	JMP AX
disp_F15:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_F16:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2152(SB)
	JEQ slot_fused13
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+2184(SB)
	JEQ slot_fused17
	CMPQ AX, ·slotAddr+2176(SB)
	JEQ slot_fused16
	JMP AX
disp_F17:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2152(SB)
	JEQ slot_fused13
	CMPQ AX, ·slotAddr+688(SB)
	JEQ slot_56
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+1104(SB)
	JEQ slot_8a
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	JMP AX
disp_F18:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2160(SB)
	JEQ slot_fused14
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+728(SB)
	JEQ slot_5b
	CMPQ AX, ·slotAddr+1016(SB)
	JEQ slot_7f
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	JMP AX
disp_F19:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2184(SB)
	JEQ slot_fused17
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+680(SB)
	JEQ slot_55
	JMP AX
disp_F20:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+2136(SB)
	JEQ slot_fused11
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	JMP AX
disp_F21:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+24(SB)
	JEQ slot_03
	CMPQ AX, ·slotAddr+656(SB)
	JEQ slot_52
	CMPQ AX, ·slotAddr+680(SB)
	JEQ slot_55
	CMPQ AX, ·slotAddr+88(SB)
	JEQ slot_0b
	CMPQ AX, ·slotAddr+224(SB)
	JEQ slot_1c
	CMPQ AX, ·slotAddr+256(SB)
	JEQ slot_20
	CMPQ AX, ·slotAddr+216(SB)
	JEQ slot_1b
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	JMP AX
disp_F22:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2176(SB)
	JEQ slot_fused16
	CMPQ AX, ·slotAddr+648(SB)
	JEQ slot_51
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+8(SB)
	JEQ slot_01
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	JMP AX
disp_J_56:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2176(SB)
	JEQ slot_fused16
	CMPQ AX, ·slotAddr+2208(SB)
	JEQ slot_fused20
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	JMP AX
disp_J_57:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1944(SB)
	JEQ slot_f3
	CMPQ AX, ·slotAddr+2152(SB)
	JEQ slot_fused13
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+784(SB)
	JEQ slot_62
	JMP AX
disp_J_F0:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+32(SB)
	JEQ slot_04
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	JMP AX
disp_J_F1:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2200(SB)
	JEQ slot_fused19
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	JMP AX
disp_J_F2:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1168(SB)
	JEQ slot_92
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	JMP AX
disp_J_F4:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+2152(SB)
	JEQ slot_fused13
	CMPQ AX, ·slotAddr+2160(SB)
	JEQ slot_fused14
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1176(SB)
	JEQ slot_93
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+1192(SB)
	JEQ slot_95
	JMP AX
disp_J_F5:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2128(SB)
	JEQ slot_fused10
	CMPQ AX, ·slotAddr+2224(SB)
	JEQ slot_fused22
	CMPQ AX, ·slotAddr+1152(SB)
	JEQ slot_90
	CMPQ AX, ·slotAddr+1040(SB)
	JEQ slot_82
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	JMP AX
disp_J_F6:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+2144(SB)
	JEQ slot_fused12
	CMPQ AX, ·slotAddr+1056(SB)
	JEQ slot_84
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	JMP AX
disp_J_F7:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+1160(SB)
	JEQ slot_91
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+1024(SB)
	JEQ slot_80
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+920(SB)
	JEQ slot_73
	CMPQ AX, ·slotAddr+2224(SB)
	JEQ slot_fused22
	CMPQ AX, ·slotAddr+1048(SB)
	JEQ slot_83
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	JMP AX
disp_J_F8:
	MOVQ (R12)(SI*8), AX
	CMPQ AX, ·slotAddr+776(SB)
	JEQ slot_61
	CMPQ AX, ·slotAddr+416(SB)
	JEQ slot_34
	CMPQ AX, ·slotAddr+640(SB)
	JEQ slot_50
	CMPQ AX, ·slotAddr+768(SB)
	JEQ slot_60
	CMPQ AX, ·slotAddr+2056(SB)
	JEQ slot_fused1
	CMPQ AX, ·slotAddr+1032(SB)
	JEQ slot_81
	CMPQ AX, ·slotAddr+384(SB)
	JEQ slot_30
	CMPQ AX, ·slotAddr+760(SB)
	JEQ slot_5f
	JMP AX
// END SLOTS

op_mul:
	NEED(2)
	USEGAS(5, undo5)
	MUL256(-32, -64, -64, R13)
	SUBQ $32, R13
	HNEXT_02(1)

// Pops a, sets the new top to zero.
bin_zero:
	ZERO_STORE(-64)
	SUBQ $32, R13
	NEXT(1)

// DIV, MOD, SDIV, SMOD: a (top) / b. The divisor is not zero here.
op_div:
	NEED(2)
	USEGAS(5, undo5)
	MOVQ -64(R13), AX
	ORQ -56(R13), AX
	ORQ -48(R13), AX
	ORQ -40(R13), AX
	JEQ bin_zero
	MOVQ -24(R13), AX
	ORQ -16(R13), AX
	ORQ -8(R13), AX
	ORQ -56(R13), AX
	ORQ -48(R13), AX
	ORQ -40(R13), AX
	JNE div_wide
	MOVQ -32(R13), AX
	XORL DX, DX
	DIVQ -64(R13)
	WORD_STORE(-64)
	SUBQ $32, R13
	NEXT(1)
div_wide:
	COPY4(-32, R13, L_U, SP)
	COPY4(-64, R13, L_D, SP)
	MOVQ $1, L_RET(SP)
	MOVQ $4, R8
	JMP udivrem
div_ret:
	COPY4(L_Q, SP, -64, R13)
	SUBQ $32, R13
	NEXT(1)

op_mod:
	NEED(2)
	USEGAS(5, undo5)
	MOVQ -64(R13), AX
	ORQ -56(R13), AX
	ORQ -48(R13), AX
	ORQ -40(R13), AX
	JEQ bin_zero
	MOVQ -24(R13), AX
	ORQ -16(R13), AX
	ORQ -8(R13), AX
	ORQ -56(R13), AX
	ORQ -48(R13), AX
	ORQ -40(R13), AX
	JNE mod_wide
	MOVQ -32(R13), AX
	XORL DX, DX
	DIVQ -64(R13)
	MOVQ DX, AX
	WORD_STORE(-64)
	SUBQ $32, R13
	NEXT(1)
mod_wide:
	COPY4(-32, R13, L_U, SP)
	COPY4(-64, R13, L_D, SP)
	MOVQ $2, L_RET(SP)
	MOVQ $4, R8
	JMP udivrem
mod_ret:
	COPY4(L_R, SP, -64, R13)
	SUBQ $32, R13
	NEXT(1)

op_sdiv:
	NEED(2)
	USEGAS(5, undo5)
	MOVQ -64(R13), AX
	ORQ -56(R13), AX
	ORQ -48(R13), AX
	ORQ -40(R13), AX
	JEQ bin_zero
	COPY4(-32, R13, L_U, SP)
	COPY4(-64, R13, L_D, SP)
	MOVQ -8(R13), CX
	SARQ $63, CX
	NEGIF(L_U, CX)
	MOVQ -40(R13), DX
	SARQ $63, DX
	NEGIF(L_D, DX)
	XORQ DX, CX
	MOVQ CX, L_T1(SP)
	MOVQ $3, L_RET(SP)
	MOVQ $4, R8
	JMP udivrem
sdiv_ret:
	MOVQ L_T1(SP), CX
	NEGIF(L_Q, CX)
	COPY4(L_Q, SP, -64, R13)
	SUBQ $32, R13
	NEXT(1)

op_smod:
	NEED(2)
	USEGAS(5, undo5)
	MOVQ -64(R13), AX
	ORQ -56(R13), AX
	ORQ -48(R13), AX
	ORQ -40(R13), AX
	JEQ bin_zero
	COPY4(-32, R13, L_U, SP)
	COPY4(-64, R13, L_D, SP)
	MOVQ -8(R13), CX
	SARQ $63, CX
	NEGIF(L_U, CX)
	MOVQ -40(R13), DX
	SARQ $63, DX
	NEGIF(L_D, DX)
	MOVQ CX, L_T1(SP)
	MOVQ $4, L_RET(SP)
	MOVQ $4, R8
	JMP udivrem
smod_ret:
	MOVQ L_T1(SP), CX
	NEGIF(L_R, CX)
	COPY4(L_R, SP, -64, R13)
	SUBQ $32, R13
	NEXT(1)

// ADDMOD, MULMOD: (a op b) % n with n the third item; n == 0 gives 0.
op_addmod:
	NEED(3)
	USEGAS(8, undo8)
	MOVQ -96(R13), AX
	ORQ -88(R13), AX
	ORQ -80(R13), AX
	ORQ -72(R13), AX
	JEQ addmod_zero
	COPY4(-96, R13, L_D, SP)
	MOVQ -32(R13), AX
	ADDQ -64(R13), AX
	MOVQ AX, L_U(SP)
	MOVQ -24(R13), AX
	ADCQ -56(R13), AX
	MOVQ AX, L_U+8(SP)
	MOVQ -16(R13), AX
	ADCQ -48(R13), AX
	MOVQ AX, L_U+16(SP)
	MOVQ -8(R13), AX
	ADCQ -40(R13), AX
	MOVQ AX, L_U+24(SP)
	SETCS AX
	MOVBQZX AX, AX
	MOVQ AX, L_U+32(SP)
	MOVQ $5, L_RET(SP)
	MOVQ $5, R8
	JMP udivrem
modop_ret:
	COPY4(L_R, SP, -96, R13)
	SUBQ $64, R13
	NEXT(1)
addmod_zero:
	ZERO_STORE(-96)
	SUBQ $64, R13
	NEXT(1)

op_mulmod:
	NEED(3)
	USEGAS(8, undo8)
	MOVQ -96(R13), AX
	ORQ -88(R13), AX
	ORQ -80(R13), AX
	ORQ -72(R13), AX
	JEQ addmod_zero
	COPY4(-96, R13, L_D, SP)
	// U[0..7] = a * b, schoolbook; R10-R12 are scratch until udivrem returns.
	XORL AX, AX
	MOVQ AX, L_U+32(SP)
	MOVQ AX, L_U+40(SP)
	MOVQ AX, L_U+48(SP)
	MOVQ AX, L_U+56(SP)
	XORL R8, R8 // i
	XORL CX, CX // carry
mulmod_j0:
	// row 0 initialises U[0..4]
	MOVQ -32(R13), AX
	MULQ -64(R13)(R8*8)
	ADDQ CX, AX
	ADCQ $0, DX
	MOVQ AX, L_U(SP)(R8*8)
	MOVQ DX, CX
	ADDQ $1, R8
	CMPQ R8, $4
	JCS mulmod_j0
	MOVQ CX, L_U+32(SP)
	MOVQ $1, R10 // i
mulmod_i:
	XORL R8, R8 // j
	XORL CX, CX
	MOVQ -32(R13)(R10*8), R15 // a[i]
mulmod_j:
	MOVQ R15, AX
	MULQ -64(R13)(R8*8)
	ADDQ CX, AX
	ADCQ $0, DX
	LEAQ (R10)(R8*1), R11
	ADDQ AX, L_U(SP)(R11*8)
	ADCQ $0, DX
	MOVQ DX, CX
	ADDQ $1, R8
	CMPQ R8, $4
	JCS mulmod_j
	MOVQ CX, L_U+32(SP)(R10*8)
	ADDQ $1, R10
	CMPQ R10, $4
	JCS mulmod_i
	MOVQ $6, L_RET(SP)
	MOVQ $8, R8
	JMP udivrem

// EXP: a ** b with b the exponent (second item).
op_exp:
	NEED(2)
	// R8 = bit length of the exponent.
	MOVQ $256, R8
	MOVQ -40(R13), AX
	TESTQ AX, AX
	JNE exp_len
	MOVQ $192, R8
	MOVQ -48(R13), AX
	TESTQ AX, AX
	JNE exp_len
	MOVQ $128, R8
	MOVQ -56(R13), AX
	TESTQ AX, AX
	JNE exp_len
	MOVQ $64, R8
	MOVQ -64(R13), AX
	TESTQ AX, AX
	JNE exp_len
	XORL R8, R8
	JMP exp_gas
exp_len:
	BSRQ AX, CX
	SUBQ $63, R8
	ADDQ CX, R8
exp_gas:
	// gas = 10 + bytes * (50 after EIP-158, else 10)
	LEAQ 7(R8), R9
	SHRQ $3, R9
	MOVQ $10, AX
	TESTB $const_asmFlagExpEIP158, F_FLAGS
	JEQ exp_mul
	MOVQ $50, AX
exp_mul:
	IMULQ AX, R9
	ADDQ $10, R9
	CMPQ BX, R9
	JCS host
	SUBQ R9, BX
	// Square and multiply over the exponent bits, LSB first.
	MOVQ R8, L_T2(SP)         // bit length
	MOVQ $0, L_T1(SP)         // bit index
	MOVQ $1, L_E(SP)          // result
	MOVQ $0, L_E+8(SP)
	MOVQ $0, L_E+16(SP)
	MOVQ $0, L_E+24(SP)
	COPY4(-32, R13, L_E+32, SP)  // base
	COPY4(-64, R13, L_E+96, SP)  // exponent
exp_loop:
	MOVQ L_T1(SP), CX
	CMPQ CX, L_T2(SP)
	JCC exp_done
	MOVQ CX, R8
	SHRQ $6, R8
	MOVQ L_E+96(SP)(R8*8), AX
	SHRQ CX, AX
	TESTQ $1, AX
	JEQ exp_square
	MUL256(L_E+32, L_E, L_E, SP)
exp_square:
	MOVQ L_T1(SP), CX
	ADDQ $1, CX
	MOVQ CX, L_T1(SP)
	CMPQ CX, L_T2(SP)
	JCC exp_done
	MUL256(L_E+32, L_E+32, L_E+64, SP)
	COPY4(L_E+64, SP, L_E+32, SP)
	JMP exp_loop
exp_done:
	COPY4(L_E, SP, -64, R13)
	SUBQ $32, R13
	NEXT(1)

// SIGNEXTEND: b (top) is the byte index, x the value.
op_signextend:
	NEED(2)
	USEGAS(5, undo5)
	MOVQ -24(R13), AX
	ORQ -16(R13), AX
	ORQ -8(R13), AX
	JNE signext_keep
	MOVQ -32(R13), AX
	CMPQ AX, $31
	JCC signext_keep
	LEAQ 7(AX*8), CX          // sign bit index
	MOVQ CX, R8
	SHRQ $6, R8               // its limb
	ANDQ $63, CX              // its position
	MOVQ -64(R13)(R8*8), R9
	MOVQ R9, R15
	SHRQ CX, R15
	ANDQ $1, R15
	NEGQ R15                  // sign mask
	MOVQ $2, DX
	SHLQ CX, DX
	SUBQ $1, DX               // bits at and below the sign bit
	ANDQ DX, R9
	NOTQ DX
	ANDQ R15, DX
	ORQ DX, R9
	MOVQ R9, -64(R13)(R8*8)
signext_fill:
	ADDQ $1, R8
	CMPQ R8, $4
	JCC signext_keep
	MOVQ R15, -64(R13)(R8*8)
	JMP signext_fill
signext_keep:
	SUBQ $32, R13
	HNEXT_0b(1)

// BYTE: i (top), x.
op_byte:
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -24(R13), AX
	ORQ -16(R13), AX
	ORQ -8(R13), AX
	JNE bin_zero
	MOVQ -32(R13), AX
	CMPQ AX, $32
	JCC bin_zero
	MOVQ $31, CX
	SUBQ AX, CX
	MOVBQZX -64(R13)(CX*1), AX
	WORD_STORE(-64)
	SUBQ $32, R13
	NEXT(1)

// SHL, SHR, SAR: shift (top), value. Limb shift n = s/64, bit shift s%64 with
// SHLD/SHRD over an 8-word buffer.
op_shl:
	TESTB $const_asmFlagShifts, F_FLAGS
	JEQ host
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -24(R13), AX
	ORQ -16(R13), AX
	ORQ -8(R13), AX
	JNE bin_zero
	MOVQ -32(R13), CX
	CMPQ CX, $256
	JCC bin_zero
	XORL AX, AX
	MOVQ AX, L_SCR(SP)
	MOVQ AX, L_SCR+8(SP)
	MOVQ AX, L_SCR+16(SP)
	MOVQ AX, L_SCR+24(SP)
	COPY4(-64, R13, L_SCR+32, SP)
	// p = &buf[4-n]; res[j] = p[j]<<b | p[j-1]>>(64-b)
	MOVQ CX, R8
	SHRQ $6, R8
	NEGQ R8
	LEAQ L_SCR+32(SP)(R8*8), R8
	ANDQ $63, CX
	MOVQ 24(R8), AX
	MOVQ 16(R8), DX
	SHLQ CX, DX, AX
	MOVQ AX, -40(R13)
	MOVQ 8(R8), AX
	SHLQ CX, AX, DX
	MOVQ DX, -48(R13)
	MOVQ (R8), DX
	SHLQ CX, DX, AX
	MOVQ AX, -56(R13)
	MOVQ -8(R8), AX
	SHLQ CX, AX, DX
	MOVQ DX, -64(R13)
	SUBQ $32, R13
	HNEXT_1b(1)

op_shr:
	TESTB $const_asmFlagShifts, F_FLAGS
	JEQ host
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -24(R13), AX
	ORQ -16(R13), AX
	ORQ -8(R13), AX
	JNE bin_zero
	MOVQ -32(R13), CX
	CMPQ CX, $256
	JCC bin_zero
	XORL R15, R15
	JMP shr_common

op_sar:
	TESTB $const_asmFlagShifts, F_FLAGS
	JEQ host
	NEED(2)
	USEGAS(3, undo3)
	MOVQ -40(R13), R15
	SARQ $63, R15             // fill
	MOVQ -24(R13), AX
	ORQ -16(R13), AX
	ORQ -8(R13), AX
	JNE sar_fill
	MOVQ -32(R13), CX
	CMPQ CX, $256
	JCC sar_fill

shr_common:
	COPY4(-64, R13, L_SCR, SP)
	MOVQ R15, L_SCR+32(SP)
	MOVQ R15, L_SCR+40(SP)
	MOVQ R15, L_SCR+48(SP)
	MOVQ R15, L_SCR+56(SP)
	// p = &buf[n]; res[j] = p[j]>>b | p[j+1]<<(64-b)
	MOVQ CX, R8
	SHRQ $6, R8
	LEAQ L_SCR(SP)(R8*8), R8
	ANDQ $63, CX
	MOVQ (R8), AX
	MOVQ 8(R8), DX
	SHRQ CX, DX, AX
	MOVQ AX, -64(R13)
	MOVQ 16(R8), AX
	SHRQ CX, AX, DX
	MOVQ DX, -56(R13)
	MOVQ 24(R8), DX
	SHRQ CX, DX, AX
	MOVQ AX, -48(R13)
	MOVQ 32(R8), AX
	SHRQ CX, AX, DX
	MOVQ DX, -40(R13)
	SUBQ $32, R13
	HNEXT_1c(1)

sar_fill:
	MOVQ R15, -64(R13)
	MOVQ R15, -56(R13)
	MOVQ R15, -48(R13)
	MOVQ R15, -40(R13)
	SUBQ $32, R13
	NEXT(1)

op_calldataload:
	NEED(1)
	USEGAS(3, undo3)
	MOVQ -24(R13), AX
	ORQ -16(R13), AX
	ORQ -8(R13), AX
	JNE cdl_zero
	MOVQ -32(R13), AX
	MOVQ F_INPUTLEN, CX
	CMPQ AX, CX
	JCC cdl_zero
	MOVQ F_INPUT, R8
	ADDQ AX, R8
	LEAQ 32(AX), DX
	CMPQ DX, CX
	JHI cdl_partial
	MOVQ (R8), AX
	BSWAPQ AX
	MOVQ AX, -8(R13)
	MOVQ 8(R8), AX
	BSWAPQ AX
	MOVQ AX, -16(R13)
	MOVQ 16(R8), AX
	BSWAPQ AX
	MOVQ AX, -24(R13)
	MOVQ 24(R8), AX
	BSWAPQ AX
	MOVQ AX, -32(R13)
	HNEXT_35(1)
cdl_partial:
	// Byte i of the data is byte 31-i of the little-endian word.
	SUBQ AX, CX               // bytes available, 1..31
	ZERO_STORE(-32)
	LEAQ -1(R13), R9
cdl_loop:
	MOVB (R8), AX
	MOVB AX, (R9)
	ADDQ $1, R8
	SUBQ $1, R9
	SUBQ $1, CX
	JNE cdl_loop
	NEXT(1)
cdl_zero:
	ZERO_STORE(-32)
	NEXT(1)

op_mload:
	NEED(1)
	SMALL(-32, AX)
	LEAQ 32(AX), CX
	MOVQ $3, DX
	MEMEXP(mload_charge, mload_zero, mload_done)
	MOVQ -32(R13), R8
	ADDQ F_MEM, R8
	MOVQ (R8), AX
	BSWAPQ AX
	MOVQ AX, -8(R13)
	MOVQ 8(R8), AX
	BSWAPQ AX
	MOVQ AX, -16(R13)
	MOVQ 16(R8), AX
	BSWAPQ AX
	MOVQ AX, -24(R13)
	MOVQ 24(R8), AX
	BSWAPQ AX
	MOVQ AX, -32(R13)
	HNEXT_51(1)

op_mstore:
	NEED(2)
	SMALL(-32, AX)
	LEAQ 32(AX), CX
	MOVQ $3, DX
	MEMEXP(mstore_charge, mstore_zero, mstore_done)
	MOVQ -32(R13), R8
	ADDQ F_MEM, R8
	MOVQ -40(R13), AX
	BSWAPQ AX
	MOVQ AX, (R8)
	MOVQ -48(R13), AX
	BSWAPQ AX
	MOVQ AX, 8(R8)
	MOVQ -56(R13), AX
	BSWAPQ AX
	MOVQ AX, 16(R8)
	MOVQ -64(R13), AX
	BSWAPQ AX
	MOVQ AX, 24(R8)
	SUBQ $64, R13
	HNEXT_52(1)

op_mstore8:
	NEED(2)
	SMALL(-32, AX)
	LEAQ 1(AX), CX
	MOVQ $3, DX
	MEMEXP(mstore8_charge, mstore8_zero, mstore8_done)
	MOVQ -32(R13), R8
	ADDQ F_MEM, R8
	MOVB -64(R13), AX
	MOVB AX, (R8)
	SUBQ $64, R13
	NEXT(1)

// Copy operands: a (top) destination offset, b source offset, c length.
// COPY_GAS loads the length (or host unless below 2^32) into CX... and the
// static plus copy gas 3 + 3*words(c) into DX.
#define COPY_GAS \
	SMALL(-96, CX); \
	LEAQ 31(CX), DX; \
	SHRQ $5, DX; \
	LEAQ 3(DX)(DX*2), DX

op_mcopy:
	TESTB $const_asmFlagMcopy, F_FLAGS
	JEQ host
	NEED(3)
	COPY_GAS
	TESTQ CX, CX
	JEQ copy_empty
	SMALL(-32, AX)
	SMALL(-64, R8)
	CMPQ AX, R8
	JCC mcopy_max
	MOVQ R8, AX
mcopy_max:
	ADDQ AX, CX
	MEMEXP(mcopy_charge, mcopy_zero, mcopy_done)
	MOVQ F_MEM, R8
	MOVQ R8, R9
	ADDQ -32(R13), R8         // destination
	ADDQ -64(R13), R9         // source
	MOVQ -96(R13), CX         // length
	CMPQ R8, R9
	JLS mcopy_fwd
	LEAQ (R9)(CX*1), AX
	CMPQ AX, R8
	JLS mcopy_fwd
	// Overlapping with the destination above the source: copy backwards.
	ADDQ CX, R8
	ADDQ CX, R9
mcopy_back8:
	CMPQ CX, $8
	JCS mcopy_back1
	SUBQ $8, R8
	SUBQ $8, R9
	MOVQ (R9), AX
	MOVQ AX, (R8)
	SUBQ $8, CX
	JMP mcopy_back8
mcopy_back1:
	TESTQ CX, CX
	JEQ mcopy_end
	SUBQ $1, R8
	SUBQ $1, R9
	MOVB (R9), AX
	MOVB AX, (R8)
	SUBQ $1, CX
	JMP mcopy_back1
mcopy_fwd:
	CMPQ CX, $8
	JCS mcopy_fwd1
	MOVQ (R9), AX
	MOVQ AX, (R8)
	ADDQ $8, R8
	ADDQ $8, R9
	SUBQ $8, CX
	JMP mcopy_fwd
mcopy_fwd1:
	TESTQ CX, CX
	JEQ mcopy_end
	MOVB (R9), AX
	MOVB AX, (R8)
	ADDQ $1, R8
	ADDQ $1, R9
	SUBQ $1, CX
	JMP mcopy_fwd1
mcopy_end:
	SUBQ $96, R13
	NEXT(1)

copy_empty:
	XORL CX, CX
	MEMEXP(empty_charge, empty_zero, empty_done)
	SUBQ $96, R13
	NEXT(1)

op_calldatacopy:
	NEED(3)
	COPY_GAS
	TESTQ CX, CX
	JEQ copy_empty
	SMALL(-32, AX)
	ADDQ AX, CX
	MEMEXP(cdc_charge, cdc_zero, cdc_done)
	MOVQ F_INPUT, R9
	MOVQ F_INPUTLEN, R15
	JMP copy_padded

// RETURNDATACOPY: like CALLDATACOPY from the return data (asmFrame.retPtr,
// retLen); a range past its end is an error, which the host raises.
op_returndatacopy:
	TESTB $const_asmFlagReturnData, F_FLAGS
	JEQ host
	NEED(3)
	COPY_GAS
	SMALL(-64, AX)
	LEAQ (AX)(CX*1), R9
	CMPQ R9, F_RETLEN
	JHI host
	TESTQ CX, CX
	JEQ copy_empty
	SMALL(-32, AX)
	ADDQ AX, CX
	MEMEXP(rdc_charge, rdc_zero, rdc_done)
	MOVQ asmFrame_retPtr(DI), R9
	MOVQ F_RETLEN, R15
	JMP copy_padded

op_codecopy:
	NEED(3)
	COPY_GAS
	TESTQ CX, CX
	JEQ copy_empty
	SMALL(-32, AX)
	ADDQ AX, CX
	MEMEXP(cc_charge, cc_zero, cc_done)
	MOVQ F_CODE, R9
	MOVQ F_CODELEN, R15

// Copies c bytes from source R9 (length R15) at offset b to memory offset a,
// padding with zeros like getData.
copy_padded:
	MOVQ F_MEM, R8
	ADDQ -32(R13), R8         // destination
	MOVQ -96(R13), CX         // total
	XORL DX, DX               // available
	MOVQ -56(R13), AX
	ORQ -48(R13), AX
	ORQ -40(R13), AX
	JNE copy_zero
	MOVQ -64(R13), AX
	CMPQ AX, R15
	JCC copy_zero
	MOVQ R15, DX
	SUBQ AX, DX
	CMPQ DX, CX
	JLS copy_avail
	MOVQ CX, DX
copy_avail:
	ADDQ AX, R9
	SUBQ DX, CX               // zero tail
	MOVQ DX, R15
copy_fwd:
	CMPQ R15, $8
	JCS copy_fwd1
	MOVQ (R9), AX
	MOVQ AX, (R8)
	ADDQ $8, R8
	ADDQ $8, R9
	SUBQ $8, R15
	JMP copy_fwd
copy_fwd1:
	TESTQ R15, R15
	JEQ copy_zero
	MOVB (R9), AX
	MOVB AX, (R8)
	ADDQ $1, R8
	ADDQ $1, R9
	SUBQ $1, R15
	JMP copy_fwd1
copy_zero:
	// CX bytes of zeros at R8.
	XORL AX, AX
copy_zero8:
	CMPQ CX, $8
	JCS copy_zero1
	MOVQ AX, (R8)
	ADDQ $8, R8
	SUBQ $8, CX
	JMP copy_zero8
copy_zero1:
	TESTQ CX, CX
	JEQ copy_end
	MOVB AX, (R8)
	ADDQ $1, R8
	SUBQ $1, CX
	JMP copy_zero1
copy_end:
	SUBQ $96, R13
	NEXT(1)

// KECCAK256: offset (top), size. The state and the padded last block live in
// the frame; the permutation is ·keccakF1600 (keccak_amd64.s), CALLed with
// all live registers saved in the frame like a host call.
op_keccak:
	NEED(2)
	SMALL(-64, R8)
	LEAQ 31(R8), DX
	SHRQ $5, DX
	IMULQ $6, DX
	ADDQ $30, DX
	XORL CX, CX
	TESTQ R8, R8
	JEQ keccak_mem
	SMALL(-32, CX)
	ADDQ R8, CX
keccak_mem:
	MEMEXP(keccak_charge, keccak_zero, keccak_done)
	SUBQ $32, R13             // the result replaces the offset item
	// 64-byte input: look it up in the cache (R9), first word in AX.
	CMPQ -32(R13), $64
	JNE keccak_hash
	MOVQ F_KCACHE, R9
	TESTQ R9, R9
	JEQ keccak_hash
	MOVQ F_MEM, R8
	ADDQ (R13), R8
	MOVQ keccakCache_tab(R9), R15
	TESTQ R15, R15
	JNE keccak_memo
	MOVQ (R8), AX
	LEAQ keccakCache_e(R9), R15
	MOVQ keccakCache_n(R9), DX
	IMUL3Q $96, DX, DX
	ADDQ R15, DX
	JMP keccak_more
keccak_memo:                  // direct-mapped: one entry to compare
	MOVQ 0(R8), AX
	XORQ 8(R8), AX
	XORQ 16(R8), AX
	XORQ 24(R8), AX
	XORQ 32(R8), AX
	XORQ 40(R8), AX
	XORQ 48(R8), AX
	XORQ 56(R8), AX
	MOVQ $const_slotHashMul, CX
	IMULQ CX, AX
	MOVQ keccakCache_shift(R9), CX
	SHRQ CX, AX
	IMUL3Q $96, AX, AX
	ADDQ AX, R15
	LEAQ 96(R15), DX
	MOVQ (R8), AX
	JMP keccak_probe
keccak_probe:
	CMPQ AX, 0(R15)
	JNE keccak_miss
	MOVQ 8(R8), CX
	CMPQ CX, 8(R15)
	JNE keccak_miss
	MOVQ 16(R8), CX
	CMPQ CX, 16(R15)
	JNE keccak_miss
	MOVQ 24(R8), CX
	CMPQ CX, 24(R15)
	JNE keccak_miss
	MOVQ 32(R8), CX
	CMPQ CX, 32(R15)
	JNE keccak_miss
	MOVQ 40(R8), CX
	CMPQ CX, 40(R15)
	JNE keccak_miss
	MOVQ 48(R8), CX
	CMPQ CX, 48(R15)
	JNE keccak_miss
	MOVQ 56(R8), CX
	CMPQ CX, 56(R15)
	JNE keccak_miss
	COPY4(64, R15, -32, R13)
	NEXT(1)
keccak_miss:
	ADDQ $96, R15
keccak_more:
	CMPQ R15, DX
	JCS keccak_probe
keccak_hash:
	SAVE
	MOVQ (R13), AX            // offset (unused when the size is 0)
	MOVQ AX, L_T1(SP)
	MOVQ -32(R13), AX         // size
	MOVQ AX, L_T2(SP)
	MOVQ AX, L_E(SP)          // the size again, kept
	XORL AX, AX
	LEAQ asmFrame_kstate(DI), R8
	MOVQ AX, 0(R8)
	MOVQ AX, 8(R8)
	MOVQ AX, 16(R8)
	MOVQ AX, 24(R8)
	MOVQ AX, 32(R8)
	MOVQ AX, 40(R8)
	MOVQ AX, 48(R8)
	MOVQ AX, 56(R8)
	MOVQ AX, 64(R8)
	MOVQ AX, 72(R8)
	MOVQ AX, 80(R8)
	MOVQ AX, 88(R8)
	MOVQ AX, 96(R8)
	MOVQ AX, 104(R8)
	MOVQ AX, 112(R8)
	MOVQ AX, 120(R8)
	MOVQ AX, 128(R8)
	MOVQ AX, 136(R8)
	MOVQ AX, 144(R8)
	MOVQ AX, 152(R8)
	MOVQ AX, 160(R8)
	MOVQ AX, 168(R8)
	MOVQ AX, 176(R8)
	MOVQ AX, 184(R8)
	MOVQ AX, 192(R8)
keccak_block:
	MOVQ L_T2(SP), CX
	CMPQ CX, $136
	JCS keccak_last
	MOVQ F_MEM, R8
	ADDQ L_T1(SP), R8
	ABSORB(R8)
	ADDQ $136, L_T1(SP)
	SUBQ $136, L_T2(SP)
	LEAQ asmFrame_kstate(DI), AX
	MOVQ AX, 0(SP)
	CALL ·keccakF1600(SB)
	MOVQ L_F(SP), DI
	JMP keccak_block
keccak_last:
	// Legacy Keccak padding: 0x01 after the data, 0x80 in the last byte.
	LEAQ asmFrame_kpad(DI), R8
	XORL AX, AX
	MOVQ AX, 0(R8)
	MOVQ AX, 8(R8)
	MOVQ AX, 16(R8)
	MOVQ AX, 24(R8)
	MOVQ AX, 32(R8)
	MOVQ AX, 40(R8)
	MOVQ AX, 48(R8)
	MOVQ AX, 56(R8)
	MOVQ AX, 64(R8)
	MOVQ AX, 72(R8)
	MOVQ AX, 80(R8)
	MOVQ AX, 88(R8)
	MOVQ AX, 96(R8)
	MOVQ AX, 104(R8)
	MOVQ AX, 112(R8)
	MOVQ AX, 120(R8)
	MOVQ AX, 128(R8)
	MOVQ F_MEM, R9
	ADDQ L_T1(SP), R9
	MOVQ R8, R15
keccak_pad8:
	CMPQ CX, $8
	JCS keccak_pad1
	MOVQ (R9), AX
	MOVQ AX, (R15)
	ADDQ $8, R9
	ADDQ $8, R15
	SUBQ $8, CX
	JMP keccak_pad8
keccak_pad1:
	TESTQ CX, CX
	JEQ keccak_padded
	MOVB (R9), AX
	MOVB AX, (R15)
	ADDQ $1, R9
	ADDQ $1, R15
	SUBQ $1, CX
	JMP keccak_pad1
keccak_padded:
	XORB $0x01, (R15)
	XORB $0x80, 135(R8)
	ABSORB(R8)
	LEAQ asmFrame_kstate(DI), AX
	MOVQ AX, 0(SP)
	CALL ·keccakF1600(SB)
	RELOAD
	MOVQ asmFrame_kstate+0(DI), AX
	BSWAPQ AX
	MOVQ AX, -8(R13)
	MOVQ asmFrame_kstate+8(DI), AX
	BSWAPQ AX
	MOVQ AX, -16(R13)
	MOVQ asmFrame_kstate+16(DI), AX
	BSWAPQ AX
	MOVQ AX, -24(R13)
	MOVQ asmFrame_kstate+24(DI), AX
	BSWAPQ AX
	MOVQ AX, -32(R13)
	// Remember a 64-byte input (L_T1 is still its offset: no full block).
	CMPQ L_E(SP), $64
	JNE keccak_end
	MOVQ F_KCACHE, R9
	TESTQ R9, R9
	JEQ keccak_end
	MOVQ keccakCache_tab(R9), R15
	TESTQ R15, R15
	JEQ keccak_ring
	MOVQ F_MEM, R8
	ADDQ L_T1(SP), R8
	MOVQ 0(R8), AX
	XORQ 8(R8), AX
	XORQ 16(R8), AX
	XORQ 24(R8), AX
	XORQ 32(R8), AX
	XORQ 40(R8), AX
	XORQ 48(R8), AX
	XORQ 56(R8), AX
	MOVQ $const_slotHashMul, CX
	IMULQ CX, AX
	MOVQ keccakCache_shift(R9), CX
	SHRQ CX, AX
	IMUL3Q $96, AX, AX
	ADDQ AX, R15
	JMP keccak_store
keccak_ring:
	MOVQ keccakCache_next(R9), CX
	LEAQ 1(CX), AX
	ANDQ $7, AX
	MOVQ AX, keccakCache_next(R9)
	CMPQ keccakCache_n(R9), $8
	JCC keccak_full
	ADDQ $1, keccakCache_n(R9)
keccak_full:
	IMUL3Q $96, CX, CX
	LEAQ keccakCache_e(R9)(CX*1), R15
	MOVQ F_MEM, R8
	ADDQ L_T1(SP), R8
keccak_store:
	COPY4(0, R8, 0, R15)
	COPY4(32, R8, 32, R15)
	COPY4(-32, R13, 64, R15)
keccak_end:
	NEXT(1)

// Validates the jump destination at off(R13) into AX, or jumps to host. A
// valid destination is a JUMPDEST instruction: its pre-decoded handler is the
// JUMPDEST slot (PUSH data bytes carry the INVALID handler).
#define JUMPDEST_CHECK(off) \
	MOVQ off+8(R13), R9; \
	ORQ off+16(R13), R9; \
	ORQ off+24(R13), R9; \
	JNE host; \
	MOVQ off(R13), AX; \
	CMPQ AX, F_CODELEN; \
	JCC host; \
	MOVQ (R12)(AX*8), R9; \
	CMPQ R9, ·jumpdestHandler(SB); \
	JNE host

// SLOAD of a slot that is warm in this transaction (cached in the slotTable
// under the storage account and the slot, see slottable.go): 100 gas. Anything else (cold,
// not cached, no table, older forks) runs in the host, which caches it.
op_sload:
	NEED(1)
	MOVQ F_STAB, R9
	TESTQ R9, R9
	JEQ host
	CMPQ BX, $100
	JCS host
	MOVQ -32(R13), AX
	XORQ -24(R13), AX
	XORQ -16(R13), AX
	XORQ -8(R13), AX
	XORQ F_SELFHASH, AX
	MOVQ $const_slotHashMul, CX
	IMULQ CX, AX
	MOVQ slotTable_shift(R9), CX
	SHRQ CX, AX
	MOVQ slotTable_gen(R9), DX
	MOVQ slotTable_mask(R9), R8
	MOVQ slotTable_e(R9), R9
sload_probe:
	MOVQ AX, R15
	SHLQ $7, R15
	ADDQ R9, R15
	CMPQ 120(R15), DX
	JNE sload_cold            // empty entry: not cached
	MOVQ (R15), CX
	CMPQ CX, -32(R13)
	JNE sload_next
	MOVQ 8(R15), CX
	CMPQ CX, -24(R13)
	JNE sload_next
	MOVQ 16(R15), CX
	CMPQ CX, -16(R13)
	JNE sload_next
	MOVQ 24(R15), CX
	CMPQ CX, -8(R13)
	JNE sload_next
	MOVQ 96(R15), CX
	CMPQ CX, asmFrame_self+0(DI)
	JNE sload_next
	MOVQ 104(R15), CX
	CMPQ CX, asmFrame_self+8(DI)
	JNE sload_next
	MOVQ 112(R15), CX
	CMPQ CX, asmFrame_self+16(DI)
	JNE sload_next
	SUBQ $100, BX
	COPY4(32, R15, -32, R13)
	HNEXT_54(1)
sload_next:
	ADDQ $1, AX
	ANDQ R8, AX
	JMP sload_probe

// Cold SLOAD (R15: the empty warm-table entry where the slot goes): while the
// warm table holds every warm slot of the transaction (slotTable.cold), the
// slot is cold. Its value comes from the value table (IndexStorage); the slot
// is cached as warm and a kind-1 record makes Go mark it warm (flushNative).
// 2100 gas. Anything else (not in the value table, table half full, journal
// full, gas short) runs in the host.
sload_cold:
	MOVQ F_STAB, R9
	CMPQ slotTable_cold(R9), $0
	JEQ host
	CMPQ BX, $2100
	JCS host
	MOVQ asmFrame_nj(DI), R8
	TESTQ R8, R8
	JEQ host
	CMPQ nativeJournal_n(R8), $const_nativeJournalCap
	JCC host
	MOVQ slotTable_n(R9), CX  // at most half full after the insert
	LEAQ 2(CX)(CX*1), CX
	MOVQ slotTable_mask(R9), AX
	ADDQ $1, AX
	CMPQ CX, AX
	JHI host
	MOVQ R15, L_SCR(SP)
	MOVQ slotTable_vtab(R9), R9
	MOVQ -32(R13), AX
	XORQ -24(R13), AX
	XORQ -16(R13), AX
	XORQ -8(R13), AX
	XORQ F_SELFHASH, AX
	MOVQ $const_slotHashMul, CX
	IMULQ CX, AX
	MOVQ slotTable_shift(R9), CX
	SHRQ CX, AX
	MOVQ slotTable_gen(R9), DX
	MOVQ slotTable_mask(R9), R8
	MOVQ slotTable_e(R9), R9
cold_probe:
	MOVQ AX, R15
	SHLQ $7, R15
	ADDQ R9, R15
	CMPQ 120(R15), DX
	JNE host                  // not in the value table
	MOVQ (R15), CX
	CMPQ CX, -32(R13)
	JNE cold_next
	MOVQ 8(R15), CX
	CMPQ CX, -24(R13)
	JNE cold_next
	MOVQ 16(R15), CX
	CMPQ CX, -16(R13)
	JNE cold_next
	MOVQ 24(R15), CX
	CMPQ CX, -8(R13)
	JNE cold_next
	MOVQ 96(R15), CX
	CMPQ CX, asmFrame_self+0(DI)
	JNE cold_next
	MOVQ 104(R15), CX
	CMPQ CX, asmFrame_self+8(DI)
	JNE cold_next
	MOVQ 112(R15), CX
	CMPQ CX, asmFrame_self+16(DI)
	JNE cold_next
	// R15: the value table entry. Fill the warm entry: key, value, original, account, gen.
	MOVQ L_SCR(SP), R9
	COPY4(-32, R13, 0, R9)
	COPY4(32, R15, 32, R9)
	COPY4(32, R15, 64, R9)
	MOVQ asmFrame_self+0(DI), AX
	MOVQ AX, 96(R9)
	MOVQ asmFrame_self+8(DI), AX
	MOVQ AX, 104(R9)
	MOVQ asmFrame_self+16(DI), AX
	MOVQ AX, 112(R9)
	MOVQ F_STAB, CX
	MOVQ slotTable_gen(CX), AX
	MOVQ AX, 120(R9)
	ADDQ $1, slotTable_n(CX)
	// The record: account, kind 1, slot (one Go journal entry: jWarmSlot).
	MOVQ asmFrame_nj(DI), R8
	MOVQ nativeJournal_n(R8), R9
	LEAQ 1(R9), AX
	MOVQ AX, nativeJournal_n(R8)
	IMUL3Q $112, R9, R9
	LEAQ nativeJournal_e(R8)(R9*1), R9
	MOVQ asmFrame_self+0(DI), AX
	MOVQ AX, 0(R9)
	MOVQ asmFrame_self+8(DI), AX
	MOVQ AX, 8(R9)
	MOVQ asmFrame_self+16(DI), AX
	MOVQ AX, 16(R9)
	MOVQ $1, 24(R9)
	COPY4(-32, R13, 32, R9)
	ADDQ $1, nativeJournal_pending(R8)
	SUBQ $2100, BX
	COPY4(32, R15, -32, R13)  // the value replaces the slot on the stack
	HNEXT_54(1)
cold_next:
	ADDQ $1, AX
	ANDQ R8, AX
	JMP cold_probe

// EXTCODESIZE of an account of the code table (warm, code the core may enter):
// 100 gas, its code length. A miss while the table is complete (codeTable.cold)
// is a cold account: from the code index at 2600 gas, cached and journaled
// like a cold CALL (kind-2 record). Anything else (a marker entry, an account
// the index does not have) runs in the host.
op_extcodesize:
	TESTQ $const_asmFlagCall2929, F_FLAGS
	JEQ host
	NEED(1)
	MOVQ asmFrame_ctab(DI), R8
	TESTQ R8, R8
	JEQ host
	MOVQ -32(R13), AX         // the address: low 160 bits of the word
	MOVQ AX, L_U(SP)
	MOVQ -24(R13), AX
	MOVQ AX, L_U+8(SP)
	MOVL -16(R13), AX
	MOVQ AX, L_U+16(SP)
	MOVQ L_U(SP), R9
	XORQ L_U+8(SP), R9
	XORQ AX, R9
	MOVQ $const_slotHashMul, AX
	IMULQ AX, R9
	MOVQ codeTable_shift(R8), CX
	SHRQ CX, R9
	MOVQ codeTable_gen(R8), DX
	MOVQ codeTable_mask(R8), R15
	MOVQ codeTable_e(R8), R8
ecs_probe:
	MOVQ R9, AX
	SHLQ $6, AX
	ADDQ R8, AX
	CMPQ 24(AX), DX
	JNE ecs_miss
	MOVQ L_U(SP), CX
	CMPQ CX, 0(AX)
	JNE ecs_next
	MOVQ L_U+8(SP), CX
	CMPQ CX, 8(AX)
	JNE ecs_next
	MOVQ L_U+16(SP), CX
	CMPQ CX, 16(AX)
	JNE ecs_next
	CMPQ codeEntry_code(AX), $0
	JEQ host                  // a marker: size unknown here
	CMPQ BX, $100
	JCS host
	SUBQ $100, BX
	MOVQ codeEntry_codeLen(AX), AX
	WORD_STORE(-32)
	NEXT(1)
ecs_next:
	ADDQ $1, R9
	ANDQ R15, R9
	JMP ecs_probe
ecs_miss:                     // AX: the empty code-table entry
	MOVQ asmFrame_ctab(DI), R8
	CMPQ codeTable_cold(R8), $0
	JEQ host
	CMPQ BX, $2600
	JCS host
	MOVQ asmFrame_nj(DI), CX
	TESTQ CX, CX
	JEQ host
	CMPQ nativeJournal_n(CX), $const_nativeJournalCap
	JCC host
	MOVQ codeTable_n(R8), CX  // at most half full after the insert
	LEAQ 2(CX)(CX*1), CX
	MOVQ codeTable_mask(R8), DX
	ADDQ $1, DX
	CMPQ CX, DX
	JHI host
	MOVQ AX, L_U+32(SP)
	MOVQ codeTable_cidx(R8), R8
	MOVQ L_U(SP), R9
	XORQ L_U+8(SP), R9
	XORQ L_U+16(SP), R9
	MOVQ $const_slotHashMul, AX
	IMULQ AX, R9
	MOVQ codeTable_shift(R8), CX
	SHRQ CX, R9
	MOVQ codeTable_gen(R8), DX
	MOVQ codeTable_mask(R8), R15
	MOVQ codeTable_e(R8), R8
ecs_cprobe:
	MOVQ R9, AX
	SHLQ $6, AX
	ADDQ R8, AX
	CMPQ 24(AX), DX
	JNE host                  // not in the code index
	MOVQ L_U(SP), CX
	CMPQ CX, 0(AX)
	JNE ecs_cnext
	MOVQ L_U+8(SP), CX
	CMPQ CX, 8(AX)
	JNE ecs_cnext
	MOVQ L_U+16(SP), CX
	CMPQ CX, 16(AX)
	JNE ecs_cnext
	// Cold: copy the index entry (AX) into the code table, journal the warm mark.
	MOVQ L_U+32(SP), R9
	MOVQ 0(AX), CX
	MOVQ CX, 0(R9)
	MOVQ 8(AX), CX
	MOVQ CX, 8(R9)
	MOVQ 16(AX), CX
	MOVQ CX, 16(R9)
	MOVQ codeEntry_code(AX), CX
	MOVQ CX, codeEntry_code(R9)
	MOVQ codeEntry_codeLen(AX), CX
	MOVQ CX, codeEntry_codeLen(R9)
	MOVQ codeEntry_handlers(AX), CX
	MOVQ CX, codeEntry_handlers(R9)
	MOVQ codeEntry_args(AX), CX
	MOVQ CX, codeEntry_args(R9)
	MOVQ asmFrame_ctab(DI), CX
	MOVQ codeTable_gen(CX), DX
	MOVQ DX, 24(R9)
	ADDQ $1, codeTable_n(CX)
	MOVQ codeEntry_codeLen(AX), R15
	MOVQ asmFrame_nj(DI), R8
	MOVQ nativeJournal_n(R8), R9
	LEAQ 1(R9), AX
	MOVQ AX, nativeJournal_n(R8)
	IMUL3Q $112, R9, R9
	LEAQ nativeJournal_e(R8)(R9*1), R9
	MOVQ L_U(SP), AX
	MOVQ AX, 0(R9)
	MOVQ L_U+8(SP), AX
	MOVQ AX, 8(R9)
	MOVQ L_U+16(SP), AX
	MOVQ AX, 16(R9)
	MOVQ $2, 24(R9)
	ADDQ $1, nativeJournal_pending(R8)
	SUBQ $2600, BX
	MOVQ R15, AX
	WORD_STORE(-32)
	NEXT(1)
ecs_cnext:
	ADDQ $1, R9
	ANDQ R15, R9
	JMP ecs_cprobe

// LOG0..LOG4 (R15: the topic count; the slot checked the stack): outside a
// static frame, the data goes to the nativeJournal's arena and one record (two
// with more than 2 topics) to its journal, which flushNative turns into
// addLog in order with the other records. Gas 375 * (topics + 1) + 8 * size +
// memory. Anything else (static, journal or arena full, gas short) runs in the
// host.
op_log:
	MOVQ R15, L_T1(SP)
	CMPQ asmFrame_static(DI), $0
	JNE host
	MOVQ asmFrame_nj(DI), R8
	TESTQ R8, R8
	JEQ host
	MOVQ nativeJournal_n(R8), CX
	ADDQ $1, CX
	CMPQ R15, $2
	JLS log_room
	ADDQ $1, CX
log_room:
	CMPQ CX, $const_nativeJournalCap
	JHI host
	SMALL(-64, CX)            // size
	MOVQ CX, L_T2(SP)
	MOVQ nativeJournal_dlen(R8), AX
	ADDQ CX, AX
	CMPQ AX, $const_nativeLogArena
	JHI host
	MOVQ L_T1(SP), DX
	ADDQ $1, DX
	IMUL3Q $375, DX, DX
	LEAQ (DX)(CX*8), DX
	TESTQ CX, CX
	JEQ log_mem
	SMALL(-32, AX)
	ADDQ AX, CX
log_mem:
	MEMEXP(log_charge, log_zero, log_done)
	// The data to the arena.
	MOVQ asmFrame_nj(DI), R8
	MOVQ nativeJournal_dlen(R8), R15
	LEAQ nativeJournal_data(R8)(R15*1), DX
	MOVQ L_T2(SP), CX
	ADDQ CX, nativeJournal_dlen(R8)
	TESTQ CX, CX
	JEQ log_rec
	MOVQ F_MEM, AX
	ADDQ -32(R13), AX
log_copy8:
	CMPQ CX, $8
	JCS log_copy1
	MOVQ (AX), R9
	MOVQ R9, (DX)
	ADDQ $8, AX
	ADDQ $8, DX
	SUBQ $8, CX
	JMP log_copy8
log_copy1:
	TESTQ CX, CX
	JEQ log_rec
	MOVB (AX), R9
	MOVB R9, (DX)
	ADDQ $1, AX
	ADDQ $1, DX
	SUBQ $1, CX
	JMP log_copy1
log_rec:
	MOVQ nativeJournal_n(R8), R9
	LEAQ 1(R9), AX
	MOVQ AX, nativeJournal_n(R8)
	IMUL3Q $112, R9, R9
	LEAQ nativeJournal_e(R8)(R9*1), R9
	MOVQ asmFrame_self+0(DI), AX
	MOVQ AX, 0(R9)
	MOVQ asmFrame_self+8(DI), AX
	MOVQ AX, 8(R9)
	MOVQ asmFrame_self+16(DI), AX
	MOVQ AX, 16(R9)
	MOVQ L_T1(SP), AX
	SHLQ $8, AX
	ORQ $3, AX
	MOVQ AX, 24(R9)
	MOVQ R15, 96(R9)          // data offset in the arena
	MOVQ L_T2(SP), AX
	MOVQ AX, 104(R9)          // data length
	MOVQ L_T1(SP), CX
	CMPQ CX, $1
	JCS log_done_rec
	COPY4(-96, R13, 32, R9)   // topic 0
	CMPQ CX, $2
	JCS log_done_rec
	COPY4(-128, R13, 64, R9)  // topic 1
	CMPQ CX, $3
	JCS log_done_rec
	MOVQ nativeJournal_n(R8), R9 // the kind-4 record: topics 2 and 3
	LEAQ 1(R9), AX
	MOVQ AX, nativeJournal_n(R8)
	IMUL3Q $112, R9, R9
	LEAQ nativeJournal_e(R8)(R9*1), R9
	MOVQ $4, 24(R9)
	COPY4(-160, R13, 32, R9)  // topic 2
	CMPQ CX, $4
	JCS log_done_rec
	COPY4(-192, R13, 64, R9)  // topic 3
log_done_rec:
	ADDQ $1, nativeJournal_pending(R8) // one Go journal entry: jLog
	ADDQ $2, CX
	SHLQ $5, CX
	SUBQ CX, R13
	NEXT(1)

// CALL (value 0), STATICCALL and DELEGATECALL into an account of the code
// table (codetable.go: warm, has code, not a precompile) switch to the callee
// frame record (asmFrame.next) without leaving the core: gas as in
// gasCallEIP2929 for a warm address (100 + memory expansion, then all but
// 1/64 of the rest or the requested gas if less), the snapshot is the journal
// length, and the callee runs with lazy set (its Go-side Contract is built on
// its first host callback). Anything else goes to the host first, with
// nothing changed. Locals: L_T1 kind (0 CALL, 1 STATICCALL, 2 DELEGATECALL),
// L_SCR the arguments, L_U the address limbs.
op_call:
	TESTQ $const_asmFlagCall2929, F_FLAGS
	JEQ host
	NEED(7)
	MOVQ -96(R13), AX         // value
	ORQ -88(R13), AX
	ORQ -80(R13), AX
	ORQ -72(R13), AX
	JNE host
	MOVQ $0, L_T1(SP)
	LEAQ -128(R13), R15       // inOffset
	JMP call_common

op_staticcall:
	TESTQ $const_asmFlagCall2929, F_FLAGS
	JEQ host
	NEED(6)
	MOVQ $1, L_T1(SP)
	LEAQ -96(R13), R15
	JMP call_common

op_delegatecall:
	TESTQ $const_asmFlagCall2929, F_FLAGS
	JEQ host
	NEED(6)
	MOVQ $2, L_T1(SP)
	LEAQ -96(R13), R15

call_common:
	MOVQ asmFrame_next(DI), AX
	TESTQ AX, AX
	JEQ host
	MOVQ asmFrame_ctab(DI), R8
	TESTQ R8, R8
	JEQ host
	CMPQ asmFrame_depth(DI), $1024
	JHI host
	MOVQ R15, L_SCR(SP)
	MOVQ -64(R13), AX         // the address: low 160 bits of the word
	MOVQ AX, L_U(SP)
	MOVQ -56(R13), AX
	MOVQ AX, L_U+8(SP)
	MOVL -48(R13), AX
	MOVQ AX, L_U+16(SP)
	MOVQ L_U(SP), R9
	XORQ L_U+8(SP), R9
	XORQ AX, R9
	MOVQ $const_slotHashMul, AX
	IMULQ AX, R9
	MOVQ codeTable_shift(R8), CX
	SHRQ CX, R9
	MOVQ codeTable_gen(R8), DX
	MOVQ codeTable_mask(R8), R15
	MOVQ codeTable_e(R8), R8
call_probe:
	MOVQ R9, AX
	SHLQ $6, AX
	ADDQ R8, AX
	CMPQ 24(AX), DX
	JNE call_miss             // empty entry: not in the table
	MOVQ L_U(SP), CX
	CMPQ CX, 0(AX)
	JNE call_next
	MOVQ L_U+8(SP), CX
	CMPQ CX, 8(AX)
	JNE call_next
	MOVQ L_U+16(SP), CX
	CMPQ CX, 16(AX)
	JEQ call_found
call_next:
	ADDQ $1, R9
	ANDQ R15, R9
	JMP call_probe
// Code-table miss (AX: the empty entry). While the table holds every warm
// address of the tx (codeTable.cold), the account is cold: if the snapshot's
// code index has it, the CALL runs here at the cold price, else in the host.
// Nothing changes before the gas is charged.
call_miss:
	MOVQ asmFrame_ctab(DI), R8
	CMPQ codeTable_cold(R8), $0
	JEQ host
	MOVQ asmFrame_nj(DI), CX
	TESTQ CX, CX
	JEQ host
	CMPQ nativeJournal_n(CX), $const_nativeJournalCap
	JCC host
	MOVQ codeTable_n(R8), CX  // at most half full after the insert
	LEAQ 2(CX)(CX*1), CX
	MOVQ codeTable_mask(R8), DX
	ADDQ $1, DX
	CMPQ CX, DX
	JHI host
	MOVQ AX, L_U+32(SP)
	MOVQ codeTable_cidx(R8), R8
	MOVQ L_U(SP), R9
	XORQ L_U+8(SP), R9
	XORQ L_U+16(SP), R9
	MOVQ $const_slotHashMul, AX
	IMULQ AX, R9
	MOVQ codeTable_shift(R8), CX
	SHRQ CX, R9
	MOVQ codeTable_gen(R8), DX
	MOVQ codeTable_mask(R8), R15
	MOVQ codeTable_e(R8), R8
cold_cprobe:
	MOVQ R9, AX
	SHLQ $6, AX
	ADDQ R8, AX
	CMPQ 24(AX), DX
	JNE host                  // no code the core can enter
	MOVQ L_U(SP), CX
	CMPQ CX, 0(AX)
	JNE cold_cnext
	MOVQ L_U+8(SP), CX
	CMPQ CX, 8(AX)
	JNE cold_cnext
	MOVQ L_U+16(SP), CX
	CMPQ CX, 16(AX)
	JNE cold_cnext
	MOVQ $1, L_U+24(SP)
	JMP call_found_cold
cold_cnext:
	ADDQ $1, R9
	ANDQ R15, R9
	JMP cold_cprobe
call_found:
	CMPQ codeEntry_code(AX), $0
	JEQ host                  // warm, but a marker: not entered by the core
	MOVQ $0, L_U+24(SP)       // warm: 100 gas
call_found_cold:
	MOVQ AX, L_SCR+8(SP)
	MOVQ L_SCR(SP), R15
	// Input and return regions (an offset only counts with a non-zero size).
	SMALLB(-32, R15, CX)
	XORL AX, AX
	TESTQ CX, CX
	JEQ call_in0
	SMALLB(0, R15, AX)
call_in0:
	MOVQ AX, L_SCR+16(SP)
	MOVQ CX, L_SCR+24(SP)
	ADDQ AX, CX
	MOVQ CX, L_T2(SP)
	SMALLB(-96, R15, CX)
	XORL AX, AX
	TESTQ CX, CX
	JEQ call_ret0
	SMALLB(-64, R15, AX)
call_ret0:
	MOVQ AX, L_SCR+32(SP)
	MOVQ CX, L_SCR+40(SP)
	ADDQ AX, CX
	CMPQ CX, L_T2(SP)
	JCC call_mem
	MOVQ L_T2(SP), CX
call_mem:
	MOVQ $100, DX
	CMPQ L_U+24(SP), $0
	JEQ call_memx
	MOVQ $2600, DX            // cold account (EIP-2929)
call_memx:
	MEMEXP(call_charge, call_zero, call_done)
	CMPQ L_U+24(SP), $0
	JEQ call_warm
	// Cold: the account becomes warm. Its value-table entry goes to the empty
	// code-table entry found by the probe, and a kind-2 record makes Go mark
	// it warm (flushNative) before the snapshot below.
	MOVQ L_SCR+8(SP), AX      // value-table (cidx) entry
	MOVQ L_U+32(SP), R9       // empty code-table entry
	MOVQ 0(AX), CX
	MOVQ CX, 0(R9)
	MOVQ 8(AX), CX
	MOVQ CX, 8(R9)
	MOVQ 16(AX), CX
	MOVQ CX, 16(R9)
	MOVQ codeEntry_code(AX), CX
	MOVQ CX, codeEntry_code(R9)
	MOVQ codeEntry_codeLen(AX), CX
	MOVQ CX, codeEntry_codeLen(R9)
	MOVQ codeEntry_handlers(AX), CX
	MOVQ CX, codeEntry_handlers(R9)
	MOVQ codeEntry_args(AX), CX
	MOVQ CX, codeEntry_args(R9)
	MOVQ asmFrame_ctab(DI), CX
	MOVQ codeTable_gen(CX), AX
	MOVQ AX, 24(R9)
	ADDQ $1, codeTable_n(CX)
	MOVQ R9, L_SCR+8(SP)
	MOVQ asmFrame_nj(DI), R8
	MOVQ nativeJournal_n(R8), R9
	LEAQ 1(R9), AX
	MOVQ AX, nativeJournal_n(R8)
	IMUL3Q $112, R9, R9
	LEAQ nativeJournal_e(R8)(R9*1), R9
	MOVQ L_U(SP), AX
	MOVQ AX, 0(R9)
	MOVQ L_U+8(SP), AX
	MOVQ AX, 8(R9)
	MOVQ L_U+16(SP), AX
	MOVQ AX, 16(R9)
	MOVQ $2, 24(R9)
	ADDQ $1, nativeJournal_pending(R8)
call_warm:
	// Gas for the callee: all but 1/64 of the rest, or the requested gas if less.
	MOVQ BX, AX
	SHRQ $6, AX
	MOVQ BX, R8
	SUBQ AX, R8
	MOVQ -24(R13), AX
	ORQ -16(R13), AX
	ORQ -8(R13), AX
	JNE call_gas
	MOVQ -32(R13), AX
	CMPQ AX, R8
	JCC call_gas
	MOVQ AX, R8
call_gas:
	SUBQ R8, BX
	// The caller stays at its CALL with the arguments popped.
	MOVQ L_SCR(SP), R15
	LEAQ -96(R15), R13
	SAVE
	MOVQ L_SCR+32(SP), AX
	MOVQ AX, asmFrame_callRetOff(DI)
	MOVQ L_SCR+40(SP), AX
	MOVQ AX, asmFrame_callRetSize(DI)
	// The callee record.
	MOVQ asmFrame_next(DI), R9
	MOVQ R8, asmFrame_gas(R9)
	MOVQ L_SCR+8(SP), AX
	MOVQ codeEntry_code(AX), CX
	MOVQ CX, asmFrame_code(R9)
	MOVQ codeEntry_codeLen(AX), CX
	MOVQ CX, asmFrame_codeLen(R9)
	MOVQ codeEntry_handlers(AX), CX
	MOVQ CX, asmFrame_handlers(R9)
	MOVQ codeEntry_args(AX), CX
	MOVQ CX, asmFrame_args(R9)
	MOVQ F_MEM, CX
	ADDQ L_SCR+16(SP), CX
	MOVQ CX, asmFrame_input(R9)
	MOVQ L_SCR+24(SP), CX
	MOVQ CX, asmFrame_inputLen(R9)
	MOVQ F_FLAGS, CX
	MOVQ CX, asmFrame_flags(R9)
	MOVQ F_ABORT, CX
	MOVQ CX, asmFrame_abort(R9)
	MOVQ F_TIME, CX
	MOVQ CX, asmFrame_time(R9)
	MOVQ F_KCACHE, CX
	MOVQ CX, asmFrame_kcache(R9)
	MOVQ F_STAB, CX
	MOVQ CX, asmFrame_stab(R9)
	MOVQ asmFrame_ctab(DI), CX
	MOVQ CX, asmFrame_ctab(R9)
	MOVQ asmFrame_journal(DI), CX
	MOVQ CX, asmFrame_journal(R9)
	MOVQ 8(CX), CX            // len(journal) + pending native SSTOREs: the snapshot
	MOVQ asmFrame_nj(DI), AX
	MOVQ AX, asmFrame_nj(R9)
	TESTQ AX, AX
	JEQ call_snap
	ADDQ nativeJournal_pending(AX), CX
call_snap:
	MOVQ CX, asmFrame_snap(R9)
	MOVQ asmFrame_depth(DI), CX
	ADDQ $1, CX
	MOVQ CX, asmFrame_depth(R9)
	MOVQ DI, asmFrame_parent(R9)
	MOVQ $1, asmFrame_lazy(R9)
	XORL CX, CX
	MOVQ CX, asmFrame_pc(R9)
	MOVQ CX, asmFrame_sp(R9)
	MOVQ CX, asmFrame_status(R9)
	MOVQ CX, asmFrame_memLen(R9)
	MOVQ CX, asmFrame_memCost(R9)
	MOVQ CX, asmFrame_retLen(R9)
	MOVQ CX, asmFrame_retPtr(R9)
	MOVQ L_U(SP), AX
	MOVQ AX, asmFrame_codeWord+0(R9)
	MOVQ L_U+8(SP), AX
	MOVQ AX, asmFrame_codeWord+8(R9)
	MOVQ L_U+16(SP), AX
	MOVQ AX, asmFrame_codeWord+16(R9)
	MOVQ CX, asmFrame_codeWord+24(R9)
	MOVQ asmFrame_static(DI), AX
	CMPQ L_T1(SP), $2
	JEQ call_delegate
	CMPQ L_T1(SP), $1
	JNE call_static
	MOVQ $1, AX
call_static:
	MOVQ AX, asmFrame_static(R9)
	// CALL, STATICCALL: self = the callee, caller = our self, value 0.
	COPY4(asmFrame_codeWord, R9, asmFrame_env, R9)
	COPY4(asmFrame_env, DI, asmFrame_env+32, R9)
	XORL AX, AX
	MOVQ AX, asmFrame_env+64(R9)
	MOVQ AX, asmFrame_env+72(R9)
	MOVQ AX, asmFrame_env+80(R9)
	MOVQ AX, asmFrame_env+88(R9)
	MOVQ L_U(SP), AX
	MOVQ AX, asmFrame_self+0(R9)
	MOVQ L_U+8(SP), CX
	MOVQ CX, asmFrame_self+8(R9)
	XORQ CX, AX
	MOVQ L_U+16(SP), CX
	MOVQ CX, asmFrame_self+16(R9)
	XORQ CX, AX
	MOVQ AX, asmFrame_selfHash(R9)
	JMP call_enter
call_delegate:
	// DELEGATECALL: self, caller and value of the caller.
	MOVQ AX, asmFrame_static(R9)
	COPY4(asmFrame_env, DI, asmFrame_env, R9)
	COPY4(asmFrame_env+32, DI, asmFrame_env+32, R9)
	COPY4(asmFrame_env+64, DI, asmFrame_env+64, R9)
	MOVQ asmFrame_self+0(DI), AX
	MOVQ AX, asmFrame_self+0(R9)
	MOVQ asmFrame_self+8(DI), AX
	MOVQ AX, asmFrame_self+8(R9)
	MOVQ asmFrame_self+16(DI), AX
	MOVQ AX, asmFrame_self+16(R9)
	MOVQ asmFrame_selfHash(DI), AX
	MOVQ AX, asmFrame_selfHash(R9)
call_enter:
	MOVQ R9, DI
	MOVQ DI, L_F(SP)
	RELOAD
	JMP (R12)(SI*8)

// ZF = (the words at a(ab) and b(bb) are equal). Clobbers AX, CX.
#define WEQ(a, ab, b, bb) \
	MOVQ a(ab), AX; \
	XORQ b(bb), AX; \
	MOVQ a+8(ab), CX; \
	XORQ b+8(bb), CX; \
	ORQ CX, AX; \
	MOVQ a+16(ab), CX; \
	XORQ b+16(bb), CX; \
	ORQ CX, AX; \
	MOVQ a+24(ab), CX; \
	XORQ b+24(bb), CX; \
	ORQ CX, AX

// ZF = (the word at a(ab) is zero). Clobbers AX.
#define WZERO(a, ab) \
	MOVQ a(ab), AX; \
	ORQ a+8(ab), AX; \
	ORQ a+16(ab), AX; \
	ORQ a+24(ab), AX

// SSTORE of a slot that is warm in this transaction (in the slotTable, which
// also has its original value), outside a static frame and above the EIP-2200
// sentry: gas and refund changes as gasSStoreEIP2929, the new value goes to
// the slotTable entry and a record to the nativeJournal, which Go replays
// before it next reads the state (slottable.go). Anything else (cold, not
// cached, journal full, static, sentry, older forks) runs in the host.
// Locals: L_T1, L_T2 the refund changes, L_SCR the gas cost.
op_sstore:
	TESTQ $const_asmFlagSstore2929, F_FLAGS
	JEQ host
	NEED(2)
	CMPQ asmFrame_static(DI), $0
	JNE host
	CMPQ BX, $2300
	JLS host
	MOVQ asmFrame_nj(DI), R8
	TESTQ R8, R8
	JEQ host
	CMPQ nativeJournal_n(R8), $const_nativeJournalCap
	JCC host
	MOVQ F_STAB, R9
	TESTQ R9, R9
	JEQ host
	MOVQ -32(R13), AX
	XORQ -24(R13), AX
	XORQ -16(R13), AX
	XORQ -8(R13), AX
	XORQ F_SELFHASH, AX
	MOVQ $const_slotHashMul, CX
	IMULQ CX, AX
	MOVQ slotTable_shift(R9), CX
	SHRQ CX, AX
	MOVQ slotTable_gen(R9), DX
	MOVQ slotTable_mask(R9), R8
	MOVQ slotTable_e(R9), R9
sstore_probe:
	MOVQ AX, R15
	SHLQ $7, R15
	ADDQ R9, R15
	CMPQ 120(R15), DX
	JNE host                  // empty entry: not cached
	MOVQ (R15), CX
	CMPQ CX, -32(R13)
	JNE sstore_next
	MOVQ 8(R15), CX
	CMPQ CX, -24(R13)
	JNE sstore_next
	MOVQ 16(R15), CX
	CMPQ CX, -16(R13)
	JNE sstore_next
	MOVQ 24(R15), CX
	CMPQ CX, -8(R13)
	JNE sstore_next
	MOVQ 96(R15), CX
	CMPQ CX, asmFrame_self+0(DI)
	JNE sstore_next
	MOVQ 104(R15), CX
	CMPQ CX, asmFrame_self+8(DI)
	JNE sstore_next
	MOVQ 112(R15), CX
	CMPQ CX, asmFrame_self+16(DI)
	JEQ sstore_found
sstore_next:
	ADDQ $1, AX
	ANDQ R8, AX
	JMP sstore_probe
sstore_found:
	// R15: entry (current at 32, original at 64); the new value at -64(R13).
	XORL AX, AX
	MOVQ AX, L_T1(SP)
	MOVQ AX, L_T2(SP)
	MOVQ $100, L_SCR(SP)
	WEQ(32, R15, -64, R13)
	JEQ sstore_charge         // no-op
	WEQ(64, R15, 32, R15)
	JNE sstore_dirty
	MOVQ $20000, L_SCR(SP)    // clean: original == current
	WZERO(64, R15)
	JEQ sstore_charge
	MOVQ $2900, L_SCR(SP)
	WZERO(-64, R13)
	JNE sstore_charge
	MOVQ $4800, L_T1(SP)
	JMP sstore_charge
sstore_dirty:
	WZERO(64, R15)
	JEQ sstore_orig
	WZERO(32, R15)
	JNE sstore_dirty_v
	MOVQ $-4800, L_T1(SP)
	JMP sstore_orig
sstore_dirty_v:
	WZERO(-64, R13)
	JNE sstore_orig
	MOVQ $4800, L_T1(SP)
sstore_orig:
	WEQ(64, R15, -64, R13)
	JNE sstore_charge
	MOVQ $2800, L_T2(SP)
	WZERO(64, R15)
	JNE sstore_charge
	MOVQ $19900, L_T2(SP)
sstore_charge:
	MOVQ L_SCR(SP), CX
	CMPQ BX, CX
	JCS host
	SUBQ CX, BX
	COPY4(-64, R13, 32, R15)  // the slot's new current value
	// The record: account, slot, value, refund changes.
	MOVQ asmFrame_nj(DI), R8
	MOVQ nativeJournal_n(R8), R9
	LEAQ 1(R9), AX
	MOVQ AX, nativeJournal_n(R8)
	IMUL3Q $112, R9, R9
	LEAQ nativeJournal_e(R8)(R9*1), R9
	MOVQ asmFrame_self+0(DI), AX
	MOVQ AX, 0(R9)
	MOVQ asmFrame_self+8(DI), AX
	MOVQ AX, 8(R9)
	MOVQ asmFrame_self+16(DI), AX
	MOVQ AX, 16(R9)
	MOVQ $0, 24(R9)           // kind 0: SSTORE
	COPY4(-32, R13, 32, R9)
	COPY4(-64, R13, 64, R9)
	MOVQ $1, CX               // Go journal entries the record adds
	MOVQ L_T1(SP), AX
	MOVQ AX, 96(R9)
	TESTQ AX, AX
	JEQ sstore_r2
	ADDQ $1, CX
sstore_r2:
	MOVQ L_T2(SP), AX
	MOVQ AX, 104(R9)
	TESTQ AX, AX
	JEQ sstore_done
	ADDQ $1, CX
sstore_done:
	ADDQ CX, nativeJournal_pending(R8)
	SUBQ $64, R13
	HNEXT_55(1)

op_jump:
	NEED(1)
	CMPQ BX, $8
	JCS host
	ABORT_CHECK
	JUMPDEST_CHECK(-32)
	SUBQ $8, BX
	SUBQ $32, R13
	JUMP_TO(HNEXT_J_56)

op_jumpi:
	NEED(2)
	CMPQ BX, $10
	JCS host
	ABORT_CHECK
	MOVQ -64(R13), R9
	ORQ -56(R13), R9
	ORQ -48(R13), R9
	ORQ -40(R13), R9
	JNE jumpi_taken
	SUBQ $10, BX
	SUBQ $64, R13
	HNEXT_57(1)
jumpi_taken:
	JUMPDEST_CHECK(-32)
	SUBQ $10, BX
	SUBQ $64, R13

	JUMP_TO(HNEXT_J_57)

jump_yield:
	SAVE
	CALL ·asmYield(SB)
	JMP reload

op_return:
	NEED(2)
	MOVQ $const_asmReturn, L_T1(SP)
	JMP return_common

op_revert:
	TESTB $const_asmFlagRevert, F_FLAGS
	JEQ host
	NEED(2)
	MOVQ $const_asmRevert, L_T1(SP)

return_common:
	SMALL(-64, R8)
	XORL CX, CX
	XORL AX, AX
	TESTQ R8, R8
	JEQ return_mem
	SMALL(-32, CX)
	MOVQ CX, AX
	ADDQ R8, CX
return_mem:
	MOVQ AX, L_T2(SP)
	XORL DX, DX
	MEMEXP(return_charge, return_zero, return_done)
	MOVQ L_T2(SP), AX
	MOVQ AX, F_RET0
	MOVQ -64(R13), AX
	MOVQ AX, F_RET1
	SUBQ $64, R13
	MOVQ L_T1(SP), AX
	JMP exit

// udivrem divides the R8-word number at L_U by the non-zero 4-word number at
// L_D (which it normalises in place), leaving 8 quotient words at L_Q and 4
// remainder words at L_R. Knuth algorithm D as in uint256 udivrem, with DIVQ
// for the 2-by-1 steps. Uses all scratch registers and R10-R12, restores
// R10-R12, then returns to the caller selected by L_RET.
udivrem:
	XORL AX, AX
	MOVQ AX, L_Q(SP)
	MOVQ AX, L_Q+8(SP)
	MOVQ AX, L_Q+16(SP)
	MOVQ AX, L_Q+24(SP)
	MOVQ AX, L_Q+32(SP)
	MOVQ AX, L_Q+40(SP)
	MOVQ AX, L_Q+48(SP)
	MOVQ AX, L_Q+56(SP)
	MOVQ AX, L_R(SP)
	MOVQ AX, L_R+8(SP)
	MOVQ AX, L_R+16(SP)
	MOVQ AX, L_R+24(SP)
	MOVQ $4, R9               // dLen
udivrem_dlen:
	MOVQ L_D-8(SP)(R9*8), AX
	TESTQ AX, AX
	JNE udivrem_dlen_done
	SUBQ $1, R9
	JMP udivrem_dlen
udivrem_dlen_done:
	BSRQ AX, CX
	XORQ $63, CX              // shift = leading zeros of the top divisor word
udivrem_ulen:
	TESTQ R8, R8
	JEQ udivrem_ulen_done
	MOVQ L_U-8(SP)(R8*8), AX
	TESTQ AX, AX
	JNE udivrem_ulen_done
	SUBQ $1, R8
	JMP udivrem_ulen
udivrem_ulen_done:
	CMPQ R8, R9
	JCC udivrem_norm
	// u < d: the remainder is u, which has at most 4 words here.
	COPY4(L_U, SP, L_R, SP)
	JMP udivrem_ret
udivrem_norm:
	// dn = d << shift, in place.
	MOVQ R9, R10
	SUBQ $1, R10
udivrem_dn:
	TESTQ R10, R10
	JEQ udivrem_dn_last
	MOVQ L_D(SP)(R10*8), AX
	MOVQ L_D-8(SP)(R10*8), DX
	SHLQ CX, DX, AX
	MOVQ AX, L_D(SP)(R10*8)
	SUBQ $1, R10
	JMP udivrem_dn
udivrem_dn_last:
	SHLQ CX, L_D(SP)
	// un = u << shift, with one more word.
	XORL AX, AX
	MOVQ L_U-8(SP)(R8*8), DX
	SHLQ CX, DX, AX
	MOVQ AX, L_UN(SP)(R8*8)
	MOVQ R8, R10
	SUBQ $1, R10
udivrem_un:
	TESTQ R10, R10
	JEQ udivrem_un_last
	MOVQ L_U(SP)(R10*8), AX
	MOVQ L_U-8(SP)(R10*8), DX
	SHLQ CX, DX, AX
	MOVQ AX, L_UN(SP)(R10*8)
	SUBQ $1, R10
	JMP udivrem_un
udivrem_un_last:
	MOVQ L_U(SP), AX
	SHLQ CX, AX
	MOVQ AX, L_UN(SP)
	MOVQ CX, L_T2(SP)         // shift
	MOVQ L_D-8(SP)(R9*8), R12 // dh
	CMPQ R9, $1
	JNE udivrem_knuth
	// One-word divisor.
	MOVQ L_UN(SP)(R8*8), DX
	MOVQ R8, R10
udivrem_by1:
	SUBQ $1, R10
	MOVQ L_UN(SP)(R10*8), AX
	DIVQ R12
	MOVQ AX, L_Q(SP)(R10*8)
	TESTQ R10, R10
	JNE udivrem_by1
	SHRQ CX, DX
	MOVQ DX, L_R(SP)
	JMP udivrem_ret

udivrem_knuth:
	MOVQ R8, R10
	SUBQ R9, R10              // j
udivrem_j:
	LEAQ (R10)(R9*1), R8      // j + dLen
	MOVQ L_UN(SP)(R8*8), DX   // u2
	MOVQ $-1, R15             // qhat
	CMPQ DX, R12
	JCC udivrem_mulsub
	MOVQ L_UN-8(SP)(R8*8), AX // u1
	DIVQ R12
	MOVQ AX, R15              // qhat
	MOVQ DX, CX               // rhat
	MULQ L_D-16(SP)(R9*8)     // qhat * dl
	CMPQ DX, CX
	JHI udivrem_dec
	JNE udivrem_mulsub
	CMPQ AX, L_UN-16(SP)(R8*8)
	JLS udivrem_mulsub
udivrem_dec:
	SUBQ $1, R15
udivrem_mulsub:
	// un[j:j+dLen] -= qhat * dn; CX = borrow.
	XORL CX, CX
	XORL R8, R8
udivrem_ms:
	MOVQ L_D(SP)(R8*8), AX
	MULQ R15
	ADDQ CX, AX
	ADCQ $0, DX
	LEAQ (R10)(R8*1), R11
	SUBQ AX, L_UN(SP)(R11*8)
	ADCQ $0, DX
	MOVQ DX, CX
	ADDQ $1, R8
	CMPQ R8, R9
	JCS udivrem_ms
	LEAQ (R10)(R9*1), R11
	SUBQ CX, L_UN(SP)(R11*8)
	JCC udivrem_store
	// Too much subtracted: add dn back.
	SUBQ $1, R15
	XORL CX, CX
	XORL R8, R8
udivrem_add:
	MOVQ L_D(SP)(R8*8), AX
	XORL DX, DX
	ADDQ CX, AX
	ADCQ $0, DX
	LEAQ (R10)(R8*1), R11
	ADDQ AX, L_UN(SP)(R11*8)
	ADCQ $0, DX
	MOVQ DX, CX
	ADDQ $1, R8
	CMPQ R8, R9
	JCS udivrem_add
	LEAQ (R10)(R9*1), R11
	ADDQ CX, L_UN(SP)(R11*8)
udivrem_store:
	MOVQ R15, L_Q(SP)(R10*8)
	SUBQ $1, R10
	JCC udivrem_j
	// rem[i] = un[i]>>shift | un[i+1]<<(64-shift)
	MOVQ L_T2(SP), CX
	XORL R8, R8
udivrem_rem:
	LEAQ 1(R8), AX
	CMPQ AX, R9
	JCC udivrem_rem_last
	MOVQ L_UN(SP)(R8*8), DX
	MOVQ L_UN+8(SP)(R8*8), AX
	SHRQ CX, AX, DX
	MOVQ DX, L_R(SP)(R8*8)
	ADDQ $1, R8
	JMP udivrem_rem
udivrem_rem_last:
	MOVQ L_UN(SP)(R8*8), DX
	SHRQ CX, DX
	MOVQ DX, L_R(SP)(R8*8)
udivrem_ret:
	RESTORE_BASES
	MOVQ L_RET(SP), AX
	CMPQ AX, $1
	JEQ div_ret
	CMPQ AX, $2
	JEQ mod_ret
	CMPQ AX, $3
	JEQ sdiv_ret
	CMPQ AX, $4
	JEQ smod_ret
	JMP modop_ret

// func asmPreempt() bool: the jump checks are built in (preempt_amd64.go).
TEXT ·asmPreempt(SB), NOSPLIT, $0-1
#ifdef ASMEVM_PREEMPT
	MOVB $1, ret+0(FP)
#else
	MOVB $0, ret+0(FP)
#endif
	RET
