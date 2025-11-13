@echo off

.\send.exe COM3 115200 0xFF
timeout /t 1 /nobreak >nul

:: Move cursor to the top left with 10% padding
.\send.exe COM3 115200 0x05 0x09 0x9A 0x01 0x9A 0x01
timeout /t 1 /nobreak >nul

:: Move cursor to the bottom left corner
.\send.exe COM3 115200 0x05 0x09 0x9A 0x01 0x5D 0x0E
timeout /t 1 /nobreak >nul

:: Move cursor to the bottom right corner
.\send.exe COM3 115200 0x05 0x09 0x5D 0x0E 0x5D 0x0E
timeout /t 1 /nobreak >nul

:: Move cursor to the top right corner
.\send.exe COM3 115200 0x05 0x09 0x5D 0x0E 0x9A 0x01



