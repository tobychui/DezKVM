@echo off
:: Press Delete twice with a delay of 3 seconds between each press
echo "Pressing Delete"
send.exe COM4 115200 0x01 0x07 0xD4

timeout /t 3 /nobreak >nul

echo "Releasing Delete"
send.exe COM4 115200 0x01 0x08 0xD4
