@echo off

echo "Testing 6 key presses"
.\send.exe COM3 115200 0xFF
timeout /t 1 /nobreak >nul

echo "Pressing A"
.\send.exe COM3 115200 0x02 0x01 0x41
echo "Pressing B"
.\send.exe COM3 115200 0x02 0x01 0x42
echo "Pressing C"
.\send.exe COM3 115200 0x02 0x01 0x43
echo "Pressing D"
.\send.exe COM3 115200 0x02 0x01 0x44
echo "Pressing E"
.\send.exe COM3 115200 0x02 0x01 0x45
echo "Pressing F"
.\send.exe COM3 115200 0x02 0x01 0x46
timeout /t 1 /nobreak >nul

echo "Releasing A"
.\send.exe COM3 115200 0x02 0x02 0x41
echo "Releasing B"
.\send.exe COM3 115200 0x02 0x02 0x42
echo "Releasing C"
.\send.exe COM3 115200 0x02 0x02 0x43
echo "Releasing D"
.\send.exe COM3 115200 0x02 0x02 0x44
echo "Releasing E"
.\send.exe COM3 115200 0x02 0x02 0x45
echo "Releasing F"
.\send.exe COM3 115200 0x02 0x02 0x46
timeout /t 1 /nobreak >nul
