package remdeshid

import (
	"fmt"
	"log"

	"github.com/tarm/serial"
)

type Config struct {
	/* Bindings and callback */
	OnWriteError  func(error)         // Callback for when an error occurs while writing to USBKVM device
	OnReadError   func(error)         // Callback for when an error occurs while reading from USBKVM device
	OnDataHandler func([]byte, error) // Callback for when data is received from USBKVM device

	/* Serial port configs */
	PortName string
	BaudRate int
}

// Controller is a struct that represents a HID controller
type Controller struct {
	Config         *Config
	serialPort     *serial.Port
	serialRunning  bool
	readStopChan   chan bool
	writeStopChan  chan bool
	writeQueue     chan []byte
	lastScrollTime int64
}

func NewHIDController(config *Config) *Controller {
	return &Controller{
		Config:        config,
		serialRunning: false,
	}
}

// Connect opens the serial port and starts reading from it
func (c *Controller) Connect() error {
	// Open the serial port
	config := &serial.Config{
		Name:   c.Config.PortName,
		Baud:   c.Config.BaudRate,
		Size:   8,
		Parity: serial.ParityNone,
	}

	port, err := serial.OpenPort(config)
	if err != nil {
		return err
	}

	c.serialPort = port
	c.readStopChan = make(chan bool)
	//Start reading from the serial port
	go func() {
		buf := make([]byte, 128)
		for {
			select {
			case <-c.readStopChan:
				return
			default:
				n, err := port.Read(buf)
				if err != nil {
					if c.Config.OnReadError != nil {
						c.Config.OnReadError(err)
					} else {
						log.Println(err.Error())
					}
					c.readStopChan = nil
					return
				}
				if n > 0 {
					if c.Config.OnDataHandler != nil {
						c.Config.OnDataHandler(buf[:n], nil)
					} else {
						fmt.Print("Received bytes: ")
						for i := 0; i < n; i++ {
							fmt.Printf("0x%02X ", buf[i])
						}
						fmt.Println()
					}
				}
			}
		}
	}()

	//Create a loop to write to the serial port
	c.writeStopChan = make(chan bool)
	c.writeQueue = make(chan []byte, 1)
	c.serialRunning = true
	go func() {
		for {
			select {
			case data := <-c.writeQueue:
				_, err := port.Write(data)
				if err != nil {
					if c.Config.OnWriteError != nil {
						c.Config.OnWriteError(err)
					} else {
						log.Println(err.Error())
					}
				}
			case <-c.writeStopChan:
				c.serialRunning = false
				return
			}
		}
	}()

	//Send over an opr queue reset signal
	err = c.Send([]byte{OPR_TYPE_DATA_RESET})
	if err != nil {
		return err
	}

	//Reset keyboard press state
	err = c.Send([]byte{OPR_TYPE_KEYBOARD_WRITE, SUBTYPE_KEYBOARD_SPECIAL_RESET, 0x00})
	if err != nil {
		return err
	}

	//Reset mouse press state
	err = c.Send([]byte{OPR_TYPE_MOUSE_WRITE, SUBTYPE_MOUSE_RESET, 0x00})
	if err != nil {
		return err
	}
	return nil
}

func (c *Controller) Send(data []byte) error {
	if !c.serialRunning {
		return fmt.Errorf("serial port is not running")
	}
	c.writeQueue <- data
	return nil
}

func (c *Controller) Close() {
	if c.readStopChan != nil {
		c.readStopChan <- true
	}

	if c.writeStopChan != nil {
		c.writeStopChan <- true
	}

	c.serialPort.Close()
}
