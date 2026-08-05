# 已確認需求契約

最後確認日期：2026-08-03

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

## v1.1.0 多 IPv6 出站管理契約（2026-08-02）

本節為使用者提供 `he-ipv6.md` 後，經多輪澄清形成的增量契約；若與前述項目列表、版本或驗收範圍衝突，以本節為準。

### 版本、入口與支援範圍

- 主腳本與 README 版本更新為 `1.1.0`，保留既有 Releases，建立正式 `v1.1.0` Release。
- 主選單 8 的項目列表顯示 `1. s12ryt-多ipv6出站`；選擇後進入「s12ryt特供-多ipv6出站（web管理面板）」子選單：1 安裝、2 更新、3 設定、4 卸載、0 退出。
- 採用 `sing-box + Go` 模組化單體：面板、API、排程與靜態資產編譯為低資源單一 Go binary；sing-box 是獨立外部執行檔，不併入本專案程式碼。
- 此功能正式支援 Linux root、x86_64/amd64 與 arm64/aarch64，以及既有支援發行版；Debian/Ubuntu、RHEL 相容系、Fedora、Arch、openSUSE 使用 systemd，Alpine 使用 OpenRC。無 root、PRoot、未知 init 或不支援架構時拒絕。
- 缺少 `iproute2`、curl、openssl 或所選防火牆工具時，可由發行版官方套件來源安裝；失敗必須回滾並清楚報錯。

### 安裝、路徑、更新與卸載

- 面板 Release 資產為 `s12ryt-ipv6-linux-amd64`、`s12ryt-ipv6-linux-arm64` 與 `SHA256SUMS`；GitHub Actions cross-build，VPS 不安裝 Go。安裝器依架構下載並驗證 SHA-256。
- sing-box 固定 `v1.13.15`，只下載官方 x86_64/arm64 Release 資產並驗上游 checksum；不追蹤 latest。它採 GPL-3.0-or-later 且含名稱/關聯限制，作為獨立程式使用。
- binary、設定、機密、狀態、備份及內嵌 Web 資料集中於 `/opt/s12ryt-ipv6/`；systemd/OpenRC 定義、防火牆及 logrotate 等作業系統整合檔仍放標準位置。服務名稱為 `s12ryt-ipv6`。
- 已安裝時再選「安裝」視同更新：校驗新資產、備份 binary/設定/schema，執行 schema migration、sing-box 設定檢查與服務健康檢查；任一步失敗即原子回滾 binary、設定及服務狀態。
- 主腳本自我更新與 IPv6 面板更新分離；面板只由項目子選單的「更新」升級。
- 卸載先選保留資料或完整清除，再以 `[y/N]` 確認。兩者都停止服務並移除 binary、服務、專案防火牆規則、policy route 與專案新增 IPv6；完整清除另刪設定、密碼、節點、狀態與備份。

### CLI 與管理端點

- 首次安裝生成 24 位英數管理密碼與 `/{12 位英數}` Web 路徑，預設埠 34456；密碼以 root-only 明文保存供 CLI 再次顯示，另存雜湊供 Web 驗證。
- 設定頁顯示 `ipv4: http://{IPv4}:{port}/{path}`、`ipv6: http://[{完整 IPv6}]:{port}/{path}`，沒有可用地址時顯示 `{未獲取到}`，並顯示管理密碼。
- CLI 可更換埠、Web 路徑、管理面板監聽 IPv6及重設管理密碼；密碼只能由 CLI 重設，維持至少 24 位英數或重新隨機生成，並撤銷全部 session。
- 管理埠、路徑或監聽 IPv6 變更前驗證地址、埠與候選防火牆規則；原子保存後重啟，健康成功才刪舊規則並顯示新 URL，失敗自動回滾舊 URL 與規則。
- 管理面板依需求同時監聽 IPv4/IPv6 公網並使用 HTTP；README 與 UI 必須持續警告密碼、session 與內容可能被攔截。可設定管理來源 CIDR，預設全網公開。

### Web 安全、UI 與狀態

