# /// script
# dependencies = [
#   "bleak",
# ]
# ///
"""
Bridge a real Switch 2 Pro Controller over Bluetooth LE into a VIIPER virtual
USB Switch 2 Pro Controller.

Usage:
    uv run examples/python/virtual_ns2pro_ble_bridge.py
    uv run examples/python/virtual_ns2pro_ble_bridge.py localhost:3242

Start the VIIPER server first. The script creates the virtual USB controller
before scanning for Bluetooth so clients such as Steam can classify it from the
NS2Pro USB descriptors immediately.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import logging
import platform
import socket
import struct
from dataclasses import dataclass
from typing import Any

from bleak import BleakClient, BleakScanner


LOG = logging.getLogger("virtual_ns2pro_ble_bridge")

NINTENDO_MANUFACTURER_ID = 0x0553
NINTENDO_VID = 0x057E
NS2PRO_PID = 0x2069

INPUT_WIRE_SIZE = 27
OUTPUT_WIRE_SIZE = 32

STICK_MIN = 0x0000
STICK_CENTER = 0x0800
STICK_MAX = 0x0FFF
BATTERY_MAX = 9

BTN_B = 1 << 0
BTN_A = 1 << 1
BTN_Y = 1 << 2
BTN_X = 1 << 3
BTN_R = 1 << 4
BTN_ZR = 1 << 5
BTN_PLUS = 1 << 6
BTN_RSTICK = 1 << 7
BTN_DOWN = 1 << 8
BTN_RIGHT = 1 << 9
BTN_LEFT = 1 << 10
BTN_UP = 1 << 11
BTN_L = 1 << 12
BTN_ZL = 1 << 13
BTN_MINUS = 1 << 14
BTN_LSTICK = 1 << 15
BTN_HOME = 1 << 16
BTN_CAPTURE = 1 << 17
BTN_GR = 1 << 18
BTN_GL = 1 << 19
BTN_C = 1 << 20
BTN_HEADSET = 1 << 21

FEATURE_BUTTONS = 0x01
FEATURE_STICKS = 0x02
FEATURE_IMU = 0x04
DEFAULT_FEATURE_FLAGS = FEATURE_BUTTONS | FEATURE_STICKS | FEATURE_IMU
DEFAULT_LED_PATTERN = 0b0110

# Bleak on Windows addresses GATT attributes one lower than the documented
# handles used in switch2_input_viewer.py.
BLE_INIT_WRITE = 0x0005 - 1
BLE_INPUT_COMMON = 0x000A - 1
BLE_INPUT_COMMON_REPORT_RATE = BLE_INPUT_COMMON + 3
BLE_VIBRATION = 0x0012 - 1
BLE_COMMAND = 0x0014 - 1
BLE_COMMAND_RESPONSE = 0x001A - 1


class ViiperAPIError(RuntimeError):
    def __init__(self, problem: dict[str, Any]):
        self.problem = problem
        super().__init__(
            f"VIIPER API error {problem.get('status', 0)}: "
            f"{problem.get('title', 'unknown error')} {problem.get('detail', '')}"
        )


def parse_addr(addr: str) -> tuple[str, int]:
    host, sep, port = addr.rpartition(":")
    if not sep or not host:
        raise ValueError(f"invalid API address {addr!r}; expected host:port")
    return host, int(port)


def viiper_request(addr: tuple[str, int], path: str, payload: Any = None) -> dict[str, Any]:
    if isinstance(payload, (dict, list)):
        payload_bytes = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    elif isinstance(payload, bytes):
        payload_bytes = payload
    elif payload is None:
        payload_bytes = b""
    else:
        payload_bytes = str(payload).encode("utf-8")

    request = path.encode("utf-8")
    if payload_bytes:
        request += b" " + payload_bytes
    request += b"\0"

    with socket.create_connection(addr, timeout=5.0) as sock:
        sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        sock.sendall(request)
        chunks = []
        while True:
            chunk = sock.recv(4096)
            if not chunk:
                break
            chunks.append(chunk)

    data = b"".join(chunks).strip()
    if not data:
        return {}

    result = json.loads(data.decode("utf-8"))
    if isinstance(result, dict) and (result.get("status", 0) or result.get("title")):
        raise ViiperAPIError(result)
    return result


def open_viiper_stream(addr: tuple[str, int], bus_id: int, dev_id: str) -> socket.socket:
    sock = socket.create_connection(addr, timeout=5.0)
    sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
    sock.sendall(f"bus/{bus_id}/{dev_id}\0".encode("utf-8"))
    sock.setblocking(False)
    return sock


def find_or_create_bus(addr: tuple[str, int]) -> tuple[int, bool]:
    buses = viiper_request(addr, "bus/list").get("buses", [])
    if buses:
        bus_id = min(int(b) for b in buses)
        LOG.info("Using existing VIIPER bus %d", bus_id)
        return bus_id, False

    resp = viiper_request(addr, "bus/create", "0")
    bus_id = int(resp["busId"])
    LOG.info("Created VIIPER bus %d", bus_id)
    return bus_id, True


@dataclass
class NS2ProInput:
    buttons: int = 0
    lx: int = STICK_CENTER
    ly: int = STICK_CENTER
    rx: int = STICK_CENTER
    ry: int = STICK_CENTER
    accel_x: int = 0
    accel_y: int = 0
    accel_z: int = 0
    gyro_x: int = 0
    gyro_y: int = 0
    gyro_z: int = 0
    battery_level: int = BATTERY_MAX
    charging: bool = False
    external_power: bool = True


def clamp(value: int, lo: int, hi: int) -> int:
    return max(lo, min(hi, value))


def pack_input_state(state: NS2ProInput) -> bytes:
    return struct.pack(
        "<IHHHHhhhhhhBBB",
        state.buttons & 0xFFFFFFFF,
        clamp(state.lx, STICK_MIN, STICK_MAX),
        clamp(state.ly, STICK_MIN, STICK_MAX),
        clamp(state.rx, STICK_MIN, STICK_MAX),
        clamp(state.ry, STICK_MIN, STICK_MAX),
        clamp(state.accel_x, -32768, 32767),
        clamp(state.accel_y, -32768, 32767),
        clamp(state.accel_z, -32768, 32767),
        clamp(state.gyro_x, -32768, 32767),
        clamp(state.gyro_y, -32768, 32767),
        clamp(state.gyro_z, -32768, 32767),
        clamp(state.battery_level, 0, BATTERY_MAX),
        1 if state.charging else 0,
        1 if state.external_power else 0,
    )


def unpack_stick12(data: bytes | bytearray | memoryview) -> tuple[int, int]:
    if len(data) < 3:
        raise ValueError("12-bit stick data requires 3 bytes")
    x = data[0] | ((data[1] & 0x0F) << 8)
    y = (data[1] >> 4) | (data[2] << 4)
    return x, y


def map_common_buttons(raw: bytes | bytearray | memoryview) -> int:
    if len(raw) < 4:
        raise ValueError("common button data requires 4 bytes")

    buttons = 0
    if raw[0] & 0x01:
        buttons |= BTN_Y
    if raw[0] & 0x02:
        buttons |= BTN_X
    if raw[0] & 0x04:
        buttons |= BTN_B
    if raw[0] & 0x08:
        buttons |= BTN_A
    if raw[0] & 0x40:
        buttons |= BTN_R
    if raw[0] & 0x80:
        buttons |= BTN_ZR

    if raw[1] & 0x01:
        buttons |= BTN_MINUS
    if raw[1] & 0x02:
        buttons |= BTN_PLUS
    if raw[1] & 0x04:
        buttons |= BTN_RSTICK
    if raw[1] & 0x08:
        buttons |= BTN_LSTICK
    if raw[1] & 0x10:
        buttons |= BTN_HOME
    if raw[1] & 0x20:
        buttons |= BTN_CAPTURE
    if raw[1] & 0x40:
        buttons |= BTN_C

    if raw[2] & 0x01:
        buttons |= BTN_DOWN
    if raw[2] & 0x02:
        buttons |= BTN_UP
    if raw[2] & 0x04:
        buttons |= BTN_RIGHT
    if raw[2] & 0x08:
        buttons |= BTN_LEFT
    if raw[2] & 0x40:
        buttons |= BTN_L
    if raw[2] & 0x80:
        buttons |= BTN_ZL

    if raw[3] & 0x01:
        buttons |= BTN_GR
    if raw[3] & 0x02:
        buttons |= BTN_GL
    if raw[3] & 0x10:
        buttons |= BTN_HEADSET

    return buttons


def parse_common_report(data: bytes | bytearray) -> NS2ProInput | None:
    if len(data) < 0x10:
        return None

    buttons = map_common_buttons(data[0x04:0x08])
    lx, ly = unpack_stick12(data[0x0A:0x0D])
    rx, ry = unpack_stick12(data[0x0D:0x10])

    voltage = struct.unpack_from("<H", data, 0x1F)[0] if len(data) >= 0x21 else 3800
    charging_state = data[0x21] if len(data) > 0x21 else 0x20
    battery_level = BATTERY_MAX
    if voltage:
        battery_level = clamp((voltage - 3200) * BATTERY_MAX // 800, 0, BATTERY_MAX)

    state = NS2ProInput(
        buttons=buttons,
        lx=lx,
        ly=ly,
        rx=rx,
        ry=ry,
        battery_level=battery_level,
        charging=charging_state == 0x34,
        external_power=voltage > 0,
    )

    if len(data) >= 0x3C:
        state.accel_x = struct.unpack_from("<h", data, 0x30)[0]
        state.accel_y = struct.unpack_from("<h", data, 0x32)[0]
        state.accel_z = struct.unpack_from("<h", data, 0x34)[0]
        state.gyro_x = struct.unpack_from("<h", data, 0x36)[0]
        state.gyro_y = struct.unpack_from("<h", data, 0x38)[0]
        state.gyro_z = struct.unpack_from("<h", data, 0x3A)[0]

    return state


class BLEController:
    def __init__(self, feature_flags: int):
        self.feature_flags = feature_flags
        self.client: BleakClient | None = None
        self.connected = False
        self.latest_input: NS2ProInput | None = None
        self.command_event = asyncio.Event()
        self.command_response = b""
        self.device_info: dict[str, Any] = {}

    @property
    def is_connected(self) -> bool:
        return bool(self.connected and self.client and self.client.is_connected)

    async def scan(self):
        LOG.info("Scanning for an advertising Switch 2 Pro Controller...")
        found = None
        stop_event = asyncio.Event()

        def on_advertisement(device, adv_data):
            nonlocal found
            manu = adv_data.manufacturer_data.get(NINTENDO_MANUFACTURER_ID)
            if not manu or len(manu) < 13:
                return
            vid = struct.unpack("<H", manu[3:5])[0]
            pid = struct.unpack("<H", manu[5:7])[0]
            is_standard_advertisement = manu[12] == 0
            if vid == NINTENDO_VID and pid == NS2PRO_PID and is_standard_advertisement:
                found = device
                LOG.info("Found NS2Pro BLE device %s (%s)", device.address, device.name)
                stop_event.set()

        async with BleakScanner(on_advertisement):
            await stop_event.wait()
        return found

    async def connect(self, device) -> None:
        def on_disconnect(_client):
            self.connected = False
            LOG.warning("BLE controller disconnected")

        LOG.info("Connecting to BLE controller %s...", device.address)
        self.client = BleakClient(device, disconnected_callback=on_disconnect)
        await self.client.connect()
        self.connected = True
        LOG.info("Connected to BLE controller")

    async def initialize(self) -> None:
        if self.client is None:
            raise RuntimeError("BLE client is not connected")

        LOG.info("Initializing BLE controller over basic command channel")
        try:
            self.device_info["remote_name"] = bytes(await self.client.read_gatt_char("2a00"))
        except Exception as exc:
            LOG.debug("Could not read BLE device name: %s", exc)

        await self.client.write_gatt_char(BLE_INIT_WRITE, b"\x01\x00", response=True)
        await self.client.start_notify(BLE_COMMAND_RESPONSE, self._on_command_response)

        await self.read_spi_memory(0x13000, 0x40)
        await self.play_vibration_sample(0x03)
        await self.set_player_leds(DEFAULT_LED_PATTERN)
        await self.configure_features(0xFF)

        await self.read_spi_memory(0x13080, 0x40)
        await self.read_spi_memory(0x130C0, 0x40)
        await self.read_spi_memory(0x1FC040, 0x40)
        await self.read_spi_memory(0x13040, 0x10)
        await self.read_spi_memory(0x13100, 0x18)
        await self.read_spi_memory(0x1FA000, 0x40)
        await self.enable_features(self.feature_flags)
        await self.get_version_info()

        try:
            await self.client.write_gatt_descriptor(BLE_INPUT_COMMON_REPORT_RATE, b"\x85\x00")
        except Exception as exc:
            LOG.debug("Could not set BLE report-rate descriptor: %s", exc)

        await self._try_windows_throughput_params()
        await self.client.start_notify(BLE_INPUT_COMMON, self._on_input_report)
        LOG.info("BLE input notifications are active")

    async def disconnect(self) -> None:
        if self.client and self.client.is_connected:
            try:
                await self.client.disconnect()
            except Exception as exc:
                LOG.debug("BLE disconnect failed: %s", exc)
        self.connected = False

    async def send_rumble(self, left: bytes, right: bytes) -> None:
        if not self.is_connected or self.client is None:
            return
        packet = b"\x00" + left[:16].ljust(16, b"\x00") + right[:16].ljust(16, b"\x00") + b"\x00" * 9
        try:
            await self.client.write_gatt_char(BLE_VIBRATION, packet, response=False)
        except Exception as exc:
            LOG.debug("BLE rumble write failed: %s", exc)

    async def send_command(self, command: bytes) -> bytes:
        if self.client is None:
            raise RuntimeError("BLE client is not connected")
        self.command_response = b""
        self.command_event.clear()
        await self.client.write_gatt_char(BLE_COMMAND, command, response=False)
        try:
            await asyncio.wait_for(self.command_event.wait(), timeout=2.0)
        except asyncio.TimeoutError:
            LOG.warning("Command timeout: %s", command.hex())
        return self.command_response

    async def read_spi_memory(self, address: int, size: int) -> bytes:
        command = bytes(
            [
                0x02,
                0x91,
                0x01,
                0x04,
                0x00,
                0x08,
                0x00,
                0x00,
                size,
                0x7E,
                0x00,
                0x00,
                address & 0xFF,
                (address >> 8) & 0xFF,
                (address >> 16) & 0xFF,
                (address >> 24) & 0xFF,
            ]
        )
        response = await self.send_command(command)
        self._parse_spi_response(address, response)
        if len(response) < 16:
            return b""
        data_len = response[8]
        return response[0x10 : 0x10 + data_len]

    async def play_vibration_sample(self, index: int) -> None:
        await self.send_command(bytes([0x0A, 0x91, 0x01, 0x02, 0x00, 0x04, 0x00, 0x00, index, 0, 0, 0]))

    async def set_player_leds(self, led_mask: int) -> None:
        await self.send_command(
            bytes([0x09, 0x91, 0x01, 0x07, 0x00, 0x08, 0x00, 0x00, led_mask, 0, 0, 0, 0, 0, 0, 0])
        )

    async def configure_features(self, flags: int) -> None:
        await self.send_command(bytes([0x0C, 0x91, 0x01, 0x02, 0x00, 0x04, 0x00, 0x00, flags, 0, 0, 0]))

    async def enable_features(self, flags: int) -> None:
        await self.send_command(bytes([0x0C, 0x91, 0x01, 0x04, 0x00, 0x04, 0x00, 0x00, flags, 0, 0, 0]))

    async def get_version_info(self) -> None:
        response = await self.send_command(bytes([0x10, 0x91, 0x01, 0x01, 0, 0, 0, 0]))
        if len(response) < 20:
            return
        data = response[8:20]
        prefixes = ["OJL", "OJR", "OFK", "LG"]
        if data[3] < len(prefixes):
            version = f"{prefixes[data[3]]}.{data[0]:02d}.{data[1]:02d}.{data[2]:02d}"
            self.device_info["firmware_version"] = version
            LOG.info("Controller firmware %s", version)

    def _on_command_response(self, _sender, data: bytearray) -> None:
        self.command_response = bytes(data)
        self.command_event.set()

    def _on_input_report(self, _sender, data: bytearray) -> None:
        parsed = parse_common_report(data)
        if parsed is not None:
            self.latest_input = parsed

    def _parse_spi_response(self, address: int, response: bytes) -> None:
        if len(response) < 16:
            return
        data_len = response[8]
        data = response[0x10 : 0x10 + data_len]
        if address == 0x13000 and len(data) >= 0x16:
            serial = data[0x02:0x12].rstrip(b"\0")
            vid = struct.unpack("<H", data[0x12:0x14])[0]
            pid = struct.unpack("<H", data[0x14:0x16])[0]
            self.device_info.update(serial=serial, vendor_id=vid, product_id=pid)
            LOG.info("Controller SPI identity VID=%04X PID=%04X serial=%s", vid, pid, serial.decode(errors="replace"))
        elif address == 0x1FA000 and len(data) >= 0x3A:
            self.device_info["host_address1"] = data[0x08:0x0E]
            self.device_info["ltk"] = data[0x1A:0x2A][::-1]

    async def _try_windows_throughput_params(self) -> None:
        if platform.system() != "Windows" or self.client is None:
            return
        try:
            build_number = int(platform.version().split(".")[-1])
            if build_number < 22000:
                return
            from bleak.backends.winrt.client import BleakClientWinRT
            from winrt.windows.devices.bluetooth import BluetoothLEPreferredConnectionParameters

            backend = self.client._backend
            if isinstance(backend, BleakClientWinRT):
                backend._requester.request_preferred_connection_parameters(
                    BluetoothLEPreferredConnectionParameters.throughput_optimized
                )
                LOG.debug("Requested Windows BLE throughput-optimized parameters")
        except Exception as exc:
            LOG.debug("Could not request Windows BLE throughput parameters: %s", exc)


class Bridge:
    def __init__(self, api_addr: str, feature_flags: int):
        self.addr = parse_addr(api_addr)
        self.controller = BLEController(feature_flags)
        self.bus_id = 0
        self.dev_id = ""
        self.created_bus = False
        self.stream: socket.socket | None = None
        self.running = True
        self.loop: asyncio.AbstractEventLoop | None = None
        self.sent_frames = 0

    async def run(self) -> None:
        self.loop = asyncio.get_running_loop()
        idle_task = None
        rumble_task = None
        try:
            await self.setup_viiper()
            idle_task = asyncio.create_task(self.input_loop())
            rumble_task = asyncio.create_task(self.rumble_loop())
            await self.ble_reconnect_loop()
        finally:
            self.running = False
            for task in (idle_task, rumble_task):
                if task:
                    task.cancel()
                    try:
                        await task
                    except asyncio.CancelledError:
                        pass
            await self.shutdown()

    async def setup_viiper(self) -> None:
        ping = viiper_request(self.addr, "ping")
        LOG.info("Connected to VIIPER %s %s", ping.get("server", "server"), ping.get("version", ""))
        self.bus_id, self.created_bus = find_or_create_bus(self.addr)
        device = viiper_request(self.addr, f"bus/{self.bus_id}/add", {"type": "ns2pro"})
        self.dev_id = str(device["devId"])
        self.stream = open_viiper_stream(self.addr, self.bus_id, self.dev_id)
        LOG.info(
            "Virtual USB NS2Pro is active on bus=%d dev=%s; connect Steam before/while BLE scan runs",
            self.bus_id,
            self.dev_id,
        )

    async def ble_reconnect_loop(self) -> None:
        while self.running:
            try:
                device = await self.controller.scan()
                await self.controller.connect(device)
                await self.controller.initialize()
                while self.running and self.controller.is_connected:
                    await asyncio.sleep(0.5)
            except asyncio.CancelledError:
                raise
            except Exception as exc:
                LOG.error("BLE connection/init failed: %s", exc)
            finally:
                await self.controller.disconnect()
            if self.running:
                LOG.info("Resuming idle USB input and retrying BLE in 3 seconds")
                await asyncio.sleep(3.0)

    async def input_loop(self) -> None:
        idle = NS2ProInput()
        while self.running:
            state = self.controller.latest_input if self.controller.is_connected and self.controller.latest_input else idle
            await self.send_to_viiper(pack_input_state(state))
            self.sent_frames += 1
            if self.sent_frames % 300 == 0:
                LOG.info(
                    "Sent frame %d source=%s buttons=0x%06X lx=%04X ly=%04X",
                    self.sent_frames,
                    "ble" if state is not idle else "idle",
                    state.buttons,
                    state.lx,
                    state.ly,
                )
            await asyncio.sleep(0.016)

    async def rumble_loop(self) -> None:
        buf = b""
        while self.running:
            if not self.stream or self.loop is None:
                await asyncio.sleep(0.05)
                continue
            try:
                data = await self.loop.sock_recv(self.stream, 4096)
                if not data:
                    LOG.warning("VIIPER stream closed")
                    self.running = False
                    return
                buf += data
                while len(buf) >= OUTPUT_WIRE_SIZE:
                    packet, buf = buf[:OUTPUT_WIRE_SIZE], buf[OUTPUT_WIRE_SIZE:]
                    left, right = packet[:16], packet[16:32]
                    if any(left) or any(right):
                        await self.controller.send_rumble(left, right)
            except asyncio.CancelledError:
                raise
            except BlockingIOError:
                await asyncio.sleep(0.01)
            except OSError as exc:
                if self.running:
                    LOG.warning("VIIPER stream read failed: %s", exc)
                await asyncio.sleep(0.1)

    async def send_to_viiper(self, data: bytes) -> None:
        if not self.stream or self.loop is None:
            return
        try:
            await self.loop.sock_sendall(self.stream, data)
        except OSError as exc:
            LOG.error("VIIPER stream write failed: %s", exc)
            self.running = False

    async def shutdown(self) -> None:
        LOG.info("Shutting down")
        await self.controller.disconnect()
        if self.stream:
            try:
                self.stream.close()
            except OSError:
                pass
            self.stream = None
        if self.dev_id:
            try:
                viiper_request(self.addr, f"bus/{self.bus_id}/remove", self.dev_id)
                LOG.info("Removed virtual device %d-%s", self.bus_id, self.dev_id)
            except Exception as exc:
                LOG.debug("Virtual device removal failed: %s", exc)
        if self.created_bus:
            try:
                viiper_request(self.addr, "bus/remove", str(self.bus_id))
                LOG.info("Removed VIIPER bus %d", self.bus_id)
            except Exception as exc:
                LOG.debug("Bus removal failed: %s", exc)


def run_self_test() -> None:
    packed = pack_input_state(NS2ProInput(buttons=BTN_A, lx=1, ly=2, rx=0x0FFE, ry=0x0FFF, accel_x=-1))
    assert len(packed) == INPUT_WIRE_SIZE
    assert struct.unpack_from("<I", packed, 0)[0] == BTN_A
    assert struct.unpack_from("<H", packed, 4)[0] == 1
    assert struct.unpack_from("<h", packed, 12)[0] == -1

    assert unpack_stick12(bytes([0x23, 0x61, 0x45])) == (0x123, 0x456)

    assert map_common_buttons(bytes([0xCC, 0x7F, 0xCF, 0x13])) == (
        BTN_A
        | BTN_B
        | BTN_R
        | BTN_ZR
        | BTN_MINUS
        | BTN_PLUS
        | BTN_LSTICK
        | BTN_RSTICK
        | BTN_HOME
        | BTN_CAPTURE
        | BTN_C
        | BTN_DOWN
        | BTN_RIGHT
        | BTN_LEFT
        | BTN_UP
        | BTN_L
        | BTN_ZL
        | BTN_GR
        | BTN_GL
        | BTN_HEADSET
    )

    report = bytearray(0x3F)
    report[0x04:0x08] = bytes([0x08, 0x10, 0x04, 0x00])
    report[0x0A:0x0D] = bytes([0x23, 0x61, 0x45])
    report[0x0D:0x10] = bytes([0x89, 0xC7, 0xAB])
    struct.pack_into("<H", report, 0x1F, 3800)
    report[0x21] = 0x34
    struct.pack_into("<hhhhhh", report, 0x30, 1, 2, 3, 4, 5, 6)
    parsed = parse_common_report(report)
    assert parsed is not None
    assert parsed.buttons == (BTN_A | BTN_HOME | BTN_RIGHT)
    assert (parsed.lx, parsed.ly, parsed.rx, parsed.ry) == (0x123, 0x456, 0x789, 0xABC)
    assert (parsed.accel_x, parsed.accel_y, parsed.accel_z) == (1, 2, 3)
    assert (parsed.gyro_x, parsed.gyro_y, parsed.gyro_z) == (4, 5, 6)
    assert parsed.charging
    print("self-test passed")


def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Bridge NS2Pro BLE input to a VIIPER virtual USB NS2Pro.")
    parser.add_argument("api_addr", nargs="?", default="localhost:3242", help="VIIPER API address, default localhost:3242")
    parser.add_argument("--log-level", default="info", choices=["debug", "info", "warning", "error"])
    parser.add_argument(
        "--feature-flags",
        default=f"0x{DEFAULT_FEATURE_FLAGS:02x}",
        help="BLE feature flags to enable after init, default 0x07 (buttons, sticks, IMU)",
    )
    parser.add_argument("--self-test", action="store_true", help="Run parser/packer checks and exit")
    return parser


async def async_main(args: argparse.Namespace) -> None:
    feature_flags = int(str(args.feature_flags), 0) & 0xFF
    bridge = Bridge(args.api_addr, feature_flags)
    await bridge.run()


def main() -> None:
    parser = build_arg_parser()
    args = parser.parse_args()
    logging.basicConfig(
        level=getattr(logging, args.log_level.upper()),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        datefmt="%H:%M:%S",
    )
    if args.self_test:
        run_self_test()
        return
    try:
        asyncio.run(async_main(args))
    except KeyboardInterrupt:
        LOG.info("Interrupted")


if __name__ == "__main__":
    main()
