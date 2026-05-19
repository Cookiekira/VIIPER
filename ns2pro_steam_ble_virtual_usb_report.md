# NS2 Pro / Switch 2 Pro 蓝牙转 Steam 原生识别方案调研报告

> 目标：在 Windows PC 已有蓝牙能力的前提下，实现“NS2 Pro / Switch 2 Pro Controller 通过 BLE 连接 PC，但在 Steam 侧表现为有线 USB Switch 2 Pro Controller”，尽量保留 Steam Input 原生识别、GL/GR/C、gyro/IMU、rumble/haptics 等能力。  
> 日期：2026-05-18  
> 推荐实现语言：Go 优先；Rust/C# 可作为 GUI 或后备 BLE/工具链。  
> 推荐路线：fork VIIPER，新增 `NS2Pro` / `ns2pro` device type，在同一个 Go 进程中实现虚拟 USB composite device + BLE client + endpoint bridge。

---

## 0. 2026-05-19 抓包修正

本报告最初参考公开资料时，曾假设 Switch 2 Pro USB 路径是 `bcdDevice=0x0400`、5-interface audio composite，并且 Steam-visible USB input 可以直接使用 report `0x09`。  
本仓库后续对这只 NS2 Pro 的真实有线 USB 路径做了 USBPcap 抓包，结论以本地抓包为准：

- 抓包摘要：`captures/ns2pro/ns2pro-usb-20260519-005307/analysis/summary.md`
- bulk 初始化序列：`captures/ns2pro/ns2pro-usb-20260519-005307/analysis/bulk_nonempty.tsv`
- 设备身份：`VID=0x057E`、`PID=0x2069`、`bcdDevice=0x0101`
- configuration：2 interfaces，而不是 5-interface audio composite
- interface 0：HID，`0x81` interrupt IN + `0x01` interrupt OUT
- interface 1：vendor bulk，`0x02` OUT + `0x82` IN
- HID input：实际 Steam-visible 主输入是 report ID `0x05`，64 字节总长
- HID output / rumble：report ID `0x02`，64 字节总长
- HID report descriptor：97 bytes，包含 `0x05` 主输入、`0x09` secondary/structured input、`0x02` output

因此当前实现优先级修正为：

```text
USB 侧:
  复刻本地抓包中的 2-interface descriptor
  EP 0x81 返回 report 0x05
  EP 0x01 接收 report 0x02
  EP 0x02 / 0x82 做 captured bulk replay

BLE 侧:
  不再假设 BLE report 0x09 可以直接补 0x09 给 Steam
  后续需要 BLE state/report → USB report 0x05 转换
  或确认存在可直接映射到 USB 0x05 payload 的 BLE input characteristic
```

### 0.1 2026-05-19 VIIPER 验证进展

当前 fork 中的 `device/ns2pro` 已经按本地抓包复刻了 2-interface descriptor、HID report descriptor、neutral input report `0x05` 和 bulk replay。`examples/go/virtual_ns2pro` 已新增键盘/终端 feeder，用于验证 Steam 是否能收到 `0x05` 输入：

```text
go run ./examples/go/virtual_ns2pro --terminal --bulk-replay=true
go run ./examples/go/virtual_ns2pro --keyboard --bulk-replay=true
```

实测结论：

```text
Steam 能识别虚拟 NS2Pro / Nintendo Switch Pro Controller 路径
A/B/X/Y、D-pad、左右摇杆、肩键、系统键可反映到 Steam
GL/GR/C 可反映到 Steam；终端/键盘 feeder 提供 O/P/8/9/C/V 等兼容别名
左右摇杆 Y 轴默认 W/T=上、S/G=下；如目标 UI/游戏方向相反，可加 --invert-stick-y
```

下一步从 Steam → 设备方向验证：捕获 HID OUT report `0x02`，确认 Steam rumble test 的输出包，并预览/落盘后续可桥到 BLE output characteristic 的 payload。

```text
go run ./examples/go/virtual_ns2pro --terminal --bulk-replay=true --hid-out-ble-preview --hid-out-log rumble.tsv
```

---

## 1. 结论摘要

### 1.1 最推荐方案

最简单干练的工程方案是：

```text
NS2 Pro / Switch 2 Pro Controller
        │
        │ BLE / GATT, Windows 自带蓝牙
        ▼
Go BLE client
        │
        │ BLE input state/report → USB report 0x05
        │ output 0x02 / config commands
        ▼
VIIPER fork: NS2Pro device type
        │
        │ USB/IP virtual USB composite device
        ▼
usbip-win2 UDE driver
        │
        │ Windows USB stack
        ▼
Steam Input
```

