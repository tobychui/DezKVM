@echo off
.\send.exe COM3 115200 0xFF
timeout /t 1 /nobreak >nul

:: Scroll up (Tested on Windows)
echo "Scrolling Up"
.\send.exe COM3 115200 0x02 0x07 0x08
timeout /t 3 /nobreak >nul
:: Scroll down
echo "Scrolling down"
.\send.exe COM3 115200 0x02 0x08 0x08
