@echo off
.\send.exe COM3 115200 0xFF

:: Move down
.\send.exe COM3 115200 0x03 0x0A 0x00 0x7F 
timeout /t 1 /nobreak >nul

:: Move up
.\send.exe COM3 115200 0x03 0x0A 0x00 0x80
timeout /t 1 /nobreak >nul

:: Move right
.\send.exe COM3 115200 0x03 0x0A 0x7F 0x00 
timeout /t 1 /nobreak >nul

:: Move left
.\send.exe COM3 115200 0x03 0x0A 0x80 0x00 