这个方案的核心是：

1. **不要把 NS2 Pro 翻译成 XInput / DS4 / SInput。**
2. **不要只做普通 HID。**
3. **直接模拟本地抓包确认的 2-interface NS2 Pro USB composite device。**
4. **Steam-visible USB input 优先生成 report 0x05，而不是直接透传 BLE report 0x09。**
5. **Steam 发出的 HID output report 0x02 尽量转发回 BLE output characteristic。**
6. **Steam 通过 vendor bulk configuration interface 发来的 init / feature / sensor / calibration 命令，先记录，后逐步本地响应或转发给 BLE 手柄。**

---

## 2. 背景和外部证据

### 2.1 Steam 官方目前支持的是 USB 路径

Steam Client Beta 2025-11-25 更新说明中明确写了：

```text
Added support for Nintendo Switch 2 controllers connected over USB on Windows
```

这说明 Steam Input 的公开支持路径是 Windows + USB，而不是蓝牙直连。

工程含义：

```text
真实手柄侧：BLE
Steam 侧：必须看起来像 USB 有线 Switch 2 Pro Controller
```

### 2.2 本地抓包确认的 NS2 Pro USB composite device

本地 USBPcap 抓包中，这只 NS2 Pro 的 USB device descriptor 关键字段为：

```text
VID: 0x057E
PID: 0x2069
bcdDevice: 0x0101
Manufacturer: Nintendo
Product: Pro Controller
bNumInterfaces: 2
```

interfaces：

```text
Interface 0: HID
  EP 0x81 interrupt IN
  EP 0x01 interrupt OUT

Interface 1: vendor-specific configuration
  EP 0x02 bulk OUT
  EP 0x82 bulk IN
```

工程含义：

```text
不能只模拟 HID。
必须提供 interface 0 + interface 1。
不应为这只设备优先实现 earlier public notes 里的 audio interfaces。
USB input report 主路径是 0x05。
```

### 2.3 Linux patch 和 SDL 都指向 split-interface 设计

Linux input 邮件列表中的 Switch 2 driver patch 说明：

```text
input and rumble occur on the main HID interface,
but all other communication occurs over a "configuration" interface.
```

并且它明确提到初版支持一般输入和 basic rumble，但 IMU 尚未在该 patch 中实现。

SDL 的 Switch 2 实现也会在 USB 初始化中寻找 interface 1 的 bulk OUT endpoint，并发送初始化、LED、feature、rumble 相关命令。

工程含义：

```text
input / rumble:
  HID interface 0 可能足够承载

gyro / calibration / feature enable:
  大概率需要 configuration bulk interface
```

### 2.4 VIIPER 正好适合作为虚拟 USB 基底

VIIPER 是一个基于 USB/IP 的 userspace virtual USB input framework。Windows 上它依赖 usbip-win2 的 signed kernel driver，不需要为每种设备写内核驱动。它的设备模型支持：

```go
HandleTransfer(ep, dir, out []byte) []byte
GetDescriptor() *Descriptor
```

并提供 `ControlDevice` 接口处理 EP0 control request，例如 HID GET_REPORT / SET_REPORT。

工程含义：

```text
新增 NS2Pro device type：
  - descriptor.go 复刻 Pro Controller 2 descriptors
  - device.go 处理 EP0 / EP81 / EP01 / EP02 / EP82
  - bridge.go 连接 BLE input/output
```

---

## 3. 可复用项目和资料

| 类别 | 项目 / 资料 | 用途 | 是否推荐直接依赖 |
|---|---|---|---|
| 虚拟 USB | VIIPER | userspace USB/IP virtual device；可 fork 新增 device type | 强烈推荐 |
| Windows USB/IP | usbip-win2 | Windows USB/IP client；底层用 UDE driver | 间接依赖 |
| 协议资料 | ndeadly/switch2_controller_research | USB descriptors、HID reports、GATT UUID、commands | 强烈参考 |
| 用户态实现 | SDL `SDL_hidapi_switch2.c` | USB init、rumble、IMU、calibration 参考 | 强烈参考 |
| Linux driver | Linux `hid-switch2` patch | Valve copyright；split-interface 设计参考 | 强烈参考 |
| BLE 参考 | joycon2cpp | Windows 下 Switch 2 BLE 连接和低延迟策略 | 参考，不复用输出层 |
| BLE 参考 | Joypad OS `switch2_ble.c` | Switch 2 BLE 识别、按钮、GL/GR/C、当前 rumble TODO | 参考 |
| Go BLE | tinygo.org/x/bluetooth | Go BLE central，支持 Windows/macOS/Linux | 第一选择 |
| 旧输出层 | ViGEmBus / BetterJoy / DS4Windows | XInput/DS4 输出，不适合原生 Switch 2 Pro | 不推荐 |

