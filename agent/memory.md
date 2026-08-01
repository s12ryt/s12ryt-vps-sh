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
