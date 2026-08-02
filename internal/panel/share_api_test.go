package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
	"github.com/s12ryt/s12ryt-vps-sh/internal/share"
)

func TestShareRevealRequiresBoundSessionCSRFAndPassword(t *testing.T) {
	server, service, _ := newShareAPIServer(t)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")
	secretURI := service.bundle.Nodes[0].URI

	missingCSRF := performShareRevealRequest(t, server, cookie, "", "198.51.100.8", []byte(`{"password":"panel-password"}`))
	if missingCSRF.Code != http.StatusForbidden || strings.Contains(missingCSRF.Body.String(), secretURI) {
		t.Fatalf("missing CSRF response = %d %s", missingCSRF.Code, missingCSRF.Body.String())
	}
	differentClient := performShareRevealRequest(t, server, cookie, csrfToken, "203.0.113.9", []byte(`{"password":"panel-password"}`))
	if differentClient.Code != http.StatusUnauthorized || strings.Contains(differentClient.Body.String(), secretURI) {
		t.Fatalf("different client response = %d %s", differentClient.Code, differentClient.Body.String())
	}
	wrongPassword := performShareRevealRequest(t, server, cookie, csrfToken, "198.51.100.8", []byte(`{"password":"wrong"}`))
	if wrongPassword.Code != http.StatusUnauthorized || strings.Contains(wrongPassword.Body.String(), secretURI) {
		t.Fatalf("wrong password response = %d %s", wrongPassword.Code, wrongPassword.Body.String())
	}
	if service.calls != 0 {
		t.Fatalf("share service calls before authentication = %d, want 0", service.calls)
	}

	revealed := performShareRevealRequest(t, server, cookie, csrfToken, "198.51.100.8", []byte(`{"password":"panel-password"}`))
	if revealed.Code != http.StatusOK {
		t.Fatalf("reveal status = %d, want 200: %s", revealed.Code, revealed.Body.String())
	}
	var payload struct {
		Nodes []struct {
			NodeID              string `json:"node_id"`
			URI                 string `json:"uri"`
			ClientJSON          string `json:"client_json"`
			FullClientJSON      string `json:"full_client_json"`
			FullClientBase64    string `json:"full_client_base64"`
			SplitRoutingWarning string `json:"split_routing_warning"`
			QRURL               string `json:"qr_url"`
		} `json:"nodes"`
		Subscription     string `json:"subscription"`
		ExpiresInSeconds int64  `json:"expires_in_seconds"`
	}
	if err := json.Unmarshal(revealed.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode reveal response: %v", err)
	}
	if len(payload.Nodes) != 1 {
		t.Fatalf("revealed nodes = %d, want 1", len(payload.Nodes))
	}
	node := payload.Nodes[0]
	if node.NodeID != "edge-v6" || node.URI != secretURI || node.ClientJSON != `{"outbounds":[{"uuid":"secret-uuid"}]}` {
		t.Fatalf("revealed node = %+v", node)
	}
	if node.FullClientJSON != `{"route":"split"}` || node.FullClientBase64 != "eyJyb3V0ZSI6InNwbGl0In0=" || node.SplitRoutingWarning == "" {
		t.Fatalf("full client output = %+v", node)
	}
	if node.QRURL != "/abcdefghijkl/api/shares/edge-v6/qr" {
		t.Fatalf("QR URL = %q", node.QRURL)
	}
	if payload.Subscription != "c2VjcmV0LXN1YnNjcmlwdGlvbg==" || payload.ExpiresInSeconds != 300 {
		t.Fatalf("share response = %+v", payload)
	}
	if service.calls != 1 {
		t.Fatalf("share service calls = %d, want 1", service.calls)
	}
}