- Web 為繁體中文、響應式、無外部 CDN；HTML/CSS/JS/圖示嵌入 Go binary，採安靜、工具導向的操作介面。
- 導覽順序為「出口模式」>「拓撲」>「協議節點」，不是強制 wizard。各設定以按鈕開啟 modal；點 backdrop 不關閉，Escape 與明確關閉/取消/完成按鈕可關閉。
- 登入使用密碼雜湊、伺服器端 session、HttpOnly、SameSite=Strict cookie、CSRF token 與登入限速。因公開 HTTP，cookie `Secure=false` 並顯示風險。session cookie 無持久期限，伺服器端最長 24 小時；同 IP 5 次失敗鎖定 15 分鐘，服務重啟或改密碼撤銷全部 session。
- 匯入代理的 URI、JSON、密碼與私鑰以 root-only 明文保存供 sing-box 使用；Web 預設遮罩。重新輸入管理密碼後可顯示/複製敏感值 5 分鐘。
- 面板狀態使用含 schema 版本的 root-only JSON；寫入必須 temp + fsync + atomic rename，保留可回滾備份，不引入資料庫。
- 變更先顯示差異；確認後生成候選設定，執行輸入、網路、防火牆、憑證及 `sing-box check` 驗證，成功才原子替換並 reload，失敗回滾。

### IPv6 地址與系統網路

- 面板列出各介面的既有 global IPv6 供選擇，CLI/Web 一律完整展開為八組四位十六進位；底層 JSON 與分享格式可使用標準合法表示。
- 另可由可路由 CIDR 生成 1--256 個 IPv6，預設 16。管理員選介面並輸入 CIDR；gateway 從該介面現有 IPv6 route 自動偵測，沒有唯一可用結果時拒絕，不覆蓋整台 VPS 的 default route。
- 新地址首次以安全隨機生成後持久保存，檢查範圍、重複、保留位址、DAD 與可達性；服務啟動前恢復。卸載只移除本專案新增地址，不修改發行版網路設定檔。
- 管理員可重置地址池；重置前預覽所有受影響節點並二次確認，生成新池、重寫節點與分享資料，全部驗證成功後原子切換，只影響新連線。
- 防火牆正式支援既有啟用的 ufw、firewalld 或 nftables；新增具專案標記的最小 TCP/UDP 規則，設定更新同步，卸載只刪專案規則。未知、衝突或無法判定的後端拒絕操作。

### 入站協議、端點與輸出

- 本機入站節點支援 VLESS、VMess、Hysteria2、TUIC、SOCKS5、AnyTLS、Shadowsocks。每節點使用獨立隨機 UUID、密碼或金鑰。
- VLESS 支援 TCP+TLS、WebSocket+TLS、gRPC+TLS、TCP+Reality；VMess 支援 TCP、WebSocket、gRPC + TLS；Hysteria2/TUIC 使用 QUIC+TLS；AnyTLS 使用 TLS；Shadowsocks/SOCKS5 使用原生 TCP/UDP。
- TLS 同時支援自簽與網域模式。自簽輸出安全的憑證/指紋版本及標明風險的 `insecure` 版本。網域模式支援 ACME HTTP-01（網域必填、email 可空，80 埠被占用即拒絕，續期失敗保留舊憑證）與使用者 cert/key。
- cert/key 可選驗證後複製到 `/opt/s12ryt-ipv6/` 的 root-only TLS 目錄，或引用原路徑。引用模式每次套用與啟動前檢查可讀性、權限、配對及有效期；失敗保留舊配置並拒絕 reload。
- 每個節點的入站可選指定 VPS IPv4、指定完整 IPv6，或 IPv4+IPv6 雙入站。雙入站各選一個地址，使用相同協議埠與認證（例如兩者皆 8001），建立兩個明確 listener 並輸出兩個端點。
- 協議埠預設從 20000--49999 安全隨機選擇未占用且可開防火牆的 TCP/UDP 埠；重試失敗才要求手動輸入。
- 每節點提供分享 URI、sing-box JSON、複製及 QR Code；聚合標準 Base64 訂閱只包含啟用且健康的本機入站節點，不洩露匯入的遠端出口。

### 出口模式與拓撲

