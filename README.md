# s12ryt 的 VPS 腳本

[![CI](https://github.com/s12ryt/s12ryt-vps-sh/actions/workflows/ci.yml/badge.svg)](https://github.com/s12ryt/s12ryt-vps-sh/actions/workflows/ci.yml)

版本：`v1.1.0`

一個跨發行版 VPS 管理工具，提供系統資訊、一般系統升級、網路診斷、PRoot 客體、Supervisor 服務管理、Fanout、Node.js、Python、多 IPv6 出站 Web 管理面板與自我更新。主選單以 Bash 實作；多 IPv6 出站面板使用 Go 單一執行檔與獨立的 sing-box 資料平面。

> **English summary:** A Traditional Chinese VPS toolkit for diagnostics, runtime management, and a low-footprint multi-IPv6 egress control panel powered by Go and sing-box. See [Limitations](#限制與安全說明) before using privileged or public-network features.

## 快速開始

### 可變 main process substitution

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/main/s12ryt.sh)
```

這條命令會直接執行可隨時變更的 `main` 分支內容，適合快速試用，但不具版本可重現性，執行前應先審閱腳本。process substitution 是暫時來源，因此腳本會再下載一次完整腳本，通過 Bash 語法與版本驗證後才建立穩定副本及 `s`。若第二次下載或驗證失敗，當次選單仍可使用，但會警告「僅臨時執行；s 可能不存在或仍是舊版」。

### 固定 `v1.1.0` 實體暫存檔

```bash
(tmp="$(mktemp)" && trap 'rm -f -- "$tmp"' EXIT && curl -fsSL --connect-timeout 5 --max-time 30 https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v1.1.0/s12ryt.sh -o "$tmp" && bash -n "$tmp" && bash "$tmp")
```

此命令先下載可重現的固定版本至實體暫存檔，通過 `bash -n` 後才執行，並於 subshell 結束時自動清理。需要同時下載 PRoot 或多 IPv6 helper 時，請使用下方完整安裝流程。

## 安裝與啟動

建議從固定 Release 下載主腳本與兩個 helper，先做 Bash 語法檢查再執行：

```bash
(
  set -e
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf -- "$tmp_dir"' EXIT
  curl -fsSL --connect-timeout 5 --max-time 30 \
    https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v1.1.0/s12ryt.sh \
    -o "$tmp_dir/s12ryt.sh"
  curl -fsSL --connect-timeout 5 --max-time 30 \
    https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v1.1.0/install-proot.sh \
    -o "$tmp_dir/install-proot.sh"
  curl -fsSL --connect-timeout 5 --max-time 30 \
    https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v1.1.0/install-ipv6.sh \
    -o "$tmp_dir/install-ipv6.sh"
  bash -n "$tmp_dir/s12ryt.sh" "$tmp_dir/install-proot.sh" "$tmp_dir/install-ipv6.sh"
  bash "$tmp_dir/s12ryt.sh"
)
```

第一次啟動會建立穩定副本及 `s` 命令：

| 執行身分 | 主腳本 | 啟動命令 |
| --- | --- | --- |
| root | `/usr/local/lib/s12ryt/s12ryt.sh` | `/usr/local/bin/s` |
| 非 root | `~/.local/share/s12ryt/s12ryt.sh` | `~/.local/bin/s` |

之後執行：

```bash
s
```

**重要：安裝程序會無條件覆蓋既有的 `s` 路徑，即使該命令不屬於本專案。** 非 root 的 `~/.local/bin` 不在 `PATH` 時，腳本只顯示應執行的 `export PATH=...` 指令，不會修改 `.bashrc` 或其他 shell 設定檔。

## 終端互動

互動終端首次顯示主選單前，以及執行選項 1 至 11 的頂層功能前，會清除目前畫面與 scrollback。功能無論成功、取消或錯誤，都會顯示 `按隨意鍵以返回腳本`，接收一個免 Enter 的按鍵後再次清除，再回到主選單。PRoot 與 Supervisor 只在進出整個子選單時套用一次此流程。

這項功能只輸出終端控制碼，不會清除 Bash 指令歷史，也不會修改 `HISTFILE`。stdin 或 stdout 不是 TTY，或 `TERM=dumb` 時，非互動環境會自動略過清除與等待，避免 CI、管線與重新導向卡住。

## 功能

| 選項 | 功能 | 行為摘要 |
| --- | --- | --- |
| 1 | 系統資訊 | OS、核心、架構、CPU、記憶體、磁碟、負載、uptime 及各介面開機累計流量 |
| 2 | 更新系統 | 確認後執行套件索引更新與一般升級，不做 full-upgrade 或自動重啟 |
| 3 | IP 資訊 | ipapi.is IPv4/IPv6 資訊、11 站點連通性及 7 項有限服務地區推測 |
| 4 | 自動 PRoot（腳本） | 準備 `proot-distro` 5.5.0 與主機依賴 |
| 5 | 自動 PRoot（安裝虛擬機） | 安裝、登入、列表、重裝或移除固定 PRoot 客體 |
| 6 | 自動偽造 systemd | 以 Supervisor 管理 PRoot 客體服務，不提供 `systemctl` shim |
| 7 | 自動安裝 Joey 的 fanout | 前置檢查通過後下載、語法驗證並直接執行官方上游安裝器 |
| 8 | s12ryt 項目列表 | 進入 `s12ryt-多ipv6出站`，提供安裝、更新、設定及卸載 |
| 9 | 安裝 Python | 透過專案私有 uv 選擇安裝 Python 3.10 至 3.14、direct pip 與固定 venv |
| 10 | 安裝 Node.js | 從 NodeSource 選擇安裝 Node.js 20、22 或 24，並驗證 Node.js 與 npm |
| 11 | 檢查更新 | 比對最新 GitHub Release，驗證後原子替換穩定副本 |

## 支援範圍

- 發行版：官方仍支援的 Debian、Ubuntu、CentOS Stream、Rocky Linux、AlmaLinux、Oracle Linux、Fedora、Alpine、Arch Linux、openSUSE。
- 套件管理器：`apt-get`、`dnf`、`yum`、`apk`、`pacman`、`zypper`。
- 架構：`x86_64`、`arm64`/`aarch64`。
- 權限：root 與非 root 均可啟動；需要管理權限的功能會要求 root 或可用的非互動 `sudo`。
- 基本工具：Bash、常見 coreutils 與 `curl`。IP 與更新 JSON 解析另需 `jq` 或 Python 3。
- Node.js：NodeSource 僅支援 `apt-get`、`dnf`、`yum` 及 x86_64/arm64；其他套件管理器會清楚拒絕。
- 多 IPv6 出站：僅支援 Linux root、systemd 或 OpenRC，以及 x86_64/amd64、arm64/aarch64；PRoot、非 root 與未知 init 會清楚拒絕。

CI 會在 GitHub-hosted runner 執行 Bash、Go、Playwright、release build、資源與各發行版容器驗證。一般 VPS 功能與 arm64 binary 尚未在真實 VPS 實機驗證；arm64 只完成交叉編譯。

## PRoot 與 Supervisor

支援下列固定 OCI 映像及本機名稱：

| 系統 | OCI 映像 | 本機名稱 |
| --- | --- | --- |
| Debian 13 | `debian:13` | `s12-debian13` |
| Ubuntu 26.04 LTS | `ubuntu:26.04` | `s12-ubuntu2604` |
| Alpine 3.23 | `alpine:3.23` | `s12-alpine323` |

資料位於 `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/proot`。`proot-distro` 會安裝在該根目錄的隔離 Python venv，版本固定為 `5.5.0`；OCI layer 由上游依 digest 逐層驗證 SHA256。主機仍須能從其官方套件來源取得 `proot` 與 Python 3.9 以上版本。部分 RHEL 相容發行版的預設來源可能沒有 `proot`，此時腳本會停止並要求確認套件來源，不會自動啟用第三方 repository。

選單 6 在已安裝客體中安裝 Supervisor，並提供：

```text
s12-service start|stop|restart|status|enable|disable|log SERVICE
```

服務名稱只接受英數字、點、底線與連字號。Debian/Ubuntu 使用 `/etc/supervisor/supervisord.conf`，Alpine 使用 `/etc/supervisord.conf`。

## Node.js 與 Python

### Node.js

選項 10 提供 Node.js 20、22、24。Node.js 20 已於 2026-03-24 EOL，選擇後會顯示警告並要求第二次確認；Node.js 26 目前不是 LTS，因此不列入選單。若已存在可執行的 `node`，腳本只顯示版本並跳過，不會升級。

安裝流程僅接受 NodeSource 支援的 DEB/RPM 系統與 x86_64/arm64 架構。確認後，腳本會以 HTTPS 將對應版本的 `setup_<major>.x` 下載到暫存檔，套用連線逾時並通過 `bash -n` 後，才以 root 或 `sudo -n` 執行、安裝 `nodejs`，最後同時驗證 `node` 與 `npm`。

NodeSource setup 腳本沒有獨立 checksum；固定 HTTPS 來源與 Bash 語法檢查無法取代內容簽章或完整安全審計。該腳本會新增第三方套件來源，執行前應審閱下載內容。

### Python

選項 9 提供 Python 3.10、3.11、3.12、3.13、3.14，使用專案私有 `uv` 0.12.1 管理，不編譯或替換系統 Python。相關路徑如下：

| 內容 | 路徑 |
| --- | --- |
| 私有 uv | `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/python/uv/uv` |
| 受管 Python | `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/python/versions` |
| 固定 venv | `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/python/venvs/3.X` |
| root 版本命令 | `/usr/local/bin/python3.X` |
| 非 root 版本命令 | `~/.local/bin/python3.X` |

每個新版本都必須能以 `python3.X -m pip` 使用 direct pip，並建立含 pip 的固定 seeded venv。腳本不建立或覆寫無版本的 `python`、`python3`、`pip`、`pip3`。若既有版本缺 pip 或固定 venv，會先詢問是否補齊；對非 uv 管理的系統 Python，只建立固定 venv，不執行 `ensurepip` 修改系統 Python。此功能依契約仍要求 root 或可用的非互動 `sudo`。

uv installer 本身沒有獨立 checksum；腳本使用固定 0.12.1 HTTPS URL、下載逾時與 `bash -n` 保護。installer 下載 uv 執行檔時會使用內嵌的 SHA-256 驗證 artifact，但 installer 腳本內容本身仍以 Astral Release 與 HTTPS 為信任根。

## s12ryt-多ipv6出站

主選單選項 8 會開啟 `s12ryt 項目列表`，選擇 `1. s12ryt-多ipv6出站` 後提供：

```text
1. 安裝
2. 更新
3. 設定
4. 卸載
0. 退出
```

此功能使用 Go 單一執行檔提供 API、排程器及嵌入式響應式 Web UI，並以獨立的 `sing-box v1.13.15` 執行代理資料平面。專案根目錄固定為 `/opt/s12ryt-ipv6`，服務名稱為 `s12ryt-ipv6`；安裝器會依 systemd 或 OpenRC 建立面板服務、開機網路恢復服務與受限日誌輪替設定。

### 安裝、更新與卸載

- 正式支援 Linux root、systemd/OpenRC、amd64/arm64。安裝器不會在 PRoot、非 root 或未知 init 環境繼續。
- 面板 Release 資產固定為 `s12ryt-ipv6-linux-amd64`、`s12ryt-ipv6-linux-arm64` 與 `SHA256SUMS`。下載後先驗 SHA-256，再部署為 executable。
- sing-box 固定下載官方 v1.13.15 Linux 資產，並核對釘選的 GitHub Release API SHA-256 digest；不追蹤上游 latest。
- 首次安裝會產生 24 位英數管理密碼及 `/{12 位英數}` Web 路徑。設定、密碼雜湊、明文管理密碼、runtime state 與整合 manifest 均以 root-only 權限保存。
- 更新會備份 binary 與設定，先執行 `sing-box check`，重啟後再檢查 scoped health endpoint；任何失敗會恢復舊 binary、設定及服務。
- 卸載前會先依 protected manifest 清除本專案新增的防火牆、policy route 與 IPv6。可選擇保留設定、機密與備份，或完整清除 `/opt/s12ryt-ipv6`，且都需再次確認。

### 公開 HTTP 與登入安全

Web 面板依需求使用公開 HTTP，預設埠 `34456`，同時提供 IPv4 與 IPv6 URL。**公開 HTTP 不加密管理密碼、session 或設定內容；路徑上的攻擊者可能攔截或竄改流量。** 建議以防火牆 CIDR 限制可信來源，或在外層部署受信任的 TLS 反向代理。

面板仍實作下列應用層防護，但這些措施不能替代 TLS：

- PBKDF2-SHA256 密碼雜湊、server-side session、HttpOnly 與 SameSite=Strict cookie。
- session 綁定登入來源 IP、CSRF token、同 IP 五次失敗鎖定 15 分鐘、最長 24 小時 session。
- 節點及遠端出口秘密不出現在一般列表或設定 API；重新輸入管理密碼後，只在同一 session 揭露 5 分鐘。
- 登出、重設管理密碼或服務重啟會撤銷記憶體內 session；公開 HTTP 下 cookie 的 Secure 必須為 false。
- UI 無外部 CDN，modal 點擊背景不會關閉；只能按明確按鈕或 Escape 關閉，敏感 modal 關閉時會清除內容與 QR 圖片。

### 協議、TLS 與分享

本機入站支援 VLESS、VMess、Hysteria2、TUIC、SOCKS5、AnyTLS、Shadowsocks。每個節點使用獨立隨機認證，預設從 `20000-49999` 自動挑選同時可用的 TCP/UDP 埠，也可手動指定。

- VLESS：TCP+TLS、WebSocket+TLS、gRPC+TLS、TCP+Reality。
- VMess：TCP、WebSocket 或 gRPC 搭配 TLS。
- Hysteria2、TUIC：QUIC+TLS；AnyTLS：TLS；SOCKS5 與 Shadowsocks：原生 TCP/UDP。
- 憑證可使用 Ed25519 自簽、使用者提供的 cert/key，或 Let's Encrypt ACME HTTP-01。ACME 建立前會先檢查 TCP/80；失敗會保留舊憑證與 runtime。
- 每節點提供分享 URI、sing-box client JSON、QR PNG。模式 1 另提供含 TUN IPv4/IPv6 分流規則的完整 client JSON 與 Base64；一般 URI/QR 不包含該分流規則。
- 聚合 Base64 訂閱只包含 enabled 且健康的本機節點，不會包含匯入的遠端代理秘密。

### 出口模式、拓撲與健康切換

Web 導覽依序為「出口模式 > 拓撲 > 協議」，不是強制 wizard：

1. Client IPv4：client 的 IPv4 目的地由 client 直接連線，IPv6 目的地經 VPS；完整 client JSON 才包含這組分流規則。
2. VPS IPv4：流量先進 VPS；IPv6 經本機 IPv6 或遠端代理池，IPv4 可依序使用 VPS direct、SOCKS 或 HTTP(S) fallback。
3. IPv6-only：入站可雙棧，但 IPv4 目的地明確 reject。

拓撲支援多 IPv6 多節點、單 IPv6 單節點、單固定入站共用輪換池、以及多固定入站共用且錯位輪換。預設輪換週期一小時，可調整；輪換與健康 fallback 只影響新連線，不中斷既有連線。健康監測預設每 30 秒以 5 秒 timeout 檢查 HTTPS endpoint，連續三次失敗切換，首選連續三次成功後恢復。

遠端出口可匯入七種標準分享 URI、單一 sing-box outbound JSON，或最多 1 MiB／1000 節點的 Base64 多 URI；不會抓取訂閱 URL。direct IPv6 與遠端代理可混入同一輪換池，敏感內容只保存在 0600 runtime state。

### IPv6、路由與防火牆

- 可使用主機現有 global IPv6，或從指定可路由前綴安全生成 1-256 個持久地址，預設 16 個。
- gateway 必須能從指定介面的唯一 IPv6 default route 推導；找不到或多個結果時拒絕，不改寫全機 default route 或發行版網路設定檔。
- 每個專案 IPv6 使用專用 policy routing table 與 priority。DAD、重複、前綴範圍及 global-unicast 都會先驗證。
- 支援目前 active 且唯一的 ufw、firewalld 或 nftables，規則帶專案標記，只開放面板 CIDR 及節點所需 TCP/UDP 埠；未知或多個 active backend 會拒絕。
- root-only integration manifest 記錄本專案地址、route 與 firewall。套用、替換、開機恢復及卸載均具反向 rollback，不會以全域 flush 清除其他規則。

### CLI 設定

`設定` 子選單可重新顯示雙棧 URL 與 root-only 管理密碼、隨機或自訂重設密碼，以及變更面板埠、Web 路徑與指定監聽 IPv6。端點變更由 Go 交易層執行：保存候選、重啟並通過新 health 後才替換防火牆；任一步失敗會恢復舊設定與舊端點。

### Release 資產

v1.1.0 Release 應包含：

```text
s12ryt-ipv6-linux-amd64
s12ryt-ipv6-linux-arm64
SHA256SUMS
```

兩個 Go binary 都由 GitHub-hosted runner 以 `CGO_ENABLED=0` 及 `-trimpath` 交叉編譯，並在上傳前以 `SHA256SUMS` 自驗。amd64 會進行實際資源測試；arm64 只完成交叉編譯，未宣稱真實 arm64 VPS 實機驗證。

## 限制與安全說明

### PRoot 不是虛擬機

PRoot 是以 `ptrace` 實作的使用者空間檔案系統與程序環境，不提供真正 root、kernel namespace、cgroup、seccomp 或 systemd 隔離。它不應被視為安全邊界。背景服務只在對應的 PRoot Supervisor 工作階段存活；主機終止工作階段後不保證持續執行。

### IP 與服務地區只是推測

「可能家寬」只在 ipapi.is 的 datacenter、mobile、proxy、vpn 四個訊號皆為否定時顯示，仍可能誤判。Netflix、Disney+、YouTube Premium、Spotify、TikTok、ChatGPT、Gemini 的地區結果依賴公開網頁回應或非公開端點，可能隨時失效，也不保證登入後可播放或使用。

### Fanout 會執行第三方 root 腳本

Fanout 功能要求 Linux、root、`/dev/net/tun`、可用 network namespace，以及 systemd 或 OpenRC。通過後會從 [`byJoey/fanout`](https://github.com/byJoey/fanout) 的 `main/install.sh` 下載至暫存檔，執行 `bash -n` 後直接以 root 執行，不再二次確認。語法檢查不是完整的安全審計；使用此功能前應自行審閱上游內容。Fanout 是獨立 MIT 專案，其程式碼未併入本倉庫。

### 自我更新驗證界線

更新來源是此倉庫最新 GitHub Release 的 `vX.Y.Z` tag。腳本透過 HTTPS 下載對應 tag 的 `s12ryt.sh`，檢查 Bash 語法與內含版本一致後，以同檔案系統內的暫存檔原子替換。失敗會保留舊版。此流程目前沒有額外的簽章驗證；GitHub 帳號、Release 與 tag 的安全性仍是信任根。

### 多 IPv6 出站仍需真實 VPS 驗證

CI 能驗證交易、權限、設定編譯、瀏覽器流程、容器相容性及單一 GitHub runner 上的真實資源工作負載，但不能證明每個雲端商的 IPv6 前綴均可路由、DAD 與防火牆行為一致，也不能取代公開 HTTP 的實際網路風險評估。正式部署前應先使用可丟棄 VPS 或快照。

## 測試

自動化驗證只在 GitHub-hosted runner 執行，避免碰觸開發者本機或真實 VPS。測試會 mock 套件管理器、網路與 PRoot/Fanout 命令，不會：

- 真實升級 runner 系統。
- 下載或啟動 PRoot rootfs。
- 真實安裝 Supervisor 或 Fanout。
- 真實執行 NodeSource setup、安裝 Node.js，或下載 uv/Python。
- 呼叫 ipapi.is 或串流服務作為決定性斷言。
- 在真實 arm64 VPS 執行多 IPv6 面板；arm64 只完成交叉編譯。

另外，CI 會執行：

- Go format、unit/httptest、`go vet`，以及 Bash syntax、ShellCheck 與 Bats。
- Playwright 桌面 Chromium 與 Pixel 7 行動版登入、導覽、modal、CSRF、秘密揭露、QR 及無重疊／無 CDN 驗收。
- amd64/arm64 Release binary cross-build、`SHA256SUMS` 產生及驗證。
- Debian、Ubuntu、CentOS Stream、Rocky、Alma、Oracle Linux、Fedora、Alpine、Arch 及 openSUSE 共 10 個 amd64 容器煙霧測試。
- GitHub-hosted Ubuntu 24.04 上配置 64 IPv6 與 28 個 VLESS 節點，啟動真 panel 與 sing-box 並穩定 60 秒。實測 idle RSS 為 `61872 KiB / 102400 KiB`：panel 17400 KiB、sing-box 44472 KiB。

Linux 環境可用以下命令重現核心檢查：

```bash
bash -n s12ryt.sh install-proot.sh install-ipv6.sh scripts/*.sh tests/*.bash tests/*.sh
shellcheck --external-sources s12ryt.sh install-proot.sh install-ipv6.sh scripts/*.sh tests/*.bash tests/*.sh
bats --print-output-on-failure tests
go test ./...
go vet ./...
npm ci && npx playwright install --with-deps chromium && npm run test:e2e
```

### 手動 VPS 煙霧測試

請先使用可丟棄 VPS 或快照，並依風險逐步驗證：

1. 以 root 與非 root 各啟動一次，確認穩定副本、`s` 命令與 PATH 提示。
2. 執行系統資訊、IP 資訊與項目列表，確認功能前會清除畫面，完成後可按任意單鍵返回，且 Bash 指令歷史不受影響。
3. 在系統更新確認提示選擇 `N`，確認沒有執行套件命令；需要時再於快照環境確認一般升級。
4. 安裝一個 PRoot 客體，測試列表、登入；先取消重裝/移除，再於可丟棄客體確認操作。
5. 安裝 Supervisor，啟動工作階段並以測試服務驗證 `s12-service` 七項操作。
6. 在可丟棄主機測試 Node.js 22 或 24，確認 NodeSource 來源、`node --version` 與 `npm --version`；Node.js 20 只在理解 EOL 風險時測試。
7. 測試一個 Python minor，確認 `python3.X -m pip`、固定 venv Python 與 venv pip 可用，且系統 `python3` 未被替換。
8. 僅在了解上游 root 腳本風險且主機具備 TUN/netns/init 條件時測試 Fanout。
9. 在具可路由 IPv6 前綴的可丟棄 root VPS 進入選項 8，先確認公開 HTTP 風險，再安裝多 IPv6 面板；檢查雙棧 URL、隨機路徑與管理密碼。
10. 先建立單一 VLESS 節點與一個 IPv6 出口，驗證分享 URI/QR、實際連線與防火牆；再測 IPv6 池、三種模式、四種拓撲及只影響新連線的輪換。
11. 測試端點與密碼設定、更新 rollback、保留資料卸載及完整卸載；確認只移除 manifest 內的專案 IPv6、policy route 與 firewall 規則。
12. 發布較新測試 Release 後檢查自我更新，確認失敗情境保留舊版。

## 專案結構

```text
s12ryt.sh                 主選單、診斷、執行環境安裝、項目入口與自我更新
install-proot.sh          PRoot 與 Supervisor 管理
install-ipv6.sh           多 IPv6 面板資產、服務、更新、設定與卸載管理
cmd/s12ryt-ipv6/          Go 面板執行檔入口
internal/                 安全設定、API、代理編譯、網路、交易與 UI 模組
scripts/                  Release cross-build 與真實資源驗收腳本
tests/                    Bats、mock、fixture、Playwright 與容器煙霧測試
.github/workflows/ci.yml  GitHub-hosted 完整自動驗證
```

## 學習參考與第三方

本專案的 VPS 工具箱方向參考並學習自：

- [`kejilion.sh`](https://github.com/kejilion/sh)
- [`ssh_tool.eooce.com`](https://github.com/eooce/ssh_tool)

PRoot 核心使用獨立的 [`termux/proot-distro`](https://github.com/termux/proot-distro)，Fanout 安裝器來自獨立的 [`byJoey/fanout`](https://github.com/byJoey/fanout)。多 IPv6 資料平面執行獨立的 [`SagerNet/sing-box`](https://github.com/SagerNet/sing-box) v1.13.15；其為 GPL-3.0-or-later，另有不得冒用專案名稱或宣稱關聯的條款。本專案使用 MIT 授權的 [`piglig/go-qr`](https://github.com/piglig/go-qr) v1.1.0 產生 QR PNG。各第三方專案保留其各自授權，未因本 README 說明而改變授權範圍。

## 授權

本專案採用 [`GPL-3.0-only`](LICENSE)。對外散布修改版時，須依 GPL 提供相同授權下的對應原始碼；GPL 不要求未對外散布的私人修改必須公開。本專案不提供任何擔保。
