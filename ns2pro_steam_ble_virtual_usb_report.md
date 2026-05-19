# NS2Pro BLE -> Steam USB Report 0x05 实施说明

> 日期：2026-05-19  
> 当前阶段：Phase 3，真实 NS2 Pro / Switch 2 Pro Controller BLE 输入进入 Steam  
> 核心结论：**BLE Pro Controller 2 主输入走 GATT report `0x09`；Steam-visible 虚拟 USB 路径继续输出 HID report `0x05`。**

---

## 1. 当前已验证事实

### 1.1 本地 USB / Steam 抓包事实

本仓库的真实有线 NS2 Pro 抓包是当前 USB 侧实现的最高优先级依据：

- 摘要：`captures/ns2pro/ns2pro-usb-20260519-005307/analysis/summary.md`
- 设备身份：`VID=0x057E`、`PID=0x2069`、`bcdDevice=0x0101`
- configuration：2 interfaces
  - interface 0：HID，`0x81` interrupt IN + `0x01` interrupt OUT
  - interface 1：vendor bulk，`0x02` OUT + `0x82` IN
- Steam-visible HID IN：本地抓包中 25,116 个非空 input reports 全部以 report ID `0x05` 开头
- Steam HID OUT / rumble：非空 output reports 全部以 report ID `0x02` 开头
- HID report descriptor 同时包含：
  - input report `0x05`：63-byte opaque payload，USB 帧总长 64 bytes
  - input report `0x09`：structured input
  - output report `0x02`

因此，当前虚拟 USB 设备必须继续向 Steam 输出：

```text
USB HID IN:
  [0x05][63-byte payload]

USB HID OUT:
  [0x02][63-byte payload]
```

不要把 BLE `0x09` 直接补 report ID 后作为 USB `0x09` 交给 Steam；这不符合当前本地 Steam 抓包路径。

### 1.2 外部 BLE / GATT 事实

外部资料共同支持 Pro Controller 2 BLE 主输入走 report `0x09`：

- `ndeadly/switch2_controller_research`：
  - Pro Controller 2 input report ID #1 是 `0x05`
  - Pro Controller 2 input report ID #2 是 `0x09`
  - Pro Controller 2 output report 是 `0x02`
  - Bluetooth reports 不带 HID report ID，而是通过 GATT characteristic 区分
- BLE service UUID：

```text
ab7de9be-89fe-49ad-828f-118f09df7fd0
```

- BLE input report `0x09` characteristic：

```text
UUID:   7492866c-ec3e-4619-8258-32755ffcc0f8
Handle: 0x000E
```

- BLE input report `0x05` characteristic 也存在：

```text
UUID:   ab7de9be-89fe-49ad-828f-118f09df7fd2
Handle: 0x000A
```

但 Phase 3 默认路径应使用 `0x09`。`0x05` 只作为探测、对照或后备调试路径。

### 1.3 当前仓库状态

已经完成并验证：

- `device/ns2pro`：2-interface USB descriptor、HID report descriptor、neutral input report `0x05`、bulk replay
- `examples/go/virtual_ns2pro`：键盘/终端 feeder 生成 USB report `0x05`
- Steam 人工验证：A/B/X/Y、D-pad、sticks、shoulders、system buttons、GL/GR/C 可进入 Steam
- HID OUT `0x02` 已可记录，并可预览 USB rumble payload 到 BLE output `0x02` 的转换

Phase 3 不需要重做 USB descriptor，也不需要改变 Steam 输入路径。

---

## 2. Phase 3 目标

目标：真实 NS2 Pro / Switch 2 Pro Controller 通过 BLE 连接 Windows PC，输入进入当前虚拟 USB NS2Pro 设备，并在 Steam 中表现为有线 Switch 2 Pro Controller 输入。

目标路径固定为：

```text
NS2 Pro BLE notification
  report 0x09 characteristic payload, no HID report ID
        |
        v
Parse buttons / sticks / extras
        |
        v
Build USB HID input report 0x05
        |
        v
VIIPER NS2Pro EP 0x81
        |
        v
Steam Input
```

Phase 3 验收标准：

- A/B/X/Y 正常
- D-pad 正常
- 左右摇杆正常
- L/R/ZL/ZR、Plus/Minus、Home/Capture 正常
- GL/GR/C 正常，且能在 Steam 中绑定或显示
- BLE notification 堆积时使用 latest-frame 策略，避免输入延迟堆积

Phase 3 非目标：

- 不实现 gyro/IMU 完整桥接
- 不实现 rumble 实震闭环
- 不实现完整 feature select / calibration command bridge
- 不改变 USB descriptor、bulk replay 或 Steam-visible report ID

---

## 3. BLE 0x09 -> USB 0x05 转换

### 3.1 BLE `0x09` payload 布局

BLE notification payload 不带 HID report ID。对 Pro Controller 2 report `0x09`，按 63-byte payload 解析：

```text
offset  size  含义
0x00    1     8-bit counter
0x01    1     power info
0x02    3     buttons
0x05    3     left stick, packed 12-bit X/Y
0x08    3     right stick, packed 12-bit X/Y
0x0B    1     unknown, often 0x30/0x38 depending on enabled features
0x0C    1     NFC state
0x0D    1     headset audio state
0x0E    1     motion data length
0x0F    0x28  motion data / reserved tail
0x37    0x08  reserved
```

Button bits:

