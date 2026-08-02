package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/manifest"
	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
	"github.com/s12ryt/s12ryt-vps-sh/internal/networksetup"
	"github.com/s12ryt/s12ryt-vps-sh/internal/nodes"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
	projectshare "github.com/s12ryt/s12ryt-vps-sh/internal/share"
)

const SessionCookieName = "s12ryt_panel_session"

const maxConfigBodySize = 1 << 20

const maxRemoteRequestBodySize = 6*(1<<20) + 4096

const credentialElevationLifetime = 5 * time.Minute

var remoteTagPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

type ConfigStore interface {
	Save(domain.Config) error
}

type NodeManager interface {
	Snapshot() domain.Config
	ReplaceConfig(domain.Config) error
	Create(nodes.CreateInput) (domain.Node, error)
	Update(nodes.UpdateInput) (domain.Node, error)
	Delete(string) error
}

type RemoteManager interface {
	RemoteOutbounds() []nodes.RemoteOutboundSummary
	ImportRemoteOutbounds(nodes.ImportRemoteInput) ([]nodes.RemoteOutboundSummary, error)
	UpdateRemoteOutbound(string, bool) (nodes.RemoteOutboundSummary, error)
	DeleteRemoteOutbound(string) error
	SetIPv4Fallback([]string) error
}

type NetworkManager interface {
	GlobalIPv6Addresses(context.Context) ([]projectnetwork.InterfaceAddress, error)
	Apply(context.Context, networksetup.Request) (manifest.Manifest, error)
}

type ACMEChallengeChecker interface {
	Available(context.Context) (bool, error)
}

type ShareService interface {
	Bundle(context.Context) (projectshare.Bundle, error)
}

type Options struct {
	BasePath             string
	PasswordHash         string
	Hasher               *auth.PasswordHasher
	Sessions             *auth.SessionManager
	Limiter              *auth.LoginLimiter
	Config               domain.Config
	Store                ConfigStore
	NodeManager          NodeManager
	RemoteManager        RemoteManager
	NetworkManager       NetworkManager
	ACMEChallengeChecker ACMEChallengeChecker
	ShareService         ShareService
	Clock                func() time.Time
}

type Server struct {
	basePath             string
	passwordHash         string
	hasher               *auth.PasswordHasher
	sessions             *auth.SessionManager
	limiter              *auth.LoginLimiter
	mutex                sync.RWMutex
	config               domain.Config
	store                ConfigStore
	nodeManager          NodeManager
	remoteManager        RemoteManager
	networkManager       NetworkManager
	acmeChallengeChecker ACMEChallengeChecker
	shareService         ShareService
	clock                func() time.Time
	elevationMu          sync.Mutex
	elevations           map[string]time.Time
}

func NewServer(options Options) *Server {
	config := options.Config
	if options.NodeManager != nil {
		config = options.NodeManager.Snapshot()
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Server{
		basePath:             strings.TrimRight(options.BasePath, "/"),
		passwordHash:         options.PasswordHash,
		hasher:               options.Hasher,
		sessions:             options.Sessions,
		limiter:              options.Limiter,
		config:               config,
		store:                options.Store,
		nodeManager:          options.NodeManager,
		remoteManager:        options.RemoteManager,
		networkManager:       options.NetworkManager,
		acmeChallengeChecker: options.ACMEChallengeChecker,
		shareService:         options.ShareService,
		clock:                clock,
		elevations:           make(map[string]time.Time),
	}
}

func (server *Server) Handler() http.Handler {
	return http.HandlerFunc(server.serveHTTP)
}

func (server *Server) serveHTTP(response http.ResponseWriter, request *http.Request) {
	setSecurityHeaders(response)
	switch {
	case request.Method == http.MethodGet && request.URL.Path == server.basePath+"/healthz":
		server.showHealth(response)
	case request.Method == http.MethodGet && request.URL.Path == server.basePath:
		server.showPanel(response, request)
	case request.Method == http.MethodPost && request.URL.Path == server.basePath+"/login":
		server.handleLogin(response, request)
	case request.Method == http.MethodPost && request.URL.Path == server.basePath+"/logout":
		server.handleLogout(response, request)
	case request.Method == http.MethodPost && request.URL.Path == server.basePath+"/api/config/validate":
		server.validateConfigRequest(response, request)
	case request.Method == http.MethodGet && request.URL.Path == server.basePath+"/api/config":
		server.getConfig(response, request)
	case request.Method == http.MethodPost && request.URL.Path == server.basePath+"/api/config/apply":
		server.applyConfig(response, request)
	case request.Method == http.MethodGet && request.URL.Path == server.basePath+"/api/nodes":
		server.listNodes(response, request)
	case request.Method == http.MethodPost && request.URL.Path == server.basePath+"/api/nodes":
		server.createNode(response, request)
	case request.Method == http.MethodGet && request.URL.Path == server.basePath+"/api/remotes":
		server.listRemoteOutbounds(response, request)
	case request.Method == http.MethodPost && request.URL.Path == server.basePath+"/api/remotes":
		server.importRemoteOutbounds(response, request)
	case strings.HasPrefix(request.URL.Path, server.basePath+"/api/remotes/"):
		server.handleNamedRemoteOutbound(response, request)
	case request.Method == http.MethodPut && request.URL.Path == server.basePath+"/api/ipv4-fallback":
		server.setIPv4Fallback(response, request)
	case request.Method == http.MethodGet && request.URL.Path == server.basePath+"/api/network/addresses":
		server.listNetworkAddresses(response, request)
	case request.Method == http.MethodPost && request.URL.Path == server.basePath+"/api/network/apply":
		server.applyNetworkIntegration(response, request)
	case request.Method == http.MethodPost && request.URL.Path == server.basePath+"/api/shares/reveal":
		server.revealShares(response, request)
	case strings.HasPrefix(request.URL.Path, server.basePath+"/api/shares/"):
		server.serveShareArtifact(response, request)
	case strings.HasPrefix(request.URL.Path, server.basePath+"/api/nodes/"):
		server.handleNamedNode(response, request)
	default:
		http.NotFound(response, request)
	}
}

type shareArtifactResponse struct {
	NodeID              string `json:"node_id"`
	URI                 string `json:"uri"`
	ClientJSON          string `json:"client_json"`
	FullClientJSON      string `json:"full_client_json,omitempty"`
	FullClientBase64    string `json:"full_client_base64,omitempty"`
	SplitRoutingWarning string `json:"split_routing_warning,omitempty"`
	QRURL               string `json:"qr_url,omitempty"`
}

type shareRevealResponse struct {
	Nodes            []shareArtifactResponse `json:"nodes"`
	Subscription     string                  `json:"subscription"`
	ExpiresInSeconds int64                   `json:"expires_in_seconds"`
}

func (server *Server) revealShares(response http.ResponseWriter, request *http.Request) {
	if server.shareService == nil {
		http.Error(response, "分享服務不可用。", http.StatusServiceUnavailable)
		return
	}
	session, cookie, authorized := server.authorizeCredentialReveal(response, request)
	if !authorized {
		return
	}
	var input credentialRevealRequest
	if !decodeStrictJSON(response, request, &input) {
		return
	}
	expiresAt, elevated := server.credentialElevation(cookie.Value)
	if !elevated {
		if input.Password == "" {
			http.Error(response, "請重新輸入管理密碼。", http.StatusUnauthorized)
			return
		}
		verified, err := server.hasher.Verify(server.passwordHash, input.Password)
		if err != nil {
			http.Error(response, "面板認證設定無效。", http.StatusInternalServerError)
			return
		}
		if !verified {
			http.Error(response, "管理密碼錯誤。", http.StatusUnauthorized)
			return
		}
	}
	bundle, err := server.shareService.Bundle(request.Context())
	if err != nil {
		http.Error(response, "無法建立分享資料。", http.StatusBadGateway)
		return
	}
	if !elevated {
		expiresAt = server.clock().Add(credentialElevationLifetime)
		server.setCredentialElevation(session.Token, expiresAt)
	}
	remaining := expiresAt.Sub(server.clock()) / time.Second
	if remaining < 0 {
		remaining = 0
	}
	result := shareRevealResponse{
		Nodes:            make([]shareArtifactResponse, 0, len(bundle.Nodes)),
		Subscription:     bundle.Subscription,
		ExpiresInSeconds: int64(remaining),
	}
	for _, artifact := range bundle.Nodes {
		node := shareArtifactResponse{
			NodeID:              artifact.NodeID,
			URI:                 artifact.URI,
			ClientJSON:          string(artifact.ClientJSON),
			FullClientJSON:      string(artifact.FullClientJSON),
			FullClientBase64:    artifact.FullClientBase64,
			SplitRoutingWarning: artifact.SplitRoutingWarning,
		}
		if len(artifact.QRPNG) > 0 {
			node.QRURL = server.basePath + "/api/shares/" + url.PathEscape(artifact.NodeID) + "/qr"
		}
		result.Nodes = append(result.Nodes, node)
	}
	writeJSON(response, http.StatusOK, result)
}

func (server *Server) serveShareArtifact(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", "GET")
		http.Error(response, "不支援的分享操作。", http.StatusMethodNotAllowed)
		return
	}
	remainder := strings.TrimPrefix(request.URL.Path, server.basePath+"/api/shares/")
	if !strings.HasSuffix(remainder, "/qr") {
		http.NotFound(response, request)
		return
	}
	nodeID, err := url.PathUnescape(strings.TrimSuffix(remainder, "/qr"))
	if err != nil || !remoteTagPattern.MatchString(nodeID) {
		http.Error(response, "分享節點 ID 無效。", http.StatusBadRequest)
		return
	}
	if server.shareService == nil {
		http.Error(response, "分享服務不可用。", http.StatusServiceUnavailable)
		return
	}
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		http.Error(response, "需要登入。", http.StatusUnauthorized)
		return
	}
	if _, valid := server.sessions.Lookup(cookie.Value, requestClientIP(request)); !valid {
		http.Error(response, "登入工作階段無效。", http.StatusUnauthorized)
		return
	}
	if _, elevated := server.credentialElevation(cookie.Value); !elevated {
		http.Error(response, "請重新輸入管理密碼。", http.StatusUnauthorized)
		return
	}
	bundle, bundleErr := server.shareService.Bundle(request.Context())
	if bundleErr != nil {
		http.Error(response, "無法建立分享資料。", http.StatusBadGateway)
		return
	}
	for _, artifact := range bundle.Nodes {
		if artifact.NodeID != nodeID {
			continue
		}
		if len(artifact.QRPNG) == 0 {
			http.Error(response, "此節點沒有 QR 圖片。", http.StatusNotFound)
			return
		}
		response.Header().Set("Content-Type", "image/png")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(artifact.QRPNG)
		return
	}
	http.Error(response, "找不到分享節點。", http.StatusNotFound)
}

type networkAddressSummary struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	Prefix    string `json:"prefix"`
}

type networkApplyRequest struct {
	Interface       string                  `json:"interface"`
	Prefix          string                  `json:"prefix"`
	Count           int                     `json:"count"`
	FirewallBackend string                  `json:"firewall_backend"`
	PanelPort       int                     `json:"panel_port"`
	AllowedCIDRs    []string                `json:"allowed_cidrs"`
	NodePorts       []manifest.PortManifest `json:"node_ports"`
}

type networkApplyResponse struct {
	Interface       string `json:"interface"`
	AddressCount    int    `json:"address_count"`
	FirewallBackend string `json:"firewall_backend"`
}

func (server *Server) listNetworkAddresses(response http.ResponseWriter, request *http.Request) {
	if _, valid := server.requestSession(request); !valid {
		http.Error(response, "需要登入。", http.StatusUnauthorized)
		return
	}
	if server.networkManager == nil {
		http.Error(response, "網路整合服務不可用。", http.StatusServiceUnavailable)
		return
	}
	addresses, err := server.networkManager.GlobalIPv6Addresses(request.Context())
	if err != nil {
		http.Error(response, "無法取得全域 IPv6 地址。", http.StatusBadGateway)
		return
	}
	summaries := make([]networkAddressSummary, 0, len(addresses))
	for _, address := range addresses {
		summaries = append(summaries, networkAddressSummary{
			Interface: address.Interface,
			Address:   projectnetwork.FormatIPv6Full(address.Prefix.Addr()),
			Prefix:    address.Prefix.String(),
		})
	}
	writeJSON(response, http.StatusOK, summaries)
}

func (server *Server) applyNetworkIntegration(response http.ResponseWriter, request *http.Request) {
	if !server.authorizeStateChange(response, request) {
		return
	}
	if request.Header.Get("X-S12ryt-Confirm") != "apply" {
		http.Error(response, "網路整合變更需要明確確認。", http.StatusConflict)
		return
	}
	if server.networkManager == nil {
		http.Error(response, "網路整合服務不可用。", http.StatusServiceUnavailable)
		return
	}
	var input networkApplyRequest
	if !decodeStrictJSON(response, request, &input) {
		return
	}
	result, err := server.networkManager.Apply(request.Context(), networksetup.Request{
		Interface: input.Interface, Prefix: input.Prefix, Count: input.Count,
		FirewallBackend: input.FirewallBackend, PanelPort: input.PanelPort,
		AllowedCIDRs: append([]string(nil), input.AllowedCIDRs...),
		NodePorts:    append([]manifest.PortManifest(nil), input.NodePorts...),
	})
	if err != nil {
		http.Error(response, "無法套用網路整合設定。", http.StatusUnprocessableEntity)
		return
	}
	writeJSON(response, http.StatusOK, networkApplyResponse{
		Interface: result.Interface, AddressCount: len(result.Addresses),
		FirewallBackend: result.Firewall.Backend,
	})
}

