#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
}

@test "ipapi fixture 顯示網路資料並僅在四項訊號全否時推測家寬" {
    run env S12RYT_SOURCE_ONLY=1 /bin/bash -c \
        'source "$1"; print_ip_record "IPv4" "$2"' _ \
        "${PROJECT_ROOT}/s12ryt.sh" "${PROJECT_ROOT}/tests/fixtures/ipapi-residential.json"

    [ "$status" -eq 0 ]
    [[ "$output" == *"IPv4: 198.51.100.10"* ]]
    [[ "$output" == *"ASN: AS64500"* ]]
    [[ "$output" == *"ISP: Example Residential ISP"* ]]
    [[ "$output" == *"國家/地區: Taiwan / Taipei"* ]]
    [[ "$output" == *"資料中心: 否"* ]]
    [[ "$output" == *"行動網路: 否"* ]]
    [[ "$output" == *"Proxy: 否"* ]]
    [[ "$output" == *"VPN: 否"* ]]
    [[ "$output" == *"可能家寬（僅為推測）"* ]]
}

@test "任一風險訊號成立時不推測為家寬" {
    run env S12RYT_SOURCE_ONLY=1 /bin/bash -c \
        'source "$1"; print_ip_record "IPv6" "$2"' _ \
        "${PROJECT_ROOT}/s12ryt.sh" "${PROJECT_ROOT}/tests/fixtures/ipapi-hosting.json"

    [ "$status" -eq 0 ]
    [[ "$output" == *"IPv6: 2001:db8::10"* ]]
    [[ "$output" == *"資料中心: 是"* ]]
    [[ "$output" == *"Proxy: 是"* ]]
    [[ "$output" != *"可能家寬"* ]]
}

@test "連通性檢測使用逾時限制並輸出全部站點的三態結果" {
    cat > "${MOCK_BIN}/curl" <<'EOF'
#!/bin/bash
printf '%s\n' "$*" >> "$MOCK_LOG"
url="${!#}"
case "$url" in
    *github.com*) printf '200'; exit 0 ;;
    *://www.google.com*) printf '403'; exit 0 ;;
    *cloudflare.com*) exit 28 ;;
    *) printf '200'; exit 0 ;;
esac
EOF
    chmod +x "${MOCK_BIN}/curl"

    run env PATH="$MOCK_BIN:/usr/bin:/bin" MOCK_LOG="$MOCK_LOG" S12RYT_SOURCE_ONLY=1 \
        /bin/bash -c 'source "$1"; check_connectivity' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"GitHub: 可達"* ]]
    [[ "$output" == *"Google: 受限"* ]]
    [[ "$output" == *"Cloudflare: 逾時/失敗"* ]]
    for service in YouTube Netflix Disney+ Spotify TikTok ChatGPT Gemini Telegram; do
        [[ "$output" == *"${service}: 可達"* ]]
    done
    [ "$(wc -l < "$MOCK_LOG")" -eq 11 ]
    grep -q -- '--connect-timeout' "$MOCK_LOG"
    grep -q -- '--max-time' "$MOCK_LOG"
}

@test "有限服務解析器從 fixture 回應推測地區並呈現可用性" {
    local body
    body="${TEST_ROOT}/response.txt"

    printf '%s\n' '{"country":"US"}' > "$body"
    run env S12RYT_SOURCE_ONLY=1 /bin/bash -c 'source "$1"; parse_stream_region Netflix "$2"' \
        _ "${PROJECT_ROOT}/s12ryt.sh" "$body"
    [ "$status" -eq 0 ]
    [ "$output" = "US" ]

    printf '%s\n' '{"countryCode":"CA"}' > "$body"
    for service in Disney+ 'YouTube Premium' Gemini; do
        run env S12RYT_SOURCE_ONLY=1 /bin/bash -c 'source "$1"; parse_stream_region "$2" "$3"' \
            _ "${PROJECT_ROOT}/s12ryt.sh" "$service" "$body"
        [ "$status" -eq 0 ]
        [ "$output" = "CA" ]
    done

    printf '%s\n' '{"country":"DE"}' > "$body"
    run env S12RYT_SOURCE_ONLY=1 /bin/bash -c 'source "$1"; parse_stream_region Spotify "$2"' \
        _ "${PROJECT_ROOT}/s12ryt.sh" "$body"
    [ "$output" = "DE" ]

    printf '%s\n' '{"region":"SG"}' > "$body"
    run env S12RYT_SOURCE_ONLY=1 /bin/bash -c 'source "$1"; parse_stream_region TikTok "$2"' \
        _ "${PROJECT_ROOT}/s12ryt.sh" "$body"
    [ "$output" = "SG" ]

    printf '%s\n' 'loc=GB' > "$body"
    run env S12RYT_SOURCE_ONLY=1 /bin/bash -c 'source "$1"; parse_stream_region ChatGPT "$2"' \
        _ "${PROJECT_ROOT}/s12ryt.sh" "$body"
    [ "$output" = "GB" ]

    run env S12RYT_SOURCE_ONLY=1 /bin/bash -c \
        'source "$1"; render_stream_result Netflix 0 200 US; render_stream_result Gemini 0 403 ""; render_stream_result TikTok 28 000 ""' \
        _ "${PROJECT_ROOT}/s12ryt.sh"
    [[ "$output" == *"Netflix: 可達（推測地區: US）"* ]]
    [[ "$output" == *"Gemini: 受限"* ]]
    [[ "$output" == *"TikTok: 逾時/失敗"* ]]
}

@test "選單 3 呼叫整合檢測並顯示結果限制" {
    run env HOME="$HOME" S12RYT_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        install_launcher() { :; }
        show_network_information() { printf "NETWORK_MARKER\n結果只代表檢測當下，不保證登入後可播放。\n"; }
        printf "3\n0\n" | main
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"NETWORK_MARKER"* ]]
    [[ "$output" == *"不保證登入後可播放"* ]]
}