```text
payload[0x02]:
  bit 0 B
  bit 1 A
  bit 2 Y
  bit 3 X
  bit 4 R
  bit 5 ZR
  bit 6 Plus
  bit 7 RightStick

payload[0x03]:
  bit 0 Down
  bit 1 Right
  bit 2 Left
  bit 3 Up
  bit 4 L
  bit 5 ZL
  bit 6 Minus
  bit 7 LeftStick

payload[0x04]:
  bit 0 Home
  bit 1 Capture
  bit 2 GR
  bit 3 GL
  bit 4 C
```

Stick bytes can be copied into the current USB `0x05` builder output after parsing buttons:

```text
BLE left stick:  payload[0x05:0x08] -> USB report[0x0B:0x0E]
BLE right stick: payload[0x08:0x0B] -> USB report[0x0E:0x11]
```

### 3.2 USB `0x05` output requirements

Build every Steam-facing input frame as 64 bytes:

```text
report[0]    = 0x05
report[1:5]  = 32-bit counter
report[5:9]  = USB 0x05 button bytes
report[11:14] = left stick packed 12-bit X/Y
report[14:17] = right stick packed 12-bit X/Y
rest         = neutral report base unless intentionally filled
```

使用 `ns2pro.NeutralInputReport()` 作为基础帧，再覆盖 counter、buttons 和 sticks。这样可以保留 Phase 2.5 中已经被 Steam 接受的稳定字节。

现有 `examples/go/virtual_ns2pro/ConvertBLEInput09ToUSB05` 是正确的实现入口，但它必须是经过验证的转换器，而不是盲目透传：

- 接受 63-byte raw BLE payload
- 可选接受带 `0x09` 前缀的 64-byte debug buffer
- 从 payload offset `0x02` 到 `0x04` 解析 button bits
- 从 payload offset `0x05` 和 `0x08` 复制 packed sticks
- 只输出 USB report ID `0x05`
- 对过短 payload 直接拒绝，不发送看似 neutral 但实际 malformed 的帧

### 3.3 默认 CLI 行为

Phase 3 实现应让 BLE report `0x09` 成为默认值：

```text
--ble-input-report=09
```

推荐验证命令：

```powershell
go run ./examples/go/virtual_ns2pro --ble-input --ble-input-report=09 --bulk-replay=true
```

保留 report `0x05`，但只作为显式 fallback：

```powershell
go run ./examples/go/virtual_ns2pro --ble-input --ble-input-report=05 --bulk-replay=true
```

---

## 4. Phase 3 调试日志

增加足够的 BLE 日志，方便和 `joycon2cpp`、BlueZ、raw captures 对照；正常运行时保持 opt-in，不刷屏。

建议新增 flags：

```text
--ble-input-log path.tsv
--ble-input-decode-log path.tsv
```

Raw log TSV 列：

```text
time
address
report
len
hex
```

Decoded log TSV 列：

```text
time
counter
buttons_hex
left_x
left_y
right_x
right_y
home
capture
gl
gr
c
motion_len
```

实用检查项：

- 每次只按一个按钮，确认只有预期 decoded bit 变化。
- 将每个摇杆轴推到极值，确认 packed 12-bit values 平滑变化。
- 单独测试 GL/GR/C；如果把 `payload[0x04]` 当成 padding，这几个键最容易丢。
- 确认 Steam-facing report 永远以 `0x05` 开头。
- 确认 BLE raw payload 永远不会被直接作为 USB `0x09` 转发。

---

## 5. 后续阶段

Phase 4：rumble bridge

- 继续捕获 Steam HID OUT `0x02`。
- 将 USB `[0x02][left 16][right 16]...` 转换成 BLE output `0x02` payload：

```text
[0x00][left LRA 16 bytes][right LRA 16 bytes][reserved 9 bytes]
```

- 写入 BLE characteristic `cc483f51-9258-427d-a939-630c31f72b05`。

Phase 5：feature select / gyro / IMU

- 处理 command `0x0C` feature select。
- 按需启用 buttons、analog sticks、IMU 和 rumble feature bits。
- 除非新的本地 Steam 抓包证明 gyro 需要 `0x09`，否则继续保留 USB report `0x05` 作为 Steam-facing path。
- 使用 BLE `0x09` 的 motion length 和 motion tail 作为后续 IMU mapping 来源。

Phase 6：稳定性

- BLE 重连时不销毁虚拟 USB device。
- BLE 断开时保持 neutral USB `0x05` state。
- 为多手柄场景增加更清晰的 scan selection。
- 日志保持 opt-in。

---

## 6. 资料来源

本地：

- `captures/ns2pro/ns2pro-usb-20260519-005307/analysis/summary.md`
- `captures/ns2pro/ns2pro-usb-20260519-005307/analysis/hid_in_81.tsv`
- `captures/ns2pro/ns2pro-usb-20260519-005307/analysis/hid_out_01.tsv`
- `captures/ns2pro/ns2pro-usb-20260519-005307/analysis/bulk_nonempty.tsv`

外部：

- `ndeadly/switch2_controller_research` HID reports  
  https://github.com/ndeadly/switch2_controller_research/blob/master/hid_reports.md
- `ndeadly/switch2_controller_research` descriptors  
  https://github.com/ndeadly/switch2_controller_research/blob/master/descriptors.md
- `ndeadly/switch2_controller_research` commands  
  https://github.com/ndeadly/switch2_controller_research/blob/master/commands.md
- `joycon2cpp`  
  https://github.com/TheFrano/joycon2cpp
- joypad OS switch 2 BLE driver: ./references/

---

## 7. 一句话实现规则

> 订阅 BLE report `0x09`，将 63-byte payload 解析成 controller state，重建 Steam-facing USB HID report `0x05`；在当前抓包路径上，绝不要把 BLE `0x09` 当成 USB `0x09` 直接交给 Steam。
