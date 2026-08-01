# 已確認需求契約

最後確認日期：2026-08-02

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

## v1.0.1 擴充契約（2026-08-01）

本節為使用者在 v1.0.0 交付後確認的增量契約；若與前述版本、選單或功能敘述衝突，以本節為準。

### 版本、選單與發行

- 主腳本與 README 版本更新為 `1.0.1`，保留既有 `v1.0.0` Release，並建立正式 `v1.0.1` Release。
- 主選單在選項 9 後新增：
  - `10. 安裝 Node.js`
  - `11. 安裝 Python`
- 原有選項及行為維持不變；`0` 仍為退出。

### README 快速開始

- README 新增且只列一條 `main` 分支的一行快速開始命令：
  `bash <(curl -fsSL https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/main/s12ryt.sh)`。
- 快速開始段落必須明示該命令會直接執行可變的 `main` 分支內容；既有固定 Release、先下載再 `bash -n` 的安全安裝說明保留為一般安裝方式，不列為另一條「快速開始」。

### 10. 安裝 Node.js

- 先檢查 `node`；若已存在，顯示現有 `node --version` 並完全跳過，不新增 repository、不安裝或升級任何套件。
- 尚未安裝時，先顯示偵測到的套件管理器並要求 `[y/N]` 確認；取消時不得下載或執行任何安裝命令。
- 提供 Node.js `20`、`22`、`24` 三個 NodeSource 版本選項。Node.js 26 截至 2026-08-01 仍為 Current、不是 LTS，因此不提供。
- Node.js 20 已於 2026-03-24 EOL；選擇 20 時必須明示已停止官方安全維護，並要求第二次 `[y/N]` 確認，取消時不得繼續。
- NodeSource 僅支援 DEB 與 RHEL/RPM 套件來源：`apt-get` 使用 `https://deb.nodesource.com/setup_<major>.x`；`dnf`/`yum` 使用 `https://rpm.nodesource.com/setup_<major>.x`。
- `apk`、`pacman`、`zypper` 及其他套件管理器必須清楚拒絕，不得靜默改用發行版套件、nvm 或其他來源。
- 只支援 NodeSource 公布的 amd64/arm64（RPM 為 x86_64/aarch64）架構；其他架構清楚拒絕。
- 官方 setup 腳本必須以 HTTPS 下載至暫存檔，設置連線及總逾時，通過 `bash -n` 後才以 root 或非互動 `sudo -n` 執行；不得使用 `curl | bash`。
- setup 腳本下載、語法或執行失敗時，不得執行 `nodejs` 安裝；上游未提供 setup 腳本 checksum，README 必須揭露此信任界線。
- setup 成功後，以相同權限執行 `apt-get install -y nodejs`、`dnf install -y nodejs` 或 `yum install -y nodejs`；完成後驗證並顯示 `node --version` 與 `npm --version`，驗證失敗需回報錯誤。
- 需要 root 或可用的非互動 `sudo -n`；無權限時清楚拒絕且不得下載或安裝。

### 11. 安裝 Python

- 提供 Python `3.10`、`3.11`、`3.12`、`3.13`、`3.14` 五個版本選項；使用 Astral `uv` 管理版本，不依賴各發行版是否提供該 minor、不編譯 Python，也不改變系統 `python` 或 `python3` 預設命令。
- 尚無所選 `python3.X` 時，先顯示偵測到的套件管理器與 uv 來源，再要求 `[y/N]` 確認；取消時不得下載或安裝。
- 即使 uv 採目前帳號的私有路徑，仍依使用者要求強制需要 root 或可用的非互動 `sudo -n`；無權限時清楚拒絕且不得下載或安裝。
- 腳本使用專案私有、精確釘選的 uv `0.12.1`，安裝於 `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/python/uv`，不沿用 PATH 中任意版本的 uv，也不修改 shell 設定檔。
- uv installer 固定下載自 `https://releases.astral.sh/github/uv/releases/download/0.12.1/uv-installer.sh`，設置連線及總逾時，先通過 `bash -n` 再以 `UV_NO_MODIFY_PATH=1` 與專案私有安裝目錄執行。installer 內含各平台 uv 二進位的 SHA-256 並在安裝時驗證；installer 本身只由固定版本 HTTPS URL 與語法檢查保護，README 必須揭露此界線。
- uv 管理的 Python 版本資料放在 `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/python/versions`；版本化命令放在 root 的 `/usr/local/bin` 或非 root 的 `$HOME/.local/bin`。只建立 `python3.X`，不得建立或覆寫無版本的 `python`、`python3`、`pip` 或 `pip3`。
- 新安裝完成後，所選 uv 受管直譯器必須支援 `python3.X -m pip`；若缺 pip，只能對該 uv 受管直譯器使用 `ensurepip` 補齊，不得修改系統 Python。
- 每個版本建立固定虛擬環境 `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/python/venvs/3.X`，使用 `uv venv --python 3.X --seed`，並驗證該環境的 Python minor 與 `python -m pip --version`。
- 若 `python3.X` 已存在，先顯示版本並檢查直接 pip 與固定 venv。兩者皆存在時完全跳過；任一缺少時詢問是否補齊。
- 使用者拒絕補齊時不得下載或修改任何內容。若既有 `python3.X` 不是 uv 管理且缺直接 pip，即使選擇補齊也不得修改該系統 Python，只建立或補齊固定、含 pip 的 venv，並清楚說明直接 pip 仍由系統安裝狀態決定。
- 任一 uv 下載、語法、安裝、Python 下載、pip、venv 或驗證步驟失敗時回報錯誤，不得誤報成功。