- 模式 1「本地 IPv4」是客戶端分流：產生完整 sing-box 客戶端 JSON，IPv4 目的地由客戶端電腦直接連線，IPv6 目的地送入 VPS 節點。分享 URI/QR 只代表節點且需明示不含分流；另提供原始 JSON 下載/複製及標準 Base64 完整配置。
- 模式 2 是 VPS 雙棧出口：IPv6 目的地使用所選 IPv6/輪換池；IPv4 目的地可選 VPS 本機 IPv4 direct 或匯入的 SOCKS/HTTP outbound，並可排序自動 fallback。HTTP 只作遠端 outbound，不新增本機 HTTP inbound。
- 模式 3 為雙棧入站、僅 IPv6 出口；IPv4 目的地不回退，管理面板仍維持雙棧。
- 拓撲 1「多 IPv6、多節點」由管理員建立多個獨立入站節點並為其指定出口；拓撲 2 為單 IPv6、單節點。
- 拓撲 3 為單一固定入站節點，其新連線按可調週期（預設 1 小時）切換共用 IPv6/遠端代理池；拓撲 4 為多個固定獨立入站節點，共用同一候選池並以錯位順序輪換。既有連線不中斷。
- direct IPv6 與匯入遠端代理可混合於輪換池。出口每 30 秒使用可設定 HTTPS URL 健康檢查，預設 `https://www.cloudflare.com/cdn-cgi/trace`、timeout 5 秒；連續 3 次失敗按管理員排序切換，首選連續 3 次成功後切回，只影響新連線。

### 遠端代理匯入

- 接受七種代理的標準分享 URI、單一 sing-box outbound JSON，以及最多 1 MiB/1000 節點的 Base64 多 URI 訂閱內容；逐筆驗證及去重，不主動追蹤遠端訂閱 URL。
- 模式 2 額外接受 SOCKS/HTTP(S) 代理 URI 或 sing-box outbound JSON作 IPv4 出口；HTTP 不作本機 inbound。
- 匯入的 direct IPv6、七種遠端代理及 SOCKS/HTTP 可依模式加入候選/fallback；儲存與套用前驗證 schema、必要欄位、地址、TLS 與敏感資料。

### 日誌、資源與驗收

- 面板與 sing-box 使用資訊級日誌，不記錄密碼、token、私鑰或完整 URI；systemd journal 或 OpenRC logrotate 最多保留 7 天或 100 MiB，先到者為準。
- GitHub-hosted CI 以 64 個 IPv6、28 個節點、無活躍流量、穩定 60 秒的基準量測面板與 sing-box 合計 idle RSS，必須不超過 100 MiB。
- 嚴格 TDD：Go domain/config/auth/HTTP 使用 unit test 與 `httptest`；Bash 安裝/更新/系統操作全部 mock；不得在 CI 真改 host IPv6、route、防火牆、服務或真實 VPS。
- Playwright 在 GitHub-hosted runner 驗證桌面與手機登入、三頁導覽、modal backdrop 不關閉、Escape/按鈕關閉、CSRF/錯誤狀態與響應式無重疊；前端不得使用外部 CDN。
- GitHub Actions 執行 Go test、go vet、格式檢查、Bats、ShellCheck、Bash 語法、Playwright、x86_64/arm64 cross-build、checksum、資源基準與既有 10 發行版 x86_64 容器 smoke。arm64 只宣稱 cross-build，不宣稱真實 arm64 VPS 驗證。

## v1.1.1 Web UI/UX 自主升級契約（2026-08-03）

本節為 `v1.1.0` 完整交付後，使用者要求「自主疊代升級，進行 Web 頁面以及操作優化」形成的增量契約。此輪只改善資訊架構、視覺、操作回饋、響應式與無障礙；不得新增或移除業務功能，不得改變既有 HTTP API、狀態 schema、認證、安全、CLI、sing-box 或系統整合公開契約。

### 版本與設計方向

- 發布正式 `v1.1.1` Release，保留 `v1.1.0` 與更早版本；主腳本、README、面板 Release 資產與自我更新版本同步為 `1.1.1`。
- 採深色 NOC 控制台視覺，維持平衡資訊密度：深石墨背景、清楚的中性色層級、綠色正常狀態、琥珀色警告與紅色危險操作；不得使用單一色系、裝飾性漸層光球或行銷式大卡片。
- 維持無外部 CDN、無外部字體與無外部前端 runtime；介面仍嵌入單一 Go binary。繁中使用本機 CJK 字體 fallback，技術值使用等寬字體，不為純視覺增加大型字體資產或框架。
- 登入與 dashboard 都必須保留公開 HTTP 風險警告；此輪不得降低 session、CSRF、client-IP binding、登入限速、秘密遮罩或五分鐘重新驗證強度。

### 五工作區資訊架構

