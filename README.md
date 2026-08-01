# s12ryt 的 VPS 腳本

[![CI](https://github.com/s12ryt/s12ryt-vps-sh/actions/workflows/ci.yml/badge.svg)](https://github.com/s12ryt/s12ryt-vps-sh/actions/workflows/ci.yml)

版本：`v1.0.1`

一個以 Bash 撰寫的跨發行版 VPS 管理選單，提供系統資訊、一般系統升級、網路診斷、PRoot 客體、Supervisor 服務管理、Fanout、Node.js、Python 安裝與自我更新。

> **English summary:** A Traditional Chinese, menu-driven Bash toolkit for common VPS diagnostics and maintenance. It supports multiple Linux distribution families, PRoot guests, Supervisor-based guest services, guarded Fanout installation, and release-based self-updates. See [Limitations](#限制與安全說明) before using privileged features.

## 快速開始

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/main/s12ryt.sh)
```

這條命令會直接執行可隨時變更的 `main` 分支內容，適合快速試用，但不具版本可重現性，執行前應先審閱腳本。需要固定版本與先做語法檢查時，請使用下方安裝流程。

## 安裝與啟動

建議從固定 Release 下載主腳本與 PRoot helper，先做 Bash 語法檢查再執行：

```bash
(
  set -e
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf -- "$tmp_dir"' EXIT
  curl -fsSL --connect-timeout 5 --max-time 30 \
    https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v1.0.1/s12ryt.sh \
    -o "$tmp_dir/s12ryt.sh"
  curl -fsSL --connect-timeout 5 --max-time 30 \
    https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v1.0.1/install-proot.sh \
    -o "$tmp_dir/install-proot.sh"
  bash -n "$tmp_dir/s12ryt.sh" "$tmp_dir/install-proot.sh"
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
| 8 | s12ryt 項目列表 | 第一版固定顯示「暫無項目」 |
| 9 | 檢查更新 | 比對最新 GitHub Release，驗證後原子替換穩定副本 |
| 10 | 安裝 Node.js | 從 NodeSource 選擇安裝 Node.js 20、22 或 24，並驗證 Node.js 與 npm |
| 11 | 安裝 Python | 透過專案私有 uv 選擇安裝 Python 3.10 至 3.14、direct pip 與固定 venv |

## 支援範圍

- 發行版：官方仍支援的 Debian、Ubuntu、CentOS Stream、Rocky Linux、AlmaLinux、Oracle Linux、Fedora、Alpine、Arch Linux、openSUSE。
- 套件管理器：`apt-get`、`dnf`、`yum`、`apk`、`pacman`、`zypper`。
- 架構：`x86_64`、`arm64`/`aarch64`。
- 權限：root 與非 root 均可啟動；需要管理權限的功能會要求 root 或可用的非互動 `sudo`。
- 基本工具：Bash、常見 coreutils 與 `curl`。IP 與更新 JSON 解析另需 `jq` 或 Python 3。
- Node.js：NodeSource 僅支援 `apt-get`、`dnf`、`yum` 及 x86_64/arm64；其他套件管理器會清楚拒絕。

CI 會在 GitHub-hosted x86_64 runner 執行 Bats、ShellCheck、`bash -n`、mock/fixture 測試，以及各發行版容器的最小煙霧驗證。專案不宣稱已在真實 VPS 或 arm64 實機完成驗證。

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

選項 11 提供 Python 3.10、3.11、3.12、3.13、3.14，使用專案私有 `uv` 0.12.1 管理，不編譯或替換系統 Python。相關路徑如下：

| 內容 | 路徑 |
| --- | --- |
| 私有 uv | `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/python/uv/uv` |
| 受管 Python | `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/python/versions` |
| 固定 venv | `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/python/venvs/3.X` |
| root 版本命令 | `/usr/local/bin/python3.X` |
| 非 root 版本命令 | `~/.local/bin/python3.X` |

每個新版本都必須能以 `python3.X -m pip` 使用 direct pip，並建立含 pip 的固定 seeded venv。腳本不建立或覆寫無版本的 `python`、`python3`、`pip`、`pip3`。若既有版本缺 pip 或固定 venv，會先詢問是否補齊；對非 uv 管理的系統 Python，只建立固定 venv，不執行 `ensurepip` 修改系統 Python。此功能依契約仍要求 root 或可用的非互動 `sudo`。

uv installer 本身沒有獨立 checksum；腳本使用固定 0.12.1 HTTPS URL、下載逾時與 `bash -n` 保護。installer 下載 uv 執行檔時會使用內嵌的 SHA-256 驗證 artifact，但 installer 腳本內容本身仍以 Astral Release 與 HTTPS 為信任根。

## 限制與安全說明

### PRoot 不是虛擬機

PRoot 是以 `ptrace` 實作的使用者空間檔案系統與程序環境，不提供真正 root、kernel namespace、cgroup、seccomp 或 systemd 隔離。它不應被視為安全邊界。背景服務只在對應的 PRoot Supervisor 工作階段存活；主機終止工作階段後不保證持續執行。

### IP 與服務地區只是推測

「可能家寬」只在 ipapi.is 的 datacenter、mobile、proxy、vpn 四個訊號皆為否定時顯示，仍可能誤判。Netflix、Disney+、YouTube Premium、Spotify、TikTok、ChatGPT、Gemini 的地區結果依賴公開網頁回應或非公開端點，可能隨時失效，也不保證登入後可播放或使用。

### Fanout 會執行第三方 root 腳本

Fanout 功能要求 Linux、root、`/dev/net/tun`、可用 network namespace，以及 systemd 或 OpenRC。通過後會從 [`byJoey/fanout`](https://github.com/byJoey/fanout) 的 `main/install.sh` 下載至暫存檔，執行 `bash -n` 後直接以 root 執行，不再二次確認。語法檢查不是完整的安全審計；使用此功能前應自行審閱上游內容。Fanout 是獨立 MIT 專案，其程式碼未併入本倉庫。

### 自我更新驗證界線

更新來源是此倉庫最新 GitHub Release 的 `vX.Y.Z` tag。腳本透過 HTTPS 下載對應 tag 的 `s12ryt.sh`，檢查 Bash 語法與內含版本一致後，以同檔案系統內的暫存檔原子替換。失敗會保留舊版。此流程目前沒有額外的簽章驗證；GitHub 帳號、Release 與 tag 的安全性仍是信任根。

## 測試

自動化驗證只在 GitHub-hosted runner 執行，避免碰觸開發者本機或真實 VPS。測試會 mock 套件管理器、網路與 PRoot/Fanout 命令，不會：

- 真實升級 runner 系統。
- 下載或啟動 PRoot rootfs。
- 真實安裝 Supervisor 或 Fanout。
- 真實執行 NodeSource setup、安裝 Node.js，或下載 uv/Python。
- 呼叫 ipapi.is 或串流服務作為決定性斷言。
- 驗證 arm64 實機行為。

Linux 環境可用以下命令重現核心檢查：

```bash
bash -n s12ryt.sh install-proot.sh tests/*.bash tests/*.sh
shellcheck --external-sources s12ryt.sh install-proot.sh tests/*.bash tests/*.sh
bats --print-output-on-failure tests
```

### 手動 VPS 煙霧測試

請先使用可丟棄 VPS 或快照，並依風險逐步驗證：

1. 以 root 與非 root 各啟動一次，確認穩定副本、`s` 命令與 PATH 提示。
2. 執行系統資訊、IP 資訊與項目列表，確認輸出可讀且失敗時能返回選單。
3. 在系統更新確認提示選擇 `N`，確認沒有執行套件命令；需要時再於快照環境確認一般升級。
4. 安裝一個 PRoot 客體，測試列表、登入；先取消重裝/移除，再於可丟棄客體確認操作。
5. 安裝 Supervisor，啟動工作階段並以測試服務驗證 `s12-service` 七項操作。
6. 在可丟棄主機測試 Node.js 22 或 24，確認 NodeSource 來源、`node --version` 與 `npm --version`；Node.js 20 只在理解 EOL 風險時測試。
7. 測試一個 Python minor，確認 `python3.X -m pip`、固定 venv Python 與 venv pip 可用，且系統 `python3` 未被替換。
8. 僅在了解上游 root 腳本風險且主機具備 TUN/netns/init 條件時測試 Fanout。
9. 發布較新測試 Release 後檢查自我更新，確認失敗情境保留舊版。

## 專案結構

```text
s12ryt.sh                 主選單、診斷、執行環境安裝、Fanout 與自我更新
install-proot.sh          PRoot 與 Supervisor 管理
tests/                    Bats、mock、fixture 及容器煙霧腳本
.github/workflows/ci.yml  GitHub-hosted 自動驗證
```

## 學習參考與第三方

本專案的 VPS 工具箱方向參考並學習自：

- [`kejilion.sh`](https://github.com/kejilion/sh)
- [`ssh_tool.eooce.com`](https://github.com/eooce/ssh_tool)

PRoot 核心使用獨立的 [`termux/proot-distro`](https://github.com/termux/proot-distro)，Fanout 安裝器來自獨立的 [`byJoey/fanout`](https://github.com/byJoey/fanout)。各第三方專案保留其各自授權。

## 授權

本專案採用 [`GPL-3.0-only`](LICENSE)。對外散布修改版時，須依 GPL 提供相同授權下的對應原始碼；GPL 不要求未對外散布的私人修改必須公開。本專案不提供任何擔保。
