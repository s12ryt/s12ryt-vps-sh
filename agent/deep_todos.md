# 深度任務紀錄

## 目前任務：建立 s12ryt VPS 管理腳本

- [x] 盤點初始工作區、來源需求與可用工具。
- [x] 查核 PRoot、Fanout、GitHub Actions、發行版版本與參考專案。
- [x] 完成三輪需求澄清並凍結驗收契約。
- [x] 建立 GitHub-hosted 測試工作流程與無既有程式碼的 RED 基線證據（run `30680389156`；禁止使用本機 WSL）。
- [x] TDD：主選單與自我安裝、系統資訊、系統更新（GREEN run `30680665994`）。
- [x] TDD：IP 資訊、連通性及有限地區解析（RED run `30680830413`；GREEN run `30680943003`）。
- [x] TDD：PRoot rootfs 管理與工具鏈安裝（GREEN runs `30681357331`、`30681608095`）。
- [x] TDD：Supervisor 服務管理（GREEN run `30681871074`；Alpine 設定路徑回歸 GREEN run `30681997634`）。
- [x] TDD：Fanout、項目列表與自我更新（RED run `30682189480`；GREEN run `30682309550`）。
- [x] TDD：系統更新的實際 sudo 命令維持非互動（RED run `30682930533`；GREEN run `30683030627`）。
- [x] 建立完整 README、canonical GPL-3.0-only 授權與 GitHub Actions。
- [x] 執行完整回歸、ShellCheck、Bash 語法、文件校驗及 10 發行版 x86_64 容器煙霧測試（GREEN run `30683189641`）。
- [x] 建立公開倉庫、推送 `main` 並發行 `v1.0.0` GitHub Release（最終 GREEN run `30683355393`）。

## 目前任務：v1.0.1 快速開始與執行環境安裝

- [x] 澄清 README 快速開始、NodeSource 版本/平台/執行方式、Python 元件、權限與發行版本。
- [x] 查核 Node.js 官方生命週期與 NodeSource DEB/RPM 安裝範圍；確認 24 為最新 LTS、26 仍為 Current、20 已 EOL。
- [x] 查核 uv 官方 Python 管理、installer、SHA-256、版本化命令、pip 與 venv 行為；凍結 Python 3.10-3.14 契約。
- [x] TDD：主選單 10/11、Node.js 20/22/24 安裝與失敗保護（有效 RED run `30695510348`；Node GREEN run `30696040451`；整合 GREEN run `30696449199`）。
- [x] TDD：uv Python 3.10-3.14、受管 pip、固定 venv 與失敗保護（有效 RED run `30695510348`；Python GREEN run `30696230288`；整合 GREEN run `30696449199`）。
- [x] 更新 README main 快速開始、版本、風險說明與測試界線（文件 RED run `30696517584`；GREEN run `30696618928`）。
- [x] 執行完整 GitHub Actions 回歸與品質審查（GREEN runs `30696618928`、`30696771191`）。
- [x] 建立並驗證 `v1.0.1` GitHub Release，保留 `v1.0.0`（tag commit `fb4865b6266ff885ff8099c657e909144ca743e9`）。

## 目前任務：v1.0.2 process substitution 修復

- [x] 重現並定位短暫 `/proc/.../fd/pipe` 被當成穩定來源複製的原因。
- [x] 澄清兩種快速開始、暫時來源失敗降級與 `v1.0.2` 發行契約。
- [x] TDD：process substitution 重新下載、驗證、原子安裝與失敗降級（RED run `30697302635`；GREEN run `30697414395`）。
- [x] 更新版本、README 兩種快速開始與文件驗證（RED run `30697495664`；GREEN run `30697608195`）。
- [x] 執行完整 GitHub Actions 回歸與品質審查（GREEN run `30697608195`）。
- [x] 建立並驗證 `v1.0.2` GitHub Release，保留既有 Releases（tag commit `41c5590de21d793bd4d465b172918c63350de26e`）。

## 目前任務：v1.0.3 終端互動與選單排版

- [x] 讀取新版 `需求.md` 並定位主選單迴圈、功能接線與既有測試。
- [x] 澄清清除範圍、單鍵返回、TTY 邊界、子選單範圍、選單編號與發行版本。
- [ ] TDD：首次與功能前後清除終端、單鍵返回及非互動略過。
- [ ] TDD：9 Python、10 Node.js、11 檢查更新的新排版與功能接線。
- [ ] 更新版本、README、文件驗證與固定 Release URL。
- [ ] 執行完整 GitHub Actions 回歸與品質審查。
- [ ] 建立並驗證 `v1.0.3` GitHub Release，保留既有 Releases。

## 驗收依據

- 唯一需求契約：`agent/question.md`。
- 原始概念需求：`需求.md`。
