# NS2Pro BLE -> Steam USB Report 0x05 实施说明

> 日期：2026-05-19  
> 当前阶段：Phase 5，真实 NS2 Pro / Switch 2 Pro Controller BLE 输入、player LED、rumble bridge 已进入当前虚拟 USB 路径；gyro/IMU 已有实验桥接开关  
> 核心结论：**BLE Pro Controller 2 主输入走 GATT report `0x09`；Steam-visible 虚拟 USB 路径继续输出 HID report `0x05`；rumble 写 BLE output report `0x02` characteristic；gyro 使用 `--ble-gyro` opt-in。**

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
- Steam HID OUT / rumble：非空 output reports 全部以 report ID `0x02` 开头；其 63-byte payload 可转换为 BLE output report `0x02` payload
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
  - command header 的 transport byte：`0x00 = USB`、`0x01 = Bluetooth`
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

- BLE command characteristic：

```text
UUID:   649d4ac9-8eb7-4e6c-af44-1ea54fe5f005
```

用于 player LED、rumble enable / feature command 等命令。写入 BLE command 时 header 必须使用 Bluetooth transport byte `0x01`，例如 player LED 1：

```text
09 91 01 07 00 08 00 00 01 00 00 00 00 00 00 00
```

- BLE output report `0x02` / rumble characteristic：

```text
UUID:   cc483f51-9258-427d-a939-630c31f72b05
```

用于写入 rumble payload。不要把 SDL USB-side/internal `0x50` rumble packet 写到 BLE command characteristic；当前真实可用路径是写 BLE output report `0x02` characteristic。

### 1.3 当前仓库状态

已经完成并验证：

- `device/ns2pro`：2-interface USB descriptor、HID report descriptor、neutral input report `0x05`、bulk replay
- `examples/go/virtual_ns2pro`：键盘/终端 feeder 生成 USB report `0x05`
- Steam 人工验证：A/B/X/Y、D-pad、sticks、shoulders、system buttons、GL/GR/C 可进入 Steam
- BLE input report `0x09` 已可转换为 Steam-facing USB report `0x05`
- player slot LED 可通过 BLE command characteristic 设置为常亮
- HID OUT `0x02` 已可记录，并可转换为 BLE output report `0x02` rumble payload；真实手柄 rumble 已验证可工作
- gyro/IMU 已有实验路径：启用 BLE IMU feature bits，并把 BLE `0x09` compact motion tail 填入 Steam/SDL 在 USB `0x05` 中读取的 sensor 区域

Phase 3/4/5 不需要重做 USB descriptor，也不需要改变 Steam 输入路径。

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
- player slot LED 常亮，不再保持闪烁
- Steam rumble 可通过 BLE output report `0x02` 进入真实手柄

Phase 3 非目标：

- 不把 gyro/IMU 作为 Phase 3 验收项；Phase 5 只提供实验桥接，轴向/标定仍需真实游戏验证
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
go run ./examples/go/virtual_ns2pro --ble-input --ble-input-report=09 --ble-rumble=true --bulk-replay=true
```

保留 report `0x05`，但只作为显式 fallback：

```powershell
go run ./examples/go/virtual_ns2pro --ble-input --ble-input-report=05 --ble-rumble=true --bulk-replay=true
```

可选调试：

```powershell
go run ./examples/go/virtual_ns2pro --ble-input --ble-input-report=09 --ble-rumble=true --bulk-replay=true --hid-out-ble-preview --ble-input-log ble_raw.tsv --ble-input-decode-log ble_decoded.tsv
```

默认 player LED pattern 为 `0x01`，可通过 `--ble-player-led` 调整；`--ble-player-led=0` 禁用 LED command 写入。

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
has_motion
motion_source
motion_meta_hex
accel_x
accel_y
accel_z
gyro_x
gyro_y
gyro_z
```

实用检查项：

- 每次只按一个按钮，确认只有预期 decoded bit 变化。
- 将每个摇杆轴推到极值，确认 packed 12-bit values 平滑变化。
- 单独测试 GL/GR/C；如果把 `payload[0x04]` 当成 padding，这几个键最容易丢。
- 确认 Steam-facing report 永远以 `0x05` 开头。
- 确认 BLE raw payload 永远不会被直接作为 USB `0x09` 转发。