func (server *Server) showHealth(response http.ResponseWriter) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(response, "{\"status\":\"ok\"}\n")
}

func setSecurityHeaders(response http.ResponseWriter) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("X-Frame-Options", "DENY")
}

func (server *Server) showPanel(response http.ResponseWriter, request *http.Request) {
	session, authenticated := server.requestSession(request)
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	if !authenticated {
		_, _ = fmt.Fprintf(response, loginPage, html.EscapeString(server.basePath+"/login"))
		return
	}
	server.mutex.RLock()
	config := server.config
	server.mutex.RUnlock()
	var page bytes.Buffer
	_, _ = fmt.Fprintf(
		&page,
		dashboardPage,
		html.EscapeString(session.CSRFToken),
		html.EscapeString(server.basePath+"/api/config"),
		html.EscapeString(server.basePath+"/api/config/validate"),
		html.EscapeString(server.basePath+"/api/config/apply"),
		html.EscapeString(server.basePath+"/logout"),
		html.EscapeString(session.CSRFToken),
		checked(config.Routing.Mode == domain.RoutingModeClientIPv4),
		checked(config.Routing.Mode == domain.RoutingModeVPSIPv4),
		checked(config.Routing.Mode == domain.RoutingModeIPv6Only),
		checked(config.Routing.Topology == domain.TopologyMultiIPv6MultiNode),
		checked(config.Routing.Topology == domain.TopologySingleIPv6SingleNode),
		checked(config.Routing.Topology == domain.TopologyMultiIPv6RotatingNode),
		checked(config.Routing.Topology == domain.TopologyMultiIPv6RotatingNodes),
	)
	rendered := strings.Replace(page.String(), `</head>`, nocDashboardStyle+mobileDashboardStyle+accessibilityStyle+`</head>`, 1)
	rendered = strings.Replace(rendered, `<html lang="zh-Hant">`, `<html lang="zh-Hant" data-ui-theme="noc">`, 1)
	rendered = strings.Replace(
		rendered,
		`<main class="shell"`,
		`<main class="shell" data-nodes-endpoint="`+html.EscapeString(server.basePath+"/api/nodes")+`" data-credential-endpoint-template="`+html.EscapeString(server.basePath+"/api/nodes/{id}/credential")+`" data-remotes-endpoint="`+html.EscapeString(server.basePath+"/api/remotes")+`" data-ipv4-fallback-endpoint="`+html.EscapeString(server.basePath+"/api/ipv4-fallback")+`" data-network-addresses-endpoint="`+html.EscapeString(server.basePath+"/api/network/addresses")+`" data-network-apply-endpoint="`+html.EscapeString(server.basePath+"/api/network/apply")+`" data-share-reveal-endpoint="`+html.EscapeString(server.basePath+"/api/shares/reveal")+`" data-share-qr-endpoint-template="`+html.EscapeString(server.basePath+"/api/shares/{id}/qr")+`"`,
		1,
	)
	nodeWorkspace := strings.Replace(nodeWorkspaceHTML, `<section class="node-workspace" aria-labelledby="nodes-title">`, `<section id="workspace-nodes" class="workspace-panel node-workspace" data-workspace="nodes" role="tabpanel" aria-labelledby="workspace-tab-nodes" hidden>`, 1)
	remoteWorkspace := strings.Replace(remoteWorkspaceHTML, `<section class="remote-workspace" data-remote-workspace aria-labelledby="remotes-title">`, `<section id="workspace-remotes" class="workspace-panel remote-workspace" data-workspace="remotes" role="tabpanel" aria-labelledby="workspace-tab-remotes" data-remote-workspace hidden>`, 1)
	networkWorkspace := strings.Replace(networkWorkspaceHTML, `<section class="network-workspace" data-network-workspace aria-labelledby="network-title">`, `<section id="workspace-network" class="workspace-panel network-workspace" data-workspace="network" role="tabpanel" aria-labelledby="workspace-tab-network" data-network-workspace hidden>`, 1)
	shareWorkspace := strings.Replace(shareWorkspaceHTML, `<section class="share-workspace" data-share-workspace aria-labelledby="shares-title">`, `<section id="workspace-shares" class="workspace-panel share-workspace" data-workspace="shares" role="tabpanel" aria-labelledby="workspace-tab-shares" data-share-workspace hidden>`, 1)
	rendered = strings.Replace(rendered, `<body><main class="shell"`, `<body><a class="skip-link" href="#main-content">跳到主要內容</a><div class="operation-notice" data-operation-notice role="status" aria-live="polite" aria-atomic="true" hidden></div><div class="dashboard-frame">`+workspaceNavigationHTML+`<main id="main-content" tabindex="-1" class="shell"`, 1)
	rendered = strings.Replace(rendered, `<nav aria-label="設定導覽">`, `<div class="strategy-actions" data-strategy-actions aria-label="策略操作">`, 1)
	rendered = strings.Replace(rendered, `</nav><section class="grid">`, `</div><section id="workspace-strategy" class="workspace-panel" data-workspace="strategy" role="tabpanel" aria-labelledby="workspace-tab-strategy"><div class="grid">`, 1)
	rendered = strings.Replace(rendered, `</section></main>`, `</div></section>`+nodeWorkspace+remoteWorkspace+networkWorkspace+shareWorkspace+`</main></div>`, 1)
	rendered = strings.Replace(rendered, `<script>`, nodeModalsHTML+remoteModalHTML+networkModalHTML+shareModalHTML+`<script>`, 1)
	rendered = strings.Replace(rendered, `<label class="choice"><input name="node_enabled"`, nodeDeploymentFieldsHTML+`<label class="choice"><input name="node_enabled"`, 1)
	rendered = strings.Replace(rendered, `</script>`, workspaceNavigationScript+modalAccessibilityScript+operationStateScript+nodeManagementScript+nodeDeploymentScript+remoteManagementScript+networkManagementScript+shareManagementScript+mutationReliabilityScript+`</script>`, 1)
	_, _ = io.WriteString(response, rendered)
}

func checked(value bool) string {
	if value {
		return " checked"
	}
	return ""
}

func (server *Server) handleLogin(response http.ResponseWriter, request *http.Request) {
	clientIP := requestClientIP(request)
	if allowed, _ := server.limiter.Allow(clientIP); !allowed {
		http.Error(response, "登入嘗試過多，請稍後再試。", http.StatusTooManyRequests)
		return
	}
	if err := request.ParseForm(); err != nil {
		http.Error(response, "無效的登入資料。", http.StatusBadRequest)
		return
	}
	verified, err := server.hasher.Verify(server.passwordHash, request.FormValue("password"))
	if err != nil {
		http.Error(response, "面板認證設定無效。", http.StatusInternalServerError)
		return
	}
	if !verified {
		server.limiter.RecordFailure(clientIP)
		http.Error(response, "密碼錯誤。", http.StatusUnauthorized)
		return
	}
	server.limiter.RecordSuccess(clientIP)
	session, err := server.sessions.Create(clientIP)
	if err != nil {
		http.Error(response, "無法建立登入工作階段。", http.StatusInternalServerError)
		return
	}
	http.SetCookie(response, &http.Cookie{
		Name:     SessionCookieName,
		Value:    session.Token,
		Path:     server.basePath,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(response, request, server.basePath, http.StatusSeeOther)
}

func (server *Server) handleLogout(response http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		http.Error(response, "需要登入。", http.StatusUnauthorized)
		return
	}
	clientIP := requestClientIP(request)
	session, valid := server.sessions.Lookup(cookie.Value, clientIP)
	if !valid {
		http.Error(response, "登入工作階段無效。", http.StatusUnauthorized)
		return
	}
	csrfToken := request.Header.Get("X-CSRF-Token")
	if csrfToken == "" {
		if err := request.ParseForm(); err == nil {
			csrfToken = request.FormValue("csrf_token")
		}
	}
	if !server.sessions.Validate(cookie.Value, csrfToken, clientIP) {
		http.Error(response, "CSRF 驗證失敗。", http.StatusForbidden)
		return
	}
	server.sessions.Revoke(session.Token)
	server.revokeCredentialElevation(session.Token)
	http.SetCookie(response, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     server.basePath,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	http.Redirect(response, request, server.basePath, http.StatusSeeOther)
}

func (server *Server) validateConfigRequest(response http.ResponseWriter, request *http.Request) {
	if !server.authorizeStateChange(response, request) {
		return
	}
	candidate, valid := server.decodeCandidateConfig(response, request)
	if !valid {
		return
	}
	server.mutex.RLock()
	changed := !reflect.DeepEqual(server.config, candidate)
	server.mutex.RUnlock()
	writeJSON(response, http.StatusOK, map[string]bool{"changed": changed, "valid": true})
}

func (server *Server) getConfig(response http.ResponseWriter, request *http.Request) {
	if _, valid := server.requestSession(request); !valid {
		http.Error(response, "需要登入。", http.StatusUnauthorized)
		return
	}
	writeJSON(response, http.StatusOK, editableConfigFrom(server.currentConfig()))
}

func (server *Server) applyConfig(response http.ResponseWriter, request *http.Request) {
	if !server.authorizeStateChange(response, request) {
		return
	}
	if request.Header.Get("X-S12ryt-Confirm") != "apply" {
		http.Error(response, "套用設定需要明確確認。", http.StatusConflict)
		return
	}
	candidate, valid := server.decodeCandidateConfig(response, request)
	if !valid {
		return
	}
	if server.nodeManager != nil {
		if err := server.nodeManager.ReplaceConfig(candidate); err != nil {
			http.Error(response, "設定包含不允許的節點變更。", http.StatusUnprocessableEntity)
			return
		}
		server.replaceCurrentConfig(server.nodeManager.Snapshot())
		writeJSON(response, http.StatusOK, map[string]bool{"applied": true})
		return
	}
	if server.store == nil {
		http.Error(response, "設定儲存服務不可用。", http.StatusServiceUnavailable)
		return
	}
	if err := server.store.Save(candidate); err != nil {
		http.Error(response, "無法保存設定。", http.StatusInternalServerError)
		return
	}
	server.replaceCurrentConfig(candidate)
	writeJSON(response, http.StatusOK, map[string]bool{"applied": true})
}

type nodeSummary struct {
	ID                   string          `json:"id"`
	Protocol             domain.Protocol `json:"protocol"`
	Port                 int             `json:"port"`
	Enabled              bool            `json:"enabled"`
	CredentialConfigured bool            `json:"credential_configured"`
}

type createNodeRequest struct {
	ID         string                 `json:"id"`
	Protocol   domain.Protocol        `json:"protocol"`
	Port       int                    `json:"port"`
	Enabled    bool                   `json:"enabled"`
	Deployment *nodeDeploymentRequest `json:"deployment"`
}

type nodeDeploymentRequest struct {
	Listeners []netip.Addr                           `json:"listeners"`
	TLS       runtimeconfig.PersistedTLSConfig       `json:"tls"`
	Transport runtimeconfig.PersistedTransportConfig `json:"transport"`
}

type updateNodeRequest struct {
	Port    int  `json:"port"`
	Enabled bool `json:"enabled"`
}

type editableConfig struct {
	SchemaVersion int                   `json:"schema_version"`
	Panel         domain.PanelConfig    `json:"panel"`
	IPv6          domain.IPv6PoolConfig `json:"ipv6"`
	Routing       domain.RoutingConfig  `json:"routing"`
	Health        domain.HealthConfig   `json:"health"`
	Nodes         []nodeSummary         `json:"nodes"`
}

func (server *Server) listNodes(response http.ResponseWriter, request *http.Request) {
	if _, valid := server.requestSession(request); !valid {
		http.Error(response, "需要登入。", http.StatusUnauthorized)
		return
	}
	if server.nodeManager == nil {
		http.Error(response, "節點管理服務不可用。", http.StatusServiceUnavailable)
		return
	}
	config := server.nodeManager.Snapshot()
	summaries := make([]nodeSummary, 0, len(config.Nodes))
	for _, node := range config.Nodes {
		summaries = append(summaries, summarizeNode(node))
	}
	writeJSON(response, http.StatusOK, summaries)
}

func (server *Server) createNode(response http.ResponseWriter, request *http.Request) {
	if !server.authorizeNodeMutation(response, request) {
		return
	}
	var input createNodeRequest
	if !decodeStrictJSON(response, request, &input) {
		return
	}
	if input.Deployment == nil {
		http.Error(response, "節點部署設定不可省略。", http.StatusBadRequest)
		return
	}
	if input.Deployment.TLS.ACME != nil && !server.checkACMEChallengeAvailability(response, request) {
		return
	}
	node, err := server.nodeManager.Create(nodes.CreateInput{
		ID: input.ID, Protocol: input.Protocol, Port: input.Port, Enabled: input.Enabled,
		Deployment: runtimeconfig.PersistedNodeDeployment{
			NodeID:    input.ID,
			Listeners: append([]netip.Addr(nil), input.Deployment.Listeners...),
			TLS:       input.Deployment.TLS,
			Transport: input.Deployment.Transport,
		},
	})
	if err != nil {
		http.Error(response, "無法建立節點。", http.StatusUnprocessableEntity)
		return
	}
	server.replaceCurrentConfig(server.nodeManager.Snapshot())
	writeJSON(response, http.StatusCreated, summarizeNode(node))
}

func (server *Server) checkACMEChallengeAvailability(response http.ResponseWriter, request *http.Request) bool {
	if server.acmeChallengeChecker == nil {
		http.Error(response, "ACME HTTP-01 前置檢查服務不可用。", http.StatusServiceUnavailable)
		return false
	}
	available, err := server.acmeChallengeChecker.Available(request.Context())
	if err != nil {
		http.Error(response, "無法檢查 ACME HTTP-01 的 TCP/80。", http.StatusUnprocessableEntity)
		return false
	}
	if !available {
		http.Error(response, "TCP/80 已被占用，無法使用 ACME HTTP-01。", http.StatusConflict)
		return false
	}
	return true
}

func (server *Server) handleNamedNode(response http.ResponseWriter, request *http.Request) {
	remainder := strings.TrimPrefix(request.URL.Path, server.basePath+"/api/nodes/")
	if strings.HasSuffix(remainder, "/credential") {
		id := strings.TrimSuffix(remainder, "/credential")
		if id == "" || strings.Contains(id, "/") {
			http.Error(response, "節點 ID 無效。", http.StatusBadRequest)
			return
		}
		if request.Method != http.MethodPost {
			response.Header().Set("Allow", "POST")
			http.Error(response, "不支援的憑證操作。", http.StatusMethodNotAllowed)
			return
		}
		server.revealNodeCredential(response, request, id)
		return
	}
	id := remainder
	if id == "" || strings.Contains(id, "/") {
		http.Error(response, "節點 ID 無效。", http.StatusBadRequest)
		return
	}
	switch request.Method {
	case http.MethodPatch:
		server.updateNode(response, request, id)
	case http.MethodDelete:
		server.deleteNode(response, request, id)
	default:
		response.Header().Set("Allow", "PATCH, DELETE")
		http.Error(response, "不支援的節點操作。", http.StatusMethodNotAllowed)
	}
}

type credentialRevealRequest struct {
	Password string `json:"password"`
}

type credentialRevealResponse struct {
	Credential       domain.NodeCredential `json:"credential"`
	ExpiresInSeconds int64                 `json:"expires_in_seconds"`
}

func (server *Server) revealNodeCredential(response http.ResponseWriter, request *http.Request, id string) {
	session, cookie, authorized := server.authorizeCredentialReveal(response, request)
	if !authorized {
		return
	}
	var input credentialRevealRequest
	if !decodeStrictJSON(response, request, &input) {
		return
	}

	expiresAt, elevated := server.credentialElevation(cookie.Value)
	if !elevated {
		if input.Password == "" {
			http.Error(response, "請重新輸入管理密碼。", http.StatusUnauthorized)
			return
		}
		verified, err := server.hasher.Verify(server.passwordHash, input.Password)
		if err != nil {
			http.Error(response, "面板認證設定無效。", http.StatusInternalServerError)
			return
		}
		if !verified {
			http.Error(response, "管理密碼錯誤。", http.StatusUnauthorized)
			return
		}
	}

	credential, found := server.nodeCredential(id)
	if !found {
		http.Error(response, "找不到節點。", http.StatusNotFound)
		return
	}
	if !elevated {
		expiresAt = server.clock().Add(credentialElevationLifetime)
		server.setCredentialElevation(session.Token, expiresAt)
	}
	remaining := expiresAt.Sub(server.clock()) / time.Second
	if remaining < 0 {
		remaining = 0
	}
	writeJSON(response, http.StatusOK, credentialRevealResponse{
		Credential:       credential,
		ExpiresInSeconds: int64(remaining),
	})
}

func (server *Server) authorizeCredentialReveal(response http.ResponseWriter, request *http.Request) (auth.Session, *http.Cookie, bool) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		http.Error(response, "需要登入。", http.StatusUnauthorized)
		return auth.Session{}, nil, false
	}
	clientIP := requestClientIP(request)
	session, valid := server.sessions.Lookup(cookie.Value, clientIP)
	if !valid {
		http.Error(response, "登入工作階段無效。", http.StatusUnauthorized)
		return auth.Session{}, nil, false
	}
	if !server.sessions.Validate(cookie.Value, request.Header.Get("X-CSRF-Token"), clientIP) {
		http.Error(response, "CSRF 驗證失敗。", http.StatusForbidden)
		return auth.Session{}, nil, false
	}
	return session, cookie, true
}

