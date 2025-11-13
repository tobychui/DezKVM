@echo off


echo "Pressing Power Button"
.\send.exe COM3 115200 0x58 0xEE 0x00
timeout /t 3 /nobreak >nul

.\send.exe COM3 115200 0x58 0xEE 0x01
timeout /t 3 /nobreak >nul
