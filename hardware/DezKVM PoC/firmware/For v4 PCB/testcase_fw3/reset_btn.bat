@echo off


echo "Pressing Power Button"
.\send.exe COM3 115200 0x58 0xEE 0x02
timeout /t 3 /nobreak >nul

.\send.exe COM3 115200 0x58 0xEE 0x03
timeout /t 3 /nobreak >nul