---

## 4. 推荐架构

### 4.1 进程模型

第一版建议做成一个 Go 可执行文件：

```text
viiper ns2pro
```

职责：

```text
1. 启动 VIIPER USB/IP runtime
2. 创建虚拟 NS2Pro USB composite device
3. 自动 attach 到本机 usbip-win2
4. 扫描并连接 NS2 Pro BLE 手柄
5. 订阅 BLE input report/state
6. 将 BLE input 转为 USB HID report 0x05 interrupt IN
7. 将 Steam HID output 0x02 转为 BLE output report
8. 记录并响应 Steam bulk configuration commands
```

### 4.2 代码目录建议

```text
VIIPER fork
├─ device/
│  └─ ns2pro/
│     ├─ descriptor.go       # USB device/config/HID descriptors
│     ├─ hid_report.go       # HID report descriptor / report constants
│     ├─ device.go           # HandleTransfer / HandleControl
│     ├─ bridge.go           # USB endpoint ↔ BLE channel
│     ├─ commands.go         # bulk config command parser/responder
│     ├─ rumble.go           # USB output 0x02 → BLE output 0x02
│     └─ neutral_report.go   # 空闲 input report 0x05
│
├─ internal/
│  └─ ns2ble/
│     ├─ scan.go             # 扫描 / 选择 / 重连
│     ├─ client.go           # BLE client 生命周期
│     ├─ gatt.go             # service/characteristic discovery
│     ├─ input.go            # BLE input notification → USB 0x05
│     ├─ output.go           # output report 0x02 write
│     ├─ feature.go          # feature enable / IMU enable
│     └─ debug_parse.go      # 可选 debug parser
│
└─ cmd/
   └─ viiper/
      └─ ns2pro.go           # CLI command: viiper ns2pro
```

---

## 5. USB 侧实现设计

### 5.1 设备身份

第一版为了验证 Steam 原生识别，需要模拟：

```text
idVendor:  0x057E
idProduct: 0x2069
bcdDevice: 0x0101
Manufacturer: Nintendo
Product: Pro Controller
```

注意：这适合个人研究和本机测试。公开分发时直接使用 Nintendo VID/PID/Product string 可能涉及 USB VID 授权、商标和兼容性风险。若要公开发布，最好换成自有 VID/PID，并争取 Steam 加白名单；但这样会影响 Steam 原生识别验证。

### 5.2 Interface 0: HID

```text
Interface 0:
  bInterfaceClass    = 0x03 HID
  bNumEndpoints      = 2

Endpoint 0x81:
  Direction          = IN
  Type               = Interrupt
  MaxPacketSize      = 64
  作用               = Steam 读取 input report

Endpoint 0x01:
  Direction          = OUT
  Type               = Interrupt
  MaxPacketSize      = 64
  作用               = Steam 写 output report / rumble
```

核心报告：

```text
Input report:
  report ID 0x05
  total USB payload: [0x05][63-byte report payload]
  当前抓包中，Steam-visible HID IN 主要都是 0x05

Output report:
  report ID 0x02
  Steam 写入: [0x02][payload...]
```

### 5.3 Interface 1: vendor-specific configuration

```text
Interface 1:
  bInterfaceClass    = 0xFF vendor-specific
  bNumEndpoints      = 2

Endpoint 0x02:
  Direction          = OUT
  Type               = Bulk
  MaxPacketSize      = 64
  作用               = Steam 发 init / feature / LED / calibration / sensor 命令

Endpoint 0x82:
  Direction          = IN
  Type               = Bulk
  MaxPacketSize      = 64
  作用               = 返回命令响应
```

第一版策略：

```text
EP 0x02 OUT:
  全量记录 bytes
  按 command id / subcommand id dispatch
  能本地响应的本地响应
  需要真实手柄状态的从 BLE 缓存/转发

EP 0x82 IN:
  从 response queue 取一帧
  没有响应时返回 NAK/空等待，具体按 VIIPER/USBIP 能力实现
```

