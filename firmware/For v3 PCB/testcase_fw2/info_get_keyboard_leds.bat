@echo off
.\send.exe COM3 115200 0xFF 
timeout /t 1 /nobreak >nul
.\send.exe COM3 115200 0x01 0x0B