---

## 5. Phase 4：player LED / rumble bridge

### 5.1 Player LED

player LED 使用 BLE command characteristic：

```text
649d4ac9-8eb7-4e6c-af44-1ea54fe5f005
```

命令格式为：

```text
09 91 01 07 00 08 00 00 [pattern] 00 00 00 00 00 00 00
```

注意第三字节 `0x01` 是 Bluetooth transport byte；USB 抓包中的同类命令可能是 `0x00`，BLE 写入时不能照抄为 `0x00`。

当前默认：

```text
--ble-player-led=1
```

常用 pattern：

```text
0x01 LED 1
0x02 LED 2
0x04 LED 3
0x08 LED 4
0x00 不写 player LED command
```

### 5.2 Rumble bridge

继续捕获 Steam HID OUT `0x02`。Steam-facing USB report 是：

```text
[0x02][63-byte payload]
```

BLE output report `0x02` 写入 characteristic：

```text
cc483f51-9258-427d-a939-630c31f72b05
```

BLE payload 为 42 bytes：

```text
[0x00][left LRA 16 bytes][right LRA 16 bytes][reserved 9 bytes]
```

转换规则：

```text
USB report[1:17]  -> BLE payload[1:17]
USB report[17:33] -> BLE payload[17:33]
BLE payload[0]    = 0x00
BLE payload[33:42] = 0x00...
```

不要写到 BLE command characteristic，也不要把 SDL USB/internal `0x50` rumble packet 直接作为 BLE command。`0x50` 是 SDL Switch 2 USB-side rumble packet 逻辑，当前 BLE Pro Controller 2 rumble 的可用路径是 output report `0x02` characteristic。

在启用 rumble 时，当前实现还会向 command characteristic 发送 SDL 参考初始化命令，且 BLE command header 使用 transport byte `0x01`：

```text
0a 91 01 08 00 14 00 00 01 ff ff ff ff ff ff ff ff 35 00 46 00 00 00 00 00 00 00 00
01 91 01 01 00 00 00 00
```

### 5.3 当前 CLI flags

```text
--ble-input
--ble-input-report=09
--ble-rumble=true
--ble-player-led=1
--hid-out-ble-preview
--ble-write-with-response
```

`--ble-input --ble-rumble` 同时启用时，rumble 会复用同一个 BLE connection，避免对同一只手柄建立第二条连接。

---

## 6. Phase 5：experimental gyro / IMU bridge

### 6.1 启用 BLE IMU feature

`--ble-gyro` 会通过 BLE command characteristic 发送两条 feature command。第三字节仍然必须是 Bluetooth transport byte `0x01`：

```text
0c 91 01 02 00 04 00 00 27 00 00 00
0c 91 01 04 00 04 00 00 27 00 00 00
```

`0x27` 沿用 SDL USB 初始化路径中的 feature bits，包含 buttons、analog sticks、IMU 等当前需要的输出位。`ndeadly/switch2_input_viewer.py` 中也把 feature bit 2 标为 IMU reporting。

### 6.2 BLE motion tail -> USB `0x05` sensor 区域

SDL Switch 2 参考实现从 USB report `0x05` 的以下 offset 读取 sensor：

```text
USB report[0x2b:0x2f] = sensor timestamp, little-endian uint32
USB report[0x31:0x33] = accel raw 0
USB report[0x33:0x35] = accel raw 1
USB report[0x35:0x37] = accel raw 2
USB report[0x37:0x39] = gyro raw 0
USB report[0x39:0x3b] = gyro raw 1
USB report[0x3b:0x3d] = gyro raw 2
```

BLE report `0x09` compact format中，Pro Controller 2 motion tail 为：

```text
payload[0x0e]      = motion length
payload[0x0f:0x37] = compact motion tail
```

当前实验映射：

```text
synthetic monotonic us timestamp -> USB report[0x2b:0x2f]
BLE payload[0x0f:0x11]           -> USB report[0x2f:0x31]
BLE payload[0x11:0x1d]           -> USB report[0x31:0x3d]
```