### 5.4 Interface 2/3/4: 暂不实现

本地抓包中的这只 NS2 Pro 有线 USB 路径没有暴露 earlier public notes 中的 audio interfaces。  
因此第一版不实现 interface 2/3/4，也不做 audio stub。若后续遇到另一版固件或另一只设备枚举出 5-interface audio composite，再作为单独 descriptor variant 处理。

---

## 6. BLE input 设计

### 6.1 BLE GATT mapping

Pro Controller 2 的 input report #2 是 `0x09`。

BLE characteristic：

```text
Service UUID:
  ab7de9be-89fe-49ad-828f-118f09df7fd0

Input report 0x09 characteristic:
  UUID:   7492866c-ec3e-4619-8258-32755ffcc0f8
  Handle: 0x000E

Output report 0x02 characteristic:
  UUID:   cc483f51-9258-427d-a939-630c31f72b05
  Handle: 0x0012
```

关键点：

```text
BLE report payload 不带 HID report ID。
USB/HID 侧需要 report ID，但本地抓包确认 Steam-visible 主输入是 USB report 0x05。
```

因此旧的输入桥接假设不再成立：

```text
BLE notification:
  [63-byte payload]

不要直接当作 Steam-visible USB HID input:
  [0x09][63-byte payload]
```

当前 MVP 应先做：

```text
键盘输入 / 未来 BLE parsed state
        ▼
USB HID input report 0x05
        ▼
Steam Input
```

### 6.2 BLE input 第一版需要转换成 USB 0x05

旧建议是不要做：

```text
BLE report → buttons/axes/gyro normalized state → 重新组包
```

但本地抓包修正后，不能再做：

```text
BLE report → 补 report ID → 原样给 Steam
```

原因是：

```text
1. Steam 在这只 NS2 Pro USB 路径上看到的是 report 0x05
2. 真实 HID IN 抓包中，非空 input reports 主要都是 0x05
3. BLE report 0x09 不能直接补 0x09 当 USB 输入给 Steam
```

因此第一版输入验证先从键盘生成 USB report 0x05；后续 BLE 阶段需要确认 BLE 侧是否有等价 0x05 payload，或将 BLE state 转换为 USB 0x05 layout。解析/日志仍可用于 debug，例如：

```text
counter
battery
A/B/X/Y
GL/GR/C
left/right stick 12-bit raw value
motion length
```

### 6.3 Go BLE 库

优先使用：

```text
tinygo.org/x/bluetooth
```

理由：

```text
1. Go 原生
2. 支持 Windows/macOS/Linux
3. Windows 后端走 WinRT
4. 支持 scan、connect、characteristic write、notifications
```

如果 Windows BLE 配对/重连不稳定，后备方案：

```text
Go: VIIPER USB device
Rust: btleplug BLE bridge
或
C#: WinRT BLE pairing/helper
```

但第一版建议坚持 all-Go。

### 6.4 USB 0x05 input normalization 伪代码

```go
func BuildUSBInput05(state NS2InputState) []byte {
    usb := NeutralInputReport05()
    usb[0] = 0x05
    binary.LittleEndian.PutUint32(usb[1:5], state.Counter)
    encodeButtons05(usb[5:9], state.Buttons)
    encodeStick12(usb[11:14], state.LeftStick)
    encodeStick12(usb[14:17], state.RightStick)
    return usb
}
```

---

## 7. Rumble / haptics 设计

### 7.1 Steam → HID output

Steam 在原生 Switch 2 Pro 路径下，预期会向 HID OUT endpoint 写 output report `0x02`：

```text
USB HID OUT:
  [0x02][payload...]
```

### 7.2 HID output → BLE output

Pro Controller 2 BLE output report `0x02` 写入：

```text
UUID: cc483f51-9258-427d-a939-630c31f72b05
Handle: 0x0012
```

公开资料中的 BLE output report 0x02 layout：

```text
offset 0x00: Bluetooth 下固定为 0x00
offset 0x01: left LRA rumble data,  0x10 bytes
offset 0x11: right LRA rumble data, 0x10 bytes
offset 0x21: reserved, 0x09 bytes
```

第一版转换策略：

```text
Steam USB report:
  [0x02][payload...]

BLE write:
  [0x00][payload adjusted/truncated/padded to expected BLE format]
```

注意事项：

