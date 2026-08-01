package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

const passwordIterations = 600000
const passwordSaltLength = 16
const passwordKeyLength = 32
const sessionTokenLength = 32
const sessionLifetime = 24 * time.Hour
const loginFailureLimit = 5
const loginLockDuration = 15 * time.Minute

type PasswordHasher struct {
	reader io.Reader
}

func NewPasswordHasher(reader io.Reader) *PasswordHasher {
	if reader == nil {
		reader = rand.Reader
	}
	return &PasswordHasher{reader: reader}
}

func (hasher *PasswordHasher) Hash(password string) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	salt := make([]byte, passwordSaltLength)
	if _, err := io.ReadFull(hasher.reader, salt); err != nil {
		return "", fmt.Errorf("read password salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyLength)
	if err != nil {
		return "", fmt.Errorf("derive password key: %w", err)
	}
	return fmt.Sprintf(
		"$pbkdf2-sha256$%d$%s$%s",
		passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func (hasher *PasswordHasher) Verify(encoded string, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "" || parts[1] != "pbkdf2-sha256" {
		return false, errors.New("invalid password hash format")
	}
	iterations, err := strconv.Atoi(parts[2])
	if err != nil || iterations != passwordIterations {
		return false, errors.New("invalid password hash iterations")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) != passwordSaltLength {
		return false, errors.New("invalid password hash salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(expected) != passwordKeyLength {
		return false, errors.New("invalid password hash key")
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(expected))
	if err != nil {
		return false, fmt.Errorf("derive password key: %w", err)
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

type Session struct {
	Token     string
	CSRFToken string
	ClientIP  string
	ExpiresAt time.Time
}

type SessionManager struct {
	mutex    sync.Mutex
	reader   io.Reader
	clock    func() time.Time
	sessions map[string]Session
}

func NewSessionManager(reader io.Reader, clock func() time.Time) *SessionManager {
	if reader == nil {
		reader = rand.Reader
	}
	if clock == nil {
		clock = time.Now
	}
	return &SessionManager{reader: reader, clock: clock, sessions: make(map[string]Session)}
}

func (manager *SessionManager) Create(clientIP string) (Session, error) {
	token, err := manager.randomToken()
	if err != nil {
		return Session{}, err
	}
	csrfToken, err := manager.randomToken()
	if err != nil {
		return Session{}, err
	}
	session := Session{
		Token:     token,
		CSRFToken: csrfToken,
		ClientIP:  clientIP,
		ExpiresAt: manager.clock().Add(sessionLifetime),
	}
	manager.mutex.Lock()
	manager.sessions[token] = session
	manager.mutex.Unlock()
	return session, nil
}

func (manager *SessionManager) Validate(token string, csrfToken string, clientIP string) bool {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	session, exists := manager.sessions[token]
	if !exists {
		return false
	}
	if !manager.clock().Before(session.ExpiresAt) {
		delete(manager.sessions, token)
		return false
	}
	if session.ClientIP != clientIP {
		return false
	}
	return constantTimeStringEqual(session.CSRFToken, csrfToken)
}

func (manager *SessionManager) RevokeAll() {
	manager.mutex.Lock()
	manager.sessions = make(map[string]Session)
	manager.mutex.Unlock()
}

func (manager *SessionManager) randomToken() (string, error) {
	value := make([]byte, sessionTokenLength)
	if _, err := io.ReadFull(manager.reader, value); err != nil {
		return "", fmt.Errorf("read session entropy: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func constantTimeStringEqual(left string, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

type loginAttempt struct {
	failures    int
	lockedUntil time.Time
}

type LoginLimiter struct {
	mutex    sync.Mutex
	clock    func() time.Time
	attempts map[string]loginAttempt
}

func NewLoginLimiter(clock func() time.Time) *LoginLimiter {
	if clock == nil {
		clock = time.Now
	}
	return &LoginLimiter{clock: clock, attempts: make(map[string]loginAttempt)}
}

func (limiter *LoginLimiter) Allow(clientIP string) (bool, time.Duration) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()
	attempt, exists := limiter.attempts[clientIP]
	if !exists || attempt.lockedUntil.IsZero() {
		return true, 0
	}
	now := limiter.clock()
	if !now.Before(attempt.lockedUntil) {
		delete(limiter.attempts, clientIP)
		return true, 0
	}
	return false, attempt.lockedUntil.Sub(now)
}

func (limiter *LoginLimiter) RecordFailure(clientIP string) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()
	attempt := limiter.attempts[clientIP]
	attempt.failures++
	if attempt.failures >= loginFailureLimit {
		attempt.lockedUntil = limiter.clock().Add(loginLockDuration)
	}
	limiter.attempts[clientIP] = attempt
}

func (limiter *LoginLimiter) RecordSuccess(clientIP string) {
	limiter.mutex.Lock()
	delete(limiter.attempts, clientIP)
	limiter.mutex.Unlock()
}
