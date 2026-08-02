package panel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/nodes"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
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

type Options struct {
	BasePath      string
	PasswordHash  string
	Hasher        *auth.PasswordHasher
	Sessions      *auth.SessionManager
	Limiter       *auth.LoginLimiter
	Config        domain.Config
	Store         ConfigStore
	NodeManager   NodeManager
	RemoteManager RemoteManager
	Clock         func() time.Time
}

type Server struct {
	basePath      string
	passwordHash  string
	hasher        *auth.PasswordHasher
	sessions      *auth.SessionManager
	limiter       *auth.LoginLimiter
	mutex         sync.RWMutex
	config        domain.Config
	store         ConfigStore
	nodeManager   NodeManager
	remoteManager RemoteManager
	clock         func() time.Time
	elevationMu   sync.Mutex
	elevations    map[string]time.Time
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
		basePath:      strings.TrimRight(options.BasePath, "/"),
		passwordHash:  options.PasswordHash,
		hasher:        options.Hasher,
		sessions:      options.Sessions,
		limiter:       options.Limiter,
		config:        config,
		store:         options.Store,
		nodeManager:   options.NodeManager,
		remoteManager: options.RemoteManager,
		clock:         clock,
		elevations:    make(map[string]time.Time),
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
	case strings.HasPrefix(request.URL.Path, server.basePath+"/api/nodes/"):
		server.handleNamedNode(response, request)
	default:
		http.NotFound(response, request)
	}
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
	rendered := strings.Replace(
		page.String(),
		`<main class="shell"`,
		`<main class="shell" data-nodes-endpoint="`+html.EscapeString(server.basePath+"/api/nodes")+`" data-credential-endpoint-template="`+html.EscapeString(server.basePath+"/api/nodes/{id}/credential")+`"`,
		1,
	)
	rendered = strings.Replace(rendered, `</section></main>`, nodeWorkspaceHTML+`</section></main>`, 1)
	rendered = strings.Replace(rendered, `<script>`, nodeModalsHTML+`<script>`, 1)
	rendered = strings.Replace(rendered, `<label class="choice"><input name="node_enabled"`, nodeDeploymentFieldsHTML+`<label class="choice"><input name="node_enabled"`, 1)
	rendered = strings.Replace(rendered, `</script>`, nodeManagementScript+nodeDeploymentScript+`</script>`, 1)
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
<html lang="zh-Hant"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>s12ryt IPv6 管理面板</title><style>
:root{color-scheme:light;--ink:#15191c;--paper:#f2f4f1;--line:#a8b0ab;--accent:#087f5b;--warn:#a12622}*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font-family:Georgia,"Noto Serif TC",serif;min-height:100vh;display:grid;place-items:center;padding:24px}.login{width:min(440px,100%%);border:1px solid var(--line);border-top:6px solid var(--accent);padding:32px;background:#fff;box-shadow:8px 8px 0 #d8ddd9}.eyebrow{font:700 12px Consolas,monospace;text-transform:uppercase;color:var(--accent)}h1{font-size:28px;margin:8px 0 24px}.warning{border-left:4px solid var(--warn);padding:10px 12px;background:#fff1f0;color:#701b18;font-size:14px}label{display:block;margin:24px 0 8px;font-weight:700}input{width:100%%;padding:12px;border:1px solid #717975;font:16px Consolas,monospace}button{width:100%%;margin-top:16px;padding:12px;border:0;background:var(--ink);color:#fff;font-weight:700;cursor:pointer}button:hover{background:var(--accent)}</style></head>
<body><main class="login"><div class="eyebrow">s12ryt / network control</div><h1>登入 IPv6 管理面板</h1><p class="warning">目前使用公開 HTTP，密碼可能被攔截。請只在理解風險後繼續。</p><form method="post" action="%s"><label for="password">管理密碼</label><input id="password" name="password" type="password" autocomplete="current-password" required><button type="submit">登入</button></form></main></body></html>`

const nodeWorkspaceHTML = `<section class="node-workspace" aria-labelledby="nodes-title"><style>
.node-workspace{margin-top:16px;background:#fff;border:1px solid var(--line);padding:20px}.node-heading{display:flex;align-items:center;justify-content:space-between;gap:16px}.node-heading h2{margin:0}.node-heading button,.node-actions button{border:1px solid var(--line);background:#fff;color:var(--ink);padding:8px 11px;cursor:pointer}.node-heading button{background:var(--ink);color:#fff}.node-table-wrap{overflow-x:auto;margin-top:16px}.node-table{width:100%;border-collapse:collapse;min-width:680px}.node-table th,.node-table td{text-align:left;border-bottom:1px solid var(--line);padding:10px 8px}.node-table th{font:700 12px Consolas,monospace;text-transform:uppercase;color:var(--muted)}.node-actions{display:flex;gap:6px;white-space:nowrap}.field-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}.field-grid label{display:grid;gap:6px;font-weight:700}.field-grid input,.field-grid select{width:100%;padding:10px;border:1px solid var(--line);background:#fff}.secret-output{border:1px solid var(--line);background:#f5f7f5;padding:12px;white-space:pre-wrap;overflow-wrap:anywhere}.danger{color:#8f1d18}.danger-button{background:#8f1d18!important}@media(max-width:700px){.node-heading{align-items:flex-start;flex-direction:column}.field-grid{grid-template-columns:1fr}.node-actions{flex-wrap:wrap}}
</style><div class="node-heading"><div><h2 id="nodes-title">協議節點</h2><p class="muted">認證預設遮罩；揭露前必須重新驗證管理密碼。</p></div><button type="button" data-node-create>新增節點</button></div><p class="error" data-node-error role="alert"></p><div class="node-table-wrap"><table class="node-table" data-node-table><thead><tr><th>節點</th><th>協議</th><th>連接埠</th><th>狀態</th><th>操作</th></tr></thead><tbody data-node-table-body><tr><td colspan="5">正在載入節點...</td></tr></tbody></table></div></section>`

const nodeDeploymentFieldsHTML = `<label>IPv4 監聽地址<input name="node_listener_ipv4" inputmode="decimal" placeholder="198.51.100.10"></label><label>IPv6 監聽地址<input name="node_listener_ipv6" placeholder="2001:db8::10"></label><label class="choice"><input name="node_tls_enabled" type="checkbox">啟用 TLS</label><label>伺服器名稱<input name="node_server_name" placeholder="node.example.com"></label><label>憑證路徑<input name="node_certificate_path" placeholder="/opt/s12ryt-ipv6/tls/server.crt"></label><label>私鑰路徑<input name="node_key_path" placeholder="/opt/s12ryt-ipv6/tls/server.key"></label><label>傳輸<select name="node_transport"><option value="tcp">TCP</option><option value="websocket">WebSocket</option><option value="grpc">gRPC</option></select></label><label>WebSocket 路徑<input name="node_transport_path" placeholder="/edge"></label><label>gRPC 服務名稱<input name="node_grpc_service_name" placeholder="edge.Service"></label>`

const nodeModalsHTML = `<div class="modal" data-modal="node-editor" data-modal-backdrop="static" hidden><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="node-editor-title"><h2 id="node-editor-title">節點設定</h2><form data-node-form><div class="field-grid"><label>節點 ID<input name="node_id" pattern="[A-Za-z0-9][A-Za-z0-9_.-]{0,63}" maxlength="64" required></label><label>協議<select name="node_protocol" required><option value="vless">VLESS</option><option value="vmess">VMess</option><option value="hysteria2">Hysteria2</option><option value="tuic">TUIC</option><option value="socks5">SOCKS5</option><option value="anytls">AnyTLS</option><option value="shadowsocks">Shadowsocks</option></select></label><label>連接埠<input name="node_port" type="number" min="20000" max="49999" placeholder="留空自動分配"></label><label class="choice"><input name="node_enabled" type="checkbox" checked>啟用節點</label></div><p class="muted">建立後會產生此節點專用的隨機認證。編輯不會更換既有認證。</p><div class="actions"><button type="button" class="secondary" data-modal-close="button">取消</button><button type="submit">確認並套用</button></div></form><span data-modal-close="escape" hidden></span></section></div>
<div class="modal" data-modal="credential" data-modal-backdrop="static" hidden><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="credential-title"><h2 id="credential-title">揭露節點認證</h2><p class="warning">公開 HTTP 可能洩漏密碼與節點認證。請先限制允許來源。</p><form data-credential-form><input name="credential_node_id" type="hidden"><label>管理密碼<input name="management_password" type="password" autocomplete="current-password"></label><p class="muted">首次揭露必須輸入密碼；同一工作階段驗證後五分鐘內可留空。</p><div class="actions"><button type="button" class="secondary" data-modal-close="button">取消</button><button type="submit" data-credential-reveal>驗證並揭露</button></div></form><pre class="secret-output" data-credential-value hidden></pre><p class="muted" data-credential-expiry></p><div class="actions"><button type="button" class="secondary" data-credential-copy hidden>複製認證</button></div><span data-modal-close="escape" hidden></span></section></div>`

const nodeManagementScript = `
;const nodesEndpoint=shell.dataset.nodesEndpoint;const credentialEndpointTemplate=shell.dataset.credentialEndpointTemplate;const nodeTableBody=document.querySelector('[data-node-table-body]');const nodeError=document.querySelector('[data-node-error]');const nodeForm=document.querySelector('[data-node-form]');const credentialForm=document.querySelector('[data-credential-form]');const credentialValue=document.querySelector('[data-credential-value]');const credentialExpiry=document.querySelector('[data-credential-expiry]');const credentialCopy=document.querySelector('[data-credential-copy]');let managedNodes=[];let editingNodeID='';function openManagedModal(name){document.querySelector('[data-modal="'+name+'"]').hidden=false}function mutationHeaders(){return{'Content-Type':'application/json','X-CSRF-Token':csrf,'X-S12ryt-Confirm':'apply'}}function actionButton(label,attribute,handler){const button=document.createElement('button');button.type='button';button.textContent=label;button.setAttribute(attribute,'');button.addEventListener('click',handler);return button}function renderNodes(){nodeTableBody.textContent='';if(managedNodes.length===0){const row=document.createElement('tr');const cell=document.createElement('td');cell.colSpan=5;cell.textContent='尚未建立節點。';row.append(cell);nodeTableBody.append(row);return}managedNodes.forEach((node)=>{const row=document.createElement('tr');[node.id,node.protocol,String(node.port),node.enabled?'啟用':'停用'].forEach((value)=>{const cell=document.createElement('td');cell.textContent=value;row.append(cell)});const actions=document.createElement('td');actions.className='node-actions';actions.append(actionButton('編輯','data-node-edit',()=>openNodeEditor(node)));actions.append(actionButton('認證','data-credential-reveal',()=>openCredential(node.id)));const remove=actionButton('刪除','data-node-delete',()=>deleteNode(node.id));remove.className='danger-button';actions.append(remove);row.append(actions);nodeTableBody.append(row)})}async function loadNodes(){try{managedNodes=await requestJSON(nodesEndpoint);renderNodes()}catch(error){nodeError.textContent=error.message}}function openNodeEditor(node=null,protocol='vless'){editingNodeID=node?node.id:'';nodeForm.reset();nodeForm.elements.node_id.value=node?node.id:'';nodeForm.elements.node_protocol.value=node?node.protocol:protocol;nodeForm.elements.node_port.value=node?String(node.port):'';nodeForm.elements.node_enabled.checked=node?node.enabled:true;nodeForm.elements.node_id.disabled=Boolean(node);nodeForm.elements.node_protocol.disabled=Boolean(node);openManagedModal('node-editor')}document.querySelector('[data-node-create]').addEventListener('click',()=>openNodeEditor());document.querySelectorAll('[data-protocol]').forEach((button)=>button.addEventListener('click',()=>{closeModal(button.closest('[data-modal]'));openNodeEditor(null,button.dataset.protocol)}));nodeForm.addEventListener('submit',async(event)=>{event.preventDefault();nodeError.textContent='';const port=Number(nodeForm.elements.node_port.value||0);const payload=editingNodeID?{port:port,enabled:nodeForm.elements.node_enabled.checked}:{id:nodeForm.elements.node_id.value,protocol:nodeForm.elements.node_protocol.value,port:port,enabled:nodeForm.elements.node_enabled.checked};try{const url=editingNodeID?nodesEndpoint+'/'+encodeURIComponent(editingNodeID):nodesEndpoint;await requestJSON(url,{method:editingNodeID?'PATCH':'POST',headers:mutationHeaders(),body:JSON.stringify(payload)});closeModal(nodeForm.closest('[data-modal]'));await loadNodes()}catch(error){nodeError.textContent=error.message}});async function deleteNode(id){if(!window.confirm('確定刪除節點 '+id+'？此操作不會顯示認證。')){return}nodeError.textContent='';try{const response=await fetch(nodesEndpoint+'/'+encodeURIComponent(id),{method:'DELETE',headers:mutationHeaders()});if(!response.ok){throw new Error(await response.text())}await loadNodes()}catch(error){nodeError.textContent=error.message}}function clearCredential(){credentialForm.reset();credentialValue.textContent='';credentialValue.hidden=true;credentialExpiry.textContent='';credentialCopy.hidden=true}function openCredential(id){clearCredential();credentialForm.elements.credential_node_id.value=id;openManagedModal('credential')}credentialForm.addEventListener('submit',async(event)=>{event.preventDefault();nodeError.textContent='';const id=credentialForm.elements.credential_node_id.value;const endpoint=credentialEndpointTemplate.replace('{id}',encodeURIComponent(id));try{const result=await requestJSON(endpoint,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify({password:credentialForm.elements.management_password.value})});credentialForm.elements.management_password.value='';credentialValue.textContent=JSON.stringify(result.credential,null,2);credentialValue.hidden=false;credentialExpiry.textContent='本次工作階段尚可揭露 '+result.expires_in_seconds+' 秒';credentialCopy.hidden=false}catch(error){nodeError.textContent=error.message}});credentialCopy.addEventListener('click',async()=>{try{await navigator.clipboard.writeText(credentialValue.textContent);credentialExpiry.textContent='已複製；敏感資料仍會在五分鐘後要求重新驗證。'}catch(error){credentialExpiry.textContent='瀏覽器拒絕剪貼簿存取，請手動複製。'}});document.querySelectorAll('[data-modal-close="button"]').forEach((button)=>button.addEventListener('click',()=>{if(button.closest('[data-modal="credential"]')){clearCredential()}}));document.addEventListener('keydown',(event)=>{if(event.key==='Escape'){clearCredential()}});loadNodes();`

const nodeDeploymentScript = `
;nodeForm.addEventListener('submit',async(event)=>{if(editingNodeID){return}event.preventDefault();event.stopImmediatePropagation();nodeError.textContent='';const data=new FormData(nodeForm);const listeners=[String(data.get('node_listener_ipv4')||'').trim(),String(data.get('node_listener_ipv6')||'').trim()].filter(Boolean);if(listeners.length===0){nodeError.textContent='至少需要一個 IPv4 或 IPv6 監聽地址。';return}const transportType=String(data.get('node_transport')||'tcp');const transport={};if(transportType==='websocket'){transport.type='websocket';transport.path=String(data.get('node_transport_path')||'').trim()}else if(transportType==='grpc'){transport.type='grpc';transport.service_name=String(data.get('node_grpc_service_name')||'').trim()}const tls={enabled:data.get('node_tls_enabled')==='on'};if(tls.enabled){tls.server_name=String(data.get('node_server_name')||'').trim();tls.certificate_path=String(data.get('node_certificate_path')||'').trim();tls.key_path=String(data.get('node_key_path')||'').trim()}const portValue=String(data.get('node_port')||'').trim();const payload={id:String(data.get('node_id')||'').trim(),protocol:String(data.get('node_protocol')||''),port:portValue?Number(portValue):0,enabled:data.get('node_enabled')==='on',deployment:{listeners:listeners,tls:tls,transport:transport}};try{await requestJSON(nodesEndpoint,{method:'POST',headers:mutationHeaders(),body:JSON.stringify(payload)});await loadNodes();closeModal(nodeForm.closest('[data-modal]'));nodeForm.reset()}catch(error){nodeError.textContent=error.message}},true)
`

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