- Desktop 使用緊湊左側導覽，Mobile 使用可水平捲動的頂部分頁；分成五個工作區：
  - `策略`：出口模式、拓撲與協議設定入口。
  - `節點`：本機入站節點及敏感憑證管理。
  - `遠端出口`：代理匯入、啟停、刪除及 IPv4 fallback。
  - `網路`：IPv6 地址、地址池、policy route 與防火牆整合。
  - `分享`：受保護 URI、JSON、Base64 與 QR 分享。
- 一次只顯示一個工作區，不保留所有工作區同時堆疊的長頁。Desktop 側欄與 Mobile 分頁操作相同的 tablist/tab/tabpanel 語意。
- 使用 URL hash 保存工作區：`#strategy`、`#nodes`、`#remotes`、`#network`、`#shares`。初次或未知 hash 回到 `#strategy`；重新整理、瀏覽器返回/前進與直接連結必須維持正確工作區，且不得把秘密寫進 URL。
- Desktop 表格維持便於比較的平衡密度；Mobile 不得依賴整頁水平捲動，主要資料改為可掃描的單欄資訊列或受控表格容器。最長標籤、IPv6、URI 與操作按鈕不得互相覆蓋或撐破 viewport。

### 完整操作優化

- 所有會送出 HTTP mutation 的表單與按鈕，在請求期間提供一致 loading/disabled/`aria-busy` 狀態並阻止重複提交；請求結束後必須可再次操作。
- 成功結果以不打斷流程的 `aria-live` 狀態通知顯示；錯誤保留既有安全訊息，同時在相關表單或欄位旁提供可恢復、可聚焦的提示。不得把後端秘密、完整 URI、token、密碼或內部命令錯誤寫入通知。
- 高風險的刪除、網路套用、秘密揭露等操作維持既有二次確認、安全 header 與伺服器驗證；視覺上必須與一般操作有明確層級差異。
- 表單欄位使用正確 label、autocomplete、inputmode、min/max/pattern 與必要狀態；錯誤後不得清掉可安全保留的使用者輸入。
- 增加頁面 skip link、全域可見 `:focus-visible`、合理 heading/landmark、狀態文字及鍵盤操作；文字與控制項對比至少符合 WCAG AA。

### Modal 與鍵盤契約

- 既有 modal 仍由按鈕開啟，點 backdrop 不關閉；Escape 與明確關閉、取消或完成按鈕可關閉，敏感 modal 關閉時仍清除秘密。
- Modal 開啟後焦點移到第一個合理控制項或 dialog 標題，Tab/Shift+Tab 焦點鎖定於目前 modal；關閉後焦點回到原開啟按鈕。
- Modal 必須具有 `role="dialog"`、`aria-modal="true"`、可解析的標題關聯與背景不可互動狀態；同一時間只允許一個 modal 處於開啟狀態。

### v1.1.1 TDD 與驗收

- 正式 UI 修改前先補 Go HTML characterization 與 Playwright RED 契約；RED 必須因缺少深色 NOC、五工作區 hash 導覽、操作狀態或焦點行為而失敗，不得因 fixture、格式或環境錯誤失敗。
- Go/`httptest` 驗證必要 landmarks、tab/tabpanel、hash targets、skip link、dialog ARIA、busy/live/error markers、無外部 CDN，並確保登入、安全 header、API 與秘密遮罩契約不退化。
- Playwright desktop/mobile 驗證：五工作區切換、hash 直接進入與返回/前進、一次只顯示一個 panel、沒有水平溢出或重疊、mutation 防重複提交、loading/成功/錯誤回饋、modal 初始焦點/焦點鎖定/Escape/backdrop/焦點返回、完整鍵盤流程及無外部請求。
- 維持既有 Playwright 登入、CSRF、分享揭露/QR/copy/清理、安全 header、96 項 Bats、Go format/test/vet、ShellCheck、10 發行版 smoke、amd64/arm64 cross-build/checksum及 64 IPv6 + 28 nodes、60 秒、100 MiB idle RSS 門檻。
- 所有程式驗證仍只使用 GitHub-hosted Actions；不得使用本機 WSL、Git Bash、本機 Bash/Bats/ShellCheck/Go test 作為證據。

## v1.1.2 Web 操作可靠性修復契約（2026-08-03）