### v1.0.1 TDD 與驗收

- 新增 Bats/mock 測試先形成 RED，涵蓋完整 12 項選單、既有版本跳過、確認與 EOL 二次確認、管理權限、NodeSource 平台/架構白名單、三個 Node.js major URL 與套件命令、node/npm 驗證，以及下載/語法/執行失敗保護。
- Python mock 契約涵蓋五個 minor、私有 uv 0.12.1 installer URL、逾時/語法/失敗保護、uv 二進位與資料路徑、版本化命令、不改系統預設、受管 Python pip、固定 seeded venv、既有版本補齊確認及非 uv 系統 Python 不被修改。
- CI 不真實建立 NodeSource repository、不安裝 Node.js/Python，也不執行遠端 setup 腳本；所有安裝行為以 mock 驗證。
- 更新 README、版本與 Release 後，完整 GitHub-hosted Bash 語法、ShellCheck、Bats、文件驗證及發行版容器煙霧矩陣不得新增錯誤。

## v1.0.2 暫時來源修復契約（2026-08-01）

本節為使用者回報 `bash <(curl ...)` 啟動失敗後確認的增量契約；若與前述快速開始、版本或安裝行為衝突，以本節為準。

### 缺陷與版本

- 已確認缺陷為 process substitution 讓 `${BASH_SOURCE[0]}` 指向短暫的 `/dev/fd` 或 `/proc/.../fd/pipe`；原實作解析該路徑後再複製，可能得到 `cp: cannot stat` 並無法建立穩定副本。
- 主腳本與 README 版本更新為 `1.0.2`，保留既有 `v1.0.0`、`v1.0.1` Release，並建立正式 `v1.0.2` Release。

### 暫時來源安裝與降級

- 一般實體腳本來源維持既有穩定副本與 `s` 建立行為。
- 當來源是 process substitution、pipe 或其他不可作為完整實體檔複製的暫時來源時，不得再嘗試複製解析後的 `/proc/.../fd/pipe` 路徑。
- 暫時來源預設從 `https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/main/s12ryt.sh` 重新下載完整主腳本；測試可透過環境變數覆寫 URL，但正式預設不得改用非 HTTPS 來源。
- 重新下載必須設置連線及總逾時，寫入穩定副本同目錄的暫存檔，通過 `bash -n`，並確認下載腳本內含版本與目前執行中的 `VERSION` 完全一致後，才可原子替換穩定副本並建立或覆寫 `s`。
- 下載、語法、版本或原子替換失敗時，必須保留既有穩定副本與 `s`，清理暫存檔，顯示具體錯誤及醒目警告「僅臨時執行；s 可能不存在或仍是舊版」，然後繼續當次主選單，不因自我安裝失敗直接退出。
- 一般實體來源發生目錄、複製或 launcher 寫入錯誤時，仍視為安裝失敗並停止，不套用暫時來源的寬容降級。

### README 快速開始

