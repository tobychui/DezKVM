package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"imuslab.com/remdeskvm/remdeskd/mod/remdeshid"
)

const development = true

var (
	usbKVMDeviceName  = flag.String("usbkvm", "COM6", "USB KVM device file path")
	usbKVMBaudRate    = flag.Int("baudrate", 115200, "USB KVM baud rate")
	captureDeviceName = flag.String("capture", "/dev/video0", "Video capture device file path")
	usbKVM            *remdeshid.Controller
)

/* Web Server Static Files */
//go:embed www
var embeddedFiles embed.FS
var webfs http.FileSystem

func init() {
	// Initiate the web server static files
	if development {
		webfs = http.Dir("./www")
	} else {
		// Embed the ./www folder and trim the prefix
		subFS, err := fs.Sub(embeddedFiles, "www")
		if err != nil {
			log.Fatal(err)
		}
		webfs = http.FS(subFS)
	}

	// Initiate the HID controller
	usbKVM = remdeshid.NewHIDController(&remdeshid.Config{
		PortName: *usbKVMDeviceName,
		BaudRate: *usbKVMBaudRate,
	})

}

func main() {
	//Start the HID controller
	err := usbKVM.Connect()
	if err != nil {
		log.Fatal(err)
	}

	// Handle program exit to close the HID controller
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Shutting down...")
		usbKVM.Close()
		os.Exit(0)
	}()

	// Start the web server
	http.HandleFunc("/hid", usbKVM.HIDWebSocketHandler)
	http.Handle("/", http.FileServer(webfs))
	addr := ":9000"
	log.Printf("Serving on http://localhost%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))

}