```text
1. 不要先转 XInput rumble。
2. 优先转发 Steam 已经生成的 Switch 2 Pro haptics packet。
3. 若不震动，再参考 SDL rumble 编码实现。
4. SDL preliminary rumble support 中 Pro Controller 2 使用 report id 0x02，并为左右 LRA 填数据。
```

---

## 8. Configuration / gyro / calibration 设计

### 8.1 为什么 gyro 依赖 configuration interface

报告里的 motion data 在 HID input report 0x09 中，但 IMU 是否启用、校准数据如何读取，通常依赖 configuration / bulk command。

SDL 的 IMU 支持做了几件事：

```text
1. 读取 flash block 0x13040 作为 gyro bias
2. 读取 flash block 0x13100 作为 accel bias
3. 添加 gyro / accel sensor
4. 通过 bulk command enable sensors
5. 后续解析 input report 中的 motion bytes
```

因此第一版若只透传 input report，可能会出现：

```text
按钮/摇杆可用
rumble 可用
gyro 不出现或漂移严重
```

### 8.2 Feature Select command

Feature Select 是 command `0x0C`。相关 flags：

```text
0x01 = Button state
0x02 = Analog sticks
0x04 = IMU linear accelerometer + gyro
0x10 = Mouse data, Joy-Con only
0x20 = Rumble
0x80 = Magnetometer
```

USB command header 中 transport 是 `0x00`，Bluetooth 是 `0x01`。因此当 Steam 通过 USB bulk 发 enable IMU 时，你的 bridge 可以：

```text
USB bulk command:
  transport = 0x00

转发到 BLE / 或本地模拟:
  transport = 0x01
```

### 8.3 第一版 command strategy

建议分三层实现：

#### Layer A: log-only

所有 bulk OUT 先记录：

```text
timestamp
endpoint
length
hex bytes
decoded command id
decoded subcommand
```

#### Layer B: replay responder

从真实有线 NS2 Pro 抓包中建立响应表：

```text
key:
  command id + subcommand + request prefix

value:
  response bytes
```

先满足 Steam 初始化流程。

#### Layer C: semantic responder / BLE forwarder

逐步替换 replay：

```text
0x0C feature select:
  本地维护 feature mask/enabled flags
  同步 BLE feature enable

0x02 flash read:
  优先从真实 BLE 手柄读取/缓存
  或返回有线抓包中的合理 calibration block

0x01 / 0x0A vibration:
  直接转 BLE output/haptic

0x09 LED:
  本地 ACK；可后续转 BLE

0x03 initialization:
  本地 ACK / 必要时转 BLE start output
```

---

## 9. VIIPER device type 设计

### 9.1 Go struct

```go
type NS2ProDevice struct {
    desc *usb.Descriptor

    ble *ns2ble.Client

    latestInput atomic.Value // []byte, 64 bytes: [0x05][63-byte payload]

    bulkIn chan []byte
    log    Logger
}
```

### 9.2 HandleTransfer

```go
func (d *NS2ProDevice) HandleTransfer(ep uint32, dir uint32, out []byte) []byte {
    switch {
    case ep == 1 && dir == usbip.DirIn:
        // Host polling HID interrupt IN 0x81.
        return d.getLatestOrNeutralInputReport05()

    case ep == 1 && dir == usbip.DirOut:
        // Host wrote HID interrupt OUT 0x01.
        d.handleHidOut(out)
        return nil

    case ep == 2 && dir == usbip.DirOut:
        // Host wrote vendor/config bulk OUT 0x02.
        d.handleBulkCommand(out)
        return nil

    case ep == 2 && dir == usbip.DirIn:
        // Host polling vendor/config bulk IN 0x82.
        return d.popBulkResponse()

    default:
        d.log.UnknownTransfer(ep, dir, out)
        return nil
    }
}
```

### 9.3 HandleControl

VIIPER 支持 optional `ControlDevice`。建议实现 EP0 日志和必要 HID class requests：

```go
func (d *NS2ProDevice) HandleControl(
    bmRequestType, bRequest uint8,
    wValue, wIndex, wLength uint16,
    data []byte,
) (resp []byte, handled bool) {
    d.log.Control(bmRequestType, bRequest, wValue, wIndex, wLength, data)

    // 必要时处理：
    // HID GET_REPORT
    // HID SET_REPORT
    // HID GET_IDLE / SET_IDLE
    // Audio class-specific minimal responses

    return nil, false // 默认交给 VIIPER 内置处理
}
```

