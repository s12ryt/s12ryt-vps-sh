# s12ryt 的 VPS 腳本

[![CI](https://github.com/s12ryt/s12ryt-vps-sh/actions/workflows/ci.yml/badge.svg)](https://github.com/s12ryt/s12ryt-vps-sh/actions/workflows/ci.yml)

版本：`v1.1.1`

一個跨發行版 VPS 管理工具，提供系統資訊、一般系統升級、網路診斷、PRoot 客體、Supervisor 服務管理、Fanout、Node.js、Python、多 IPv6 出站專案入口與自我更新。主選單以 Bash 實作；多 IPv6 功能由獨立的 [`s12ryt/s12ryt-ipv6`](https://github.com/s12ryt/s12ryt-ipv6) 專案提供。

> **English summary:** A Traditional Chinese VPS toolkit for diagnostics and runtime management. Multi-IPv6 proxy management is delegated to the standalone `s12ryt/s12ryt-ipv6` project.

## 快速開始

### 可變 main process substitution

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/main/s12ryt.sh)
```

這條命令會直接執行可隨時變更的 `main` 分支內容。process substitution 是暫時來源，因此腳本會再下載一次完整腳本，通過 Bash 語法與版本驗證後才建立穩定副本及 `s`。若第二次下載或驗證失敗，當次選單仍可使用，但會保留既有安裝並顯示警告。

### 固定 `v1.1.1` 實體暫存檔

```bash
(tmp="$(mktemp)" && trap 'rm -f -- "$tmp"' EXIT && curl -fsSL --connect-timeout 5 --max-time 30 https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v1.1.1/s12ryt.sh -o "$tmp" && bash -n "$tmp" && bash "$tmp")
```

此命令先下載可重現的固定版本至實體暫存檔，通過 `bash -n "$tmp"` 後才執行。

## 安裝與啟動

建議從固定 Release 下載主腳本與兩個 helper，先做 Bash 語法檢查再執行：

```bash
(
  set -e
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf -- "$tmp_dir"' EXIT
  curl -fsSL --connect-timeout 5 --max-time 30 \
    https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v1.1.1/s12ryt.sh \
    -o "$tmp_dir/s12ryt.sh"
  curl -fsSL --connect-timeout 5 --max-time 30 \
    https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v1.1.1/install-proot.sh \
    -o "$tmp_dir/install-proot.sh"
  curl -fsSL --connect-timeout 5 --max-time 30 \
    https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v1.1.1/install-ipv6.sh \
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

安裝程序會覆蓋既有的 `s` 路徑，即使該命令不屬於本專案。非 root 的 `~/.local/bin` 不在 `PATH` 時，腳本只顯示應執行的 `export PATH=...`，不修改 shell 設定檔。

## 終端互動

互動終端首次顯示主選單前，以及執行選項 1 至 11 的頂層功能前，會清除目前畫面與 scrollback。功能無論成功、取消或錯誤，都會顯示 `按隨意鍵以返回腳本`，接收一個免 Enter 的按鍵後回到主選單。

這只輸出終端控制碼，不會清除 Bash 指令歷史，也不會修改 `HISTFILE`。stdin 或 stdout 不是 TTY，或 `TERM=dumb` 時，非互動環境會自動略過清除與等待。

## 功能

| 選項 | 功能 | 行為摘要 |
| --- | --- | --- |
| 1 | 系統資訊 | OS、核心、架構、CPU、記憶體、磁碟、負載、uptime 及介面流量 |
| 2 | 更新系統 | 執行套件索引更新與一般升級，不做 full-upgrade 或自動重啟 |
| 3 | IP 資訊 | IPv4/IPv6 資訊、站點連通性及有限服務地區推測 |
| 4 | 自動 PRoot（腳本） | 準備 `proot-distro` 5.5.0 與主機依賴 |
| 5 | 自動 PRoot（安裝虛擬機） | 安裝、登入、列表、重裝或移除固定 PRoot 客體 |
| 6 | 自動偽造 systemd | 以 Supervisor 管理 PRoot 客體服務 |
| 7 | 自動安裝 Joey 的 fanout | 驗證環境及上游安裝器後執行 |
| 8 | s12ryt 項目列表 | 進入 `s12ryt-多ipv6出站`，提供安裝、更新與卸載 |
| 9 | 安裝 Python | 透過專案私有 uv 安裝 Python 3.10 至 3.14、direct pip 與固定 venv |
| 10 | 安裝 Node.js | 從 NodeSource 選擇安裝 Node.js 20、22 或 24 |
| 11 | 檢查更新 | 驗證最新 GitHub Release 後原子替換穩定副本 |

## 支援範圍

- 主工具：官方仍支援的 Debian、Ubuntu、CentOS Stream、Rocky Linux、AlmaLinux、Oracle Linux、Fedora、Alpine、Arch Linux、openSUSE。
- 主工具套件管理器：`apt-get`、`dnf`、`yum`、`apk`、`pacman`、`zypper`。
- 主工具架構：`x86_64`、`arm64`/`aarch64`。
- Node.js：NodeSource 僅支援 DEB/RPM 系統及 x86_64/arm64；其他平台會拒絕。
- 多 IPv6：僅支援 Linux root、systemd、amd64/arm64，以及 Debian 12/13 或 Ubuntu 24.04。其他平台會在碰觸舊部署或下載上游腳本前停止。

## PRoot 與 Supervisor

PRoot 支援 Debian 13、Ubuntu 26.04 LTS 與 Alpine 3.23 固定 OCI 映像。資料位於 `${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/proot`，`proot-distro` 固定為 5.5.0。

Supervisor 提供：

```text
s12-service start|stop|restart|status|enable|disable|log SERVICE
```

服務名稱只接受英數字、點、底線與連字號。Debian/Ubuntu 使用 `/etc/supervisor/supervisord.conf`，Alpine 使用 `/etc/supervisord.conf`。

## Node.js 與 Python

Node.js 選項提供 20、22、24。Node.js 20 已 EOL，選擇時會再次確認。安裝器從 NodeSource 下載對應 `setup_<major>.x` 到暫存檔，通過 `bash -n` 後才執行並驗證 `node` 與 `npm`。NodeSource setup 腳本沒有獨立 checksum，HTTPS 與語法檢查不能取代內容簽章。

Python 選項使用私有 `uv` 0.12.1 管理 3.10 至 3.14，不替換系統 `python3`。每個版本均提供 `python3.X -m pip` 與固定 seeded venv。uv installer 本身沒有獨立 checksum；固定 HTTPS URL 與語法檢查仍以 Astral Release 為信任根。

## s12ryt-多ipv6出站

主選單選項 8 會開啟 `s12ryt 項目列表`，其子選單只有：

```text
1. 安裝
2. 更新
3. 卸載
0. 退出
```

面板、代理核心、Web UI、設定 schema 與服務生命週期均由獨立的 [`s12ryt/s12ryt-ipv6`](https://github.com/s12ryt/s12ryt-ipv6) 維護。本倉庫只保留安全轉接 helper，不再建置或發布面板 binary，也不提供面板設定入口；進階操作請使用上游 CLI 或 `systemctl`。

### 上游版本與下載

- 每次操作查詢上游 GitHub 最新正式 Release，只接受非草稿、非預發布且符合 `vX.Y.Z` 的 tag。
- 安裝與更新從同一 tag 下載 `install.sh`，卸載下載 `deploy/uninstall.sh`；下載後先通過 `bash -n`。
- 執行上游腳本時傳入同一個 `VERSION`，避免 latest metadata、`main` 與實際資產漂移。
- 首次安裝沿用上游預設管理埠 `34466`；更新不主動改埠。進階使用者可在呼叫本工具前設定 `MANAGEMENT_PORT`。
- 管理介面預設為公開 HTTP。密碼與管理操作不受傳輸層加密保護，應以防火牆限制來源或在外層配置可信 TLS。

### 舊版遷移

安裝或更新前若偵測到本倉庫過往部署於 `/opt/s12ryt-ipv6` 的舊版，helper 會：

1. 執行舊 binary 的 `cleanup-system`，清理其 IPv6、route 與 firewall 狀態。
2. 停用並移除舊的 `s12ryt-ipv6` 與 `s12ryt-ipv6-network` systemd unit。
3. 完整保留 `/opt/s12ryt-ipv6` 資料，再執行上游安裝器。

任一步驟失敗都會中止新版安裝，並盡力恢復舊服務原本的啟用與運行狀態。清理命令已造成的網路變更不保證能完全復原，因此仍建議先建立主機快照。

### 上游資料與卸載

上游 binary 位於 `/usr/local/bin/s12ryt-ipv6`，systemd unit 位於 `/etc/systemd/system/s12ryt-ipv6.service`，資料目錄為 `/etc/s12ryt-ipv6`。卸載完全遵循上游：移除服務與 binary，但保留 `/etc/s12ryt-ipv6`；本工具不提供完整刪除資料選項。

## 限制與安全說明

### PRoot 不是虛擬機

PRoot 不提供真正 root、kernel namespace、cgroup、seccomp 或 systemd 隔離，不應被視為安全邊界。

### IP 與服務地區只是推測

IP 類型與串流地區依賴外部服務回應，可能誤判或隨時失效，不保證登入後可使用。

### 第三方 root 腳本

Fanout、NodeSource、uv 與多 IPv6 功能會執行經語法檢查的第三方下載內容。`bash -n` 不是安全審計；執行前應審閱來源，並理解 GitHub 帳號、Release、tag 與 HTTPS 是信任根。

### 多 IPv6 仍需真實 VPS 驗證

本倉庫 CI 只以 mock 驗證轉接、版本綁定、平台阻擋與舊版遷移，不會真的變更 IPv6、route、firewall 或 systemd。實際網路、DNS64/NAT64、代理協議與 UI 行為由上游專案負責，正式部署前應使用可丟棄 VPS 或快照驗證。

## 測試

CI 執行 Bash syntax、ShellCheck、Bats、文件契約與 10 個 amd64 發行版容器煙霧測試。多 IPv6 測試會 mock GitHub API、下載腳本、systemd 與舊 binary，不接觸真實 VPS。

Linux 環境可重現核心檢查：

```bash
bash -n s12ryt.sh install-proot.sh install-ipv6.sh tests/*.bash tests/*.sh
shellcheck --external-sources s12ryt.sh install-proot.sh install-ipv6.sh tests/*.bash tests/*.sh
bats --print-output-on-failure tests
```

### 手動 VPS 煙霧測試

1. 以 root 與非 root 啟動，確認穩定副本、`s` 命令與 PATH 提示。
2. 驗證系統資訊、IP 資訊、套件更新取消流程、PRoot 與 Supervisor。
3. 在可丟棄主機驗證 Node.js、Python 與 Fanout 的實際上游安裝。
4. 在 Debian 12/13 或 Ubuntu 24.04 的 systemd root VPS 執行多 IPv6 安裝，確認上游服務、管理埠與首次密碼。
5. 以快照環境驗證舊 `/opt/s12ryt-ipv6` 遷移、失敗中止與資料保留。
6. 驗證更新保留管理埠；卸載後確認服務與 binary 已移除，`/etc/s12ryt-ipv6` 仍存在。

## 專案結構

```text
s12ryt.sh                 主選單、診斷、執行環境安裝、項目入口與自我更新
install-proot.sh          PRoot 與 Supervisor 管理
install-ipv6.sh           上游多 IPv6 專案轉接、平台檢查與舊版遷移
tests/                    Bats、mock、fixture 與容器煙霧測試
.github/workflows/ci.yml  Bash、文件及發行版容器驗證
```

## 學習參考與第三方

本專案的 VPS 工具箱方向參考 [`kejilion.sh`](https://github.com/kejilion/sh) 與 [`ssh_tool.eooce.com`](https://github.com/eooce/ssh_tool)。PRoot 使用獨立的 [`termux/proot-distro`](https://github.com/termux/proot-distro)，Fanout 安裝器來自 [`byJoey/fanout`](https://github.com/byJoey/fanout)，多 IPv6 功能來自 [`s12ryt/s12ryt-ipv6`](https://github.com/s12ryt/s12ryt-ipv6)。各第三方專案保留各自授權。

## 授權

本專案採用 [`GPL-3.0-only`](LICENSE)，不提供任何擔保。
