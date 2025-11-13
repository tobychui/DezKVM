@echo off
:: Hold the left mouse button for 3 seconds
.\send.exe COM4 115200 0x02 0x02 0x01
timeout /t 3 /nobreak >nul
.\send.exe COM4 115200 0x02 0x03 0x01
timeout /t 3 /nobreak >nul
:: Do a right click
.\send.exe COM4 115200 0x02 0x01 0x02
timeout /t 3 /nobreak >nul
:: Do a middle click
.\send.exe COM4 115200 0x02 0x01 0x03