本節為 `v1.1.1` 完整交付後，使用者授權依本檔既有契約自主疊代並完整發布 `v1.1.2` 所形成的增量契約。此輪只修復既有 v1.1.1 操作狀態與 modal 隔離承諾未完整覆蓋之處，不新增業務功能，不變更 HTTP API、狀態 schema、認證、安全、CLI、sing-box 或系統整合公開契約。

### Mutation 操作狀態

- 策略設定「驗證 -> 差異確認 -> 套用」必須以一次完整使用者操作為 busy 生命週期；從開始驗證到套用成功或任一步驟失敗前，觸發按鈕維持 disabled 與 `aria-busy="true"`，不得在驗證請求結束後提前恢復。
- 動態節點刪除、遠端出口啟用/停用與刪除按鈕必須納入相同的一致操作狀態：請求期間按鈕 disabled、`aria-busy="true"`、阻止重複 mutation，完成後恢復可操作。
- 使用者在瀏覽器確認對話框選擇取消，不得送出 HTTP mutation、不得顯示錯誤通知，控制項保持可操作；請求成功與失敗仍沿用既有不洩密的 live status 與錯誤語意。
- 操作狀態 helper 必須同時處理 scope 本身為按鈕及 scope 內的控制項，並在成功、失敗或同步例外後可靠清理；不得以弱化既有分享、表單或 CSRF 測試換取通過。

### Modal 隔離與單一開啟

- 開啟任一 modal 時，若另一 modal 已開啟，必須先依既有關閉流程關閉舊 modal，再開啟目標 modal；任何時刻最多一個 dialog 可見且 `aria-hidden="false"`。
- Modal 開啟期間，dashboard 的主要背景容器必須套用原生 `inert`，使背景控制項無法被滑鼠、觸控或鍵盤聚焦；關閉最後一個 modal 後必須移除 `inert`。
- 切換 modal 時不得短暫把焦點返回舊觸發器；最終關閉時焦點回到目前 modal 的開啟按鈕。既有初始焦點、Tab/Shift+Tab 鎖定、Escape、static backdrop、明確關閉按鈕與敏感資料清理契約維持不變。

### 版本、TDD 與發布驗收

- 主腳本、IPv6 helper、Playwright package metadata 與 README 同步更新為 `1.1.2`；保留既有 Releases，建立非 draft、非 prerelease 的正式 `v1.1.2` Release。
- 正式程式修改前先新增 Go HTML characterization 與 Playwright RED 契約；RED 必須因缺少按鈕級完整 busy 生命週期、背景 `inert` 或單一 modal 行為而失敗，不得因 fixture、格式或環境錯誤失敗。
- Playwright desktop/mobile 至少驗證策略多請求操作、動態 mutation 按鈕防重複、取消不誤報，以及 modal 背景不可互動、單一開啟、焦點鎖定與返回；Go/`httptest` 驗證必要腳本與 DOM 契約標記。
- 維持既有 Go format/test/vet、Bash 語法、ShellCheck、96 項 Bats、README/LICENSE、Playwright、amd64/arm64 cross-build/checksum、64 IPv6 + 28 nodes 60 秒 100 MiB idle RSS 門檻及 10 發行版 x86_64 smoke，不得刪除、略過或弱化既有斷言。
- RED、GREEN、完整回歸、發行候選與 Release 驗證全部只使用 GitHub-hosted Actions；不得在本機執行 Go test、Bash、Bats、Playwright 或 ShellCheck作為驗收證據。

## 多 IPv6 專案完整外部化契約（2026-08-05）

本節取代本倉庫內嵌多 IPv6 面板的實作、安裝、發佈與驗收契約；早期章節保留為歷史紀錄。外部倉庫 `s12ryt/s12ryt-ipv6` 是此功能唯一的程式碼、Release 與部署來源，本倉庫只保留主選單入口及安全轉接 helper。

### 範圍與入口

- 刪除本倉庫內已失去用途的 Go/Web 面板實作、Go module、Playwright 工具鏈、舊面板 release build、資源基準腳本及其專屬測試；不得刪除主 VPS 腳本其他功能共用的檔案或測試。
- 主選單 8 的項目仍顯示 `s12ryt-多ipv6出站`，子選單只提供 `1. 安裝`、`2. 更新`、`3. 卸載`、`0. 退出`。
- 不再提供設定入口；服務管理、管理埠與密碼等進階操作由使用者直接使用外部專案的 `systemctl` 或 CLI。
- `install-ipv6.sh` 僅作為外部專案轉接與舊部署遷移 helper，不再自行安裝 binary、sing-box、服務或管理設定。

