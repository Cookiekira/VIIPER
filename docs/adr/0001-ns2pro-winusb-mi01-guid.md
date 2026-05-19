# Expose NS2Pro MI_01 as WinUSB with DeviceInterfaceGUIDs

For the NS2Pro composite device, we bind interface `MI_01` to WinUSB and publish `DeviceInterfaceGUIDs` via Microsoft OS descriptors. We chose this because Steam/libusb-style user-space discovery on Windows depends on a discoverable interface GUID for the composite interface, and HID-only visibility was not enough for reliable setup behavior.