func (server *Server) nodeCredential(id string) (domain.NodeCredential, bool) {
	if server.nodeManager == nil {
		return domain.NodeCredential{}, false
	}
	for _, node := range server.nodeManager.Snapshot().Nodes {
		if node.ID == id {
			return node.Credential, true
		}
	}
	return domain.NodeCredential{}, false
}

func (server *Server) credentialElevation(token string) (time.Time, bool) {
	server.elevationMu.Lock()
	defer server.elevationMu.Unlock()
	expiresAt, exists := server.elevations[token]
	if !exists {
		return time.Time{}, false
	}
	if !server.clock().Before(expiresAt) {
		delete(server.elevations, token)
		return time.Time{}, false
	}
	return expiresAt, true
}

func (server *Server) setCredentialElevation(token string, expiresAt time.Time) {
	server.elevationMu.Lock()
	server.elevations[token] = expiresAt
	server.elevationMu.Unlock()
}

func (server *Server) revokeCredentialElevation(token string) {
	server.elevationMu.Lock()
	delete(server.elevations, token)
	server.elevationMu.Unlock()
}

func (server *Server) updateNode(response http.ResponseWriter, request *http.Request, id string) {
	if !server.authorizeNodeMutation(response, request) {
		return
	}
	var input updateNodeRequest
	if !decodeStrictJSON(response, request, &input) {
		return
	}
	node, err := server.nodeManager.Update(nodes.UpdateInput{ID: id, Port: input.Port, Enabled: input.Enabled})
	if err != nil {
		http.Error(response, "無法更新節點。", http.StatusUnprocessableEntity)
		return
	}
	server.replaceCurrentConfig(server.nodeManager.Snapshot())
	writeJSON(response, http.StatusOK, summarizeNode(node))
}

func (server *Server) deleteNode(response http.ResponseWriter, request *http.Request, id string) {
	if !server.authorizeNodeMutation(response, request) {
		return
	}
	if err := server.nodeManager.Delete(id); err != nil {
		http.Error(response, "無法刪除節點。", http.StatusUnprocessableEntity)
		return
	}
	server.replaceCurrentConfig(server.nodeManager.Snapshot())
	response.WriteHeader(http.StatusNoContent)
}

func (server *Server) authorizeNodeMutation(response http.ResponseWriter, request *http.Request) bool {
	if !server.authorizeStateChange(response, request) {
		return false
	}
	if request.Header.Get("X-S12ryt-Confirm") != "apply" {
		http.Error(response, "節點變更需要明確確認。", http.StatusConflict)
		return false
	}
	if server.nodeManager == nil {
		http.Error(response, "節點管理服務不可用。", http.StatusServiceUnavailable)
		return false
	}
	return true
}

type remoteOutboundSummary struct {
	Tag                  string `json:"tag"`
	Type                 string `json:"type"`
	Server               string `json:"server"`
	Port                 int    `json:"port"`
	Enabled              bool   `json:"enabled"`
	IPv4FallbackPosition int    `json:"ipv4_fallback_position"`
}

type importRemoteRequest struct {
	Payload        string `json:"payload"`
	AllowIPv4Proxy bool   `json:"allow_ipv4_proxy"`
	Enabled        bool   `json:"enabled"`
}

type updateRemoteRequest struct {
	Enabled bool `json:"enabled"`
}

type ipv4FallbackRequest struct {
	Tags []string `json:"tags"`
}

func (server *Server) listRemoteOutbounds(response http.ResponseWriter, request *http.Request) {
	if _, valid := server.requestSession(request); !valid {
		http.Error(response, "需要登入。", http.StatusUnauthorized)
		return
	}
	if server.remoteManager == nil {
		http.Error(response, "遠端出口管理服務不可用。", http.StatusServiceUnavailable)
		return
	}
	writeJSON(response, http.StatusOK, summarizeRemoteOutboundList(server.remoteManager.RemoteOutbounds()))
}

func (server *Server) importRemoteOutbounds(response http.ResponseWriter, request *http.Request) {
	if !server.authorizeRemoteMutation(response, request) {
		return
	}
	var input importRemoteRequest
	if !decodeRemoteJSON(response, request, &input) {
		return
	}
	created, err := server.remoteManager.ImportRemoteOutbounds(nodes.ImportRemoteInput{
		Payload:        []byte(input.Payload),
		AllowIPv4Proxy: input.AllowIPv4Proxy,
		Enabled:        input.Enabled,
	})
	if err != nil {
		http.Error(response, "無法匯入遠端出口。", http.StatusUnprocessableEntity)
		return
	}
	writeJSON(response, http.StatusCreated, summarizeRemoteOutboundList(created))
}

func (server *Server) handleNamedRemoteOutbound(response http.ResponseWriter, request *http.Request) {
	tag := strings.TrimPrefix(request.URL.Path, server.basePath+"/api/remotes/")
	if !remoteTagPattern.MatchString(tag) {
		http.Error(response, "遠端出口標籤無效。", http.StatusBadRequest)
		return
	}
	switch request.Method {
	case http.MethodPatch:
		server.updateRemoteOutbound(response, request, tag)
	case http.MethodDelete:
		server.deleteRemoteOutbound(response, request, tag)
	default:
		response.Header().Set("Allow", "PATCH, DELETE")
		http.Error(response, "不支援的遠端出口操作。", http.StatusMethodNotAllowed)
	}
}

func (server *Server) updateRemoteOutbound(response http.ResponseWriter, request *http.Request, tag string) {
	if !server.authorizeRemoteMutation(response, request) {
		return
	}
	var input updateRemoteRequest
	if !decodeRemoteJSON(response, request, &input) {
		return
	}
	updated, err := server.remoteManager.UpdateRemoteOutbound(tag, input.Enabled)
	if err != nil {
		http.Error(response, "無法更新遠端出口。", http.StatusUnprocessableEntity)
		return
	}
	writeJSON(response, http.StatusOK, summarizeRemoteOutbound(updated))
}