### 外部版本與上游執行

- 安裝與更新每次查詢 `https://api.github.com/repos/s12ryt/s12ryt-ipv6/releases/latest`，只接受非 draft、非 prerelease 且 tag 精確符合 `vX.Y.Z` 的正式 Release。
- 從解析出的同一個 tag 下載 `https://raw.githubusercontent.com/s12ryt/s12ryt-ipv6/{tag}/install.sh`，設置連線與總逾時，先通過 `bash -n`，再以 `VERSION={tag}` 執行，避免可變 `main` 與 Release 資產漂移。
- 首次安裝不主動傳入 `MANAGEMENT_PORT`，沿用上游預設 `34466`；更新不主動傳入該變數，使上游保留既有管理埠。使用者明確設定的 `MANAGEMENT_PORT` 環境變數必須原樣傳給上游。
- 更新沿用上游 installer 的校驗、健康檢查與回滾契約；本 helper 不重複實作外部專案內部部署流程。
- 卸載從同一個最新正式 tag 下載並驗證 `deploy/uninstall.sh` 後執行，完全遵循上游行為：移除 systemd 服務與 `/usr/local/bin/s12ryt-ipv6`，保留 `/etc/s12ryt-ipv6`；本工具不提供完整刪除資料選項。

### 平台與舊部署遷移

- 所有安裝、更新與卸載操作先驗證 Linux root、systemd、amd64/x86_64 或 arm64/aarch64，並依 `/etc/os-release` 只接受 Debian 12/13 或 Ubuntu 24.04；OpenRC、其他發行版/版本及其他架構必須在碰觸舊部署前明確拒絕。
- 舊部署以 `/opt/s12ryt-ipv6`、`/etc/systemd/system/s12ryt-ipv6-network.service` 或 `/etc/init.d/s12ryt-ipv6-network` 等舊版特徵辨識；不得只因新版同名主服務存在就誤判為舊部署。
- 安裝或更新新版前若偵測到舊部署，先以 `/opt/s12ryt-ipv6/bin/s12ryt-ipv6 cleanup-system` 清除舊版新增的 IPv6、policy route 與防火牆狀態，再停用舊 `s12ryt-ipv6.service` 與 `s12ryt-ipv6-network.service`，移除舊 systemd unit 並執行 daemon-reload；完整保留 `/opt/s12ryt-ipv6` 內所有資料與 binary。
- 舊版清理或服務移除任一步驟失敗時不得下載或執行新版 installer；helper 必須盡力恢復先前已啟用/運行的舊服務狀態，清楚報錯並以非零狀態結束。
- 因新版不支援 OpenRC，OpenRC 主機必須在遷移前直接拒絕，不得修改任何舊 OpenRC 服務或資料。

### TDD 與驗收

- 先以現有相關 Bats 建立基線，再重寫/新增 Bats mock 契約形成 RED，且失敗原因必須是缺少上述外部化行為。
- 隔離測試需涵蓋三項子選單接線、正式 Release JSON 驗證、tag 綁定下載、`VERSION` 與可選 `MANAGEMENT_PORT` 傳遞、安裝/更新/卸載腳本選擇、下載與語法失敗保護。
- 平台測試需涵蓋 Debian 12/13、Ubuntu 24.04 的允許路徑，以及 OpenRC、非支援發行版/版本、非支援架構、非 Linux、非 root 在舊版遷移前拒絕。
- 遷移測試需涵蓋無舊版直接安裝、成功清理且保留 `/opt`、清理失敗不執行新版、服務步驟失敗時盡力恢復舊服務，以及新版同名服務不被誤判為舊版。
- 卸載測試需證明執行上游 `deploy/uninstall.sh` 且不刪除 `/etc/s12ryt-ipv6`。
- 本次只以本機隔離 Bats/mock、Bash 語法與可用 ShellCheck/CI 靜態驗證作為證據；不在真實 VPS 上安裝、更新、卸載或修改網路、路由、防火牆及 systemd。
- 完成後，所有保留的主專案回歸測試與文件/CI 設定不得引用已刪除的內嵌 Go/Web、release build、資源或 Playwright 資產。
