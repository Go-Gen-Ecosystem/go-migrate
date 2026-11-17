#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# @author Admilson B. F. Cossa
set -euo pipefail

engine=${1:-}
case "$engine" in
  oracle|db2) ;;
  *) printf 'Expected an Oracle or Db2 fixture.\n' >&2; exit 2 ;;
esac

if [[ $(git rev-parse HEAD) != "${GOMIGRATE_NATIVE_SOURCE_SHA:?Missing test source SHA}" ]]; then
  printf 'Native conformance source does not match the requested commit.\n' >&2
  exit 1
fi
if [[ ! ${GOMIGRATE_NATIVE_FIXTURE_SEED:-} =~ ^[a-f0-9]{64}$ ]]; then
  printf 'The fixture needs its environment-scoped bootstrap secret.\n' >&2
  exit 1
fi

# Use different passwords every attempt, including local tests. Keep only the
# real credentials needed by each child, and keep the bootstrap seed in Bash.
entropy=$(openssl rand -hex 32)
derive_password() {
  local digest
  digest=$(printf '%s\n%s\n%s\n' "$GOMIGRATE_NATIVE_FIXTURE_SEED" "$entropy" "$1" | openssl dgst -sha256 -r)
  printf 'Gm%.*sZ9\n' 24 "${digest%% *}"
}
fixture_password=$(derive_password "$engine/admin")
fixture_app_password=$(derive_password "$engine/app")
unset GOMIGRATE_NATIVE_FIXTURE_SEED entropy
if [[ ${GITHUB_ACTIONS:-false} == true ]]; then
  printf '::add-mask::%s\n' "$fixture_password" "$fixture_app_password"
fi

export MSYS_NO_PATHCONV=1 GOWORK=off
owner=go-migrate-native-ci
run="native-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-0}-$engine-$$-$RANDOM"
temp_parent=$(cd "${RUNNER_TEMP:-${TMPDIR:-/tmp}}" && pwd -P)
fixture_dir=$(mktemp -d "$temp_parent/gomigrate-$run.XXXXXXXX")
fixture_id=''
cleanup() {
  local result=$? identity layer
  trap - EXIT
  set +e
  printf 'Cleaning up the owned %s fixture for %s.\n' "$engine" "$run"
  if [[ -n "$fixture_id" ]]; then
    identity=$(docker inspect -f '{{index .Config.Labels "project"}}|{{index .Config.Labels "owner"}}|{{index .Config.Labels "run"}}' "$fixture_id")
    if [[ "$identity" != "go-migrate|$owner|$run" ]]; then
      result=1
    else
      layer=$(docker inspect --size -f '{{.SizeRw}}' "$fixture_id")
      if [[ $(docker inspect -f '{{.State.Running}}' "$fixture_id") == true ]]; then docker stop --timeout 15 "$fixture_id" >/dev/null || result=1; fi
      if docker rm "$fixture_id" >/dev/null && [[ -z $(docker ps -aq --filter "id=$fixture_id") ]]; then
        printf 'Removed container %s (Docker writable layer: %s bytes). No volumes deleted.\n' "$fixture_id" "$layer"
      else
        result=1
      fi
    fi
  fi
  if [[ "$fixture_dir" == "$temp_parent/gomigrate-$run."* && -d "$fixture_dir" && ! -L "$fixture_dir" ]]; then
    rm -rf -- "$fixture_dir" || result=1
  fi
  [[ ! -e "$fixture_dir" ]] || result=1
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
printf 'Project go-migrate, owner %s, run %s, temporary directory %s. Cleanup follows the test.\n' "$owner" "$run" "$fixture_dir"

start_fixture() {
  local image=$1 mounts
  shift
  docker image inspect "$image" >/dev/null 2>&1 || docker pull --platform linux/amd64 "$image" || return 1
  fixture_id=$(docker create --pull never --name "gomigrate-$run" --label project=go-migrate \
    --label "owner=$owner" --label "run=$run" "$@" "$image") || return 1
  [[ "$fixture_id" =~ ^[a-f0-9]{64}$ ]] || return 1
  printf 'Created container %s from %s.\n' "$fixture_id" "$image"
  mounts=$(docker inspect -f '{{range .Mounts}}{{println .Type}}{{end}}' "$fixture_id") || return 1
  if grep -qx volume <<< "$mounts"; then
    printf 'Fixture created an unexpected Docker volume.\n' >&2
    return 1
  fi
  docker start "$fixture_id" >/dev/null
}

fixture_port() {
  local mapping
  mapping=$(docker port "$fixture_id" "$1/tcp") || return 1
  if [[ ! "$mapping" =~ ^127\.0\.0\.1:([0-9]+)$ ]]; then
    printf 'Fixture port was not bound exclusively to IPv4 loopback.\n' >&2
    return 1
  fi
  printf '%s\n' "${BASH_REMATCH[1]}"
}

script_dir=$(cd "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
case "$engine" in
  oracle) source "$script_dir/oracle-native-fixture.sh"; start_oracle ;;
  db2) source "$script_dir/db2-native-fixture.sh"; start_db2 ;;
esac

if [[ ${GITHUB_ACTIONS:-false} == true ]]; then
  for value in "${GOMIGRATE_TEST_ORACLE_DSN:-}" "${GOMIGRATE_TEST_DB2_DSN:-}" "${GOMIGRATE_TEST_DB2_APP_DSN:-}"; do
    if [[ -n "$value" ]]; then printf '::add-mask::%s\n' "$value"; fi
  done
fi
flags=(-race)
if [[ ${GOMIGRATE_NATIVE_LOCAL_NO_RACE:-0} == 1 && ${GITHUB_ACTIONS:-false} != true ]]; then flags=(); fi
go test -json "${flags[@]}" -count=1 -timeout=10m -run "^TestConformance\$/^$engine\$" . 2>&1 | go run ./internal/nativecheck "$engine"