func (server *Server) deleteRemoteOutbound(response http.ResponseWriter, request *http.Request, tag string) {
	if !server.authorizeRemoteMutation(response, request) {
		return
	}
	if err := server.remoteManager.DeleteRemoteOutbound(tag); err != nil {
		http.Error(response, "無法刪除遠端出口。", http.StatusUnprocessableEntity)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (server *Server) setIPv4Fallback(response http.ResponseWriter, request *http.Request) {
	if !server.authorizeRemoteMutation(response, request) {
		return
	}
	var input ipv4FallbackRequest
	if !decodeRemoteJSON(response, request, &input) {
		return
	}
	if err := server.remoteManager.SetIPv4Fallback(input.Tags); err != nil {
		http.Error(response, "無法更新 IPv4 備援順序。", http.StatusUnprocessableEntity)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (server *Server) authorizeRemoteMutation(response http.ResponseWriter, request *http.Request) bool {
	if !server.authorizeStateChange(response, request) {
		return false
	}
	if request.Header.Get("X-S12ryt-Confirm") != "apply" {
		http.Error(response, "遠端出口變更需要明確確認。", http.StatusConflict)
		return false
	}
	if server.remoteManager == nil {
		http.Error(response, "遠端出口管理服務不可用。", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func decodeRemoteJSON(response http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(response, request.Body, maxRemoteRequestBodySize)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(response, "遠端出口 JSON 無效。", http.StatusBadRequest)
		return false
	}
	if err := ensureJSONEnd(decoder); err != nil {
		http.Error(response, "遠端出口 JSON 只能包含一個物件。", http.StatusBadRequest)
		return false
	}
	return true
}

func summarizeRemoteOutbound(summary nodes.RemoteOutboundSummary) remoteOutboundSummary {
	return remoteOutboundSummary{
		Tag: summary.Tag, Type: summary.Type, Server: summary.Server, Port: summary.Port,
		Enabled: summary.Enabled, IPv4FallbackPosition: summary.IPv4FallbackPosition,
	}
}

func summarizeRemoteOutboundList(summaries []nodes.RemoteOutboundSummary) []remoteOutboundSummary {
	result := make([]remoteOutboundSummary, 0, len(summaries))
	for _, summary := range summaries {
		result = append(result, summarizeRemoteOutbound(summary))
	}
	return result
}

func decodeStrictJSON(response http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(response, request.Body, maxConfigBodySize)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(response, "節點 JSON 無效。", http.StatusBadRequest)
		return false
	}
	if err := ensureJSONEnd(decoder); err != nil {
		http.Error(response, "節點 JSON 只能包含一個物件。", http.StatusBadRequest)
		return false
	}
	return true
}

func summarizeNode(node domain.Node) nodeSummary {
	return nodeSummary{
		ID: node.ID, Protocol: node.Protocol, Port: node.Port, Enabled: node.Enabled,
		CredentialConfigured: node.Credential.Validate(node.Protocol) == nil,
	}
}

func summarizeNodes(nodes []domain.Node) []nodeSummary {
	if nodes == nil {
		return nil
	}
	summaries := make([]nodeSummary, 0, len(nodes))
	for _, node := range nodes {
		summaries = append(summaries, summarizeNode(node))
	}
	return summaries
}

func editableConfigFrom(config domain.Config) editableConfig {
	return editableConfig{
		SchemaVersion: config.SchemaVersion,
		Panel:         config.Panel,
		IPv6:          config.IPv6,
		Routing:       config.Routing,
		Health:        config.Health,
		Nodes:         summarizeNodes(config.Nodes),
	}
}

func (server *Server) currentConfig() domain.Config {
	server.mutex.RLock()
	defer server.mutex.RUnlock()
	return server.config
}

func (server *Server) replaceCurrentConfig(config domain.Config) {
	server.mutex.Lock()
	server.config = config
	server.mutex.Unlock()
}

func (server *Server) authorizeStateChange(response http.ResponseWriter, request *http.Request) bool {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		http.Error(response, "需要登入。", http.StatusUnauthorized)
		return false
	}
	clientIP := requestClientIP(request)
	if _, valid := server.sessions.Lookup(cookie.Value, clientIP); !valid {
		http.Error(response, "登入工作階段無效。", http.StatusUnauthorized)
		return false
	}
	if !server.sessions.Validate(cookie.Value, request.Header.Get("X-CSRF-Token"), clientIP) {
		http.Error(response, "CSRF 驗證失敗。", http.StatusForbidden)
		return false
	}
	return true
}

func (server *Server) decodeCandidateConfig(response http.ResponseWriter, request *http.Request) (domain.Config, bool) {
	request.Body = http.MaxBytesReader(response, request.Body, maxConfigBodySize)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var editable editableConfig
	if err := decoder.Decode(&editable); err != nil {
		http.Error(response, "設定 JSON 無效。", http.StatusBadRequest)
		return domain.Config{}, false
	}
	if err := ensureJSONEnd(decoder); err != nil {
		http.Error(response, "設定 JSON 只能包含一個物件。", http.StatusBadRequest)
		return domain.Config{}, false
	}
	current := server.currentConfig()
	if !reflect.DeepEqual(editable.Nodes, summarizeNodes(current.Nodes)) {
		http.Error(response, "節點必須透過節點管理 API 變更。", http.StatusUnprocessableEntity)
		return domain.Config{}, false
	}
	candidate := domain.Config{
		SchemaVersion: editable.SchemaVersion,
		Panel:         editable.Panel,
		IPv6:          editable.IPv6,
		Routing:       editable.Routing,
		Health:        editable.Health,
		Nodes:         current.Nodes,
	}
	if err := candidate.Validate(); err != nil {
		http.Error(response, "設定驗證失敗："+err.Error(), http.StatusUnprocessableEntity)
		return domain.Config{}, false
	}
	return candidate, true
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("unexpected additional JSON value")
	}
	return err
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func (server *Server) requestSession(request *http.Request) (auth.Session, bool) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		return auth.Session{}, false
	}
	return server.sessions.Lookup(cookie.Value, requestClientIP(request))
}

func requestClientIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

const loginPage = `<!doctype html>
<html lang="zh-Hant" data-ui-theme="noc"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>s12ryt IPv6 管理面板</title><style>
:root{color-scheme:dark;--ink:#e7edf2;--paper:#080c11;--panel:#111820;--line:#30404e;--accent:#46d694;--warn:#ff8a6b}*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font-family:"IBM Plex Mono","Cascadia Mono","Noto Sans TC",Consolas,monospace;min-height:100vh;display:grid;place-items:center;padding:24px;letter-spacing:0}.login{width:min(440px,100%%);border:1px solid var(--line);border-top:5px solid var(--accent);padding:32px;background:var(--panel);box-shadow:10px 10px 0 #030507}.eyebrow{font-size:12px;font-weight:700;text-transform:uppercase;color:var(--accent)}h1{font-size:28px;margin:8px 0 24px}.warning{border-left:4px solid var(--warn);padding:10px 12px;background:#281817;color:#ffc3b4;font-size:14px}label{display:block;margin:24px 0 8px;font-weight:700}input{width:100%%;padding:12px;border:1px solid #506170;background:#0b1118;color:var(--ink);font:16px "IBM Plex Mono","Cascadia Mono",Consolas,monospace}button{width:100%%;margin-top:16px;padding:12px;border:1px solid var(--accent);background:var(--accent);color:#04100b;font-weight:800;cursor:pointer}button:hover{background:#72e5ad}button:focus-visible,input:focus-visible{outline:3px solid #ffd166;outline-offset:3px}</style></head>
<body><main class="login"><div class="eyebrow">s12ryt / network control</div><h1>登入 IPv6 管理面板</h1><p class="warning">目前使用公開 HTTP，密碼可能被攔截。請只在理解風險後繼續。</p><form method="post" action="%s"><label for="password">管理密碼</label><input id="password" name="password" type="password" autocomplete="current-password" required><button type="submit">登入</button></form></main></body></html>`

const nodeWorkspaceHTML = `<section class="node-workspace" aria-labelledby="nodes-title"><style>
.node-workspace{margin-top:16px;background:#fff;border:1px solid var(--line);padding:20px}.node-heading{display:flex;align-items:center;justify-content:space-between;gap:16px}.node-heading h2{margin:0}.node-heading button,.node-actions button{border:1px solid var(--line);background:#fff;color:var(--ink);padding:8px 11px;cursor:pointer}.node-heading button{background:var(--ink);color:#fff}.node-table-wrap{overflow-x:auto;margin-top:16px}.node-table{width:100%;border-collapse:collapse;min-width:680px}.node-table th,.node-table td{text-align:left;border-bottom:1px solid var(--line);padding:10px 8px}.node-table th{font:700 12px Consolas,monospace;text-transform:uppercase;color:var(--muted)}.node-actions{display:flex;gap:6px;white-space:nowrap}.field-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}.field-grid label{display:grid;gap:6px;font-weight:700}.field-grid input,.field-grid select{width:100%;padding:10px;border:1px solid var(--line);background:#fff}.listener-field-group{grid-column:1/-1;display:grid;grid-template-columns:1fr 1fr;gap:10px 14px;margin:0;padding:14px;border:1px solid var(--line)}.listener-field-group legend{padding:0 6px;font-weight:800}.listener-help,.field-error{grid-column:1/-1;margin:0}.field-error{color:#b42318;font-weight:700}.listener-field-group input[aria-invalid="true"]{border-color:#b42318;box-shadow:0 0 0 1px #b42318}.secret-output{border:1px solid var(--line);background:#f5f7f5;padding:12px;white-space:pre-wrap;overflow-wrap:anywhere}.danger{color:#8f1d18}.danger-guidance{border-left:3px solid #8f1d18;padding-left:10px}.danger-button,[data-danger-action]{background:#8f1d18!important;color:#fff!important}@media(max-width:700px){.node-heading{align-items:flex-start;flex-direction:column}.field-grid,.listener-field-group{grid-template-columns:1fr}.node-actions{flex-wrap:wrap}}
</style><div class="node-heading"><div><h2 id="nodes-title">協議節點</h2><p class="muted">認證預設遮罩；揭露前必須重新驗證管理密碼。</p></div><button type="button" data-node-create>新增節點</button></div><p class="danger-guidance" id="node-danger-guidance" data-danger-guidance><strong>危險操作：</strong>刪除節點會永久移除其設定與認證，執行前仍會要求確認。</p><p class="error" data-node-error role="alert"></p><div class="node-table-wrap"><table class="node-table" data-node-table><thead><tr><th>節點</th><th>協議</th><th>連接埠</th><th>狀態</th><th>操作</th></tr></thead><tbody data-node-table-body><tr><td colspan="5">正在載入節點...</td></tr></tbody></table></div></section>`

const nodeDeploymentFieldsHTML = `<fieldset class="listener-field-group" data-listener-field-group aria-describedby="node-listener-help node-listener-error"><legend>監聽地址</legend><label>IPv4 監聽地址<input name="node_listener_ipv4" inputmode="decimal" placeholder="198.51.100.10" aria-describedby="node-listener-help node-listener-error"></label><label>IPv6 監聽地址<input name="node_listener_ipv6" placeholder="2001:db8::10" aria-describedby="node-listener-help node-listener-error"></label><small class="muted listener-help" id="node-listener-help">至少填寫一個可供節點監聽的 IPv4 或 IPv6 地址。</small><p class="field-error" id="node-listener-error" data-node-listener-error role="alert" hidden></p></fieldset><label class="choice"><input name="node_tls_enabled" type="checkbox">啟用 TLS</label><label>TLS 來源<select name="node_tls_mode"><option value="certificate">既有憑證與私鑰</option><option value="acme">ACME HTTP-01</option></select></label><label>伺服器名稱<input name="node_server_name" placeholder="node.example.com"></label><label>憑證路徑<input name="node_certificate_path" placeholder="/opt/s12ryt-ipv6/tls/server.crt"></label><label>私鑰路徑<input name="node_key_path" placeholder="/opt/s12ryt-ipv6/tls/server.key"></label><label>ACME 網域<input name="node_acme_domains" placeholder="node.example.com, edge.example.com"></label><label>ACME 預設伺服器名稱<input name="node_acme_default_server_name" placeholder="node.example.com"></label><label>ACME Email（選填）<input name="node_acme_email" type="email" autocomplete="email" placeholder="admin@example.com"></label><p class="muted">ACME 憑證資料固定保存於 /opt/s12ryt-ipv6/tls/acme，建立前會確認 TCP/80 可用。</p><label>傳輸<select name="node_transport"><option value="tcp">TCP</option><option value="websocket">WebSocket</option><option value="grpc">gRPC</option></select></label><label>WebSocket 路徑<input name="node_transport_path" placeholder="/edge"></label><label>gRPC 服務名稱<input name="node_grpc_service_name" placeholder="edge.Service"></label>`

const nodeModalsHTML = `<div class="modal" data-modal="node-editor" data-modal-backdrop="static" hidden><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="node-editor-title"><h2 id="node-editor-title">節點設定</h2><form data-node-form><div class="field-grid"><label>節點 ID<input name="node_id" pattern="[A-Za-z0-9][A-Za-z0-9_.-]{0,63}" maxlength="64" required></label><label>協議<select name="node_protocol" required><option value="vless">VLESS</option><option value="vmess">VMess</option><option value="hysteria2">Hysteria2</option><option value="tuic">TUIC</option><option value="socks5">SOCKS5</option><option value="anytls">AnyTLS</option><option value="shadowsocks">Shadowsocks</option></select></label><label>連接埠<input name="node_port" type="number" min="20000" max="49999" placeholder="留空自動分配"></label><label class="choice"><input name="node_enabled" type="checkbox" checked>啟用節點</label></div><p class="muted">建立後會產生此節點專用的隨機認證。編輯不會更換既有認證。</p><div class="actions"><button type="button" class="secondary" data-modal-close="button">取消</button><button type="submit">確認並套用</button></div></form><span data-modal-close="escape" hidden></span></section></div>
<div class="modal" data-modal="credential" data-modal-backdrop="static" hidden><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="credential-title"><h2 id="credential-title">揭露節點認證</h2><p class="warning">公開 HTTP 可能洩漏密碼與節點認證。請先限制允許來源。</p><form data-credential-form><input name="credential_node_id" type="hidden"><label>管理密碼<input name="management_password" type="password" autocomplete="current-password"></label><p class="muted">首次揭露必須輸入密碼；同一工作階段驗證後五分鐘內可留空。</p><div class="actions"><button type="button" class="secondary" data-modal-close="button">取消</button><button type="submit" data-credential-reveal>驗證並揭露</button></div></form><pre class="secret-output" data-credential-value hidden></pre><p class="muted" data-credential-expiry></p><div class="actions"><button type="button" class="secondary" data-credential-copy hidden>複製認證</button></div><span data-modal-close="escape" hidden></span></section></div>`

const nodeManagementScript = `
;const nodesEndpoint=shell.dataset.nodesEndpoint;const credentialEndpointTemplate=shell.dataset.credentialEndpointTemplate;const nodeTableBody=document.querySelector('[data-node-table-body]');const nodeError=document.querySelector('[data-node-error]');const nodeForm=document.querySelector('[data-node-form]');const credentialForm=document.querySelector('[data-credential-form]');const credentialValue=document.querySelector('[data-credential-value]');const credentialExpiry=document.querySelector('[data-credential-expiry]');const credentialCopy=document.querySelector('[data-credential-copy]');let managedNodes=[];let editingNodeID='';function openManagedModal(name){document.querySelector('[data-modal="'+name+'"]').hidden=false}function mutationHeaders(){return{'Content-Type':'application/json','X-CSRF-Token':csrf,'X-S12ryt-Confirm':'apply'}}function actionButton(label,attribute,handler){const button=document.createElement('button');button.type='button';button.textContent=label;button.setAttribute(attribute,'');button.addEventListener('click',handler);return button}function renderNodes(){nodeTableBody.textContent='';if(managedNodes.length===0){const row=document.createElement('tr');const cell=document.createElement('td');cell.colSpan=5;cell.textContent='尚未建立節點。';row.append(cell);nodeTableBody.append(row);return}managedNodes.forEach((node)=>{const row=document.createElement('tr');[node.id,node.protocol,String(node.port),node.enabled?'啟用':'停用'].forEach((value)=>{const cell=document.createElement('td');cell.textContent=value;row.append(cell)});const actions=document.createElement('td');actions.className='node-actions';actions.append(actionButton('編輯','data-node-edit',()=>openNodeEditor(node)));actions.append(actionButton('認證','data-credential-reveal',()=>openCredential(node.id)));const remove=actionButton('刪除','data-node-delete',()=>deleteNode(node.id));remove.className='danger-button';actions.append(remove);row.append(actions);nodeTableBody.append(row)})}async function loadNodes(){try{managedNodes=await requestJSON(nodesEndpoint);renderNodes()}catch(error){nodeError.textContent=error.message}}function openNodeEditor(node=null,protocol='vless'){editingNodeID=node?node.id:'';nodeForm.reset();nodeForm.elements.node_id.value=node?node.id:'';nodeForm.elements.node_protocol.value=node?node.protocol:protocol;nodeForm.elements.node_port.value=node?String(node.port):'';nodeForm.elements.node_enabled.checked=node?node.enabled:true;nodeForm.elements.node_id.disabled=Boolean(node);nodeForm.elements.node_protocol.disabled=Boolean(node);openManagedModal('node-editor')}document.querySelector('[data-node-create]').addEventListener('click',()=>openNodeEditor());document.querySelectorAll('[data-protocol]').forEach((button)=>button.addEventListener('click',()=>{closeModal(button.closest('[data-modal]'));openNodeEditor(null,button.dataset.protocol)}));nodeForm.addEventListener('submit',async(event)=>{event.preventDefault();nodeError.textContent='';const port=Number(nodeForm.elements.node_port.value||0);const payload=editingNodeID?{port:port,enabled:nodeForm.elements.node_enabled.checked}:{id:nodeForm.elements.node_id.value,protocol:nodeForm.elements.node_protocol.value,port:port,enabled:nodeForm.elements.node_enabled.checked};try{const url=editingNodeID?nodesEndpoint+'/'+encodeURIComponent(editingNodeID):nodesEndpoint;await requestJSON(url,{method:editingNodeID?'PATCH':'POST',headers:mutationHeaders(),body:JSON.stringify(payload)});closeModal(nodeForm.closest('[data-modal]'));await loadNodes()}catch(error){nodeError.textContent=error.message}});async function deleteNode(id){if(!window.confirm('確定刪除節點 '+id+'？此操作不會顯示認證。')){return}nodeError.textContent='';try{const response=await fetch(nodesEndpoint+'/'+encodeURIComponent(id),{method:'DELETE',headers:mutationHeaders()});if(!response.ok){throw new Error(await response.text())}await loadNodes()}catch(error){nodeError.textContent=error.message}}function clearCredential(){credentialForm.reset();credentialValue.textContent='';credentialValue.hidden=true;credentialExpiry.textContent='';credentialCopy.hidden=true}function openCredential(id){clearCredential();credentialForm.elements.credential_node_id.value=id;openManagedModal('credential')}credentialForm.addEventListener('submit',async(event)=>{event.preventDefault();nodeError.textContent='';const id=credentialForm.elements.credential_node_id.value;const endpoint=credentialEndpointTemplate.replace('{id}',encodeURIComponent(id));try{const result=await requestJSON(endpoint,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify({password:credentialForm.elements.management_password.value})});credentialForm.elements.management_password.value='';credentialValue.textContent=JSON.stringify(result.credential,null,2);credentialValue.hidden=false;credentialExpiry.textContent='本次工作階段尚可揭露 '+result.expires_in_seconds+' 秒';credentialCopy.hidden=false}catch(error){nodeError.textContent=error.message}});credentialCopy.addEventListener('click',async()=>{try{await navigator.clipboard.writeText(credentialValue.textContent);credentialExpiry.textContent='已複製；敏感資料仍會在五分鐘後要求重新驗證。'}catch(error){credentialExpiry.textContent='瀏覽器拒絕剪貼簿存取，請手動複製。'}});document.querySelectorAll('[data-modal-close="button"]').forEach((button)=>button.addEventListener('click',()=>{if(button.closest('[data-modal="credential"]')){clearCredential()}}));document.addEventListener('keydown',(event)=>{if(event.key==='Escape'){clearCredential()}});loadNodes();`

const nodeDeploymentScript = `
;const listenerIPv4Input=nodeForm.querySelector('[name="node_listener_ipv4"]');const listenerIPv6Input=nodeForm.querySelector('[name="node_listener_ipv6"]');const nodeListenerError=nodeForm.querySelector('[data-node-listener-error]');function setNodeListenerError(message){const invalid=Boolean(message);nodeListenerError.textContent=message;nodeListenerError.hidden=!invalid;[listenerIPv4Input,listenerIPv6Input].forEach((input)=>{if(invalid){input.setAttribute('aria-invalid','true')}else{input.removeAttribute('aria-invalid')}})}function clearNodeListenerErrorWhenValid(){if(listenerIPv4Input.value.trim()||listenerIPv6Input.value.trim()){setNodeListenerError('')}}[listenerIPv4Input,listenerIPv6Input].forEach((input)=>input.addEventListener('input',clearNodeListenerErrorWhenValid));document.addEventListener('click',(event)=>{if(event.target.closest('[data-node-create],[data-node-edit]')){setNodeListenerError('')}},true);nodeForm.addEventListener('submit',async(event)=>{if(editingNodeID){return}event.preventDefault();event.stopImmediatePropagation();nodeError.textContent='';setNodeListenerError('');const data=new FormData(nodeForm);const listeners=[String(data.get('node_listener_ipv4')||'').trim(),String(data.get('node_listener_ipv6')||'').trim()].filter(Boolean);if(listeners.length===0){setNodeListenerError('至少需要一個 IPv4 或 IPv6 監聽地址。');queueMicrotask(()=>listenerIPv4Input.focus());return}const transportType=String(data.get('node_transport')||'tcp');const transport={};if(transportType==='websocket'){transport.type='websocket';transport.path=String(data.get('node_transport_path')||'').trim()}else if(transportType==='grpc'){transport.type='grpc';transport.service_name=String(data.get('node_grpc_service_name')||'').trim()}const tls={enabled:data.get('node_tls_enabled')==='on'};if(tls.enabled){tls.server_name=String(data.get('node_server_name')||'').trim();const tlsMode=String(data.get('node_tls_mode')||'certificate');if(tlsMode==='acme'){Object.assign(tls,{acme:{domains:String(data.get('node_acme_domains')||'').split(/[\s,]+/).map((domain)=>domain.trim()).filter(Boolean),data_directory:'/opt/s12ryt-ipv6/tls/acme',default_server_name:String(data.get('node_acme_default_server_name')||'').trim(),email:String(data.get('node_acme_email')||'').trim(),provider:'letsencrypt'}})}else{tls.certificate_path=String(data.get('node_certificate_path')||'').trim();tls.key_path=String(data.get('node_key_path')||'').trim()}}const portValue=String(data.get('node_port')||'').trim();const payload={id:String(data.get('node_id')||'').trim(),protocol:String(data.get('node_protocol')||''),port:portValue?Number(portValue):0,enabled:data.get('node_enabled')==='on',deployment:{listeners:listeners,tls:tls,transport:transport}};try{await requestJSON(nodesEndpoint,{method:'POST',headers:mutationHeaders(),body:JSON.stringify(payload)});await loadNodes();closeModal(nodeForm.closest('[data-modal]'));nodeForm.reset()}catch(error){nodeError.textContent=error.message}},true)
`

const remoteWorkspaceHTML = `<section class="remote-workspace" data-remote-workspace aria-labelledby="remotes-title"><style>
.remote-workspace{grid-column:1/-1;background:#fff;border:1px solid var(--line);padding:20px}.remote-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.remote-heading h2{margin:0}.remote-heading button,.remote-actions button,.fallback-form button{border:1px solid var(--line);background:#fff;color:var(--ink);padding:8px 11px;cursor:pointer}.remote-heading button{background:var(--ink);color:#fff}.remote-table-wrap{overflow-x:auto;margin-top:16px}.remote-table{width:100%;min-width:720px;border-collapse:collapse}.remote-table th,.remote-table td{text-align:left;border-bottom:1px solid var(--line);padding:10px 8px}.remote-table th{font:700 12px Consolas,monospace;text-transform:uppercase;color:var(--muted)}.remote-actions{display:flex;gap:6px;white-space:nowrap}.fallback-form{display:grid;grid-template-columns:1fr auto;gap:10px;align-items:end;margin-top:18px}.fallback-form label{display:grid;gap:6px;font-weight:700}.fallback-form input{width:100%;padding:10px;border:1px solid var(--line)}@media(max-width:700px){.remote-heading{flex-direction:column}.fallback-form{grid-template-columns:1fr}.fallback-form button{width:100%}}
</style><div class="remote-heading"><div><h2 id="remotes-title">遠端出口</h2><p class="muted">只顯示類型與端點；認證秘密不會回傳至清單。</p></div><button type="button" data-remote-import>匯入遠端出口</button></div><p class="danger-guidance" id="remote-danger-guidance" data-danger-guidance><strong>危險操作：</strong>刪除遠端出口會永久移除其秘密與路由引用，執行前仍會要求確認。</p><p class="error" data-remote-error role="alert"></p><div class="remote-table-wrap"><table class="remote-table" data-remote-table><thead><tr><th>標籤</th><th>類型</th><th>伺服器</th><th>狀態</th><th>IPv4 順序</th><th>操作</th></tr></thead><tbody data-remote-table-body><tr><td colspan="6">正在載入遠端出口...</td></tr></tbody></table></div><form class="fallback-form" data-fallback-form><label>IPv4 fallback 順序<input name="ipv4_fallback_tags" placeholder="remote-socks, direct-v4" autocomplete="off"><small class="muted">依序輸入 SOCKS/HTTP 標籤；可用逗號或空白分隔。</small></label><button type="submit">保存順序</button></form></section>`

const remoteModalHTML = `<div class="modal" data-modal="remote-import" data-modal-backdrop="static" hidden><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="remote-import-title"><h2 id="remote-import-title">匯入遠端出口</h2><p class="warning">匯入內容可能包含代理密碼。資料只會保存於 root-only 執行狀態，清單不會回顯秘密。</p><form data-remote-form><label>分享 URI、sing-box outbound JSON 或 Base64 多 URI<textarea name="remote_payload" rows="9" maxlength="6291456" required></textarea></label><div class="choices"><label class="choice"><input name="allow_ipv4_proxy" type="checkbox">允許 SOCKS/HTTP(S) 作為 IPv4 fallback</label><label class="choice"><input name="remote_enabled" type="checkbox" checked>匯入後立即啟用</label></div><div class="actions"><button type="button" class="secondary" data-modal-close="button">取消</button><button type="submit">驗證並匯入</button></div></form><span data-modal-close="escape" hidden></span></section></div>`

const remoteManagementScript = `
;const remotesEndpoint=shell.dataset.remotesEndpoint;const fallbackEndpoint=shell.dataset.ipv4FallbackEndpoint;const remoteTableBody=document.querySelector('[data-remote-table-body]');const remoteError=document.querySelector('[data-remote-error]');const remoteForm=document.querySelector('[data-remote-form]');const fallbackForm=document.querySelector('[data-fallback-form]');let managedRemotes=[];function remoteMutationHeaders(){return{'Content-Type':'application/json','X-CSRF-Token':csrf,'X-S12ryt-Confirm':'apply'}}async function requestRemoteNoContent(url,options){const response=await fetch(url,options);if(!response.ok){throw new Error(await response.text())}}function renderRemotes(){remoteTableBody.textContent='';if(managedRemotes.length===0){const row=document.createElement('tr');const cell=document.createElement('td');cell.colSpan=6;cell.textContent='尚未匯入遠端出口。';row.append(cell);remoteTableBody.append(row);return}managedRemotes.forEach((remote)=>{const row=document.createElement('tr');[remote.tag,remote.type,remote.server+':'+String(remote.port),remote.enabled?'啟用':'停用',remote.ipv4_fallback_position>0?String(remote.ipv4_fallback_position):'—'].forEach((value)=>{const cell=document.createElement('td');cell.textContent=value;row.append(cell)});const actions=document.createElement('td');actions.className='remote-actions';actions.append(actionButton(remote.enabled?'停用':'啟用','data-remote-toggle',()=>toggleRemote(remote)));const remove=actionButton('刪除','data-remote-delete',()=>deleteRemote(remote.tag));remove.className='danger-button';actions.append(remove);row.append(actions);remoteTableBody.append(row)})}async function loadRemotes(){remoteError.textContent='';try{managedRemotes=await requestJSON(remotesEndpoint);renderRemotes()}catch(error){remoteError.textContent=error.message}}async function toggleRemote(remote){remoteError.textContent='';try{await requestJSON(remotesEndpoint+'/'+encodeURIComponent(remote.tag),{method:'PATCH',headers:remoteMutationHeaders(),body:JSON.stringify({enabled:!remote.enabled})});await loadRemotes()}catch(error){remoteError.textContent=error.message}}async function deleteRemote(tag){if(!window.confirm('確定刪除遠端出口 '+tag+'？')){return}remoteError.textContent='';try{await requestRemoteNoContent(remotesEndpoint+'/'+encodeURIComponent(tag),{method:'DELETE',headers:remoteMutationHeaders()});await loadRemotes()}catch(error){remoteError.textContent=error.message}}document.querySelector('[data-remote-import]').addEventListener('click',()=>openManagedModal('remote-import'));remoteForm.addEventListener('submit',async(event)=>{event.preventDefault();remoteError.textContent='';const data=new FormData(remoteForm);const payload={payload:String(data.get('remote_payload')||''),allow_ipv4_proxy:data.get('allow_ipv4_proxy')==='on',enabled:data.get('remote_enabled')==='on'};try{await requestJSON(remotesEndpoint,{method:'POST',headers:remoteMutationHeaders(),body:JSON.stringify(payload)});remoteForm.reset();closeModal(remoteForm.closest('[data-modal]'));await loadRemotes()}catch(error){remoteError.textContent=error.message}});fallbackForm.addEventListener('submit',async(event)=>{event.preventDefault();remoteError.textContent='';const data=new FormData(fallbackForm);const tags=String(data.get('ipv4_fallback_tags')||'').split(/[\s,]+/).map((tag)=>tag.trim()).filter(Boolean);try{await requestRemoteNoContent(fallbackEndpoint,{method:'PUT',headers:remoteMutationHeaders(),body:JSON.stringify({tags:tags})});await loadRemotes()}catch(error){remoteError.textContent=error.message}});loadRemotes()
`

const networkWorkspaceHTML = `<section class="network-workspace" data-network-workspace aria-labelledby="network-title"><style>
.network-workspace{grid-column:1/-1;background:#fff;border:1px solid var(--line);padding:20px}.network-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.network-heading h2{margin:0}.network-actions{display:flex;gap:8px}.network-actions button{border:1px solid var(--line);background:#fff;color:var(--ink);padding:8px 11px;cursor:pointer}.network-actions button:last-child{background:var(--ink);color:#fff}.network-table-wrap{overflow-x:auto;margin-top:16px}.network-table{width:100%;min-width:680px;border-collapse:collapse}.network-table th,.network-table td{text-align:left;border-bottom:1px solid var(--line);padding:10px 8px}.network-table th{font:700 12px Consolas,monospace;text-transform:uppercase;color:var(--muted)}.network-result{border:1px solid var(--line);background:#f5f7f5;padding:12px;margin-top:14px;white-space:pre-wrap}@media(max-width:700px){.network-heading{flex-direction:column}.network-actions{width:100%;flex-wrap:wrap}.network-actions button{flex:1}}
</style><div class="network-heading"><div><h2 id="network-title">IPv6 與防火牆整合</h2><p class="muted">只會配置專案 manifest 內的 IPv6、專用 policy route 與標記防火牆規則。</p></div><div class="network-actions"><button type="button" data-network-refresh>重新整理地址</button><button type="button" data-network-apply>建立 IPv6 池</button></div></div><p class="error" data-network-error role="alert"></p><div class="network-table-wrap"><table class="network-table" data-network-address-table><thead><tr><th>介面</th><th>完整 IPv6</th><th>前綴</th></tr></thead><tbody data-network-address-body><tr><td colspan="3">正在載入全域 IPv6...</td></tr></tbody></table></div><pre class="network-result" data-network-result hidden></pre></section>`

const networkModalHTML = `<div class="modal" data-modal="network-setup" data-modal-backdrop="static" hidden><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="network-setup-title"><h2 id="network-setup-title">建立專案 IPv6 池</h2><p class="warning">此操作會新增 IPv6、專用 policy route 與防火牆規則。只接受系統中唯一可判定的 IPv6 gateway。</p><form data-network-form><div class="field-grid"><label>網路介面<input name="network_interface" placeholder="eth0" required></label><label>可路由 IPv6 前綴<input name="network_prefix" placeholder="2001:db8:100::/64" required></label><label>生成數量<input name="network_count" type="number" min="1" max="256" value="16" required></label><label>防火牆<select name="firewall_backend" required><option value="ufw">ufw</option><option value="firewalld">firewalld</option><option value="nftables">nftables</option></select></label><label>管理面板連接埠<input name="panel_port" type="number" min="1" max="65535" value="34456" required></label><label>允許來源 CIDR<input name="allowed_cidrs" value="0.0.0.0/0, ::/0" required></label><label>節點埠規則<textarea name="node_ports" rows="4" placeholder="tcp:24443, udp:24443"></textarea><small class="muted">格式為協議:連接埠，使用逗號或空白分隔。</small></label></div><div class="actions"><button type="button" class="secondary" data-modal-close="button">取消</button><button type="submit">驗證並套用</button></div></form><span data-modal-close="escape" hidden></span></section></div>`

const networkManagementScript = `
;const networkAddressesEndpoint=shell.dataset.networkAddressesEndpoint;const networkApplyEndpoint=shell.dataset.networkApplyEndpoint;const networkAddressBody=document.querySelector('[data-network-address-body]');const networkError=document.querySelector('[data-network-error]');const networkForm=document.querySelector('[data-network-form]');const networkResult=document.querySelector('[data-network-result]');function splitNetworkValues(value){return String(value||'').split(/[\\s,]+/).map((item)=>item.trim()).filter(Boolean)}function renderNetworkAddresses(addresses){networkAddressBody.textContent='';if(addresses.length===0){const row=document.createElement('tr');const cell=document.createElement('td');cell.colSpan=3;cell.textContent='未發現可用的全域 IPv6。';row.append(cell);networkAddressBody.append(row);return}addresses.forEach((address)=>{const row=document.createElement('tr');[address.interface,address.address,address.prefix].forEach((value)=>{const cell=document.createElement('td');cell.textContent=String(value);row.append(cell)});networkAddressBody.append(row)})}async function loadNetworkAddresses(){networkError.textContent='';try{renderNetworkAddresses(await requestJSON(networkAddressesEndpoint))}catch(error){networkError.textContent=error.message}}function parseNodePorts(value){return splitNetworkValues(value).map((entry)=>{const parts=entry.split(':');if(parts.length!==2||!['tcp','udp'].includes(parts[0])||!/^\\d+$/.test(parts[1])){throw new Error('節點埠規則格式必須是 tcp:PORT 或 udp:PORT。')}return{protocol:parts[0],port:Number(parts[1])}})}document.querySelector('[data-network-refresh]').addEventListener('click',loadNetworkAddresses);document.querySelector('[data-network-apply]').addEventListener('click',()=>openManagedModal('network-setup'));networkForm.addEventListener('submit',async(event)=>{event.preventDefault();networkError.textContent='';networkResult.hidden=true;const data=new FormData(networkForm);try{const payload={interface:String(data.get('network_interface')||'').trim(),prefix:String(data.get('network_prefix')||'').trim(),count:Number(data.get('network_count')),firewall_backend:String(data.get('firewall_backend')||''),panel_port:Number(data.get('panel_port')),allowed_cidrs:splitNetworkValues(data.get('allowed_cidrs')),node_ports:parseNodePorts(data.get('node_ports'))};if(!window.confirm('確認新增專案 IPv6、policy route 與防火牆規則？')){return}const result=await requestJSON(networkApplyEndpoint,{method:'POST',headers:mutationHeaders(),body:JSON.stringify(payload)});networkResult.textContent=JSON.stringify({interface:result.interface,address_count:result.address_count,firewall_backend:result.firewall_backend},null,2);networkResult.hidden=false;closeModal(networkForm.closest('[data-modal]'));await loadNetworkAddresses()}catch(error){networkError.textContent=error.message}},false);loadNetworkAddresses()
`

const shareWorkspaceHTML = `<section class="share-workspace" data-share-workspace aria-labelledby="shares-title"><style>
.share-workspace{grid-column:1/-1;background:#fff;border:1px solid var(--line);padding:20px}.share-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.share-heading h2{margin:0}.share-heading button,.share-card button{border:1px solid var(--line);background:#fff;color:var(--ink);padding:8px 11px;cursor:pointer}.share-heading button{background:var(--ink);color:#fff}.share-nodes{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;margin-top:16px}.share-card{border:1px solid var(--line);padding:14px;min-width:0}.share-card h3{margin:0 0 10px}.share-field{margin-top:10px}.share-field pre{margin:6px 0;background:#f5f7f5;border:1px solid var(--line);padding:10px;max-height:180px;overflow:auto}.share-card img{display:block;width:min(220px,100%);height:auto;border:1px solid var(--line);background:#fff;padding:6px}.share-subscription{margin-top:14px}@media(max-width:760px){.share-heading{flex-direction:column}.share-nodes{grid-template-columns:1fr}}
</style><div class="share-heading"><div><h2 id="shares-title">節點分享</h2><p class="muted">URI、QR 與完整客戶端設定受管理密碼重新驗證保護，五分鐘後失效。</p></div><button type="button" data-share-open>驗證並查看分享</button></div></section>`

const shareModalHTML = `<div class="modal" data-modal="share-reveal" data-modal-backdrop="static" hidden><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="share-reveal-title"><h2 id="share-reveal-title">受保護的節點分享</h2><p class="warning">公開 HTTP 可能洩漏節點 URI、認證與完整設定。只應在受信任來源中短暫揭露。</p><p class="error" data-share-error role="alert"></p><form data-share-form><label>管理密碼<input name="share_management_password" type="password" autocomplete="current-password"></label><p class="muted">首次揭露必須輸入密碼；同一工作階段五分鐘內可留空重新整理。</p><div class="actions"><button type="button" class="secondary" data-modal-close="button">取消</button><button type="submit">驗證並揭露</button></div></form><p class="muted" data-share-expiry></p><div class="share-nodes" data-share-nodes></div><div class="share-subscription" hidden><h3>聚合訂閱</h3><pre data-share-subscription></pre><button type="button" data-share-copy="subscription">複製訂閱</button></div><span data-modal-close="escape" hidden></span></section></div>`

const shareManagementScript = `
;const shareRevealEndpoint=shell.dataset.shareRevealEndpoint;const shareQREndpointTemplate=shell.dataset.shareQrEndpointTemplate;const shareModal=document.querySelector('[data-modal="share-reveal"]');const shareForm=document.querySelector('[data-share-form]');const sharePassword=shareForm.querySelector('[name="share_management_password"]');const shareNodes=document.querySelector('[data-share-nodes]');const shareExpiry=document.querySelector('[data-share-expiry]');const shareSubscription=document.querySelector('[data-share-subscription]');const shareSubscriptionBox=shareSubscription.closest('.share-subscription');const shareError=document.querySelector('[data-share-error]');function clearShareSecrets(){shareModal.querySelectorAll('[data-share-qr]').forEach((image)=>image.removeAttribute('src'));sharePassword.value='';shareNodes.textContent='';shareExpiry.textContent='';shareSubscription.textContent='';shareSubscriptionBox.hidden=true}function shareCopyButton(value,label){const button=document.createElement('button');button.type='button';button.textContent=label;button.setAttribute('data-share-copy','');button.addEventListener('click',async()=>{try{await navigator.clipboard.writeText(value)}catch(error){shareError.textContent='無法複製分享內容。'}});return button}function appendShareField(card,label,value,marker){if(!value){return}const section=document.createElement('div');section.className='share-field';const heading=document.createElement('strong');heading.textContent=label;const output=document.createElement('pre');output.textContent=value;output.setAttribute(marker,'');section.append(heading,output,shareCopyButton(value,'複製'));card.append(section)}function renderProtectedShares(result){clearShareSecrets();shareExpiry.textContent='分享內容將於 '+String(result.expires_in_seconds)+' 秒後需要重新驗證。';(result.nodes||[]).forEach((node)=>{const card=document.createElement('article');card.className='share-card';card.setAttribute('data-share-node','');const title=document.createElement('h3');title.textContent=node.node_id;card.append(title);appendShareField(card,'分享 URI',node.uri,'data-share-uri');appendShareField(card,'sing-box 客戶端 JSON',node.client_json,'data-share-client-json');appendShareField(card,'模式 1 完整客戶端 JSON',node.full_client_json,'data-share-full-client-json');appendShareField(card,'模式 1 完整 Base64',node.full_client_base64,'data-share-full-client-base64');if(node.warning){const warning=document.createElement('p');warning.className='warning';warning.textContent=node.warning;warning.setAttribute('data-share-warning','');card.append(warning)}const qrURL=node.qr_url||shareQREndpointTemplate.replace('{id}',encodeURIComponent(node.node_id));if(qrURL){const image=document.createElement('img');image.alt=node.node_id+' QR Code';image.setAttribute('data-share-qr','');image.src=qrURL;card.append(image)}shareNodes.append(card)});if(result.subscription){shareSubscription.textContent=result.subscription;shareSubscriptionBox.hidden=false}}document.querySelector('[data-share-open]').addEventListener('click',()=>{clearShareSecrets();shareError.textContent='';openManagedModal('share-reveal')});shareForm.addEventListener('submit',async(event)=>{event.preventDefault();shareError.textContent='';try{const result=await requestJSON(shareRevealEndpoint,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify({password:sharePassword.value})});renderProtectedShares(result)}catch(error){clearShareSecrets();shareError.textContent=error.message}});shareModal.querySelectorAll('[data-modal-close="button"]').forEach((button)=>button.addEventListener('click',clearShareSecrets));document.addEventListener('keydown',(event)=>{if(event.key==='Escape'&&!shareModal.hidden){clearShareSecrets()}},true);shareSubscriptionBox.querySelector('[data-share-copy="subscription"]').addEventListener('click',async()=>{try{await navigator.clipboard.writeText(shareSubscription.textContent)}catch(error){shareError.textContent='無法複製訂閱。'}})
`

const workspaceNavigationHTML = `<aside class="control-rail" aria-label="主要導覽"><div class="rail-brand"><span class="rail-kicker">s12ryt / IPv6</span><strong>NOC 控制台</strong><small>資料平面與出口管理</small></div><nav class="workspace-nav" role="tablist" aria-label="工作區導覽"><button role="tab" id="workspace-tab-strategy" data-workspace-target="strategy" aria-controls="workspace-strategy" aria-selected="true" tabindex="0">策略</button><button role="tab" id="workspace-tab-nodes" data-workspace-target="nodes" aria-controls="workspace-nodes" aria-selected="false" tabindex="-1">節點</button><button role="tab" id="workspace-tab-remotes" data-workspace-target="remotes" aria-controls="workspace-remotes" aria-selected="false" tabindex="-1">遠端出口</button><button role="tab" id="workspace-tab-network" data-workspace-target="network" aria-controls="workspace-network" aria-selected="false" tabindex="-1">網路</button><button role="tab" id="workspace-tab-shares" data-workspace-target="shares" aria-controls="workspace-shares" aria-selected="false" tabindex="-1">分享</button></nav></aside>`

const workspaceNavigationScript = `
;const workspaceHashes=['#strategy','#nodes','#remotes','#network','#shares'];const workspaceTabs=[...document.querySelectorAll('[data-workspace-target]')];const workspacePanels=[...document.querySelectorAll('[data-workspace]')];function activateWorkspace(name){workspacePanels.forEach((panel)=>{panel.hidden=panel.dataset.workspace!==name});workspaceTabs.forEach((tab)=>{const active=tab.dataset.workspaceTarget===name;tab.setAttribute('aria-selected',String(active));tab.tabIndex=active?0:-1})}function syncWorkspaceFromHash(){let hash=window.location.hash;if(!workspaceHashes.includes(hash)){hash='#strategy';history.replaceState(null,'',hash)}activateWorkspace(hash.slice(1))}workspaceTabs.forEach((tab,index)=>{tab.addEventListener('click',()=>{const hash='#'+tab.dataset.workspaceTarget;if(window.location.hash===hash){activateWorkspace(tab.dataset.workspaceTarget)}else{window.location.hash=hash}});tab.addEventListener('keydown',(event)=>{let next=index;if(event.key==='Home'){next=0}else if(event.key==='End'){next=workspaceTabs.length-1}else if(['ArrowRight','ArrowDown'].includes(event.key)){next=(index+1)%workspaceTabs.length}else if(['ArrowLeft','ArrowUp'].includes(event.key)){next=(index-1+workspaceTabs.length)%workspaceTabs.length}else{return}event.preventDefault();workspaceTabs[next].focus();workspaceTabs[next].click()})});window.addEventListener('hashchange',syncWorkspaceFromHash);syncWorkspaceFromHash()
`

const modalAccessibilityScript = `
;const modalFocusableSelector='button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),a[href],[tabindex]:not([tabindex="-1"])';let previouslyFocusedElement=null;function focusableModalControls(modal){return[...modal.querySelectorAll(modalFocusableSelector)].filter((control)=>!control.hidden&&control.getAttribute('aria-hidden')!=='true'&&control.offsetParent!==null)}function openModal(modal,trigger=document.activeElement){if(!modal){return}previouslyFocusedElement=trigger instanceof HTMLElement?trigger:null;modal.hidden=false;queueMicrotask(()=>{const controls=focusableModalControls(modal);const initial=modal.querySelector('[data-modal-initial-focus]')||controls[0]||modal.querySelector('[role="dialog"]');if(initial){if(initial.matches('[role="dialog"]')&&!initial.hasAttribute('tabindex')){initial.tabIndex=-1}initial.focus()}})}function trapModalFocus(event,modal){if(event.key!=='Tab'){return}const controls=focusableModalControls(modal);if(controls.length===0){event.preventDefault();const dialog=modal.querySelector('[role="dialog"]');if(dialog){dialog.tabIndex=-1;dialog.focus()}return}const first=controls[0];const last=controls[controls.length-1];if(event.shiftKey&&(document.activeElement===first||!modal.contains(document.activeElement))){event.preventDefault();last.focus()}else if(!event.shiftKey&&(document.activeElement===last||!modal.contains(document.activeElement))){event.preventDefault();first.focus()}}const closeModalWithoutFocus=closeModal;closeModal=function(modal){closeModalWithoutFocus(modal);const returnTarget=previouslyFocusedElement;previouslyFocusedElement=null;if(returnTarget&&returnTarget.isConnected){queueMicrotask(()=>returnTarget.focus())}};function modalNameForTrigger(trigger){if(trigger.matches('[data-section-target]')){return trigger.dataset.sectionTarget}if(trigger.matches('[data-node-create],[data-node-edit],[data-protocol]')){return'node-editor'}if(trigger.matches('[data-credential-reveal]')){return'credential'}if(trigger.matches('[data-remote-import]')){return'remote-import'}if(trigger.matches('[data-network-apply]')){return'network-setup'}if(trigger.matches('[data-share-open]')){return'share-reveal'}return''}document.addEventListener('click',(event)=>{const trigger=event.target.closest('[data-section-target],[data-node-create],[data-node-edit],[data-protocol],[data-credential-reveal],[data-remote-import],[data-network-apply],[data-share-open]');if(!trigger){return}const modalName=modalNameForTrigger(trigger);if(modalName){openModal(document.querySelector('[data-modal="'+modalName+'"]'),trigger)}},true);document.addEventListener('keydown',(event)=>{if(event.key!=='Tab'){return}const openModals=modals.filter((modal)=>!modal.hidden);if(openModals.length>0){trapModalFocus(event,openModals[openModals.length-1])}},true)
`

const operationStateScript = `
;const operationNotice=document.querySelector('[data-operation-notice]');const inFlightOperations=new WeakSet();let pendingOperationScope=null;function announceOperation(message,state){operationNotice.textContent=message;operationNotice.dataset.state=state;operationNotice.hidden=false}function runOperation(scope){if(inFlightOperations.has(scope)){return false}inFlightOperations.add(scope);scope.setAttribute('aria-busy','true');scope.querySelectorAll('button,input,select,textarea').forEach((control)=>{control.dataset.operationWasDisabled=String(control.disabled);control.disabled=true});pendingOperationScope=scope;announceOperation('處理中，請稍候…','pending');queueMicrotask(()=>{if(pendingOperationScope===scope){pendingOperationScope=null;finishOperation(scope,false)}});return true}function finishOperation(scope,succeeded){scope.removeAttribute('aria-busy');scope.querySelectorAll('[data-operation-was-disabled]').forEach((control)=>{control.disabled=control.dataset.operationWasDisabled==='true';delete control.dataset.operationWasDisabled});inFlightOperations.delete(scope);announceOperation(succeeded?'操作已完成。':'操作失敗，請修正後重試。',succeeded?'success':'error')}function markDangerousActions(){document.querySelectorAll('.danger-button').forEach((button)=>{button.setAttribute('data-danger-action','');const guidance=button.closest('[data-workspace]')?.querySelector('[data-danger-guidance]');if(guidance?.id){button.setAttribute('aria-describedby',guidance.id)}})}const dangerousActionObserver=new MutationObserver(markDangerousActions);dangerousActionObserver.observe(shell,{childList:true,subtree:true});markDangerousActions();const requestJSONWithoutOperationState=requestJSON;requestJSON=async function(url,options={}){const scope=pendingOperationScope;pendingOperationScope=null;try{const result=await requestJSONWithoutOperationState(url,options);if(scope){finishOperation(scope,true)}return result}catch(error){if(scope){finishOperation(scope,false)}throw error}};document.querySelectorAll('[data-node-form],[data-credential-form],[data-remote-form],[data-fallback-form],[data-network-form]').forEach((form)=>form.addEventListener('submit',(event)=>{if(!runOperation(form)){event.preventDefault();event.stopImmediatePropagation()}},true));const protectedShareForm=document.querySelector('[data-share-form]');protectedShareForm.addEventListener('submit',async(event)=>{event.preventDefault();event.stopImmediatePropagation();if(!runOperation(protectedShareForm)){return}shareError.textContent='';try{const result=await requestJSON(shareRevealEndpoint,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify({password:sharePassword.value})});renderProtectedShares(result)}catch(error){clearShareSecrets();shareError.textContent=error.message}})
`

const mutationReliabilityScript = `
;const managedMutationSelector='[data-config-save],[data-node-delete],[data-remote-toggle],[data-remote-delete]';let managedOperation=null;function operationControls(scope){const controls=[];if(scope.matches('button,input,select,textarea')){controls.push(scope)}scope.querySelectorAll('button,input,select,textarea').forEach((control)=>controls.push(control));return controls}function beginManagedOperation(scope){if(managedOperation){return false}const controls=operationControls(scope);controls.forEach((control)=>{control.dataset.managedOperationWasDisabled=String(control.disabled);control.disabled=true});scope.setAttribute('aria-busy','true');managedOperation={scope,activeRequests:0,succeeded:true,settleTimer:null,startedRequest:false};announceOperation('處理中，請稍候…','pending');queueMicrotask(()=>{if(managedOperation?.scope===scope&&!managedOperation.startedRequest){cancelManagedOperation(scope)}});return true}function restoreManagedOperation(scope){operationControls(scope).forEach((control)=>{if(control.dataset.managedOperationWasDisabled!==undefined){control.disabled=control.dataset.managedOperationWasDisabled==='true';delete control.dataset.managedOperationWasDisabled}});scope.removeAttribute('aria-busy')}function cancelManagedOperation(scope){if(managedOperation?.scope!==scope){return}if(managedOperation.settleTimer){clearTimeout(managedOperation.settleTimer)}restoreManagedOperation(scope);managedOperation=null;operationNotice.hidden=true}function scheduleManagedOperationFinish(operation){if(operation.activeRequests!==0){return}if(operation.settleTimer){clearTimeout(operation.settleTimer)}operation.settleTimer=setTimeout(()=>{if(managedOperation!==operation||operation.activeRequests!==0){return}restoreManagedOperation(operation.scope);managedOperation=null;announceOperation(operation.succeeded?'操作已完成。':'操作失敗，請修正後重試。',operation.succeeded?'success':'error')},0)}const fetchWithoutManagedOperation=window.fetch.bind(window);window.fetch=async function(...args){const operation=managedOperation;if(operation){operation.startedRequest=true;operation.activeRequests+=1;if(operation.settleTimer){clearTimeout(operation.settleTimer);operation.settleTimer=null}}try{const response=await fetchWithoutManagedOperation(...args);if(operation&&!response.ok){operation.succeeded=false}return response}catch(error){if(operation){operation.succeeded=false}throw error}finally{if(operation){operation.activeRequests-=1;scheduleManagedOperationFinish(operation)}}};document.addEventListener('click',(event)=>{const scope=event.target.closest?.(managedMutationSelector);if(!scope){return}if(!beginManagedOperation(scope)){event.preventDefault();event.stopImmediatePropagation()}},true)
`

const accessibilityStyle = `<style>
.skip-link{position:fixed;left:14px;top:12px;z-index:60;transform:translateY(-160%);border:2px solid var(--focus);background:#111820;color:#fff;padding:10px 14px;font-weight:800;text-decoration:none}.skip-link:focus{transform:translateY(0)}:where(a,button,input,select,textarea,[tabindex]):focus-visible{outline:3px solid var(--focus);outline-offset:3px}
</style>`

const nocDashboardStyle = `<style>
:root{color-scheme:dark;--ink:#e6edf3;--paper:#080c11;--panel:#111820;--line:#2d3b47;--accent:#43d18d;--signal:#ff8469;--muted:#91a1ad;--focus:#ffd166}html[data-ui-theme="noc"] body{background:var(--paper);color:var(--ink);font-family:"IBM Plex Mono","Cascadia Mono","Noto Sans TC",Consolas,monospace;letter-spacing:0}.dashboard-frame{display:grid;grid-template-columns:228px minmax(0,1fr);min-height:100vh}.operation-notice{position:fixed;right:18px;bottom:18px;z-index:30;max-width:min(420px,calc(100vw - 36px));border:1px solid var(--line);border-left:4px solid var(--accent);background:#101820;color:var(--ink);padding:11px 14px;box-shadow:0 10px 28px rgba(0,0,0,.35);font-size:13px}.operation-notice[data-state="pending"]{border-left-color:var(--focus)}.operation-notice[data-state="error"]{border-left-color:var(--signal)}.control-rail{min-height:100vh;border-right:1px solid var(--line);background:#0b1118;padding:20px 14px}.rail-brand{display:grid;gap:5px;padding:5px 8px 22px;border-bottom:1px solid var(--line)}.rail-brand strong{font-size:18px}.rail-brand small{color:var(--muted);font-size:11px}.rail-kicker{color:var(--accent);font-size:11px;font-weight:800;text-transform:uppercase}.workspace-nav{display:flex;flex-direction:column;gap:6px;position:sticky;top:16px;margin:16px 0;border:0;background:transparent}.workspace-nav button{min-height:42px;border:1px solid transparent;border-left:3px solid transparent;background:transparent;color:var(--muted);padding:10px 12px;text-align:left;font-family:inherit;font-size:13px;font-weight:700;cursor:pointer}.workspace-nav button:hover{border-color:var(--line);color:var(--ink);background:#121c25}.workspace-nav button[aria-selected="true"]{border-color:#2b5947;border-left-color:var(--accent);background:#10251d;color:#c8ffe4}.shell{width:100%;max-width:1480px;margin:0;padding:18px 24px 32px}.masthead{border-color:var(--line)}.masthead .status{color:var(--accent)}.masthead button,.strategy-actions button{border:1px solid var(--line);background:#111820;color:var(--ink);cursor:pointer}.strategy-actions{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;margin:18px 0}.strategy-actions button{min-height:46px;padding:12px;text-align:left;font-weight:800}.strategy-actions button:hover{border-color:var(--accent);background:#10251d}.workspace-panel[hidden]{display:none!important}.workspace-panel{min-width:0}.work,.telemetry,html[data-ui-theme="noc"] .node-workspace,html[data-ui-theme="noc"] .remote-workspace,html[data-ui-theme="noc"] .network-workspace,html[data-ui-theme="noc"] .share-workspace{background:var(--panel);border-color:var(--line);color:var(--ink);box-shadow:none}.warning{color:#ffc1b3}.error{color:var(--signal)}.muted{color:var(--muted)}html[data-ui-theme="noc"] .dialog{background:#101820;color:var(--ink);border-color:var(--accent)}html[data-ui-theme="noc"] input,html[data-ui-theme="noc"] select,html[data-ui-theme="noc"] textarea{background:#0a1016;color:var(--ink);border:1px solid #40515f}html[data-ui-theme="noc"] button{letter-spacing:0}html[data-ui-theme="noc"] button:disabled,html[data-ui-theme="noc"] input:disabled,html[data-ui-theme="noc"] select:disabled,html[data-ui-theme="noc"] textarea:disabled{cursor:wait;opacity:.58}html[data-ui-theme="noc"] button:focus-visible,html[data-ui-theme="noc"] input:focus-visible,html[data-ui-theme="noc"] select:focus-visible,html[data-ui-theme="noc"] textarea:focus-visible{outline:3px solid var(--focus);outline-offset:3px}html[data-ui-theme="noc"] .choice,html[data-ui-theme="noc"] table th,html[data-ui-theme="noc"] table td{border-color:var(--line)}html[data-ui-theme="noc"] .secret-output,html[data-ui-theme="noc"] .network-result,html[data-ui-theme="noc"] .share-field pre{background:#091017;color:var(--ink);border-color:var(--line)}html[data-ui-theme="noc"] .node-heading button,html[data-ui-theme="noc"] .node-actions button,html[data-ui-theme="noc"] .remote-heading button,html[data-ui-theme="noc"] .remote-actions button,html[data-ui-theme="noc"] .fallback-form button,html[data-ui-theme="noc"] .network-actions button,html[data-ui-theme="noc"] .share-heading button,html[data-ui-theme="noc"] .share-card button{background:#111820;color:var(--ink);border-color:var(--line)}html[data-ui-theme="noc"] .node-heading button,html[data-ui-theme="noc"] .remote-heading button,html[data-ui-theme="noc"] .network-actions button:last-child,html[data-ui-theme="noc"] .share-heading button{background:var(--accent);color:#05120c;border-color:var(--accent)}
@media(max-width:760px){.dashboard-frame{display:block}.control-rail{min-height:auto;position:sticky;top:0;z-index:8;padding:8px 12px;border-right:0;border-bottom:1px solid var(--line)}.rail-brand{display:none}.workspace-nav{position:static;flex-direction:row;overflow-x:auto;overscroll-behavior-x:contain;scrollbar-width:thin;gap:4px;margin:0;padding-bottom:2px}.workspace-nav button{flex:0 0 auto;min-height:40px;white-space:nowrap;border-left-width:1px;border-bottom:3px solid transparent;padding:9px 13px}.workspace-nav button[aria-selected="true"]{border-left-color:#2b5947;border-bottom-color:var(--accent)}.shell{padding:12px}.masthead{align-items:flex-start;gap:12px}.strategy-actions{grid-template-columns:1fr}.grid{grid-template-columns:minmax(0,1fr)}.grid>*{grid-column:1!important;min-width:0}}
</style>`

const mobileDashboardStyle = `<style>@media(max-width:760px){.grid{grid-template-columns:minmax(0,1fr)}.grid>*{grid-column:1!important;min-width:0}}</style>`

const dashboardPage = `<!doctype html>
<html lang="zh-Hant"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="csrf-token" content="%s">
<title>s12ryt IPv6 管理面板</title><style>
:root{--ink:#111719;--paper:#eef1ee;--panel:#fff;--line:#aab2ad;--accent:#087f5b;--signal:#d9480f;--muted:#59635e}*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font-family:Georgia,"Noto Serif TC",serif}.shell{max-width:1180px;margin:auto;padding:20px}.masthead{display:flex;justify-content:space-between;align-items:end;border-bottom:3px solid var(--ink);padding:12px 0}.masthead h1{margin:0;font-size:26px}.status{font:12px Consolas,monospace;color:var(--accent)}nav{display:grid;grid-template-columns:repeat(3,1fr);gap:1px;background:var(--line);margin:20px 0;border:1px solid var(--line)}nav button{border:0;background:var(--panel);padding:16px;text-align:left;font-weight:700;cursor:pointer}nav button:hover{background:#dfe9e2;color:#075d43}.grid{display:grid;grid-template-columns:2fr 1fr;gap:16px}.work,.telemetry{background:var(--panel);border:1px solid var(--line);padding:20px}.work h2,.telemetry h2{margin-top:0}.warning,.error{color:var(--signal)}.muted{color:var(--muted)}.modal[hidden]{display:none}.modal{position:fixed;inset:0;background:rgba(17,23,25,.62);display:grid;place-items:center;padding:20px;z-index:10}.dialog{width:min(640px,100%%);max-height:calc(100vh - 40px);overflow:auto;background:#fff;border-top:6px solid var(--accent);padding:24px}.choices{display:grid;gap:8px;margin:16px 0}.choice{display:flex;gap:10px;align-items:flex-start;border:1px solid var(--line);padding:12px}.choice input{margin-top:4px}.protocols{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px}.protocols button{border:1px solid var(--line);background:#fff;color:var(--ink);text-align:left}.protocols button[aria-pressed="true"]{border-color:var(--accent);background:#dfe9e2}.actions{display:flex;justify-content:flex-end;gap:8px;margin-top:20px}.dialog button{padding:10px 16px;border:0;background:var(--ink);color:#fff;cursor:pointer}.dialog button.secondary{background:#e6e9e7;color:var(--ink)}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#15191c;color:#edf2ef;padding:12px;font:12px Consolas,monospace}@media(max-width:720px){.grid{grid-template-columns:1fr}nav,.protocols{grid-template-columns:1fr}.masthead{align-items:start;flex-direction:column;gap:6px}.shell{padding:12px}.dialog{padding:18px}}</style></head>
<body><main class="shell" data-config-endpoint="%s" data-validate-endpoint="%s" data-apply-endpoint="%s"><header class="masthead"><h1>s12ryt 多 IPv6 出站</h1><div><span class="status">PANEL ONLINE / PUBLIC HTTP</span><form method="post" action="%s"><input type="hidden" name="csrf_token" value="%s"><button type="submit">登出</button></form></div></header><p class="warning">公開 HTTP 不提供傳輸加密，請限制允許來源。</p><nav aria-label="設定導覽"><button type="button" data-section-target="routing">出口模式</button><button type="button" data-section-target="topology">拓撲</button><button type="button" data-section-target="protocols">協議</button></nav><section class="grid"><article class="work"><h2>設定工作區</h2><p>選擇上方分類以管理 IPv6 出站策略與節點。</p><p class="error" data-config-error role="alert"></p><pre data-config-diff hidden></pre></article><aside class="telemetry"><h2>狀態</h2><p>IPv6 池與 sing-box 驗證資訊將顯示於此。</p></aside></section></main>
<div class="modal" data-modal="routing" data-modal-backdrop="static" hidden><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="routing-title"><h2 id="routing-title">出口模式</h2><div class="choices"><label class="choice"><input type="radio" name="routing_mode" value="client-ipv4"%s><span>客戶端 IPv4 分流<br><small>IPv4 由客戶端直連，IPv6 經 VPS 節點。</small></span></label><label class="choice"><input type="radio" name="routing_mode" value="vps-ipv4"%s><span>VPS 雙棧出口<br><small>IPv4 使用 VPS 或指定代理，IPv6 使用出口池。</small></span></label><label class="choice"><input type="radio" name="routing_mode" value="ipv6-only"%s><span>僅 IPv6 出口<br><small>IPv4 目的地明確失敗，不自動回退。</small></span></label></div><div class="actions"><button type="button" class="secondary" data-modal-close="button">取消</button><button type="button" data-config-save>檢查並套用</button></div><span data-modal-close="escape" hidden></span></section></div>
<div class="modal" data-modal="topology" data-modal-backdrop="static" hidden><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="topology-title"><h2 id="topology-title">拓撲</h2><div class="choices"><label class="choice"><input type="radio" name="topology" value="multi-ipv6-multi-node"%s><span>多 IPv6、多節點</span></label><label class="choice"><input type="radio" name="topology" value="single-ipv6-single-node"%s><span>單 IPv6、單節點</span></label><label class="choice"><input type="radio" name="topology" value="multi-ipv6-rotating-node"%s><span>多 IPv6、單節點定期輪換</span></label><label class="choice"><input type="radio" name="topology" value="multi-ipv6-rotating-nodes"%s><span>多 IPv6、多節點錯位輪換</span></label></div><div class="actions"><button type="button" class="secondary" data-modal-close="button">取消</button><button type="button" data-config-save>檢查並套用</button></div><span data-modal-close="escape" hidden></span></section></div>
<div class="modal" data-modal="protocols" data-modal-backdrop="static" hidden><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="protocol-title"><h2 id="protocol-title">協議節點</h2><p class="muted">選擇協議以準備建立獨立節點；認證、監聽地址、TLS 與傳輸會在節點表單中驗證。</p><div class="protocols"><button type="button" data-protocol="vless" aria-pressed="false">VLESS</button><button type="button" data-protocol="vmess" aria-pressed="false">VMess</button><button type="button" data-protocol="hysteria2" aria-pressed="false">Hysteria2</button><button type="button" data-protocol="tuic" aria-pressed="false">TUIC</button><button type="button" data-protocol="socks5" aria-pressed="false">SOCKS5</button><button type="button" data-protocol="anytls" aria-pressed="false">AnyTLS</button><button type="button" data-protocol="shadowsocks" aria-pressed="false">Shadowsocks</button></div><div class="actions"><button type="button" data-modal-close="button">完成</button></div><span data-modal-close="escape" hidden></span></section></div><script>
const shell=document.querySelector('.shell');const csrf=document.querySelector('meta[name="csrf-token"]').content;let currentConfig=null;const errorBox=document.querySelector('[data-config-error]');const diffBox=document.querySelector('[data-config-diff]');const modals=[...document.querySelectorAll('[data-modal]')];function closeModal(modal){modal.hidden=true}document.querySelectorAll('[data-section-target]').forEach((button)=>button.addEventListener('click',()=>{const modal=document.querySelector('[data-modal="'+button.dataset.sectionTarget+'"]');modal.hidden=false}));document.querySelectorAll('[data-modal-close="button"]').forEach((button)=>button.addEventListener('click',()=>closeModal(button.closest('[data-modal]'))));document.addEventListener('keydown',(event)=>{if(event.key==='Escape'){modals.filter((modal)=>!modal.hidden).forEach(closeModal)}});document.querySelectorAll('[data-protocol]').forEach((button)=>button.addEventListener('click',()=>{button.setAttribute('aria-pressed',button.getAttribute('aria-pressed')!=='true')}));async function requestJSON(url,options={}){const response=await fetch(url,options);if(!response.ok){throw new Error(await response.text())}return response.json()}async function loadConfig(){try{currentConfig=await requestJSON(shell.dataset.configEndpoint)}catch(error){errorBox.textContent=error.message}}function candidateConfig(){const candidate=structuredClone(currentConfig);candidate.routing.mode=document.querySelector('input[name="routing_mode"]:checked').value;candidate.routing.topology=document.querySelector('input[name="topology"]:checked').value;return candidate}async function saveConfig(){errorBox.textContent='';diffBox.hidden=true;try{if(!currentConfig){throw new Error('目前設定尚未載入。')}const candidate=candidateConfig();await requestJSON(shell.dataset.validateEndpoint,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify(candidate)});diffBox.textContent=JSON.stringify({before:currentConfig.routing,after:candidate.routing},null,2);diffBox.hidden=false;if(!window.confirm('確認套用顯示的設定差異？')){return}await requestJSON(shell.dataset.applyEndpoint,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'X-S12ryt-Confirm':'apply'},body:JSON.stringify(candidate)});currentConfig=candidate;modals.forEach(closeModal)}catch(error){errorBox.textContent=error.message}}document.querySelectorAll('[data-config-save]').forEach((button)=>button.addEventListener('click',saveConfig));loadConfig();
</script></body></html>`
