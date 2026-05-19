# 1) 清掉同 VID/PID/REV 的 Microsoft OS descriptor 缓存
Remove-Item -LiteralPath 'HKLM:\SYSTEM\CurrentControlSet\Control\usbflags\057E20690101' -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath 'HKLM:\SYSTEM\CurrentControlSet\Control\usbflags\057E20690200' -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath 'HKLM:\SYSTEM\CurrentControlSet\Control\usbflags\057E20690400' -Recurse -Force -ErrorAction SilentlyContinue

# 2) 删除所有 057E:2069 设备实例，真机重插后会自动重建
Get-PnpDevice | Where-Object { $_.InstanceId -match 'VID_057E&PID_2069' } |
  ForEach-Object { pnputil /remove-device "$($_.InstanceId)" }