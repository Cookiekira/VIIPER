# VIIPER

VIIPER is a virtual input domain that lets software present controllable USB input hardware to an operating system as if it were physically attached.

## Language

### Product surfaces

**VIIPER Server**:
The standalone runtime that manages virtual buses/devices and exposes control endpoints.
_Avoid_: daemon, backend

**libVIIPER**:
The embeddable library form of the same virtual input domain capabilities.
_Avoid_: SDK

### Device model

**Virtual Bus**:
A host container that owns a set of virtual devices.
_Avoid_: hub

**Virtual Device**:
An emulated USB input device instance attached to exactly one virtual bus.
_Avoid_: pad instance, fake controller

**Device Type**:
The declared device family that defines a virtual device's identity and wire behavior.
_Avoid_: profile, skin

**Feeder Client**:
An application that drives a virtual device by sending input and receiving feedback.
_Avoid_: bot

**Device Stream**:
The long-lived bidirectional channel between a feeder client and one virtual device.
_Avoid_: session socket

### Integration terms

**USBIP Attachment**:
The act of exposing a virtual device to an operating system through a USBIP client.
_Avoid_: mount

**Auto-Attach**:
Server behavior that tries to attach newly created devices to a local USBIP client automatically.
_Avoid_: auto connect

**Proxy Mode**:
A passthrough operating mode that observes USBIP traffic between external endpoints.
_Avoid_: bridge mode

**MI_01**:
The NS2Pro vendor interface slot that Windows tooling targets for user-mode controller setup traffic.
_Avoid_: random second interface

**WinUSB Interface**:
An interface bound to the Windows WinUSB stack for generic user-mode access.
_Avoid_: HID interface

**DeviceInterfaceGUIDs**:
The Microsoft OS Extended Properties value used so Windows and user-space stacks can discover a composite interface reliably.
_Avoid_: optional metadata

**NS2Pro**:
The canonical VIIPER device type name for Nintendo Switch 2 Pro emulation.
_Avoid_: pro controller

**Steam-facing USB Report `0x05`**:
The canonical HID IN frame emitted by **NS2Pro** to Steam: report ID `0x05` plus a 63-byte payload, for a 64-byte USB HID input report.
_Avoid_: USB report `0x09`, raw BLE passthrough

**BLE Input Report `0x09`**:
The canonical Switch 2 Pro Controller BLE input notification payload used as the real-controller source for the current **NS2Pro** bridge. It arrives through a GATT input characteristic and does not include a HID report ID byte.
_Avoid_: USB HID report ID, Steam-facing report

**BLE Command Transport Byte**:
The third byte in Switch 2 command packets that identifies transport framing: `0x01` for Bluetooth/BLE, `0x00` for USB.
_Avoid_: copying USB command bytes directly into BLE writes

**GATT Attribute Handle**:
The BLE attribute table handle used to select a characteristic or descriptor, such as input notification, command write, or command response notification.
_Avoid_: HID report ID, USB endpoint

**NS2Pro Sensor Bridge**:
The opt-in path that copies real-controller BLE IMU/motion data into the Steam-facing USB report `0x05` sensor offsets.
_Avoid_: default input path, calibrated gyro implementation

## Relationships

- A **VIIPER Server** hosts zero or more **Virtual Buses**.
- A **Virtual Bus** contains zero or more **Virtual Devices**.
- Each **Virtual Device** has exactly one **Device Type**.
- A **Feeder Client** controls a **Virtual Device** through a **Device Stream**.
- **USBIP Attachment** makes a **Virtual Device** visible to the operating system.
- **Auto-Attach** is an optional policy applied by **VIIPER Server** at device creation time.
- **Proxy Mode** does not create **Virtual Devices**; it observes existing USBIP traffic paths.
- **NS2Pro** is a **Device Type**.
- **NS2Pro** exposes **MI_01** as a **WinUSB Interface**.
- **DeviceInterfaceGUIDs** is required for stable discovery of NS2Pro **MI_01** on Windows.
- **Steam-facing USB Report `0x05`** is the only current Steam-visible HID IN path for **NS2Pro**.
- **BLE Input Report `0x09`** is parsed into controller state and rebuilt as **Steam-facing USB Report `0x05`**.
- **GATT Attribute Handles** choose BLE characteristics/descriptors; they are not USB endpoints and are not HID report IDs.
- **BLE Command Transport Byte** must be `0x01` for BLE command characteristic writes even when the equivalent USB command uses `0x00`.
- **NS2Pro Sensor Bridge** is experimental and opt-in; the normal **NS2Pro** input path remains buttons/sticks/rumble/LED.

## Example dialogue

> **Dev:** "For this feature, should we add an `ns2pro` device?"
> **Domain expert:** "Use **NS2Pro** as the canonical type name."
>
> **Dev:** "Does the feeder talk to the bus directly?"
> **Domain expert:** "No, it opens a **Device Stream** for a specific **Virtual Device** on that bus."

## Flagged ambiguities

- "Pro Controller" appears as a product-facing USB name, but in VIIPER domain language the canonical **Device Type** is **NS2Pro**.
- "controller detected" can refer to HID presence only; NS2Pro setup compatibility in Windows also depends on discoverable **MI_01** WinUSB interface metadata.
- "report `0x09`" is ambiguous: in BLE bridge work it usually means **BLE Input Report `0x09`**, while Steam currently consumes **Steam-facing USB Report `0x05`**.
- "handle `0x000E`" is a **GATT Attribute Handle** for a BLE characteristic, not a report ID and not a USB endpoint.
- "gyro support" is ambiguous: **NS2Pro Sensor Bridge** can move raw/compact motion data into Steam-visible sensor offsets, but calibrated gyro behavior still needs real-game/Steam validation.
