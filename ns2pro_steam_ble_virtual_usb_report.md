# NS2 Pro / Switch 2 Pro 蓝牙转 Steam 原生识别方案调研报告

> 目标：在 Windows PC 已有蓝牙能力的前提下，实现“NS2 Pro / Switch 2 Pro Controller 通过 BLE 连接 PC，但在 Steam 侧表现为有线 USB Switch 2 Pro Controller”，尽量保留 Steam Input 原生识别、GL/GR/C、gyro/IMU、rumble/haptics 等能力。  
> 日期：2026-05-18  
> 推荐实现语言：Go 优先；Rust/C# 可作为 GUI 或后备 BLE/工具链。  
> 推荐路线：fork VIIPER，新增 `switch2pro` / `ns2pro` device type，在同一个 Go 进程中实现虚拟 USB composite device + BLE client + endpoint bridge。

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
        │ report 0x09 / output 0x02 / config commands
        ▼
VIIPER fork: switch2pro device type
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
3. **直接模拟 Switch 2 Pro Controller 的 USB composite device。**
4. **BLE input report 0x09 尽量原样透传给 Steam。**
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

### 2.2 Switch 2 Pro Controller 是 USB composite device

公开 reverse engineering 资料中，Pro Controller 2 的 USB device descriptor 关键字段为：

```text
VID: 0x057E
PID: 0x2069
bcdDevice: 0x0400
Manufacturer: Nintendo
Product: Switch 2 Pro Controller
bNumInterfaces: 5
```

最关键的 interfaces：

```text
Interface 0: HID
  EP 0x81 interrupt IN
  EP 0x01 interrupt OUT

Interface 1: vendor-specific configuration
  EP 0x02 bulk OUT
  EP 0x82 bulk IN

Interface 2/3/4:
  Audio control / audio streaming related descriptors
```

工程含义：

```text
不能只模拟 HID。
至少要提供 interface 0 + interface 1。
audio interfaces 第一版可以先 descriptor stub，但最好保留真实 descriptor 轮廓。
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
新增 switch2pro device type：
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
2. 创建虚拟 switch2pro USB composite device
3. 自动 attach 到本机 usbip-win2
4. 扫描并连接 NS2 Pro BLE 手柄
5. 订阅 BLE input report 0x09
6. 将 BLE input 转为 USB HID interrupt IN
7. 将 Steam HID output 0x02 转为 BLE output report
8. 记录并响应 Steam bulk configuration commands
```

### 4.2 代码目录建议

```text
VIIPER fork
├─ device/
│  └─ switch2pro/
│     ├─ descriptor.go       # USB device/config/HID/audio descriptors
│     ├─ hid_report.go       # HID report descriptor / report constants
│     ├─ device.go           # HandleTransfer / HandleControl
│     ├─ bridge.go           # USB endpoint ↔ BLE channel
│     ├─ commands.go         # bulk config command parser/responder
│     ├─ rumble.go           # USB output 0x02 → BLE output 0x02
│     └─ neutral_report.go   # 空闲 input report 0x09
│
├─ internal/
│  └─ ns2ble/
│     ├─ scan.go             # 扫描 / 选择 / 重连
│     ├─ client.go           # BLE client 生命周期
│     ├─ gatt.go             # service/characteristic discovery
│     ├─ input.go            # input report 0x09 notification
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
bcdDevice: 0x0400
Manufacturer: Nintendo
Product: Switch 2 Pro Controller
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
  report ID 0x09
  total USB payload: [0x09][63-byte report payload]

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

### 5.4 Interface 2/3/4: audio stub

Pro Controller 2 descriptor 中包含 audio 相关 interfaces。第一版可以：

```text
1. 尽量复刻真实 descriptors
2. 不实现实际 audio streaming
3. EP0 对 audio class-specific requests 先记录
4. 如果 Windows 枚举失败，再补最小合法响应
```

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
USB/HID 侧需要 report ID。
```

因此输入桥接是：

```text
BLE notification:
  [63-byte payload]

USB HID input report:
  [0x09][63-byte payload]
```

### 6.2 BLE input 第一版不要完整解析

第一版不要做：

```text
BLE report → buttons/axes/gyro normalized state → 重新组包
```

而要做：

