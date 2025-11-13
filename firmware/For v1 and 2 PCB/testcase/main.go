package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/tarm/serial"
)

func main() {
	if len(os.Args) < 4 {
		log.Fatalf("Usage: %s <port> <baud> <data...>", os.Args[0])
	}

	portName := os.Args[1]
	baudRate, err := strconv.Atoi(os.Args[2])
	if err != nil {
		log.Fatalf("Invalid baud rate: %v", err)
	}

	config := &serial.Config{
		Name:   portName,
		Baud:   baudRate,
		Size:   8,
		Parity: serial.ParityNone,
	}

	port, err := serial.OpenPort(config)
	if err != nil {
		log.Fatalf("Failed to open port: %v", err)
	}
	defer port.Close()

	go func() {
		buf := make([]byte, 128)
		for {
			n, err := port.Read(buf)
			if err != nil {
				log.Printf("Failed to read from port: %v", err)
				return
			}
			if n > 0 {
				fmt.Print("Received bytes: ")
				for i := 0; i < n; i++ {
					fmt.Printf("0x%02X ", buf[i])
				}
				fmt.Println()
			}
		}
	}()

	for _, arg := range os.Args[3:] {
		data, err := strconv.ParseUint(arg, 0, 8)
		if err != nil {
			log.Fatalf("Invalid data byte: %v", err)
		}
		n, err := port.Write([]byte{byte(data)})
		if err != nil {
			log.Fatalf("Failed to write to port: %v", err)
		}
		fmt.Printf("Sent %d bytes to %s\n", n, portName)
		time.Sleep(10 * time.Millisecond)
	}
}
