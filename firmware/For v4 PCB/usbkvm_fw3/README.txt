By default the CH9329 uses 9600 baudrate to communicate with MCU
However, this might significantly reduce the fps of the cursor
If you want a smoother cursor experience, follow the steps below to configure
the CH9329 to use a higher baudrate to communicate with CH552G

1. Flash the CH552G with Serial0 9600 baudrate to act as a USB to UART converter
2. Plug in the CH9239 port to your slave computer so it get power
3. Use the remdeskvmd -mode=cfgchip command to set the baudrate to 19200
4. Disconnect the CH9329 KVM port that also power down the CH9329
5. Flash the CH552G with Serial0 updated to 19200 baudrate
6. Repower the CH9329, now CH552G shd be able to communicate with it using 19200 baudrate