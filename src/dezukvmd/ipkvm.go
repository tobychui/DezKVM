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
	"imuslab.com/dezukvm/dezukvmd/mod/auth"
	"imuslab.com/dezukvm/dezukvmd/mod/dezukvm"
	"imuslab.com/dezukvm/dezukvmd/mod/logger"
)

var (
	dezukvmManager     *dezukvm.DezukVM
	listeningServerMux *http.ServeMux
	authManager        *auth.AuthManager
	systemLogger       *logger.Logger
)

func init_auth_manager() error {
	// Initialize logger
	systemLogger = logger.NewLogger(logger.WithLogLevel(logger.InfoLevel))

	// Initialize AuthManager with logger and DB path
	var err error
	authManager, err = auth.NewAuthManager(auth.Options{
		DBPath: DB_FILE_PATH,
		Log:    systemLogger.Info,
	})
	if err != nil {
		return err
	}
	return nil
}

func init_ipkvm_mode() error {
	listeningServerMux = http.NewServeMux()

	// Initialize the Auth Manager
	err := init_auth_manager()
	if err != nil {
		log.Fatal("Failed to initialize Auth Manager:", err)
		return err
	}

	//Create a new DezukVM manager
	dezukvmManager = dezukvm.NewKvmHostInstance(&dezukvm.RuntimeOptions{
		EnableLog: true,
	})

	// Experimental
	connectedUsbKvms, err := dezukvm.ScanConnectedUsbKvmDevices()
	if err != nil {
		return err
	}

	for _, dev := range connectedUsbKvms {
		err := dezukvmManager.AddUsbKvmDevice(dev)
		if err != nil {
			return err
		}
	}

	err = dezukvmManager.StartAllUsbKvmDevices()
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
		log.Println("Shutting down DezuKVM...")

		if dezukvmManager != nil {
			dezukvmManager.Close()
		}
		if authManager != nil {
			authManager.Close()
		}
		log.Println("Shutdown complete.")
		os.Exit(0)
	}()

	// Register Auth related APIs
	register_auth_apis(listeningServerMux)

	// Register DezukVM related APIs
	register_ipkvm_apis(listeningServerMux)

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
