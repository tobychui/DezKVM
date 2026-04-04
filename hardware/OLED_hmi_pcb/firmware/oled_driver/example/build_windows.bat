@echo off
REM Build script for Windows

echo Building for Windows (amd64)...
go build -v -o oled_sysinfo.exe

if %ERRORLEVEL% EQU 0 (
    echo.
    echo Build successful! Executable: oled_sysinfo.exe
    echo.
    echo Usage:
    echo   oled_sysinfo.exe -port COM3
    echo.
) else (
    echo Build failed!
    exit /b 1
)
