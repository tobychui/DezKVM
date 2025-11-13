@echo off
:: Move cursor to the top left corner
.\send.exe COM4 115200 0x02 0x04 0x00
timeout /t 3 /nobreak >nul

:: Move cursor to the bottom left corner
.\send.exe COM4 115200 0x02 0x04 0x01
timeout /t 3 /nobreak >nul

:: Move cursor to the top left corner
.\send.exe COM4 115200 0x02 0x04 0x02
timeout /t 3 /nobreak >nul

:: Move cursor to the bottom right corner
.\send.exe COM4 115200 0x02 0x04 0x03
