#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
    export UPDATE_TARGET="${TEST_ROOT}/installed/s12ryt.sh"
    mkdir -p "${UPDATE_TARGET%/*}"
    cat > "$UPDATE_TARGET" <<'EOF'
#!/usr/bin/env bash
readonly VERSION="1.0.0"
printf 'old-version\n'
EOF
    chmod +x "$UPDATE_TARGET"
}

create_update_curl_mock() {
    cat > "${MOCK_BIN}/curl" <<'EOF'
#!/bin/bash
printf 'curl %s\n' "$*" >> "$MOCK_LOG"
output=""
url=""
while (($#)); do
    case "$1" in
        -o)
            output="$2"
            shift 2
            ;;
        https://*)
            url="$1"
            shift
            ;;
        *)
            shift
            ;;
    esac
done

if [[ "$url" == *'/releases/latest' ]]; then
    if [[ "${MOCK_UPDATE_MODE:-success}" == "api-fail" ]]; then
        exit 22
    fi
    printf '{"tag_name":"%s"}\n' "${MOCK_RELEASE_TAG:-v1.1.0}" > "$output"
    exit 0
fi

case "${MOCK_UPDATE_MODE:-success}" in
    download-fail)
        exit 22
        ;;
    invalid-syntax)
        printf '%s\n' '#!/usr/bin/env bash' 'if broken' > "$output"
        ;;
    version-mismatch)
        printf '%s\n' '#!/usr/bin/env bash' 'readonly VERSION="1.0.1"' 'printf mismatch' > "$output"
        ;;
    success)
        printf '%s\n' '#!/usr/bin/env bash' 'readonly VERSION="1.1.0"' 'printf updated-version' > "$output"
        ;;
esac
EOF
    chmod +x "${MOCK_BIN}/curl"
}

run_update_function() {
    run env \
        HOME="$HOME" \
        PATH="$MOCK_BIN:/usr/bin:/bin" \
        MOCK_LOG="$MOCK_LOG" \
        MOCK_RELEASE_TAG="${MOCK_RELEASE_TAG:-v1.1.0}" \
        MOCK_UPDATE_MODE="${MOCK_UPDATE_MODE:-success}" \
        S12RYT_UPDATE_TARGET="$UPDATE_TARGET" \
        S12RYT_SOURCE_ONLY=1 \
        /bin/bash -c 'source "$1"; shift; "$@"' _ \
        "${PROJECT_ROOT}/s12ryt.sh" "$@"
}

@test "語意版本比較只接受 X.Y.Z 並正確判斷新版" {
    run env S12RYT_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        version_is_newer 1.0.0 1.0.1 && printf "patch-newer\n"
        version_is_newer 1.9.9 1.10.0 && printf "minor-newer\n"
        version_is_newer 1.0.0 2.0.0 && printf "major-newer\n"
        version_is_newer 1.0.0 1.0.0 || printf "equal\n"
        version_is_newer 2.0.0 1.99.99 || printf "older\n"
        validate_version 1.2 || printf "invalid\n"
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [ "$output" = $'patch-newer\nminor-newer\nmajor-newer\nequal\nolder\ninvalid' ]
}

@test "目前已是最新版時只查詢 Release 且不下載腳本" {
    create_update_curl_mock
    export MOCK_RELEASE_TAG=v1.0.0

    run_update_function check_for_updates

    [ "$status" -eq 0 ]
    [[ "$output" == *"目前已是最新版 v1.0.0"* ]]
    [ "$(grep -c '^curl ' "$MOCK_LOG")" -eq 1 ]
    grep -Fq 'https://api.github.com/repos/s12ryt/s12ryt-vps-sh/releases/latest' "$MOCK_LOG"
    grep -Fq 'old-version' "$UPDATE_TARGET"
}

@test "有新版時驗證版本與 Bash 語法後原子替換穩定副本" {
    create_update_curl_mock

    run_update_function check_for_updates

    [ "$status" -eq 0 ]
    [[ "$output" == *"已更新至 v1.1.0"* ]]
    [ -x "$UPDATE_TARGET" ]
    grep -Fq 'readonly VERSION="1.1.0"' "$UPDATE_TARGET"
    grep -Fq 'https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v1.1.0/s12ryt.sh' "$MOCK_LOG"
    run find "${UPDATE_TARGET%/*}" -maxdepth 1 -name '.s12ryt-update.*' -print
    [ -z "$output" ]
}

@test "更新各階段失敗時保留既有腳本" {
    create_update_curl_mock

    for mode in api-fail download-fail invalid-syntax version-mismatch; do
        export MOCK_UPDATE_MODE="$mode"
        run_update_function check_for_updates
        [ "$status" -ne 0 ]
        grep -Fq 'old-version' "$UPDATE_TARGET"
    done
    [[ "$output" == *"下載版本與 Release tag 不一致"* ]]

    export MOCK_UPDATE_MODE=success
    export MOCK_RELEASE_TAG=v1.2
    run_update_function check_for_updates
    [ "$status" -ne 0 ]
    [[ "$output" == *"Release tag 格式無效"* ]]
    grep -Fq 'old-version' "$UPDATE_TARGET"

    export MOCK_RELEASE_TAG=v1.1.0
    cat > "${MOCK_BIN}/mktemp" <<'EOF'
#!/bin/bash
exit 1
EOF
    chmod +x "${MOCK_BIN}/mktemp"
    run_update_function check_for_updates
    [ "$status" -ne 0 ]
    [[ "$output" == *"無法建立更新暫存檔"* ]]
    grep -Fq 'old-version' "$UPDATE_TARGET"
    rm -f "${MOCK_BIN}/mktemp"

    cat > "${MOCK_BIN}/mv" <<'EOF'
#!/bin/bash
exit 1
EOF
    chmod +x "${MOCK_BIN}/mv"
    run_update_function check_for_updates
    [ "$status" -ne 0 ]
    [[ "$output" == *"無法原子替換穩定副本"* ]]
    grep -Fq 'old-version' "$UPDATE_TARGET"
}

@test "項目列表固定顯示暫無項目且主選單 8 與 9 各自接線" {
    run env S12RYT_SOURCE_ONLY=1 /bin/bash -c 'source "$1"; show_projects' _ \
        "${PROJECT_ROOT}/s12ryt.sh"
    [ "$status" -eq 0 ]
    [ "$output" = "暫無項目" ]

    run env HOME="$HOME" S12RYT_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        install_launcher() { :; }
        show_projects() { printf "PROJECT_MARKER\n"; }
        check_for_updates() { printf "UPDATE_MARKER\n"; }
        printf "8\n9\n0\n" | main
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"PROJECT_MARKER"* ]]
    [[ "$output" == *"UPDATE_MARKER"* ]]
}
