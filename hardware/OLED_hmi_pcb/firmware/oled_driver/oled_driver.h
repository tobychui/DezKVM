/* Hardware definations */
#define LED_PIN 14
#define TOUCHPAD_A 15
#define TOUCHPAD_B 16
#define I2C_SDA 31
#define I2C_SCL 30

/*
    HMI command types (Length-Type-Value format)

    The length do not contain length bytes, for example:
    0x02 0xC4 0x20 (CMD_LOAD_FONT_DATA of 0x20)

    On command success, return 0xF0
    On command failed, return 0xF1
*/

/* CMD byte type enum */
#define CMD_DEF_LENGTH 0x00
#define CMD_DEF_TYPE 0x01
#define CMD_DEF_VALUE 0x02

/* CMD_MAX_LENGTH: large enough for the biggest command payload + headroom */
#define CMD_MAX_LENGTH 130

// OLED drawing commands
#define CMD_CLEAR_SCREEN 0x01 //Clear the OLED screen
#define CMD_LIGHT_SCREEN 0x02 //Light up the OLED screen with all pixels
#define CMD_SET_SCREEN_CONTENT 0x03 //Set the screen content to render, follow by OLED_MAX_CHAR_COUNT ASCII characters
#define CMD_WRITE_RAW_FONT_BUF 0x04 //Write bytes to raw font buffer, font buffer size depends on terminal mode font size
#define CMD_CLEAR_FONT_BUF 0x05 //Clear the font buffer
#define CMD_WRITE_CHAR_AT_IDX 0x06 //Write ascii char at given grid location (overwrite)

//To be added

// Touch interrupt events
#define TOUCHKEY_LONGPRESS_SECOND_THRESHOLD 2 //Seconds to trigger long press event
#define CMD_EVENT_TOUCH_A_DOWN 0xB0 // Touch button A is pressed
#define CMD_EVENT_TOUCH_A_UP 0xB1 // Touch button A is released
#define CMD_EVENT_TOUCH_B_DOWN 0xB2 // Touch button B is pressed
#define CMD_EVENT_TOUCH_B_UP 0xB3 // Touch button B is released
#define CMD_EVENT_TOUCH_A_LONG_PRESS 0xB4 // Touch button A is long pressed
#define CMD_EVENT_TOUCH_B_LONG_PRESS 0xB5 // Touch button B is long pressed

// EMMC flasing and debug commands
#define CMD_WRITE_FONT_DATA 0xC3 //Write font data to EEPROM, <len> 0xC3 <ASCII code (1 byte)> <font_data (FONT_CHAR_SIZE bytes)>
#define CMD_LOAD_FONT_DATA 0xC4 //Load font data to serial for debug 0x02 0xC4 <ASCII code (1 byte)>
#define CMD_LOAD_FONT_TO_OLED 0xC5 //Load font data to OLED screen first position, 0x02 0xC5 <ASCII code (1 byte)>
#define CMD_WRITE_EEPROM_RAW 0xC6 //Write raw data to EEPROM, [len] 0xC6 [addr_h] [addr_l] [data... up to 32 bytes]
#define CMD_READ_EEPROM_RAW  0xC7 //Read raw data from EEPROM, [0x04] 0xC7 [addr_h] [addr_l] [count], returns count raw bytes
#define CMD_RESET 0xCF //Reset and celar the cmd_bytes

/* I2C Address */
#define OLED_ADDR 0x3C
#define EEPROM_ADDR 0x50 //24C32

/* OLED */
// Commands for SSD1306
#define SSD1306_DISPLAYOFF 0xAE
#define SSD1306_SETDISPLAYCLOCKDIV 0xD5
#define SSD1306_SETMULTIPLEX 0xA8
#define SSD1306_SETDISPLAYOFFSET 0xD3
#define SSD1306_SETSTARTLINE 0x40
#define SSD1306_CHARGEPUMP 0x8D
#define SSD1306_MEMORYMODE 0x20
#define SSD1306_SEGREMAP 0xA1
#define SSD1306_COMSCANDEC 0xC8
#define SSD1306_SETCOMPINS 0xDA
#define SSD1306_SETCONTRAST 0x81
#define SSD1306_SETPRECHARGE 0xD9
#define SSD1306_SETVCOMDETECT 0xDB
#define SSD1306_DISPLAYALLON_RESUME 0xA4
#define SSD1306_NORMALDISPLAY 0xA6
#define SSD1306_DISPLAYON 0xAF

// Hardware definations
#define FONT_GRID_SIZE 8
#define OLED_COLS (128 / FONT_GRID_SIZE)
#define OLED_ROWS (64 / FONT_GRID_SIZE)
#define OLED_MAX_CHAR_COUNT (OLED_COLS * OLED_ROWS)
#define OLED_PAGES_PER_CHAR (FONT_GRID_SIZE / 8)

// Function prototypes
void OLED_WriteCmd(uint8_t command);
void OLED_WriteData(uint8_t data);
void initializeOLED();
void clearScreen();
void lightScreen();
void OLED_clear_screen(void);
void OLED_set_char(uint8_t idx, uint8_t ascii);
void OLED_render_screen(void);
void OLED_render_buf_to_screen(uint8_t idx);
void OLED_ender_boot_animation_loop();

/* EEPROM Font Mapping
   Each character: FONT_GRID_SIZE rows x FONT_BYTES_PER_ROW bytes = FONT_CHAR_SIZE bytes
   Address = (ascii_code - 0x20) * FONT_CHAR_SIZE
*/

/* Boot Animation Frames (128x64 px, 1 bit/pixel = 1024 bytes per frame) */
#define BOOT_ANIMATION_FRAME_SIZE  1024
#define BOOT_ANIMATION_START_ADDR  0x0300
#define BOOT_ANIMATION_FRAME_COUNT 3
#define BOOT_ANIMATION_FRAME_DELAY 50
#define FONT_BYTES_PER_ROW  ((FONT_GRID_SIZE + 7) / 8)
#define FONT_CHAR_SIZE      (FONT_GRID_SIZE * FONT_BYTES_PER_ROW)
#define FONT_ASCII_OFFSET   0x20

/* EEPROM Function Prototypes */
void EEPROM_WriteFontData(uint8_t ascii_code);
void EEPROM_LoadFontData(uint8_t ascii_code);
void EEPROM_WriteRaw(uint16_t addr, uint8_t* data, uint8_t len);
void EEPROM_ReadRaw(uint16_t addr, uint8_t* buf, uint8_t len);

/* Touchkey Function Prototypes */
void touch_key_init(void);
void process_touch_key_events(void);

/* UUID services, for DezKVM USB device discovery protocol */
void print_device_uuid(void);
