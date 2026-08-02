package auth

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPasswordHasherStoresSaltedPBKDF2AndVerifiesInConstantFormat(t *testing.T) {
	hasher := NewPasswordHasher(bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if !strings.HasPrefix(encoded, "$pbkdf2-sha256$600000$") {
		t.Fatalf("encoded hash = %q, want PBKDF2-SHA256 parameters", encoded)
	}
	if strings.Contains(encoded, "correct horse battery staple") {
		t.Fatal("encoded hash contains plaintext password")
	}

	verified, err := hasher.Verify(encoded, "correct horse battery staple")
	if err != nil || !verified {
		t.Fatalf("Verify(correct) = %v, %v", verified, err)
	}
	verified, err = hasher.Verify(encoded, "wrong")
	if err != nil || verified {
		t.Fatalf("Verify(wrong) = %v, %v", verified, err)
	}
}

func TestPasswordHasherRejectsMalformedHashes(t *testing.T) {
	hasher := NewPasswordHasher(bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	for _, encoded := range []string{"", "plaintext", "$pbkdf2-sha256$1$bad$bad"} {
		if verified, err := hasher.Verify(encoded, "password"); err == nil || verified {
			t.Fatalf("Verify(%q) = %v, %v; want malformed rejection", encoded, verified, err)
		}
	}
}

func TestSessionManagerBindsClientAndCSRFAndExpiresAfter24Hours(t *testing.T) {
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	manager := NewSessionManager(bytes.NewReader(bytes.Repeat([]byte{0x24}, 256)), clock)

	session, err := manager.Create("198.51.100.8")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(session.Token) < 40 || len(session.CSRFToken) < 40 {
		t.Fatalf("session token lengths = %d/%d, want at least 40", len(session.Token), len(session.CSRFToken))
	}
	if !manager.Validate(session.Token, session.CSRFToken, "198.51.100.8") {
		t.Fatal("Validate() rejected correct token, CSRF, and client")
	}
	if manager.Validate(session.Token, "wrong", "198.51.100.8") {
		t.Fatal("Validate() accepted wrong CSRF token")
	}
	if manager.Validate(session.Token, session.CSRFToken, "203.0.113.9") {
		t.Fatal("Validate() accepted a different client IP")
	}

	now = now.Add(24*time.Hour + time.Nanosecond)
	if manager.Validate(session.Token, session.CSRFToken, "198.51.100.8") {
		t.Fatal("Validate() accepted an expired session")
	}
}

func TestSessionManagerRevokeAllInvalidatesExistingSessions(t *testing.T) {
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	manager := NewSessionManager(bytes.NewReader(bytes.Repeat([]byte{0x25}, 256)), func() time.Time { return now })
	session, err := manager.Create("198.51.100.8")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	manager.RevokeAll()
	if manager.Validate(session.Token, session.CSRFToken, "198.51.100.8") {
		t.Fatal("Validate() accepted a revoked session")
	}
}

func TestSessionManagerRevokeInvalidatesOnlyNamedSession(t *testing.T) {
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	entropy := append(bytes.Repeat([]byte{0x26}, 64), bytes.Repeat([]byte{0x27}, 64)...)
	manager := NewSessionManager(bytes.NewReader(entropy), func() time.Time { return now })
	first, err := manager.Create("198.51.100.8")
	if err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	second, err := manager.Create("203.0.113.9")
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}

	manager.Revoke(first.Token)
	if manager.Validate(first.Token, first.CSRFToken, first.ClientIP) {
		t.Fatal("Validate() accepted the revoked session")
	}
	if !manager.Validate(second.Token, second.CSRFToken, second.ClientIP) {
		t.Fatal("Revoke() invalidated an unrelated session")
	}
}

func TestLoginLimiterLocksOneIPAfterFiveFailuresFor15Minutes(t *testing.T) {
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	limiter := NewLoginLimiter(func() time.Time { return now })
	const clientIP = "198.51.100.8"

	for attempt := 1; attempt <= 5; attempt++ {
		if allowed, _ := limiter.Allow(clientIP); !allowed {
			t.Fatalf("attempt %d blocked before fifth failure", attempt)
		}
		limiter.RecordFailure(clientIP)
	}
	allowed, remaining := limiter.Allow(clientIP)
	if allowed || remaining != 15*time.Minute {
		t.Fatalf("Allow() after failures = %v, %s; want false, 15m", allowed, remaining)
	}
	if allowed, _ := limiter.Allow("203.0.113.9"); !allowed {
		t.Fatal("failure lock leaked to a different IP")
	}

	now = now.Add(15 * time.Minute)
	if allowed, remaining := limiter.Allow(clientIP); !allowed || remaining != 0 {
		t.Fatalf("Allow() after expiry = %v, %s; want true, 0", allowed, remaining)
	}
}

func TestLoginLimiterSuccessClearsFailureCount(t *testing.T) {
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	limiter := NewLoginLimiter(func() time.Time { return now })
	const clientIP = "198.51.100.8"

	for range 4 {
		limiter.RecordFailure(clientIP)
	}
	limiter.RecordSuccess(clientIP)
	for range 4 {
		limiter.RecordFailure(clientIP)
	}
	if allowed, _ := limiter.Allow(clientIP); !allowed {
		t.Fatal("successful login did not reset failure count")
	}
}
