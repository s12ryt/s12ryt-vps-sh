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
- [x] TDD：首次與功能前後清除終端、單鍵返回及非互動略過（RED runs `30719122413`、`30719197832`；GREEN run `30719456769`）。
- [x] TDD：9 Python、10 Node.js、11 檢查更新的新排版與功能接線（RED runs `30719122413`、`30719197832`；GREEN run `30719456769`）。
- [x] 更新版本、README、文件驗證與固定 Release URL（RED run `30719558942`；GREEN run `30719648858`）。
- [x] 執行完整 GitHub Actions 回歸與品質審查（GREEN runs `30719648858`、`30719748209`）。
- [x] 建立並驗證 `v1.0.3` GitHub Release，保留既有 Releases（tag commit `0b75d10e6261f27f20fbc0243d0dc1b93ee640aa`）。

## 目前任務：v1.1.0 多 IPv6 出站 Web 管理面板

- [x] 讀取 `he-ipv6.md`，查核 sing-box 協議、路由、Release 與授權邊界。
- [x] 完成多輪需求澄清，凍結 CLI、Web、安全、IPv6、協議、拓撲、更新及驗收契約。
- [ ] TDD：Go 原子狀態、認證/session/CSRF、HTTP API 與嵌入式 Web UI（設定 API GREEN `30724503372`；登出/安全標頭 RED/GREEN `30724685770`/`30724842014`；互動設定 UI `30728254862`/`30728383951`；節點 API 與 credential 防洩漏 GREEN `30730469249`；五分鐘敏感值重驗 `30731078364`/`30731180354`；節點 UI `30731508897`/`30731760201`；完整遠端出口與 TLS 管理 UI 仍待完成）。
- [ ] TDD：sing-box 設定生成、七種 inbound、遠端 outbound 匯入、分享與訂閱輸出（基本設定 RED/GREEN `30725286094`/`30725422410`；遠端匯入 `30725744448`/`30725841673`；transport/Reality `30726875430`/`30727010898`；分享 `30727208080`/`30727297029`；runtime compiler `30735184303`/`30735266550`；配置檔交易 RED/GREEN `30735457626`/`30735551128`；`sing-box check` 驗證器 RED/GREEN `30735705782`/`30735785460`；完整模式輸出仍待完成）。
- [ ] TDD：三種出口模式、四種拓撲、輪換池、健康檢查與自動 fallback（selector RED/GREEN runs `30725568178`/`30725647960`；health monitor RED/GREEN runs `30727501631`/`30727608410`；完整模式與拓撲編排仍待完成）。
- [ ] TDD：IPv6 池、policy route、防火牆、systemd/OpenRC、日誌與原子回滾（IPv6 pool `30725007507`/`30725098657`；service plan `30726187017`/`30726274820`；firewall `30726551004`/`30726716194`；policy route `30727749222`/`30727820377`；命令交易 `30728076838`/`30728159954`；部署協調器 `30731284812`/`30731407857`；實際資料平面 manifest 與卸載清理仍待完成）。
- [ ] TDD：主選單 8 項目列表及安裝、更新、設定、卸載 CLI（導航 RED/GREEN `30732194649`/`30732343845`；資產 staging `30732611880`/`30732705476`；service 安裝 `30732826032`/`30733003749`；更新回滾 `30734652131`/`30734762228`；卸載 `30734828565`/`30735051812`；資料平面 manifest 與進階設定仍待完成）。
- [ ] 建立 Playwright 桌面/手機驗收、x86_64/arm64 cross-build、SHA256 與資源基準。
- [ ] 更新主腳本/README 至 `v1.1.0`，完成完整 GitHub-hosted 回歸與品質審查。
- [ ] 建立並驗證 `v1.1.0` Release，保留既有 Releases。

## 驗收依據

- 唯一需求契約：`agent/question.md`。
- 原始概念需求：`需求.md`。
