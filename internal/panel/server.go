package panel

import (
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

const SessionCookieName = "s12ryt_panel_session"

type Options struct {
	BasePath     string
	PasswordHash string
	Hasher       *auth.PasswordHasher
	Sessions     *auth.SessionManager
	Limiter      *auth.LoginLimiter
	Config       domain.Config
}

type Server struct {
	basePath     string
	passwordHash string
	hasher       *auth.PasswordHasher
	sessions     *auth.SessionManager
	limiter      *auth.LoginLimiter
	config       domain.Config
}

func NewServer(options Options) *Server {
	return &Server{
		basePath:     strings.TrimRight(options.BasePath, "/"),
		passwordHash: options.PasswordHash,
		hasher:       options.Hasher,
		sessions:     options.Sessions,
		limiter:      options.Limiter,
		config:       options.Config,
	}
}

func (server *Server) Handler() http.Handler {
	return http.HandlerFunc(server.serveHTTP)
}

func (server *Server) serveHTTP(response http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == server.basePath:
		server.showPanel(response, request)
	case request.Method == http.MethodPost && request.URL.Path == server.basePath+"/login":
		server.handleLogin(response, request)
	case request.Method == http.MethodPost && request.URL.Path == server.basePath+"/api/config/validate":
		server.validateConfigRequest(response, request)
	default:
		http.NotFound(response, request)
	}
}

func (server *Server) showPanel(response http.ResponseWriter, request *http.Request) {
	session, authenticated := server.requestSession(request)
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	if !authenticated {
		_, _ = fmt.Fprintf(response, loginPage, html.EscapeString(server.basePath+"/login"))
		return
	}
	_, _ = fmt.Fprintf(response, dashboardPage, html.EscapeString(session.CSRFToken))
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

func (server *Server) validateConfigRequest(response http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		http.Error(response, "需要登入。", http.StatusUnauthorized)
		return
	}
	clientIP := requestClientIP(request)
	if _, valid := server.sessions.Lookup(cookie.Value, clientIP); !valid {
		http.Error(response, "登入工作階段無效。", http.StatusUnauthorized)
		return
	}
	if !server.sessions.Validate(cookie.Value, request.Header.Get("X-CSRF-Token"), clientIP) {
		http.Error(response, "CSRF 驗證失敗。", http.StatusForbidden)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(`{"valid":true}`))
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

const dashboardPage = `<!doctype html>
<html lang="zh-Hant"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="csrf-token" content="%s">
<title>s12ryt IPv6 管理面板</title><style>
:root{--ink:#111719;--paper:#eef1ee;--panel:#fff;--line:#aab2ad;--accent:#087f5b;--signal:#d9480f}*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font-family:Georgia,"Noto Serif TC",serif}.shell{max-width:1180px;margin:auto;padding:20px}.masthead{display:flex;justify-content:space-between;align-items:end;border-bottom:3px solid var(--ink);padding:12px 0}.masthead h1{margin:0;font-size:26px}.status{font:12px Consolas,monospace;color:var(--accent)}nav{display:grid;grid-template-columns:repeat(3,1fr);gap:1px;background:var(--line);margin:20px 0;border:1px solid var(--line)}nav button{border:0;background:var(--panel);padding:16px;text-align:left;font-weight:700;cursor:pointer}nav button:hover{background:#dfe9e2;color:#075d43}.grid{display:grid;grid-template-columns:2fr 1fr;gap:16px}.work,.telemetry{background:var(--panel);border:1px solid var(--line);padding:20px}.work h2,.telemetry h2{margin-top:0}.warning{color:var(--signal)}.modal[hidden]{display:none}.modal{position:fixed;inset:0;background:rgba(17,23,25,.62);display:grid;place-items:center;padding:20px}.dialog{width:min(560px,100%%);background:#fff;border-top:6px solid var(--accent);padding:24px}.dialog button{padding:10px 16px;border:0;background:var(--ink);color:#fff}@media(max-width:720px){.grid{grid-template-columns:1fr}nav{grid-template-columns:1fr}.masthead{align-items:start;flex-direction:column;gap:6px}}</style></head>
<body><main class="shell"><header class="masthead"><h1>s12ryt 多 IPv6 出站</h1><span class="status">PANEL ONLINE / PUBLIC HTTP</span></header><p class="warning">公開 HTTP 不提供傳輸加密，請限制允許來源。</p><nav aria-label="設定導覽"><button type="button">出口模式</button><button type="button">拓撲</button><button type="button" data-modal-open>協議</button></nav><section class="grid"><article class="work"><h2>設定工作區</h2><p>選擇上方分類以管理 IPv6 出站策略與節點。</p></article><aside class="telemetry"><h2>狀態</h2><p>IPv6 池與 sing-box 驗證資訊將顯示於此。</p></aside></section></main><div class="modal" data-modal-backdrop="static" hidden><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="modal-title"><h2 id="modal-title">協議設定</h2><p>VLESS、VMess、Hysteria2、TUIC、SOCKS5、AnyTLS、Shadowsocks</p><button type="button" data-modal-close="button">關閉</button><span data-modal-close="escape" hidden></span></section></div><script>
const modal=document.querySelector('.modal');document.querySelector('[data-modal-open]').addEventListener('click',()=>{modal.hidden=false});document.querySelector('[data-modal-close="button"]').addEventListener('click',()=>{modal.hidden=true});document.addEventListener('keydown',(event)=>{if(event.key==='Escape'){modal.hidden=true}});
</script></body></html>`
