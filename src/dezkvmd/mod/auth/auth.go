package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"imuslab.com/dezkvm/dezkvmd/mod/db"
)

// LogFunc is a function type for logging.
type LogFunc func(format string, v ...interface{})

// Options holds configuration for AuthManager.
type Options struct {
	DB  *db.DB
	Log LogFunc
}

// AuthManager handles authentication.
type AuthManager struct {
	db  *db.DB
	log LogFunc
}

const (
	authBucket = "auth"
	passKey    = "password"
)

// NewAuthManager creates a new AuthManager with an existing DB instance.
func NewAuthManager(opt Options) (*AuthManager, error) {
	if opt.DB == nil {
		return nil, errors.New("DB instance is required")
	}
	// Ensure bucket exists
	if err := opt.DB.NewBucket(authBucket); err != nil {
		return nil, err
	}
	return &AuthManager{db: opt.DB, log: opt.Log}, nil
}

// SetPassword sets the password (overwrites any existing).
func (a *AuthManager) SetPassword(password string) error {
	return a.db.Write(authBucket, passKey, []byte(password))
}

// ChangePassword changes password if oldpassword matches.
func (a *AuthManager) ChangePassword(oldPassword, newPassword string) error {
	stored, err := a.db.Read(authBucket, passKey)
	if err != nil {
		return err
	}
	if stored == nil || string(stored) != oldPassword {
		return errors.New("old password incorrect")
	}
	return a.db.Write(authBucket, passKey, []byte(newPassword))
}

// ResetPassword removes the password.
func (a *AuthManager) ResetPassword() error {
	return a.db.Delete(authBucket, passKey)
}

// ValidatePassword checks password from request
func (a *AuthManager) ValidatePassword(password string) (bool, error) {
	stored, err := a.db.Read(authBucket, passKey)
	if err != nil {
		return false, err
	}
	ok := stored != nil && string(stored) == password
	return ok, nil
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
	stored, err := a.db.Read(authBucket, passKey)
	if err != nil {
		return err
	}
	if stored == nil || string(stored) != req.Password {
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

// Close is kept for backward compatibility but does not close the shared DB.
// The DB should be closed at the parent scope level.
func (a *AuthManager) Close() error {
	// DB is shared, don't close it here
	return nil
}
