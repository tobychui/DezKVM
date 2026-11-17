package auth

/*
	auth - single admin password + login sessions

	Login issues a random session token (32 bytes from crypto/rand) in the
	dezkvm_auth cookie. The server keeps only the SHA-256 of each token, with
	its expiry, in memory and in the "auth_sessions" bucket, so sessions
	survive a dezkvmd restart and a leaked database cannot be replayed as a
	cookie. Logout and password changes revoke sessions server side.

	The admin password is stored as a bcrypt hash ("password_hash"). Older
	installs kept it in plaintext under "password"; it is hashed and the
	plaintext deleted when the manager starts.
*/

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
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

	mu       sync.Mutex
	sessions map[string]time.Time // sha256(token) hex -> expiry

	verifyMu sync.Mutex // one bcrypt comparison at a time (CPU, brute force)

	now          func() time.Time // swappable in tests
	failureDelay time.Duration
}

const (
	authBucket    = "auth"
	sessionBucket = "auth_sessions"
	passHashKey   = "password_hash"
	legacyPassKey = "password" // plaintext, pre-hashing installs

	// CookieName is the session cookie.
	CookieName = "dezkvm_auth"
	// SessionTTL is how long a login lasts.
	SessionTTL = 24 * time.Hour
	// maxSessions bounds the stored sessions; the oldest are dropped.
	maxSessions = 64

	bcryptCost = bcrypt.DefaultCost
)

// ErrPasswordTooLong is returned for passwords bcrypt cannot hash.
var ErrPasswordTooLong = errors.New("password must be at most 72 bytes")

// NewAuthManager creates a new AuthManager with an existing DB instance.
func NewAuthManager(opt Options) (*AuthManager, error) {
	if opt.DB == nil {
		return nil, errors.New("DB instance is required")
	}
	// Ensure buckets exist
	for _, bucket := range []string{authBucket, sessionBucket} {
		if err := opt.DB.NewBucket(bucket); err != nil {
			return nil, err
		}
	}
	a := &AuthManager{
		db:           opt.DB,
		log:          opt.Log,
		sessions:     map[string]time.Time{},
		now:          time.Now,
		failureDelay: time.Second,
	}
	if err := a.migrateLegacyPassword(); err != nil {
		a.logf("Auth: could not hash the stored plaintext password: %v", err)
	}
	a.loadSessions()
	return a, nil
}

func (a *AuthManager) logf(format string, v ...interface{}) {
	if a.log != nil {
		a.log(format, v...)
	}
}

/* ------------------------------------------------------------ password */

// migrateLegacyPassword replaces a plaintext password with its hash.
func (a *AuthManager) migrateLegacyPassword() error {
	legacy, err := a.db.Read(authBucket, legacyPassKey)
	if err != nil || legacy == nil {
		return err
	}
	if hash, _ := a.db.Read(authBucket, passHashKey); hash != nil {
		// Already migrated; drop the stale plaintext copy
		return a.db.Delete(authBucket, legacyPassKey)
	}
	if err := a.writePasswordHash(string(legacy)); err != nil {
		return err
	}
	a.logf("Auth: stored password migrated to a bcrypt hash")
	return nil
}

// writePasswordHash stores the hash of password and removes any plaintext.
func (a *AuthManager) writePasswordHash(password string) error {
	if len(password) > 72 {
		return ErrPasswordTooLong
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return err
	}
	if err := a.db.Write(authBucket, passHashKey, hash); err != nil {
		return err
	}
	return a.db.Delete(authBucket, legacyPassKey)
}

// SetPassword sets the password (overwrites any existing) and ends every
// session.
func (a *AuthManager) SetPassword(password string) error {
	if password == "" {
		return errors.New("password must not be empty")
	}
	if err := a.writePasswordHash(password); err != nil {
		return err
	}
	a.RevokeAllSessions()
	return nil
}

// ChangePassword changes password if oldpassword matches.
func (a *AuthManager) ChangePassword(oldPassword, newPassword string) error {
	ok, err := a.ValidatePassword(oldPassword)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("old password incorrect")
	}
	return a.SetPassword(newPassword)
}

// ResetPassword removes the password and ends every session.
func (a *AuthManager) ResetPassword() error {
	if err := a.db.Delete(authBucket, passHashKey); err != nil {
		return err
	}
	if err := a.db.Delete(authBucket, legacyPassKey); err != nil {
		return err
	}
	a.RevokeAllSessions()
	return nil
}

// ValidatePassword checks password against the stored admin password.
// Wrong passwords return after a delay, to slow down guessing.
func (a *AuthManager) ValidatePassword(password string) (bool, error) {
	ok, err := a.checkPassword(password)
	if err == nil && !ok && a.failureDelay > 0 {
		time.Sleep(a.failureDelay)
	}
	return ok, err
}

func (a *AuthManager) checkPassword(password string) (bool, error) {
	if password == "" {
		return false, nil
	}
	hash, err := a.db.Read(authBucket, passHashKey)
	if err != nil {
		return false, err
	}
	if hash != nil {
		a.verifyMu.Lock()
		defer a.verifyMu.Unlock()
		return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil, nil
	}

	// Not migrated yet (migration failed at start): compare the plaintext
	legacy, err := a.db.Read(authBucket, legacyPassKey)
	if err != nil || legacy == nil {
		return false, err
	}
	if subtle.ConstantTimeCompare(legacy, []byte(password)) != 1 {
		return false, nil
	}
	if err := a.writePasswordHash(password); err != nil {
		a.logf("Auth: could not hash the stored plaintext password: %v", err)
	}
	return true, nil
}

