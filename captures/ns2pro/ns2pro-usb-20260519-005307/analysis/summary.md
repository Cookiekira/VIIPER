# NS2 Pro USB Capture Summary

Capture:

- File: `captures/ns2pro/ns2pro-usb-20260519-005307/ns2pro-usb-20260519-005307.pcapng`
- Effective raw capture: `captures/ns2pro/ns2pro-usb-20260519-005307/raw/USBPcap5.pcap`
- Capture window: 2026-05-19 00:53:09 to 00:54:58 Australia/Sydney
- USB bus/device in capture: bus 5, device address 14

## Device Identity

Device descriptor bytes:

```text
12 01 00 02 ef 02 01 40 7e 05 69 20 01 01 01 02 03 01
```

Decoded:

- `bcdUSB`: `0x0200`
- `bDeviceClass`: `0xef`
- `bDeviceSubClass`: `0x02`
- `bDeviceProtocol`: `0x01`
- `bMaxPacketSize0`: 64
- `idVendor`: `0x057e`
- `idProduct`: `0x2069`
- `bcdDevice`: `0x0101`
- `iManufacturer`: 1
- `iProduct`: 2
- `iSerialNumber`: 3
- `bNumConfigurations`: 1

Observed strings:

- Manufacturer: `Nintendo`
- Product: `Pro Controller`
- Serial-ish string: `00`
- Interface string 0: `If_Hid`

Steam log recognized `vid=0x057e, pid=0x2069` through HIDAPI.

## Configuration Descriptor

Configuration descriptor bytes:

```text
09 02 50 00 02 01 04 c0 fa
08 0b 00 01 03 00 00 00
09 04 00 00 02 03 00 00 05
09 21 11 01 00 01 22 61 00
07 05 81 03 40 00 04
07 05 01 03 40 00 04
08 0b 01 01 ff 00 00 00
09 04 01 00 02 ff 00 00 06
07 05 02 02 40 00 00
07 05 82 02 40 00 00
```

Decoded:

- `wTotalLength`: 80
- `bNumInterfaces`: 2
- `bmAttributes`: `0xc0`
- `bMaxPower`: 250 units / 500 mA

Interface 0:

- HID class
- Endpoint `0x81` interrupt IN, 64 bytes, interval 4
- Endpoint `0x01` interrupt OUT, 64 bytes, interval 4
- HID report descriptor length: 97

Interface 1:

- Vendor-specific class
- Endpoint `0x02` bulk OUT, 64 bytes
- Endpoint `0x82` bulk IN, 64 bytes

This capture does not expose the 5-interface audio composite layout from earlier public research notes. The captured NS2 Pro USB path is a 2-interface HID + vendor-bulk composite device.

## HID Report Descriptor

Report descriptor bytes:

```text
05 01 09 05 a1 01
85 05 05 ff 09 01 15 00 26 ff 00 95 3f 75 08 81 02
85 09 09 01 95 02 81 02
05 09 19 01 29 15 25 01 95 15 75 01 81 02
95 01 75 03 81 03
05 01 09 01 a1 00
09 30 09 31 09 33 09 35 26 ff 0f 95 04 75 0c 81 02
c0
05 ff 09 02 26 ff 00 95 34 75 08 81 02
85 02 09 01 95 3f 91 02
c0
```

Important report IDs:

- Input report `0x05`: 63-byte opaque payload, 64 bytes total with report ID.
- Input report `0x09`: structured 2-byte prefix + 21 buttons + 4x 12-bit axes + 52-byte opaque tail.
- Output report `0x02`: 63-byte output payload, 64 bytes total with report ID.

Observed HID IN endpoint `0x81`:

- 25,116 non-empty input reports.
- All observed non-empty reports start with report ID `0x05`.
- First observed report:

```text
0514050000000000000000dd678669d886000000000000000000000000000000a10e340000000000000001000000000000000000000000000000000000000000
```

Observed HID OUT endpoint `0x01`:

- 26 non-empty output reports.
- All start with report ID `0x02`.
- First rumble/haptics sample:

```text
02508789239138000000000000000000005087892391380000000000000000000000000000000000000000000000000000000000000000000000000000000000
```

## Vendor Bulk Init / Config Sequence

Non-empty bulk traffic was written to:

- `analysis/bulk_nonempty.tsv`

First init/config sequence:

```text
OUT 0x02 02910001000800000000000000300100
IN  0x82 0201000100f8000040000000003001000100484557373030303732313639303200007e056920010601232323a0a0a0e6e6e6323232ffffffffffffffffffffff
IN  0x82 ffffffffffffffffffffffffffffffff

OUT 0x02 02910001000800000000000040300100
IN  0x82 0201000100f800004000000040300100eef8df41443f153cb28cf1ba9bef48baffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
IN  0x82 ffffffffffffffffffffffffffffffff

OUT 0x02 02910001000800000000000080300100
IN  0x82 0201000100f80000400000008030010001add99a555665a0000aa0000ae2200ee2200e9aadd99aadd90aa5500aa5502ff6622ff6620affffcb97847e36648516
IN  0x82 63ffffffffffffffffffffffffffffff

OUT 0x02 029100010008000000000000c0300100
IN  0x82 0201000100f8000040000000c030010001add99a555665a0000aa0000ae2200ee2200e9aadd99aadd90aa5500aa5502ff6622ff6620affff60b8842606632016
IN  0x82 69ffffffffffffffffffffffffffffff

OUT 0x02 02910001000800000000000000310100
IN  0x82 0201000100f800004000000000310100000000000000000000000000255d55be6c6b40bdaff11e41ffffffffffffffffffffffffffffffffffffffffffffffff
IN  0x82 ffffffffffffffffffffffffffffffff

OUT 0x02 0791000100000000
IN  0x82 0701000100f8000000

OUT 0x02 0c9100020004000027000000
IN  0x82 0c01000200f8000000000000

OUT 0x02 0a9100080014000001ffffffffffffffff3500460000000000000000
IN  0x82 0a01000800f80000

OUT 0x02 0c9100040004000027000000
IN  0x82 0c01000400f8000000000000

OUT 0x02 0191000c00000000
IN  0x82 0101000c00f8000061125010

OUT 0x02 0191000100000000
IN  0x82 0104000100f80000

OUT 0x02 089100020004000001000000
IN  0x82 0804000200f80000

OUT 0x02 0391000a0004000005000000
IN  0x82 0301000a00f80000

OUT 0x02 0391000d000800000100ffffffffffff
IN  0x82 0301000d00f8000001000000
```

Repeated later:

```text
OUT 0x02 09910007000800000000000000000000
IN  0x82 0901000700f80000

OUT 0x02 09910007000800000100000000000000
IN  0x82 0901000700f80000
```

## VIIPER Implementation Notes

For a dummy Steam-recognized device, emulate this exact 2-interface descriptor set first.

Minimum behavior:

- Device descriptor: use captured bytes/fields above.
- Config descriptor: use captured 80-byte descriptor, not the earlier 5-interface audio layout.
- HID report descriptor: use captured 97-byte descriptor.
- Endpoint `0x81`: return neutral 64-byte input report with report ID `0x05`.
- Endpoint `0x01`: log and forward report ID `0x02` to BLE rumble/output bridge.
- Endpoint `0x02`: replay/respond to the captured init/config commands.
- Endpoint `0x82`: support multi-packet responses, including 64-byte + 16-byte flash/config reads.

The BLE-to-USB input bridge needs verification: the earlier assumption "BLE report 0x09 -> USB report 0x09" is not sufficient for this captured USB path because Steam-visible input traffic is report ID `0x05`.