### 9.4 BLE input callback

```go
func (d *NS2ProDevice) AttachBLE(c *ns2ble.Client) {
    go func() {
        for report := range c.InputReports {
            // 后续应存储转换后的 USB HID 0x05 report：[0x05][payload]
            d.latestInput.Store(convertBLEToUSBInput05(report))
        }
    }()
}
```

使用 “latest frame” 而非 FIFO：

```text
BLE notification 堆积时丢旧帧；
USB host 轮询时取最新帧；
低延迟优先于逐帧不丢。
```

---

## 10. 开发阶段规划

### Phase 0: 环境和抓包准备

准备：

```text
Windows 10/11 PC
Steam Beta / 当前支持 Switch 2 USB 的 Steam
usbip-win2
VIIPER build environment
USBPcap + Wireshark
NS2 Pro / Switch 2 Pro Controller
```

抓包目标：

```text
1. 真机有线插入后 USB enumeration
2. Steam 打开设备时的 bulk OUT / bulk IN
3. Steam rumble test 时 HID OUT report 0x02
4. Steam gyro enable 时 bulk commands
5. input report 0x05 的空闲帧
```

### Phase 1: dummy virtual USB device

目标：不接 BLE，仅让 Steam 识别虚拟设备。

实现：

```text
1. fork VIIPER
2. 新增 device/ns2pro
3. 复刻本地抓包中的 2-interface descriptors
4. EP 0x81 返回 neutral input report 0x05
5. EP 0x01 / 0x02 / 0x82 / EP0 全量日志
```

验收：

```text
Windows 成功枚举
Steam logs/controller.txt 出现 VID_057E PID_2069
Steam UI 显示 Switch 2 Pro / Nintendo Switch 2 controller
Steam 尝试访问 interface 1 bulk endpoint
```

### Phase 2: bulk init replay

目标：满足 Steam 初始化流程。

实现：

```text
1. 将真机抓包中的 bulk responses 做成 replay 表
2. 针对 SDL init sequence 常见命令先响应
3. 记录 Steam 是否继续发 HID output 0x02 / sensor enable
```

验收：

```text
Steam 不再卡在初始化
Steam 进入完整 controller config 页面
rumble test 能触发 HID OUT 0x02
gyro UI 至少显示 sensor capability
```

### Phase 2.5: keyboard / terminal USB 0x05 input verification

目标：不接 BLE，先证明 Steam 能正确接收 feeder 生成的 USB input report `0x05`。

实现：

```text
1. 扩展 examples/go/virtual_ns2pro
2. --keyboard 用 Windows GetAsyncKeyState 轮询键盘
3. --terminal 从当前终端 raw mode 读取 keypress 并生成短脉冲
4. 基于 neutral 0x05 report 写入 counter、button bytes、12-bit sticks
5. 支持 --invert-stick-y 处理目标 UI/游戏中的 Y 轴方向差异
```

验收：

```text
A/B/X/Y 正常
D-pad 正常
左右摇杆正常
L/R/ZL/ZR、Plus/Minus/Home/Capture 正常
GL/GR/C 正常
Steam controller test UI 能显示对应状态
```

当前状态：已完成并通过 Steam 人工验证。

### Phase 3: BLE input

目标：真实手柄输入进入 Steam。

实现：

```text
1. Go BLE scan / connect
2. 找到真实 NS2 Pro input characteristic
3. Enable notifications
4. 将 BLE report/state 转换成 USB input report 0x05
5. 写入 latestInput
```

验收：

```text
A/B/X/Y 正常
D-pad 正常
左右摇杆正常
GL/GR/C 正常
Steam 能绑定扩展键
```

### Phase 4: rumble capture / bridge

目标：先确认 Steam rumble test 产生 HID OUT report `0x02`，再桥到真实 NS2 Pro BLE output 让手柄震动。

实现：

```text
1. 监听 EP 0x01 OUT，并在 feeder 侧记录 HID OUT report
2. 识别 report 0x02，记录 hex 和 left/right LRA 是否非零
3. 预览 USB 0x02 → BLE output payload：0x00 + left 16 bytes + right 16 bytes + reserved 9 bytes
4. 后续接入 BLE 后写 cc483f51-9258-427d-a939-630c31f72b05
```

验收：

