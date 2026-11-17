package kvmhid

import (
	"bytes"
	"fmt"
	"log"
	"time"

	tserial "github.com/tarm/serial"
)

func NewHIDController(config *Config) *Controller {
	// Initialize the HID state with default values
	defaultHidState := HIDState{
		Modkey:          0x00,
		KeyboardButtons: [6]uint8{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		Leds:            0x00,
		MouseButtons:    0x00, // No mouse buttons pressed
		MouseX:          0,
		MouseY:          0,
	}

	return &Controller{
		Config:            config,
		serialRunning:     false,
		hidState:          defaultHidState,
		writeQueue:        make(chan []byte, 32),
		incomingDataQueue: make(chan []byte, 1024),
		readCloseChan:     make(chan bool),
		lastActivityTime:  time.Now(),
	}
}

const (
	// hidReplyTimeout is how long to wait for the CH9329 to acknowledge a
	// keyboard or mouse report. A report takes ~2ms at 115200 and ~25ms at 9600.
	hidReplyTimeout = 150 * time.Millisecond
	// hidConfigReplyTimeout is used for configuration commands with large replies
	hidConfigReplyTimeout = 1 * time.Second
	// fallbackBaudRate is the factory default baud rate of the CH9329
	fallbackBaudRate = 9600
)

// Connect opens the serial port and starts reading from it
func (c *Controller) Connect() error {
	// Make sure the CH9329 actually answers at the configured baud rate.
	// A newly soldered chip still runs at its factory default of 9600 baud,
	// in which case every command would time out.
	if !probeBaudRate(c.Config.PortName, c.Config.BaudRate) {
		if c.Config.BaudRate != fallbackBaudRate && probeBaudRate(c.Config.PortName, fallbackBaudRate) {
			log.Printf("Warning: CH9329 on %s answers at %d baud instead of %d. Using %d baud for now, run ./configure_chip.sh to switch the chip to %d baud for better responsiveness\n",
				c.Config.PortName, fallbackBaudRate, c.Config.BaudRate, fallbackBaudRate, c.Config.BaudRate)
			c.Config.BaudRate = fallbackBaudRate
		} else {
			log.Printf("Warning: CH9329 on %s did not answer at %d or %d baud, keyboard and mouse will not work until it does (is the remote computer's USB port powered?)\n",
				c.Config.PortName, c.Config.BaudRate, fallbackBaudRate)
		}
	}

	// Open the serial port
	config := &tserial.Config{
		Name:        c.Config.PortName,
		Baud:        c.Config.BaudRate,
		Size:        8,
		Parity:      tserial.ParityNone,
		ReadTimeout: time.Millisecond * 500,
	}

	port, err := tserial.OpenPort(config)
	if err != nil {
		return err
	}

	c.serialPort = port
	// Start reading from the serial port
	go func() {
		buf := make([]byte, 1024)
		for {
			select {
			case <-c.readCloseChan:
				return
			default:
				n, err := port.Read(buf)
				if err != nil {
					continue
				}
				if n > 0 {
					// Copy the data, buf is reused by the next read
					data := make([]byte, n)
					copy(data, buf[:n])
					c.incomingDataQueue <- data
				}
			}
		}
	}()

	//Create a loop to write to the serial port
	c.serialRunning = true
	go func() {
		for {
			data := <-c.writeQueue
			_, err := port.Write(data)
			if err != nil {
				log.Println(err.Error())
				return
			}
		}
	}()

	// Not all remote machines are on at the time of hid initiation
	// we can safely ignore the error here
	c.ChipSoftReset()

	return nil
}

func (c *Controller) Send(data []byte) error {
	if !c.serialRunning {
		return fmt.Errorf("serial port is not running")
	}
	select {
	case c.writeQueue <- data:
		return nil
	case <-time.After(30 * time.Millisecond):
		return fmt.Errorf("timeout waiting to send data")
	}
}

func (c *Controller) ClearReadQueue() {
	// Clear the incoming data queue
	for len(c.incomingDataQueue) > 0 {
		<-c.incomingDataQueue
	}
}

// transact sends a packet and waits for the reply to cmdByte. Transactions are
// serialized so concurrent senders (websocket handler, mouse jiggler) never
// read each other's replies.
func (c *Controller) transact(packet []byte, cmdByte byte, timeout time.Duration) ([]byte, error) {
	c.txMu.Lock()
	defer c.txMu.Unlock()

	// Drop replies that arrived after an earlier command timed out
	c.ClearReadQueue()
	if err := c.Send(packet); err != nil {
		return nil, err
	}
	return c.waitForReply(cmdByte, timeout)
}

// waitForReply waits for the reply to cmdByte, skipping any reply that
// belongs to an earlier command
func (c *Controller) waitForReply(cmdByte byte, timeout time.Duration) ([]byte, error) {
	succReplyByte := cmdByte | 0x80
	errorReplyByte := cmdByte | 0xC0
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var buf []byte
	for {
		select {
		case data := <-c.incomingDataQueue:
			buf = append(buf, data...)
			var packets [][]byte
			packets, buf = splitPackets(buf)
			for _, pkt := range packets {
				switch pkt[3] {
				case succReplyByte:
					return pkt[5 : len(pkt)-1], nil
				case errorReplyByte:
					code := byte(0)
					if len(pkt) > 6 {
						code = pkt[5]
					}
					return nil, fmt.Errorf("CH9329 returned error 0x%02X for command 0x%02X", code, cmdByte)
				}
				// Reply to an earlier command, ignore it
			}
		case <-timer.C:
			return nil, fmt.Errorf("timeout waiting for reply")
		}
	}
}

// splitPackets extracts complete, checksum-valid CH9329 packets
// (0x57 0xAB ADDR CMD LEN DATA SUM) from buf and returns them together with
// the bytes of an incomplete packet at the end. Garbage is skipped.
func splitPackets(buf []byte) ([][]byte, []byte) {
	var packets [][]byte
	header := []byte{0x57, 0xAB}
	for {
		i := bytes.Index(buf, header)
		if i < 0 {
			// Keep a trailing 0x57, it might be the start of the next header
			if len(buf) > 0 && buf[len(buf)-1] == 0x57 {
				return packets, buf[len(buf)-1:]
			}
			return packets, nil
		}
		buf = buf[i:]
		if len(buf) < 5 {
			return packets, buf
		}
		n := 5 + int(buf[4]) + 1
		if len(buf) < n {
			return packets, buf
		}
		if calcChecksum(buf[:n-1]) == buf[n-1] {
			packets = append(packets, buf[:n])
			buf = buf[n:]
		} else {
			// Not a real header, keep scanning after it
			buf = buf[2:]
		}
	}
}

// probeBaudRate checks whether a CH9329 on portName answers GET_INFO at baud
func probeBaudRate(portName string, baud int) bool {
	port, err := tserial.OpenPort(&tserial.Config{
		Name:        portName,
		Baud:        baud,
		Size:        8,
		Parity:      tserial.ParityNone,
		ReadTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		return false
	}
	defer port.Close()
	_ = port.Flush()

	cmd := []byte{0x57, 0xAB, 0x00, 0x01, 0x00, 0x00} // CMD_GET_INFO
	cmd[5] = calcChecksum(cmd[:5])
	tmp := make([]byte, 64)
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := port.Write(cmd); err != nil {
			return false
		}
		var buf []byte
		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			n, _ := port.Read(tmp)
			if n == 0 {
				continue
			}
			buf = append(buf, tmp[:n]...)
			var packets [][]byte
			packets, buf = splitPackets(buf)
			for _, pkt := range packets {
				if pkt[3] == 0x81 {
					return true
				}
			}
		}
	}
	return false
}

func (c *Controller) Close() {
	c.StopMouseJiggler()
	c.serialRunning = false
	c.readCloseChan <- true
	if c.serialPort != nil {
		done := make(chan struct{})
		go func() {
			c.serialPort.Close()
			close(done)
		}()
		select {
		case <-done:
			// Closed successfully
		case <-time.After(3 * time.Second):
			log.Println("serial port close timeout")
		}
	}

}
