@echo off

.\send.exe COM3 115200 0xFF
timeout /t 1 /nobreak >nul
:: Hold the left mouse button for 3 seconds
.\send.exe COM3 115200 0x02 0x05 0x01
timeout /t 3 /nobreak >nul
.\send.exe COM3 115200 0x02 0x06 0x01
timeout /t 3 /nobreak >nul
:: Do a right click
.\send.exe COM3 115200 0x02 0x05 0x02 0x02 0x06 0x02
timeout /t 3 /nobreak >nul
:: Do a middle click
.\send.exe COM3 115200 0x02 0x05 0x03 0x02 0x06 0x00
