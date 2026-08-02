#!/usr/bin/env bats

load test_helper

setup() {
  setup_sandbox
  export S12RYT_RESOURCE_SOURCE_ONLY=1
}

@test "資源驗收固定要求穩定六十秒且合計 RSS 不超過一百 MiB" {
  source "$PROJECT_ROOT/scripts/verify-resource-usage.sh"

  [ "$RESOURCE_STABLE_SECONDS" -eq 60 ]
  [ "$RESOURCE_RSS_LIMIT_KIB" -eq 102400 ]

  run assert_resource_rss 51200 51200
  [ "$status" -eq 0 ]
  [[ "$output" == *"合計 RSS：102400 KiB / 上限 102400 KiB"* ]]

  run assert_resource_rss 70000 40000
  [ "$status" -ne 0 ]
  [[ "$output" == *"資源驗收失敗：合計 RSS 110000 KiB 超過 102400 KiB"* ]]
}

@test "資源驗收拒絕非 root 與縮短穩定時間" {
  run /usr/bin/env \
    S12RYT_RESOURCE_EFFECTIVE_UID=1000 \
    S12RYT_RESOURCE_SOURCE_ONLY=0 \
    /bin/bash "$PROJECT_ROOT/scripts/verify-resource-usage.sh" verify missing-panel missing-profile
  [ "$status" -ne 0 ]
  [[ "$output" == *"資源驗收必須以 root 執行"* ]]

  run /usr/bin/env \
    S12RYT_RESOURCE_EFFECTIVE_UID=0 \
    S12RYT_RESOURCE_SOURCE_ONLY=0 \
    S12RYT_RESOURCE_STABLE_SECONDS=59 \
    /bin/bash "$PROJECT_ROOT/scripts/verify-resource-usage.sh" verify missing-panel missing-profile
  [ "$status" -ne 0 ]
  [[ "$output" == *"穩定觀察時間不得少於 60 秒"* ]]
}
