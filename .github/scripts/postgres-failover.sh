#!/usr/bin/env bash
# @author Admilson B. F. Cossa
set -euo pipefail

export MSYS_NO_PATHCONV=1 # Pass container paths unchanged from Git Bash on Windows.
export GOWORK=off
image=${GOMIGRATE_TEST_FAILOVER_IMAGE:-postgres:17.12-alpine3.23@sha256:318b8c3c90e13b6fc3a1e57b9bd260965ad578d46401b1b3cfdcc4e17ac81a3f}
owner=codex-pg-failover
run="pg-failover-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-0}-$$-$RANDOM"
primary=''
standby=''
network=''
cleanup() {
  local result=$? id label layer attached
  trap - EXIT
  set +e
  printf 'Stopping and removing the PostgreSQL failover fixtures for %s\n' "$run"
  for id in "$standby" "$primary"; do
    [[ -n "$id" ]] || continue
    label=$(docker inspect -f '{{index .Config.Labels "project"}}|{{index .Config.Labels "owner"}}|{{index .Config.Labels "run"}}' "$id")
    if [[ "$label" != "go-migrate|$owner|$run" ]]; then result=1; continue; fi
    layer=$(docker inspect --size -f '{{.SizeRw}}' "$id")
    if [[ "$(docker inspect -f '{{.State.Running}}' "$id")" == 'true' ]]; then
      docker stop --timeout 10 "$id" >/dev/null || { result=1; continue; }
    fi
    docker rm "$id" >/dev/null && [[ -z "$(docker ps -aq --filter "id=$id")" ]] || { result=1; continue; }
    printf 'Removed container %s (Docker writable layer: %s bytes)\n' "$id" "$layer"
  done
  if [[ -n "$network" ]]; then
    label=$(docker network inspect -f '{{index .Labels "project"}}|{{index .Labels "owner"}}|{{index .Labels "run"}}' "$network")
    attached=$(docker network inspect -f '{{len .Containers}}' "$network")
    if [[ "$label" == "go-migrate|$owner|$run" && "$attached" == 0 ]] && docker network rm "$network" >/dev/null &&
       [[ -z "$(docker network ls -q --filter "id=$network")" ]]; then
      printf 'Removed network %s. No volumes deleted.\n' "$network"
    else
      result=1
    fi
  fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

export GOMIGRATE_TEST_FAILOVER_PASSWORD
GOMIGRATE_TEST_FAILOVER_PASSWORD=$(openssl rand -hex 24)
if [[ ${GITHUB_ACTIONS:-false} == true ]]; then printf '::add-mask::%s\n' "$GOMIGRATE_TEST_FAILOVER_PASSWORD"; fi
labels=(--label project=go-migrate --label "owner=$owner" --label "run=$run")
printf 'Project go-migrate, owner %s, run %s. Fixture IDs below will be removed after the test.\n' "$owner" "$run"
docker image inspect "$image" >/dev/null 2>&1 || docker pull "$image"
network=$(docker network create "gomigrate-$run" "${labels[@]}")
printf 'Created network %s\n' "$network"
common=(--pull never --network "$network" --publish 127.0.0.1::5432 --tmpfs /var/lib/postgresql/data:rw "${labels[@]}")
primary=$(POSTGRES_PASSWORD=$GOMIGRATE_TEST_FAILOVER_PASSWORD docker run -d "${common[@]}" \
  --label role=primary --network-alias primary --env POSTGRES_PASSWORD "$image" \
  postgres -c synchronous_commit=remote_apply)
printf 'Started primary %s\n' "$primary"

query() {
  docker exec --user postgres "$1" psql -X -d postgres -qAt -v ON_ERROR_STOP=1 -c "$2"
}

wait_for() {
  local id=$1 expected=$2 sql=$3 deadline=$((SECONDS + 90))
  until [[ $(query "$id" "$sql" 2>/dev/null) == "$expected" ]]; do
    if ((SECONDS >= deadline)) || [[ $(docker inspect -f '{{.State.Running}}' "$id") != true ]]; then
      printf 'PostgreSQL fixture %s did not reach the expected state\n' "$id" >&2
      return 1
    fi
    sleep 1
  done
}

# TCP readiness excludes the image's temporary init-only server.
wait_for "$primary" on "SELECT CASE WHEN EXISTS (SELECT FROM pg_settings WHERE name='listen_addresses' AND setting='*') THEN 'on' ELSE 'off' END"
docker exec --user postgres "$primary" sh -ec 'printf "\nhost replication postgres samenet scram-sha-256\n" >> "$PGDATA/pg_hba.conf"'
query "$primary" 'SELECT pg_reload_conf()' >/dev/null

standby=$(PGPASSWORD=$GOMIGRATE_TEST_FAILOVER_PASSWORD docker run -d "${common[@]}" \
  --label role=standby --env PGPASSWORD --entrypoint sh "$image" -ec '
  chown postgres:postgres "$PGDATA"
  chmod 0700 "$PGDATA"
  gosu postgres pg_basebackup -D "$PGDATA" -X stream -R -w \
    -d "host=primary user=postgres application_name=gomigrate_standby connect_timeout=5"
  exec gosu postgres postgres -D "$PGDATA"
')
printf 'Started standby %s\n' "$standby"
wait_for "$standby" t 'SELECT pg_is_in_recovery()'
query "$primary" "ALTER SYSTEM SET synchronous_standby_names = 'gomigrate_standby'" >/dev/null
query "$primary" 'SELECT pg_reload_conf()' >/dev/null
wait_for "$primary" sync "SELECT sync_state FROM pg_stat_replication WHERE application_name='gomigrate_standby'"

export GOMIGRATE_TEST_FAILOVER_RUN=$run
export GOMIGRATE_TEST_FAILOVER_PRIMARY_ID=$primary
export GOMIGRATE_TEST_FAILOVER_STANDBY_ID=$standby
GOMIGRATE_TEST_FAILOVER_PRIMARY_ADDR=$(docker port "$primary" 5432/tcp)
GOMIGRATE_TEST_FAILOVER_STANDBY_ADDR=$(docker port "$standby" 5432/tcp)
export GOMIGRATE_TEST_FAILOVER_PRIMARY_ADDR GOMIGRATE_TEST_FAILOVER_STANDBY_ADDR
go_flags=(-race)
if [[ ${GOMIGRATE_TEST_FAILOVER_LOCAL_NO_RACE:-0} == 1 && ${GITHUB_ACTIONS:-false} != true ]]; then go_flags=(); fi
go test "${go_flags[@]}" -count=1 -v -timeout=4m -run '^TestPostgresFailover$' . |
  awk '{ print } /^--- PASS: TestPostgresFailover \(/ { passed = 1 } END { exit !passed }'
