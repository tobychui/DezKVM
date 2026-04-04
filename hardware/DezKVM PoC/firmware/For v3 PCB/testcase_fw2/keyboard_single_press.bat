@echo off

.\send.exe COM3 115200 0xFF
timeout /t 1 /nobreak >nul

echo "Pressing A"
.\send.exe COM3 115200 0x02 0x01 0x41
timeout /t 1 /nobreak >nul

echo "Releasing A"
.\send.exe COM3 115200 0x02 0x02 0x41
