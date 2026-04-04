@echo off
:: Press F11
echo "Pressing F11"
send.exe COM4 115200 0x01 0x06 0xCC

