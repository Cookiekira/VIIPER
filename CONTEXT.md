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

## Example dialogue

> **Dev:** "For this feature, should we add an `ns2pro` device?"
> **Domain expert:** "Use **NS2Pro** as the canonical type name."
>
> **Dev:** "Does the feeder talk to the bus directly?"
> **Domain expert:** "No, it opens a **Device Stream** for a specific **Virtual Device** on that bus."

## Flagged ambiguities

- "Pro Controller" appears as a product-facing USB name, but in VIIPER domain language the canonical **Device Type** is **NS2Pro**.
- "controller detected" can refer to HID presence only; NS2Pro setup compatibility in Windows also depends on discoverable **MI_01** WinUSB interface metadata.
