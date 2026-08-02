#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
    create_bootstrap_curl_mock
    export PATH="${MOCK_BIN}:/usr/bin:/bin"
    export MOCK_DOWNLOAD_SOURCE="${PROJECT_ROOT}/s12ryt.sh"
}

create_bootstrap_curl_mock() {
    cat > "${MOCK_BIN}/curl" <<'EOF'
#!/bin/bash
printf 'curl %s\n' "$*" >> "$MOCK_LOG"

output_file=""
while (( $# > 0 )); do
    if [[ "$1" == "-o" ]]; then
        output_file="$2"
        shift 2
    else
        shift
    fi
done

case "${S12RYT_CURL_MODE:-success}" in
    success)
        cp "$MOCK_DOWNLOAD_SOURCE" "$output_file"
        ;;
    download-fail)
        exit 22
        ;;
    syntax-fail)
        printf 'if then\n' > "$output_file"
        ;;
    version-mismatch)
        cp "$MOCK_DOWNLOAD_SOURCE" "$output_file"
        sed -Ei 's/^(readonly[[:space:]]+)?VERSION="[0-9]+\.[0-9]+\.[0-9]+"/readonly VERSION="9.9.9"/' "$output_file"
        ;;
    *)
        exit 64
        ;;
esac
EOF
    chmod +x "${MOCK_BIN}/curl"
}

run_process_bootstrap() {
    local input="$1"
    local mode="$2"

    export S12RYT_CURL_MODE="$mode"
    unset S12RYT_BOOTSTRAP_URL
    run /bin/bash -c 'printf "%s" "$1" | /bin/bash <(/bin/cat "$2")' \
        _ "$input" "${PROJECT_ROOT}/s12ryt.sh"
}

@test "process substitution 會從 main 重新下載並建立穩定副本" {
    run_process_bootstrap $'0\n' success

    [ "$status" -eq 0 ]
    [[ "$output" != *"cannot stat"* ]]
    [ -x "$HOME/.local/share/s12ryt/s12ryt.sh" ]
    [ -x "$HOME/.local/bin/s" ]
    run cmp "$PROJECT_ROOT/s12ryt.sh" "$HOME/.local/share/s12ryt/s12ryt.sh"
    [ "$status" -eq 0 ]
    run grep -F 'https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/main/s12ryt.sh' "$MOCK_LOG"
    [ "$status" -eq 0 ]
    [[ "$output" == *"--connect-timeout 5 --max-time 30"* ]]
}

@test "暫時來源下載或驗證失敗會保留既有安裝並繼續選單" {
    local stable_path="$HOME/.local/share/s12ryt/s12ryt.sh"
    local launcher_path="$HOME/.local/bin/s"
    local bootstrap_output row mode expected
    local rows=$'download-fail|錯誤：無法重新下載 s12ryt 完整腳本。\nsyntax-fail|錯誤：重新下載的 s12ryt 腳本語法驗證失敗。\nversion-mismatch|錯誤：重新下載的 s12ryt 腳本版本與目前執行版本不一致。'

    while IFS='|' read -r mode expected; do
        mkdir -p "$(dirname "$stable_path")" "$(dirname "$launcher_path")"
        printf 'stable-sentinel\n' > "$stable_path"
        printf 'launcher-sentinel\n' > "$launcher_path"
        chmod 0755 "$stable_path" "$launcher_path"
        : > "$MOCK_LOG"

        run_process_bootstrap $'8\n0\n' "$mode"

        [ "$status" -eq 0 ]
        bootstrap_output="$output"
        [[ "$bootstrap_output" == *"$expected"* ]]
        [[ "$bootstrap_output" == *"警告：僅臨時執行；s 可能不存在或仍是舊版"* ]]
        [[ "$bootstrap_output" == *"s12ryt 項目列表"* ]]
        [[ "$bootstrap_output" == *"1. s12ryt-多ipv6出站"* ]]
        [ "$(cat "$stable_path")" = "stable-sentinel" ]
        [ "$(cat "$launcher_path")" = "launcher-sentinel" ]
        [ -z "$(find "$(dirname "$stable_path")" -maxdepth 1 -name '.s12ryt-bootstrap.*' -print -quit)" ]
    done <<< "$rows"
}