/* ------------------------------------------------------------ sessions */

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// loadSessions restores the unexpired sessions from the database.
func (a *AuthManager) loadSessions() {
	now := a.now()
	var expired []string
	a.db.List(sessionBucket, func(key, value []byte) error {
		unix, err := strconv.ParseInt(string(value), 10, 64)
		expiry := time.Unix(unix, 0)
		if err != nil || !expiry.After(now) {
			expired = append(expired, string(key))
			return nil
		}
		a.sessions[string(key)] = expiry
		return nil
	})
	for _, key := range expired {
		a.db.Delete(sessionBucket, key)
	}
}

// createSession stores a new session and returns its token.
func (a *AuthManager) createSession() (string, time.Time, error) {
	token, err := newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	key := hashToken(token)
	expiry := a.now().Add(SessionTTL)

	a.mu.Lock()
	a.pruneLocked()
	a.sessions[key] = expiry
	a.mu.Unlock()

	if err := a.db.Write(sessionBucket, key, []byte(strconv.FormatInt(expiry.Unix(), 10))); err != nil {
		a.logf("Auth: could not persist session: %v", err)
	}
	return token, expiry, nil
}

// pruneLocked drops expired sessions, and the oldest ones beyond
// maxSessions - 1 to make room for a new one. Caller holds a.mu.
func (a *AuthManager) pruneLocked() {
	now := a.now()
	for key, expiry := range a.sessions {
		if !expiry.After(now) {
			delete(a.sessions, key)
			a.db.Delete(sessionBucket, key)
		}
	}
	if len(a.sessions) < maxSessions {
		return
	}
	keys := make([]string, 0, len(a.sessions))
	for key := range a.sessions {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return a.sessions[keys[i]].Before(a.sessions[keys[j]]) })
	for _, key := range keys[:len(keys)-maxSessions+1] {
		delete(a.sessions, key)
		a.db.Delete(sessionBucket, key)
	}
}

// sessionValid reports whether token belongs to an unexpired session.
func (a *AuthManager) sessionValid(token string) bool {
	if token == "" || len(token) > 128 {
		return false
	}
	key := hashToken(token)
	a.mu.Lock()
	defer a.mu.Unlock()
	expiry, ok := a.sessions[key]
	if !ok {
		return false
	}
	if !expiry.After(a.now()) {
		delete(a.sessions, key)
		a.db.Delete(sessionBucket, key)
		return false
	}
	return true
}

func (a *AuthManager) revokeSession(token string) {
	if token == "" {
		return
	}
	key := hashToken(token)
	a.mu.Lock()
	delete(a.sessions, key)
	a.mu.Unlock()
	a.db.Delete(sessionBucket, key)
}

// RevokeAllSessions logs every browser out.
func (a *AuthManager) RevokeAllSessions() {
	a.mu.Lock()
	keys := make([]string, 0, len(a.sessions))
	for key := range a.sessions {
		keys = append(keys, key)
	}
	a.sessions = map[string]time.Time{}
	a.mu.Unlock()
	// Includes sessions written by another process (e.g. -mode=setpw)
	a.db.List(sessionBucket, func(key, _ []byte) error {
		keys = append(keys, string(key))
		return nil
	})
	for _, key := range keys {
		a.db.Delete(sessionBucket, key)
	}
}

/* ---------------------------------------------------------------- HTTP */

// UserIsLoggedIn checks the session cookie of the request.
func (a *AuthManager) UserIsLoggedIn(r *http.Request) bool {
	cookie, err := r.Cookie(CookieName)
	return err == nil && a.sessionValid(cookie.Value)
}

// HandleFunc wraps an http.HandlerFunc with auth check.
func (a *AuthManager) HandleFunc(pattern string, handler http.HandlerFunc, mux *http.ServeMux) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if a.UserIsLoggedIn(r) {
			handler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	})
}

func sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		// dezkvmd serves HTTPS; plain HTTP (tests) cannot store Secure cookies
		Secure: r.TLS != nil,
		// Not sent with cross-site requests: blocks CSRF and cross-site
		// websocket hijacking of the HID / audio / terminal sockets
		SameSite: http.SameSiteLaxMode,
	}
}

// LoginUser starts a session if the password is correct.
func (a *AuthManager) LoginUser(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		return err
	}
	ok, err := a.ValidatePassword(req.Password)
	if err != nil {
		return err
	}
	if !ok {
		a.logf("Auth: failed login from %s", r.RemoteAddr)
		return errors.New("unauthorized")
	}

	// A previous session of this browser is replaced
	if old, err := r.Cookie(CookieName); err == nil {
		a.revokeSession(old.Value)
	}
	token, _, err := a.createSession()
	if err != nil {
		return err
	}
	http.SetCookie(w, sessionCookie(r, token, int(SessionTTL.Seconds())))
	return nil
}

// LogoutUser ends the session of the request and clears its cookie.
func (a *AuthManager) LogoutUser(w http.ResponseWriter, r *http.Request) error {
	if cookie, err := r.Cookie(CookieName); err == nil {
		a.revokeSession(cookie.Value)
	}
	http.SetCookie(w, sessionCookie(r, "", -1))
	return nil
}

// Close is kept for backward compatibility but does not close the shared DB.
// The DB should be closed at the parent scope level.
func (a *AuthManager) Close() error {
	// DB is shared, don't close it here
	return nil
}
