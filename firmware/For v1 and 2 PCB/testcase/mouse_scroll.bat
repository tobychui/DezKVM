@echo off
:: Scroll up (Tested on Windows)
.\send.exe COM4 115200 0x04 0x00 0x02
timeout /t 3 /nobreak >nul
:: Scroll down
.\send.exe COM4 115200 0x04 0x01 0x02
