# 操作紀錄

## 2026-08-01

- 讀取 `需求.md`；初始工作區僅有該檔案，且尚未初始化 Git。
- 確認原先不存在 `agent/deep_todos.md`、`agent/項目表.md`、`agent/memory.md`。
- 查核 `byJoey/fanout` 安裝方式、必要權限與執行環境；確認其為獨立 MIT 專案。
- 查核 PRoot 的使用者空間隔離限制，不將其描述為真正 VM、root 或 systemd。
- 查核 GitHub Actions hosted runner、支援發行版版本、ipapi.is 欄位，以及串流檢測的授權與穩定性限制。
- 使用 GitHub 身分 `s12ryt` 查詢同名倉庫，確認 `s12ryt-vps-sh` 尚不存在。
- 經三輪需求澄清，建立 `agent/question.md` 作為實作及驗收依據。
- 建立專案進度、結構及操作紀錄檔；此時尚未新增正式程式碼或測試。
- 工具盤點時曾啟動 WSL Bash 查詢版本、目錄映射及工具是否存在，未執行專案測試或正式程式；使用者隨即明確禁止使用其 WSL 測試，之後不得再呼叫 WSL。
- 使用者選定所有 Bash TDD 與驗收只在 GitHub-hosted Actions 執行；已同步更新需求契約。
- 建立首批主選單及非 root `s` 安裝 Bats 契約，GitHub Actions run `30680389156` 的 Bash 語法及 ShellCheck 通過，4 項 Bats 測試均因正式入口 `s12ryt.sh` 尚不存在而按預期失敗，形成第一輪 RED 證據。
- 第一輪 GREEN 經 ShellCheck 修正後由 GitHub Actions run `30680516931` 驗證通過，共 4 項 Bats。
- 建立系統資訊與一般系統升級契約，GitHub Actions run `30680599550` 因缺少目標函式按預期失敗；最小實作完成後 run `30680665994` 通過 Bash 語法、ShellCheck 與全部 8 項 Bats，完成第二輪 GREEN。
- 新增 IP 分類、11 站點連通性、7 項有限服務地區解析與選單整合的第三輪測試；測試只使用 fixture/mock，不呼叫外部服務。
- GitHub Actions run `30680830413` 中既有 8 項測試全綠，新增 5 項分別因網路函式及選單接線不存在而按預期失敗，形成第三輪 RED 證據。
- 完成網路診斷最小實作；GitHub Actions run `30680943003` 通過 Bash 語法、ShellCheck 與全部 13 項 Bats，完成第三輪 GREEN。
- 建立 PRoot 路徑、固定 OCI 映像、架構映射、安裝/登入/列表/重裝/移除、破壞性確認與 helper 原子保護契約；測試全程使用 mock，不下載 rootfs 或執行 PRoot。
- GitHub Actions run `30681103141` 中既有 13 項全綠，新增 6 項均因 PRoot helper 與選單接線尚不存在而按預期失敗，形成第四輪 RED 證據。
- 完成 PRoot helper、固定 OCI 客體、架構映射、管理操作、破壞性確認與主選單接線；GitHub Actions run `30681357331` 通過 Bash 語法、ShellCheck 與全部 19 項 Bats，完成第四輪 GREEN。
- 建立非互動 sudo、Python 3.9+ 選擇、隔離安裝釘選 `proot-distro==5.5.0` 與套件來源失敗語意契約；run `30681450453` 中既有 19 項全綠，新增 4 項因目標行為缺失而按預期失敗，形成第五輪 RED 證據。
- 第五輪首次 GREEN run `30681517893` 僅因測試 PATH 洩漏 runner 的 `/usr/bin/python3.12` 而失敗；正式函式已正確選到支援版本。將案例 PATH 隔離至 mock 目錄後，run `30681608095` 通過 Bash 語法、ShellCheck 與全部 23 項 Bats，完成第五輪 GREEN。
- 建立 Supervisor 已安裝客體白名單、套件安裝、`s12-service` 七項操作、服務名稱防護、detached 工作階段與主選單接線契約；run `30681760099` 中既有 23 項全綠，新增 6 項因目標行為缺失而按預期失敗，形成第六輪 RED 證據。
- 完成 PRoot 客體內 Supervisor 與 `s12-service` 管理；run `30681871074` 通過 Bash 語法、ShellCheck 與全部 29 項 Bats，完成第六輪 GREEN。
- 品質審查依 Alpine 3.23 官方套件內容識別設定檔位於 `/etc/supervisord.conf`，而 Debian/Ubuntu 位於 `/etc/supervisor/supervisord.conf`。回歸 run `30681951917` 僅新增的 Alpine 路徑斷言失敗，修正客體路徑映射後 run `30681997634` 通過全部 29 項 Bats、Bash 語法與 ShellCheck。
