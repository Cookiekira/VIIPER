NS2 Pro / Switch 2 Pro USB capture

Main capture:
  C:\Users\oddc2\Repo\VIIPER\captures\ns2pro\ns2pro-usb-20260519-005307\ns2pro-usb-20260519-005307.pcapng

Useful Wireshark display filters:
  usb.idVendor == 0x057e || usb.idProduct == 0x2069
  usb.device_address == <addr> && usb.setup.bRequest == 6
  usb.device_address == <addr> && (usb.endpoint_address == 0x02 || usb.endpoint_address == 0x82)
  usb.device_address == <addr> && (usb.endpoint_address == 0x81 || usb.endpoint_address == 0x01)

What VIIPER needs from this capture:
  - EP0 descriptors from the real wired controller
  - HID report descriptor
  - Steam init/config traffic on bulk OUT 0x02 and bulk IN 0x82
  - Idle input report 0x09 on interrupt IN 0x81
  - Rumble report 0x02 on interrupt OUT 0x01
  - Gyro/IMU feature and calibration commands on bulk endpoints