```text
Steam rumble test 时 feeder 收到 HID OUT 0x02
HID OUT 可以落盘为 TSV，便于与抓包样本对比
`--hid-out-ble-preview` 能打印待写入 BLE output characteristic 的 payload 预览
接入 BLE 后，Steam rumble test 时 BLE write 发生
NS2 Pro 实际震动
停止 rumble 后能停止震动
长时间 rumble 不堆积、不延迟
```

### Phase 5: gyro / IMU

目标：Steam gyro 正常可用。

实现：

```text
1. 处理 feature select command 0x0C
2. 支持 feature flags 0x01 / 0x02 / 0x04 / 0x20
3. 响应或转发 flash calibration reads
4. 确保 BLE report 0x09 中 motion length 非零
5. 将 motion data 原样透传给 Steam
```

验收：

```text
Steam Input gyro UI 可启用
gyro movement 有响应
静止时漂移可接受
GL/GR/C + gyro 同时工作
rumble 不影响 input latency
```

### Phase 6: 稳定性和发布

实现：

```text
1. 自动重连
2. 手柄断开时 USB device 保持 neutral state
3. Steam / Windows 休眠唤醒处理
4. 多手柄支持
5. 日志级别
6. 安装脚本
7. 合规 VID/PID 策略
```

---

## 11. Debug checklist

### Steam 只显示 generic HID

检查：

```text
VID/PID 是否 057E:2069
Product string 是否 Pro Controller
configuration descriptor 是否 2 interfaces
interface 0 HID report descriptor 是否正确
interface 1 vendor bulk 是否存在
EP0 GET_DESCRIPTOR 是否完整响应
```

### Steam 识别设备但没有输入

检查：

```text
EP 0x81 是否被 host 轮询
返回 report 是否是 [0x05][63 bytes]
neutral report 长度是否 64
BLE payload 是否被错误地直接作为 0x09 USB report 转发
```

### 有输入但没有 GL/GR/C

检查：

```text
是否正确生成 USB report 0x05 的 button bytes
是否错误改写 button bytes
是否 Steam 进入了 Switch 2 Pro path 而不是 generic HID path
```

### 有输入但没有 rumble

检查：

```text
Steam rumble test 时 EP 0x01 OUT 是否收到 report 0x02
report length 是 64 还是 65
USB report 到 BLE payload 是否正确去掉/转换 report ID
BLE output characteristic 是否正确
是否需要先 enable rumble / feature 0x20
```

### 没有 gyro

检查：

```text
Steam 是否发 sensor enable / feature select 0x0C
bulk init replay 是否完整
flash calibration reads 是否返回合理数据
USB report 0x05 / 后续 BLE 转换后的 motion bytes 是否有效
是否启用了 IMU feature flag 0x04
```

---

## 12. 风险和注意事项

### 12.1 Steam 内部实现不是公开规范

Steam 只公开了“支持 USB on Windows”，没有公开完整识别规则和协议实现。  
本方案基于：

```text
Steam 官方更新说明
SDL Switch 2 implementation
Linux hid-switch2 patch
ndeadly reverse engineering
真实有线抓包
```

因此必须通过实际日志验证。

### 12.2 VID/PID 合规风险

本地研究阶段可模拟 `057E:2069` 验证 Steam 行为；公开发布时需要考虑：

```text
USB VID 授权
Nintendo trademark / product string
Steam whitelist
用户误识别真实硬件
```

### 12.3 VIIPER license

VIIPER 是 GPL-3.0。fork 并分发修改版时，需要遵守 GPL-3.0。  
如果希望应用本体不 GPL，优先使用 VIIPER standalone server + TCP API 的方式；但本项目若直接 fork core 加 device type，整体分发基本会被 GPL 约束。

### 12.4 Go BLE on Windows 可能有配对坑

`tinygo.org/x/bluetooth` 支持 Windows BLE，但 bonding/pairing、重连和低延迟 connection parameters 可能需要实测。后备方案是：

```text
C# WinRT helper 负责配对/选择设备
Go 主进程负责 VIIPER 和已配对设备连接
或 Rust btleplug 负责 BLE
```

---

## 13. 最小可行版本定义

第一版 MVP 不追求所有功能，只追求：

```text
1. Steam 识别为 Switch 2 Pro Controller
2. 键盘生成的 USB report 0x05 输入能进 Steam，后续 BLE 输入转换也走同一路径
3. GL/GR/C 可见或可绑定
4. rumble test 能让手柄震动
5. gyro 至少能被 Steam 发现并响应
```

第一版可以暂不支持：

