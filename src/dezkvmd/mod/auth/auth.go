package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/boltdb/bolt"
)

// LogFunc is a function type for logging.
type LogFunc func(format string, v ...interface{})

// Options holds configuration for AuthManager.
type Options struct {
	DBPath string
	Log    LogFunc
}

// AuthManager handles authentication.
type AuthManager struct {
	db  *bolt.DB
	log LogFunc
	mu  sync.RWMutex
}

const (
	authBucket = "auth"
	passKey    = "password"
)

// NewAuthManager creates a new AuthManager.
func NewAuthManager(opt Options) (*AuthManager, error) {

	dir := filepath.Dir(opt.DBPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	db, err := bolt.Open(opt.DBPath, 0755, nil)
	if err != nil {
		return nil, err
	}
	// Ensure bucket exists
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(authBucket))
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &AuthManager{db: db, log: opt.Log}, nil
}

// SetPassword sets the password (overwrites any existing).
func (a *AuthManager) SetPassword(password string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(authBucket))
		return b.Put([]byte(passKey), []byte(password))
	})
}

// ChangePassword changes password if oldpassword matches.
func (a *AuthManager) ChangePassword(oldPassword, newPassword string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(authBucket))
		stored := b.Get([]byte(passKey))
		if stored == nil || string(stored) != oldPassword {
			return errors.New("old password incorrect")
		}
		return b.Put([]byte(passKey), []byte(newPassword))
	})
}

// ResetPassword removes the password.
func (a *AuthManager) ResetPassword() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(authBucket))
		return b.Delete([]byte(passKey))
	})
}

// ValidatePassword checks password from request
func (a *AuthManager) ValidatePassword(password string) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var ok bool
	err := a.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(authBucket))
		stored := b.Get([]byte(passKey))
		ok = stored != nil && string(stored) == password
		return nil
	})
	return ok, err
}

// UserIsLoggedIn checks if the user is logged in via cookie.
func (a *AuthManager) UserIsLoggedIn(r *http.Request) bool {
	cookie, err := r.Cookie("dezkvm_auth")
	return err == nil && cookie.Value == "1"
}

// HandleFunc wraps an http.HandlerFunc with auth check.
func (a *AuthManager) HandleFunc(pattern string, handler http.HandlerFunc, mux *http.ServeMux) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if a.UserIsLoggedIn(r) {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	})
}

// LoginUser sets a session/cookie if password is correct
func (a *AuthManager) LoginUser(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	var ok bool
	err := a.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(authBucket))
		stored := b.Get([]byte(passKey))
		ok = stored != nil && string(stored) == req.Password
		return nil
	})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("unauthorized")
	}
	// Set a simple session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "dezkvm_auth",
		Value:    "1",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400,
	})
	return nil
}

// LogoutUser removes the session/cookie for the user.
func (a *AuthManager) LogoutUser(w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, &http.Cookie{
		Name:     "dezkvm_auth",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	return nil
}

// Close closes the underlying DB.
func (a *AuthManager) Close() error {
	return a.db.Close()
}