实现中还保留了一个窄 fallback：如果 compact axes window `payload[0x11:0x1d]` 全零，但旧式窗口 `payload[0x30:0x3c]` 有非零 motion 数据，则将 `payload[0x30:0x3c]` 填入 USB `report[0x31:0x3d]`。`ble_decoded.tsv` 的 `motion_source` 会显示 `compact` 或 `legacy_0x30`。

timestamp 不直接使用 BLE tail，而是在虚拟 USB 输出时按本机单调时间合成。原因是虚拟 USB 输出频率通常是 250Hz，而 BLE notification 频率可能更低；如果重复发送同一个 BLE sample 且 timestamp 不变，Steam/SDL 的 sensor readiness 计算容易变差。

### 6.3 当前 CLI flags

推荐实验命令：

```powershell
go run ./examples/go/virtual_ns2pro --ble-input --ble-input-report=09 --ble-rumble=true --ble-gyro --bulk-replay=true --ble-input-decode-log ble_decoded.tsv
```

`--ble-gyro` 是 opt-in。不开这个开关时，BLE `0x09` 仍只桥接 buttons/sticks，不会把 motion tail 写入 USB report `0x05`。

### 6.4 已知风险

- compact motion tail 的轴向和 Steam 最终 gyro 轴向可能还需要按真实游戏/Steam 校准画面微调。
- 当前没有读取真实手柄的 gyro/accel factory calibration 并转换给 Steam；先让 raw IMU 数据进入 Steam path。
- 如果 `ble_decoded.tsv` 中 `has_motion=false` 或 `motion_len=0`，说明 IMU feature 还没有打开或手柄没有开始上报 motion tail。
- 如果 `motion_source=legacy_0x30`，说明当前手柄通知更接近 `joycon2cpp` 旧式 offset 解释；后续可用真实 Steam 轴向测试决定是否把它升为主映射。

## 7. 后续阶段

Phase 6：稳定性

- BLE 重连时不销毁虚拟 USB device。
- BLE 断开时保持 neutral USB `0x05` state。
- 为多手柄场景增加更清晰的 scan selection。
- 日志保持 opt-in。

---

## 8. 资料来源

本地：

- `captures/ns2pro/ns2pro-usb-20260519-005307/analysis/summary.md`
- `captures/ns2pro/ns2pro-usb-20260519-005307/analysis/hid_in_81.tsv`
- `captures/ns2pro/ns2pro-usb-20260519-005307/analysis/hid_out_01.tsv`
- `captures/ns2pro/ns2pro-usb-20260519-005307/analysis/bulk_nonempty.tsv`
- `references/SDL_hidapi_switch2.c`
- `references/rumble_output.txt`
- `joycon2cpp`

外部：

- `ndeadly/switch2_controller_research` HID reports  
  https://github.com/ndeadly/switch2_controller_research/blob/master/hid_reports.md
- `ndeadly/switch2_controller_research` descriptors  
  https://github.com/ndeadly/switch2_controller_research/blob/master/descriptors.md
- `ndeadly/switch2_controller_research` commands  
  https://github.com/ndeadly/switch2_controller_research/blob/master/commands.md
- `ndeadly/switch2_input_viewer.py`  
  https://gist.github.com/ndeadly/7d27aa63e2f653a902a2474dbcbc08b3
- `joycon2cpp`  
  https://github.com/TheFrano/joycon2cpp
- joypad OS switch 2 BLE driver: ./references/

---

## 9. 一句话实现规则

> 订阅 BLE input report `0x09`，将 63-byte payload 解析成 controller state，重建 Steam-facing USB HID report `0x05`；rumble 从 Steam HID OUT `0x02` 转为 BLE output report `0x02` 42-byte payload 写入 `cc483f51-9258-427d-a939-630c31f72b05`；`--ble-gyro` 打开时把 BLE compact motion tail 桥接到 USB `0x05` sensor offsets；在当前抓包路径上，绝不要把 BLE `0x09` 当成 USB `0x09` 直接交给 Steam，也不要把 SDL `0x50` rumble packet 当成 BLE command。
