@echo off
:: Hold Shift via HID keys
.\send.exe COM3 115200 0xFF

echo "Holding Ctrl"
.\send.exe COM3 115200 0x02 0x03 0x01
timeout /t 1 /nobreak >nul

echo "Releasing Ctrl"
.\send.exe COM3 115200 0x02 0x04 0x01
timeout /t 1 /nobreak >nul


echo "Pressing Win key"
.\send.exe COM3 115200 0x02 0x03 0x04 0x02 0x04 0x04
timeout /t 1 /nobreak >nul


echo "Pressing Shift + A"
.\send.exe COM3 115200 0x02 0x03 0x02 0x02 0x01 0x41 
timeout /t 1 /nobreak >nul
.\send.exe COM3 115200 0x02 0x02 0x41 0x02 0x04 0x02

echo "Testing combo modkeys"
.\send.exe COM3 115200 0x02 0x03 0x09 
timeout /t 1 /nobreak >nul
.\send.exe COM3 115200 0x02 0x04 0x01
timeout /t 1 /nobreak >nul
.\send.exe COM3 115200 0x02 0x04 0x03