func TestShareElevationScopesQRToSessionClientAndFiveMinutes(t *testing.T) {
	server, service, now := newShareAPIServer(t)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")

	revealed := performShareRevealRequest(t, server, cookie, csrfToken, "198.51.100.8", []byte(`{"password":"panel-password"}`))
	if revealed.Code != http.StatusOK {
		t.Fatalf("initial reveal status = %d, want 200", revealed.Code)
	}
	qr := performShareQRRequest(t, server, "edge-v6", cookie, "198.51.100.8")
	if qr.Code != http.StatusOK {
		t.Fatalf("QR status = %d, want 200: %s", qr.Code, qr.Body.String())
	}
	if qr.Header().Get("Content-Type") != "image/png" || qr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("QR headers = %#v", qr.Header())
	}
	if !bytes.Equal(qr.Body.Bytes(), service.bundle.Nodes[0].QRPNG) {
		t.Fatalf("QR body = %x", qr.Body.Bytes())
	}

	differentClient := performShareQRRequest(t, server, "edge-v6", cookie, "203.0.113.9")
	if differentClient.Code != http.StatusUnauthorized {
		t.Fatalf("different client QR status = %d, want 401", differentClient.Code)
	}
	secondCookie, _ := authenticatedSession(t, server, "203.0.113.9")
	otherSession := performShareQRRequest(t, server, "edge-v6", secondCookie, "203.0.113.9")
	if otherSession.Code != http.StatusUnauthorized {
		t.Fatalf("other session QR status = %d, want 401", otherSession.Code)
	}

	*now = now.Add(5 * time.Minute)
	expired := performShareQRRequest(t, server, "edge-v6", cookie, "198.51.100.8")
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired QR status = %d, want 401", expired.Code)
	}
}

func TestShareRevealHandlesUnavailableAndFailedServiceWithoutLeakingErrors(t *testing.T) {
	server, service, _ := newShareAPIServer(t)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")
	sentinel := errors.New("secret backend detail")
	service.err = sentinel

	failed := performShareRevealRequest(t, server, cookie, csrfToken, "198.51.100.8", []byte(`{"password":"panel-password"}`))
	if failed.Code != http.StatusBadGateway || strings.Contains(failed.Body.String(), sentinel.Error()) {
		t.Fatalf("failed service response = %d %s", failed.Code, failed.Body.String())
	}

	server.shareService = nil
	unavailable := performShareRevealRequest(t, server, cookie, csrfToken, "198.51.100.8", []byte(`{}`))
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable service status = %d, want 503", unavailable.Code)
	}
}

type fakeShareService struct {
	bundle share.Bundle
	err    error
	calls  int
}

func (service *fakeShareService) Bundle(context.Context) (share.Bundle, error) {
	service.calls++
	return service.bundle, service.err
}

func newShareAPIServer(t *testing.T) (*Server, *fakeShareService, *time.Time) {
	t.Helper()
	hasher := auth.NewPasswordHasher(bytes.NewReader(bytes.Repeat([]byte{0x71}, 64)))
	passwordHash, err := hasher.Hash("panel-password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	service := &fakeShareService{bundle: share.Bundle{
		Nodes: []share.Artifact{{
			NodeID:              "edge-v6",
			URI:                 "vless://secret-uuid@[2001:db8::10]:24443#edge",
			QRPayload:           "vless://secret-uuid@[2001:db8::10]:24443#edge",
			QRPNG:               []byte{0x89, 'P', 'N', 'G'},
			ClientJSON:          []byte(`{"outbounds":[{"uuid":"secret-uuid"}]}`),
			FullClientJSON:      []byte(`{"route":"split"}`),
			FullClientBase64:    "eyJyb3V0ZSI6InNwbGl0In0=",
			SplitRoutingWarning: "URI 與 QR 不含分流規則。",
		}},
		Subscription: "c2VjcmV0LXN1YnNjcmlwdGlvbg==",
	}}
	sessionEntropy := append(
		bytes.Repeat([]byte{0x72}, 64),
		bytes.Repeat([]byte{0x73}, 64)...,
	)
	server := NewServer(Options{
		BasePath:     "/abcdefghijkl",
		PasswordHash: passwordHash,
		Hasher:       hasher,
		Sessions: auth.NewSessionManager(
			bytes.NewReader(sessionEntropy),
			clock,
		),
		Limiter:      auth.NewLoginLimiter(clock),
		Config:       newFakeNodeManager().Snapshot(),
		ShareService: service,
		Clock:        clock,
	})
	return server, service, &now
}

func performShareRevealRequest(t *testing.T, server *Server, cookie *http.Cookie, csrfToken string, clientIP string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://panel.test/abcdefghijkl/api/shares/reveal", bytes.NewReader(body))
	request.RemoteAddr = clientIP + ":41234"
	request.Header.Set("Content-Type", "application/json")
	if csrfToken != "" {
		request.Header.Set("X-CSRF-Token", csrfToken)
	}
	request.AddCookie(cookie)
	server.Handler().ServeHTTP(response, request)
	return response
}

func performShareQRRequest(t *testing.T, server *Server, nodeID string, cookie *http.Cookie, clientIP string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://panel.test/abcdefghijkl/api/shares/"+nodeID+"/qr", nil)
	request.RemoteAddr = clientIP + ":41234"
	request.AddCookie(cookie)
	server.Handler().ServeHTTP(response, request)
	return response
}
