@echo off
:: Press CAPS LOCK twice with a delay of 3 seconds between each press
echo "Pressing CAPS LOCK"
send.exe COM3 115200 0x01 0x07 0xC1 0x01 0x08 0xC1

timeout /t 3 /nobreak >nul

echo "Pressing CAPS LOCK again"
send.exe COM3 115200 0x01 0x07 0xC1 0x01 0x08 0xC1