- 同時支援並記錄兩種方式：
  - 保留 `bash <(curl -fsSL https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/main/s12ryt.sh)`，明示它會直接執行可變 main，且暫時來源會再下載一次完整腳本以建立穩定副本。
  - 新增固定 `v1.0.2`、先下載實體暫存檔、執行 `bash -n`、再執行且自動清理的一行命令，作為可重現的替代方式。
- 既有多行固定 Release 安裝流程保留並更新為 `v1.0.2`。

### v1.0.2 TDD 與驗收

- 先新增可在 GitHub-hosted runner 穩定重現 process substitution 的 Bats 回歸測試；RED 必須因目前嘗試複製短暫 pipe 而失敗。
- mock 測試需涵蓋重新下載成功，以及下載失敗、語法失敗、版本不一致時保留既有穩定副本與 `s`、顯示降級警告並繼續選單。
- README 文件驗證需涵蓋兩條快速開始方式、固定 `v1.0.2` URL 與風險說明。
- 完成後執行完整 GitHub-hosted Bash 語法、ShellCheck、Bats、文件驗證及 10 發行版容器煙霧矩陣；不得使用本機 WSL、Git Bash 或本機 Bash 作為證據。

## v1.0.3 終端互動與選單排版契約（2026-08-02）

本節為使用者更新 `需求.md` 後確認的增量契約；若與前述版本、選單編號或返回流程衝突，以本節為準。

### 版本、選單與發行

- 主腳本與 README 版本更新為 `1.0.3`，保留既有 `v1.0.0`、`v1.0.1`、`v1.0.2` Release，並建立正式 `v1.0.3` Release。
- 主選單排版與編號必須精確為：
  - `1. 系統資訊`
  - `2. 更新系統`
  - `3. IP 資訊`
  - `4. 自動 PRoot（腳本）`
  - `5. 自動 PRoot（安裝虛擬機）`
  - `6. 自動偽造 systemd`
  - `7. 自動安裝 Joey 的 fanout`
  - `8. s12ryt 項目列表`
  - `9. 安裝 Python`
  - `10. 安裝 Node.js`
  - 分隔線後顯示 `11. 檢查更新`
  - 分隔線後顯示 `0. 退出`
- case 行為必須同步編號：9 呼叫 Python 安裝、10 呼叫 Node.js 安裝、11 呼叫自我更新。

### 清除終端與返回流程

- 「清空歷史」是清除互動終端的目前畫面與 scrollback，不得執行 `history -c`、刪除 history 檔案或以其他方式修改使用者 Bash 指令歷史。
- 腳本第一次顯示主選單前先清除畫面與 scrollback。
- 使用者選擇 1–11 的任一頂層功能後，在呼叫功能前先清除畫面與 scrollback。
- 頂層功能無論成功、取消或錯誤返回，都顯示精確提示 `按隨意鍵以返回腳本`，並以靜默單鍵讀取等待；不要求按 Enter。
- 收到返回按鍵後，再清除畫面與 scrollback，然後顯示主選單。
- 選項 0 正常退出，不顯示返回提示；無效輸入顯示錯誤後直接重顯選單，不顯示返回提示。
- PRoot 與 Supervisor 的內部子選單不逐項套用清除與暫停；只在進入頂層功能前清除，離開整個子選單後暫停一次。
- 清除與單鍵暫停只在 stdin 與 stdout 為互動 TTY 且終端可用時啟用；CI、管線或其他非互動執行自動略過，避免控制碼污染輸出或流程卡住。
- 自動化測試可使用明確的測試開關模擬互動終端，但正式預設仍必須依實際 TTY 判定。

### v1.0.3 TDD 與驗收

- 新增正式行為前，先建立 Bats RED 契約，驗證首次清除、功能前清除、精確返回提示、免 Enter 單鍵、返回後清除、成功/取消/錯誤一致返回，以及 0、無效輸入和非互動環境不暫停。
- RED 測試需驗證 9、10、11 的新編號與功能接線，並確保 PRoot/Supervisor 只套用頂層返回流程。
- 測試不得依賴真實終端控制效果；以可注入、可觀察的 mock 或測試模式驗證呼叫順序與控制碼輸出。
- README 必須更新至 `v1.0.3`，同步選單編號、終端互動說明及固定 Release 安裝 URL。
- 完成後執行完整 GitHub-hosted Bash 語法、ShellCheck、Bats、文件驗證及 10 發行版容器煙霧矩陣；不得使用本機 WSL、Git Bash 或本機 Bash 作為證據。
