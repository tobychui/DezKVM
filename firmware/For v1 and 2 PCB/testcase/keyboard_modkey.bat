@echo off
:: Ctrl + Alt
echo "Holding Ctrl + Alt"
.\send.exe COM4 115200 0x01 0x04 0x05
timeout /t 3 /nobreak >nul
echo "Releasing Delete & Ctrl + Alt"
.\send.exe COM4 115200 0x01 0x04 0x00