```text
BLE report → 补 report ID → 原样给 Steam
```

原因：

```text
1. Steam 已经有 Switch 2 Pro 原生 parser
2. 原始 report 能保留 GL/GR/C、NFC/headset state、motion data 等字段
3. 避免因重组 report 出错导致 Steam 识别异常
```

解析只用于 debug log，例如：

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

### 6.4 BLE input normalization 伪代码

```go
func NormalizePro2Input09(ble []byte) ([]byte, bool) {
    if len(ble) == 63 {
        usb := make([]byte, 64)
        usb[0] = 0x09
        copy(usb[1:], ble)
        return usb, true
    }

    // 容错：有些层可能已经带 report ID
    if len(ble) == 64 && ble[0] == 0x09 {
        usb := make([]byte, 64)
        copy(usb, ble)
        return usb, true
    }

    return nil, false
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
type Switch2ProDevice struct {
    desc *usb.Descriptor

    ble *ns2ble.Client

    latestInput atomic.Value // []byte, 64 bytes: [0x09][63-byte payload]

    bulkIn chan []byte
    log    Logger
}
```

### 9.2 HandleTransfer

```go
func (d *Switch2ProDevice) HandleTransfer(ep uint32, dir uint32, out []byte) []byte {
    switch {
    case ep == 1 && dir == usbip.DirIn:
        // Host polling HID interrupt IN 0x81.
        return d.getLatestOrNeutralInputReport()

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
func (d *Switch2ProDevice) HandleControl(
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
func (d *Switch2ProDevice) AttachBLE(c *ns2ble.Client) {
    go func() {
        for report := range c.InputReports {
            // report 已是 USB HID 格式：[0x09][payload]
            d.latestInput.Store(report)
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
5. input report 0x09 的空闲帧
```

### Phase 1: dummy virtual USB device

目标：不接 BLE，仅让 Steam 识别虚拟设备。

实现：

```text
1. fork VIIPER
2. 新增 device/switch2pro
3. 复刻 Pro Controller 2 USB descriptors
4. EP 0x81 返回 neutral input report 0x09
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

### Phase 3: BLE input

目标：真实手柄输入进入 Steam。

实现：

```text
1. Go BLE scan / connect
2. 找到 characteristic 7492866c-ec3e-4619-8258-32755ffcc0f8
3. Enable notifications
4. 收到 63-byte BLE payload 后补 0x09
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

### Phase 4: rumble bridge

目标：Steam rumble test 让真实 NS2 Pro 震动。

实现：

```text
1. 监听 EP 0x01 OUT
2. 识别 report 0x02
3. 转成 BLE output 0x02 payload
4. 写 cc483f51-9258-427d-a939-630c31f72b05
```

验收：

```text
Steam rumble test 时 BLE write 发生
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
Product string 是否 Switch 2 Pro Controller
configuration descriptor 是否 5 interfaces
interface 0 HID report descriptor 是否正确
interface 1 vendor bulk 是否存在
EP0 GET_DESCRIPTOR 是否完整响应
```

### Steam 识别设备但没有输入

检查：

```text
EP 0x81 是否被 host 轮询
返回 report 是否是 [0x09][63 bytes]
neutral report 长度是否 64
BLE payload 是否被错误地二次加 report ID
```

### 有输入但没有 GL/GR/C

检查：

```text
是否原样透传 report 0x09
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
BLE report 0x09 offset 0x0E motion length 是否为 30/40
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
2. BLE 真实输入能进 Steam
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
2. 做 switch2pro dummy USB device
3. 用 Steam 验证识别和 bulk/HID output 日志
4. 抓真实有线 NS2 Pro USBPcap
5. 做 bulk replay responder
6. 接入 Go BLE input 0x09
7. 接入 HID output 0x02 → BLE rumble
8. 补 feature select / gyro / calibration
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

> fork VIIPER，在 Go 中新增 `switch2pro` USB composite device；USB 侧模拟 `057E:2069` 的 Switch 2 Pro Controller，HID interface 负责 input/rumble，vendor bulk interface 负责 init/feature/gyro/calibration；BLE 侧订阅 NS2 Pro report 0x09 原样补 report ID 透传给 Steam，并把 Steam 的 output report 0x02 和 configuration commands 转回真实 BLE 手柄。
