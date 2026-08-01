# 已確認需求契約

最後確認日期：2026-08-01

## 目標與交付物

- 建立公開 GitHub 倉庫 `s12ryt/s12ryt-vps-sh`，描述為「s12ryt 的跨發行版 VPS 管理腳本」。
- 主程式為 `s12ryt.sh`，初始版本 `1.0.0`，授權為 `GPL-3.0-only`。
- README 以繁體中文為主並附英文簡介，明確致謝 `kejilion.sh` 與 `ssh_tool.eooce.com` 為學習參考。
- 建立 GitHub Release `v1.0.0`，後續作為腳本自我更新來源。

## 支援範圍

- 發行版：仍受官方支援的 Debian、Ubuntu、CentOS Stream、Rocky Linux、AlmaLinux、Oracle Linux、Fedora、Alpine、Arch Linux、openSUSE。
- 套件管理器：`apt-get`、`dnf`、`yum`、`apk`、`pacman`、`zypper`。
- 架構：`x86_64` 與 `arm64`/`aarch64`。
- root 與非 root 均可執行；需管理權限的功能優先使用可用的非互動 `sudo`，否則清楚拒絕。
- 所有 Bash 測試、Bats、ShellCheck、語法檢查與發行版矩陣只使用 GitHub-hosted runner、容器及 mock；禁止使用本機 WSL 作為本次測試環境或驗收證據。
- 不宣稱已在真實 VPS 或 arm64 實機驗證。

## 主選單契約

啟動時顯示名稱、版本 `1.0.0` 與以下選項：

1. 系統資訊
2. 更新系統
3. IP 資訊
4. 自動 PRoot（腳本）
5. 自動 PRoot（安裝虛擬機）
6. 自動偽造 systemd
7. 自動安裝 Joey 的 fanout
8. s12ryt 項目列表
9. 檢查更新
0. 退出

無效輸入需顯示錯誤並返回選單。互動功能完成後返回選單，`0` 正常退出。

## 安裝 `s` 命令

- 主腳本執行時建立穩定副本與啟動命令。
- root：`/usr/local/lib/s12ryt/s12ryt.sh` 與 `/usr/local/bin/s`。
- 非 root：`$HOME/.local/share/s12ryt/s12ryt.sh` 與 `$HOME/.local/bin/s`。
- 無條件覆蓋既有 `s`，即使原命令不屬於本專案；README 必須清楚警告。
- 非 root 的 `~/.local/bin` 不在 `PATH` 時，只輸出可執行的 PATH 設定指令，不修改 shell 設定檔。

## 功能行為

### 1. 系統資訊

- 顯示 OS、核心、架構、CPU、記憶體、根檔案系統磁碟、負載與 uptime。
- 解析 `/proc/net/dev`，逐介面顯示自開機後累計接收及傳送位元組。
- 不安裝 vnStat，也不量測即時速率。

### 2. 更新系統

- 執行前要求明確確認。
- 依套件管理器更新索引及執行一般升級；不做 full-upgrade、不自動重啟。
- 取消、無法取得管理權限、或不支援套件管理器時不得執行升級命令，並顯示原因。

### 3. IP、連通性及有限地區檢測

- 透過 ipapi.is 匿名 HTTPS API 顯示 IPv4/IPv6、ASN、ISP、國家/地區，以及 datacenter/mobile/proxy/vpn 訊號。
- 僅當 datacenter/mobile/proxy/vpn 均為否定時顯示「可能家寬」，並明示只是推測。
- 使用有逾時限制的 HTTP 請求檢查 GitHub、Google、Cloudflare、YouTube、Netflix、Disney+、Spotify、TikTok、ChatGPT、Gemini、Telegram；輸出可達、受限或逾時/失敗。
- 內建有限解析 Netflix、Disney+、YouTube Premium、Spotify、TikTok、ChatGPT、Gemini 的可用性及可推測地區。
- 檢測依賴網站回應或非公開端點，結果只代表檢測當下，不保證登入後可播放或服務永久有效；解析需可由 fixture/mock 決定性測試。

### 4、5. PRoot

- 選項 4 建立或開啟獨立 `install-proot.sh`。
- 管理 Debian 13、Ubuntu 26.04 LTS、Alpine 3.23，依主機自動選擇 x86_64 或 arm64 rootfs。
- 安裝根目錄為 `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/proot`。
- 提供安裝、登入、列表、重裝、移除；重裝與移除必須再次確認。
- 只使用 HTTPS；上游提供 SHA256 時必須驗證，沒有校驗值時必須警告。
- PRoot 是使用者空間隔離，不是真正虛擬機或容器，不具真正 root、cgroup 或 systemd；README 與執行輸出需說明限制。

### 6. Supervisor 服務管理

- 不提供 `systemctl` 相容層，也不聲稱是真正 systemd。
- 讓使用者選擇已安裝的 PRoot 客體，安裝 Supervisor，提供 `s12-service` 的 `start`、`stop`、`restart`、`status`、`enable`、`disable`、`log`。
- 服務只在對應 PRoot Supervisor 工作階段存活；離開或主機終止該工作階段後不保證持續執行。

### 7. Fanout

- 安裝前檢查 Linux、root、`/dev/net/tun`、network namespace，以及 systemd 或 OpenRC。
- 前置條件通過後，下載 `https://raw.githubusercontent.com/byJoey/fanout/main/install.sh` 至暫存檔，先執行 `bash -n`，再直接執行，不做第二次確認。
- 任一前置檢查、下載或語法檢查失敗時不得執行上游腳本。
- Fanout 為獨立的 MIT 上游，不將其程式碼併入本專案，也不在 PRoot 或 CI 中真實安裝。

### 8. 項目列表

- 第一版固定顯示「暫無項目」並返回主選單。

### 9. 自我更新

- 查詢 `s12ryt/s12ryt-vps-sh` 最新 GitHub Release tag `vX.Y.Z`，以語意版本比較本地 `VERSION`。
- 無新版時清楚顯示目前已是最新版。
- 有新版時經 HTTPS 下載該 tag 的 `s12ryt.sh`，執行版本格式及 `bash -n` 驗證後，以同檔案系統暫存檔原子替換穩定副本。
- API、下載、驗證、寫入或替換失敗時保留原版本並回報錯誤。

## 測試與驗收標準

- 嚴格採 RED -> GREEN -> REFACTOR；新增正式行為前先建立因缺少行為而失敗的 Bats 測試。
- 使用 GitHub-hosted runner 執行 Bash、Bats、mock、fixture、`bash -n` 與 ShellCheck；本機不執行 WSL 或 Git Bash 測試。
- 驗證選單、系統資訊解析、權限及依賴失敗、各套件管理器命令、更新確認保護、IP/地區 fixture 解析、下載失敗、PRoot 破壞性操作確認、Fanout 前置檢查與原子自我更新。
- CI 不真實升級系統、不安裝 PRoot rootfs、不執行 Fanout，也不呼叫會造成不穩定結果的外部服務。
- README 列出手動 VPS 煙霧測試步驟，以及 CI、PRoot、Supervisor、家寬推測、串流地區檢測的限制。
- 受影響測試、完整測試、ShellCheck、語法檢查及可用 build/CI 設定驗證均不得新增錯誤。

## 授權語意

- 本專案採 `GPL-3.0-only`；對外散布修改版時，須依 GPL 提供相同授權下的對應原始碼。
- 不宣稱 GPL 能強迫未散布的私人修改公開。