```text
NFC
audio/headset
firmware update
多手柄
完整 LED
完整 calibration 编辑
通用 raw USB plugin system
```

---

## 14. 推荐下一步

优先顺序：

```text
1. fork VIIPER
2. 做 NS2Pro dummy USB device
3. 用 Steam 验证识别和 bulk/HID output 日志
4. 抓真实有线 NS2 Pro USBPcap
5. 做 bulk replay responder
6. 先用键盘生成 USB input 0x05 验证 Steam 输入路径
7. 捕获 Steam HID OUT 0x02，落盘并预览 BLE output payload
8. 接入 HID output 0x02 → BLE rumble
9. 接入 Go BLE input，并转换为 USB input 0x05
10. 补 feature select / gyro / calibration
```

最重要的第一步不是写 BLE，而是验证虚拟 USB device 能否让 Steam 进入原生 Switch 2 Pro path。  
只要 Steam 对 dummy device 发出 bulk init 和 HID output 0x02，后续桥接 BLE 的成功概率就很高。

---

## 15. 外部 source 列表

### Steam / 官方支持

- Steam Client Beta - November 25th  
  https://steamcommunity.com/groups/SteamClientBeta/announcements/detail/599669707226744434

### Switch 2 Pro 协议和描述符

- ndeadly/switch2_controller_research - descriptors.md  
  https://github.com/ndeadly/switch2_controller_research/blob/master/descriptors.md
- ndeadly/switch2_controller_research - hid_reports.md  
  https://github.com/ndeadly/switch2_controller_research/blob/master/hid_reports.md
- ndeadly/switch2_controller_research - commands.md  
  https://github.com/ndeadly/switch2_controller_research/blob/master/commands.md

### SDL Switch 2 实现

- SDL: Moved Nintendo Switch 2 Controller initialization from hid.c to SDL_hidapi_switch2.c  
  https://discourse.libsdl.org/t/sdl-moved-nintendo-switch-2-controller-initialization-from-hid-c-to-sdl-hidapi-switch2-c/62149
- SDL: switch2: Send full init sequence from real hardware  
  https://discourse.libsdl.org/t/sdl-switch2-send-full-init-sequence-from-real-hardware/64162
- SDL: switch2: Preliminary rumble support  
  https://discourse.libsdl.org/t/sdl-switch2-preliminary-rumble-support/64164
- SDL: switch2: Bring up IMU support  
  https://discourse.libsdl.org/t/sdl-switch2-bring-up-imu-support/64773

### Linux driver patch

- `[PATCH] HID: switch2: Add preliminary Switch 2 controller driver`  
  https://marc.info/?l=linux-input&m=176360509412782

### VIIPER / USB/IP / Windows virtual USB

- VIIPER GitHub  
  https://github.com/Alia5/VIIPER
- VIIPER documentation  
  https://alia5.github.io/VIIPER/stable/
- VIIPER Go `usb` package  
  https://pkg.go.dev/github.com/Alia5/VIIPER/usb
- usbip-win2  
  https://github.com/vadimgrn/usbip-win2
- Microsoft UDE / UdeCx overview  
  https://learn.microsoft.com/en-us/windows-hardware/drivers/usbcon/developing-windows-drivers-for-emulated-usb-host-controllers-and-devices
- Microsoft VHF overview  
  https://learn.microsoft.com/en-us/windows-hardware/drivers/hid/virtual-hid-framework--vhf-

### BLE / 现有 Switch 2 BLE 项目

- tinygo-org/bluetooth  
  https://github.com/tinygo-org/bluetooth
- tinygo.org/x/bluetooth Go package  
  https://pkg.go.dev/tinygo.org/x/bluetooth
- TheFrano/joycon2cpp  
  https://github.com/TheFrano/joycon2cpp
- Joypad OS Switch 2 BLE driver  
  https://github.com/joypad-ai/joypad-os/blob/main/src/bt/bthid/devices/vendors/nintendo/switch2_ble.c

---

## 16. 一句话方案

> fork VIIPER，在 Go 中新增 `NS2Pro` USB composite device；USB 侧模拟本地抓包确认的 `057E:2069` / `bcdDevice=0x0101` / 2-interface Pro Controller，HID interface 负责 USB input report `0x05` 和 rumble output report `0x02`，vendor bulk interface 负责 init/feature/gyro/calibration replay；BLE 侧后续将真实输入转换为 USB report `0x05`，并把 Steam 的 output report `0x02` 和 configuration commands 转回真实 BLE 手柄。
