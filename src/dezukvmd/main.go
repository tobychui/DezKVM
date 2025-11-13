package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/google/uuid"
)

const (
	DEFAULT_DEV_MODE = true
	CONFIG_PATH      = "./config"
	USB_KVM_CFG_PATH = CONFIG_PATH + "/usbkvm.json"
	UUID_FILE        = CONFIG_PATH + "/uuid.cfg"
	DB_FILE_PATH     = CONFIG_PATH + "/sys.db"
)

var (
	nodeUUID   = "00000000-0000-0000-0000-000000000000"
	developent = flag.Bool("dev", DEFAULT_DEV_MODE, "Enable development mode with local static files")
	mode       = flag.String("mode", "ipkvm", "Mode of operation: usbkvm, ipkvm or debug")
	tool       = flag.String("tool", "", "Run debug tool, must be used with -mode=debug")
)

/* Web Server Static Files */
//go:embed www
var embeddedFiles embed.FS
var webfs http.FileSystem

func init() {
	// Initiate the web server static files
	if *developent {
		webfs = http.Dir("./www")
	} else {
		// Embed the ./www folder and trim the prefix
		subFS, err := fs.Sub(embeddedFiles, "www")
		if err != nil {
			log.Fatal(err)
		}
		webfs = http.FS(subFS)
	}

	// Initiate the config folder if not exists
	err := os.MkdirAll("./config", 0755)
	if err != nil {
		log.Fatal("Failed to create config folder:", err)
	}

}

func main() {
	flag.Parse()

	//Generate the node uuid if not set

	if _, err := os.Stat(UUID_FILE); os.IsNotExist(err) {
		newUUID := uuid.NewString()
		err = os.WriteFile(UUID_FILE, []byte(newUUID), 0644)
		if err != nil {
			log.Fatal("Failed to write UUID to file:", err)
		}
	}

	uuidBytes, err := os.ReadFile(UUID_FILE)
	if err != nil {
		log.Fatal("Failed to read UUID from file:", err)
	}
	nodeUUID = string(uuidBytes)

	switch *mode {
	case "cfgchip":
		//Load config file or create default one
		kvmCfg, err := loadUsbKvmConfig()
		if err != nil {
			log.Fatal("Failed to load or create USB KVM config:", err)
		}

		//Override the baudrate to 9600 for chip configuration
		kvmCfg.USBKVMBaudrate = 9600

		err = SetupHIDCommunication(kvmCfg)
		if err != nil {
			log.Fatal(err)
		}
	case "debug":
		err := handle_debug_tool()
		if err != nil {
			log.Fatal(err)
		}
	case "ipkvm":
		//Check runtime dependencies
		err := run_dependency_precheck()
		if err != nil {
			log.Fatal(err)
		}

		//Start IP-KVM mode
		err = init_ipkvm_mode()
		if err != nil {
			log.Fatal(err)
		}
	case "setpw":
		// Set system access password interactively
		err := init_auth_manager()
		if err != nil {
			log.Fatal("Failed to initialize Auth Manager:", err)
		}
		var pw string
		fmt.Print("Enter new password: ")
		_, err = fmt.Scanln(&pw)
		if err != nil {
			log.Fatal("Failed to read password:", err)
		}
		err = authManager.SetPassword(pw)
		if err != nil {
			log.Fatal("Failed to set password:", err)
		}
		fmt.Println("Password set successfully.")
		authManager.Close()
	default:
		log.Fatalf("Unknown mode: %s. Supported modes are: usbkvm, capture", *mode)
	}
}
