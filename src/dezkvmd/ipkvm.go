package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gorilla/csrf"
	"imuslab.com/dezkvm/dezkvmd/mod/auth"
	"imuslab.com/dezkvm/dezkvmd/mod/db"
	"imuslab.com/dezkvm/dezkvmd/mod/dezkvm"
	"imuslab.com/dezkvm/dezkvmd/mod/logger"
	"imuslab.com/dezkvm/dezkvmd/mod/sshprox"
)

var (
	dezkvmManager      *dezkvm.DezkVM
	listeningServerMux *http.ServeMux
	authManager        *auth.AuthManager
	systemLogger       *logger.Logger
	systemDB           *db.DB
	sshproxManager     *sshprox.Manager
)

func init_system_db() error {
	// Initialize the system database
	var err error
	systemDB, err = db.NewDB(DB_FILE_PATH)
	if err != nil {
		return err
	}
	return nil
}

func init_auth_manager() error {
	// Initialize logger
	systemLogger = logger.NewLogger(logger.WithLogLevel(logger.InfoLevel))

	// Initialize AuthManager with logger and shared DB instance
	var err error
	authManager, err = auth.NewAuthManager(auth.Options{
		DB:  systemDB,
		Log: systemLogger.Info,
	})
	if err != nil {
		return err
	}
	return nil
}

func init_ipkvm_mode() error {
	listeningServerMux = http.NewServeMux()

	// Initialize the system database
	err := init_system_db()
	if err != nil {
		log.Fatal("Failed to initialize system database:", err)
		return err
	}

	// Initialize the Auth Manager
	err = init_auth_manager()
	if err != nil {
		log.Fatal("Failed to initialize Auth Manager:", err)
		return err
	}

	// Initialize SSH Proxy Manager
	sshproxManager = sshprox.NewSSHProxyManager()

	//Create a new DezkVM manager
	dezkvmManager = dezkvm.NewKvmHostInstance(&dezkvm.RuntimeOptions{
		EnableLog: true,
	})

	// Experimental
	connectedUsbKvms, err := dezkvm.ScanConnectedUsbKvmDevices()
	if err != nil {
		return err
	}

	for _, dev := range connectedUsbKvms {
		err := dezkvmManager.AddUsbKvmDevice(dev)
		if err != nil {
			return err
		}
	}

	err = dezkvmManager.StartAllUsbKvmDevices()
	if err != nil {
		return err
	}
	// ~Experimental

	// Handle root routing with CSRF protection
	handle_root_routing(listeningServerMux)

	// Handle program exit to close the HID controller
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Shutting down DezKVM...")

		if dezkvmManager != nil {
			dezkvmManager.Close()
		}
		if authManager != nil {
			authManager.Close()
		}
		if systemDB != nil {
			systemDB.Close()
		}
		log.Println("Shutdown complete.")
		os.Exit(0)
	}()

	// Register Auth related APIs
	register_auth_apis(listeningServerMux)

	// Register DezkVM related APIs
	register_ipkvm_apis(listeningServerMux)

	// Register Terminal related APIs
	register_terminal_apis(listeningServerMux)

	err = http.ListenAndServe(":9000", listeningServerMux)
	return err
}

func request_url_allow_unauthenticated(r *http.Request) bool {
	// Define a list of URL paths that can be accessed without authentication
	allowedPaths := []string{
		"/login.html",
		"/api/v1/login",
		"/img/",
		"/js/",
		"/css/",
		"/favicon.png",
	}

	requestPath := r.URL.Path
	for _, path := range allowedPaths {
		if strings.HasPrefix(requestPath, path) {
			return true
		}
	}
	return false
}

func handle_root_routing(mux *http.ServeMux) {
	// Root router: check login status, redirect to login.html if not authenticated
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !authManager.UserIsLoggedIn(r) && !request_url_allow_unauthenticated(r) {
			w.Header().Set("Location", "/login.html")
			w.WriteHeader(http.StatusFound)
			return
		}
		// Only inject for .html files
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		if strings.HasSuffix(path, ".html") {
			// Read the HTML file from disk
			targetFilePath := filepath.Join("www", filepath.Clean(path))
			content, err := os.ReadFile(targetFilePath)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			htmlContent := string(content)
			// Replace CSRF token placeholder
			htmlContent = strings.ReplaceAll(htmlContent, "{{.csrfToken}}", csrf.Token(r))
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(htmlContent))
			return
		}

		// Serve static files (img, js, css, etc.)
		targetFilePath := filepath.Join("www", filepath.Clean(path))
		http.ServeFile(w, r, targetFilePath)
	})
}
