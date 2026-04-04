:: write_eeprom.bat
::  
:: This script write all the required ASCII font image
:: and boot animation frames to the onboard EEPROM
:: You must flash the firmware first before using
:: this script. Change the COM port and baudrate if needed

set COM_PORT=COM5
set BAUDRATE=115200

:: Write ASCII font to EEPROM
.\eeprom_write.exe -port=%COM_PORT% -baudrate=%BAUDRATE% -font="protracker.ttf" -fontwidth=8 -fontheight=8 -fontsize=8 -offsetx=0 -offsety=1

:: Write boot animation frames to EEPROM
.\eeprom_write.exe -port=%COM_PORT% -baudrate=%BAUDRATE% -mode=frame