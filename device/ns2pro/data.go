package ns2pro

import (
	"encoding/hex"
	"fmt"
)

const (
	InputReportID   = 0x05
	OutputReportID  = 0x02
	InputReportSize = 64
)

var neutralInputReport = mustHex("0514050000000000000000dd678669d886000000000000000000000000000000a10e340000000000000001000000000000000000000000000000000000000000")

func NeutralInputReport() []byte {
	return cloneBytes(neutralInputReport)
}

var bulkReplayResponses = map[string][][]byte{
	"02910001000800000000000000300100": {
		mustHex("0201000100f8000040000000003001000100484557373030303732313639303200007e056920010601232323a0a0a0e6e6e6323232ffffffffffffffffffffff"),
		mustHex("ffffffffffffffffffffffffffffffff"),
	},
	"02910001000800000000000040300100": {
		mustHex("0201000100f800004000000040300100eef8df41443f153cb28cf1ba9bef48baffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
		mustHex("ffffffffffffffffffffffffffffffff"),
	},
	"02910001000800000000000080300100": {
		mustHex("0201000100f80000400000008030010001add99a555665a0000aa0000ae2200ee2200e9aadd99aadd90aa5500aa5502ff6622ff6620affffcb97847e36648516"),
		mustHex("63ffffffffffffffffffffffffffffff"),
	},
	"029100010008000000000000c0300100": {
		mustHex("0201000100f8000040000000c030010001add99a555665a0000aa0000ae2200ee2200e9aadd99aadd90aa5500aa5502ff6622ff6620affff60b8842606632016"),
		mustHex("69ffffffffffffffffffffffffffffff"),
	},
	"02910001000800000000000000310100": {
		mustHex("0201000100f800004000000000310100000000000000000000000000255d55be6c6b40bdaff11e41ffffffffffffffffffffffffffffffffffffffffffffffff"),
		mustHex("ffffffffffffffffffffffffffffffff"),
	},
	"02910001000800000000000040c01f00": {
		mustHex("0201000100f800004000000040c01f00ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
		mustHex("ffffffffffffffffffffffffffffffff"),
	},
	"02910001000800000000000080c01f00": {
		mustHex("0201000100f800004000000080c01f00ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
		mustHex("ffffffffffffffffffffffffffffffff"),
	},
	"0791000100000000": {
		mustHex("0701000100f8000000"),
	},
	"0c9100020004000027000000": {
		mustHex("0c01000200f8000000000000"),
	},
	"0a9100080014000001ffffffffffffffff3500460000000000000000": {
		mustHex("0a01000800f80000"),
	},
	"0c9100040004000027000000": {
		mustHex("0c01000400f8000000000000"),
	},
	"0191000c00000000": {
		mustHex("0101000c00f8000061125010"),
	},
	"0191000100000000": {
		mustHex("0104000100f80000"),
	},
	"089100020004000001000000": {
		mustHex("0804000200f80000"),
	},
	"0391000a0004000005000000": {
		mustHex("0301000a00f80000"),
	},
	"0391000d000800000100ffffffffffff": {
		mustHex("0301000d00f8000001000000"),
	},
	"09910007000800000000000000000000": {
		mustHex("0901000700f80000"),
	},
	"09910007000800000100000000000000": {
		mustHex("0901000700f80000"),
	},
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(fmt.Sprintf("ns2pro: invalid hex fixture %q: %v", s, err))
	}
	return b
}

func cloneBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}
