#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# @author Admilson B. F. Cossa

oracle_sql() {
  timeout --kill-after=2s "${1}s" docker exec -i "$fixture_id" sqlplus -L -S /nolog
}

start_oracle() {
  local image=container-registry.oracle.com/database/free:23.26.3.0-lite-amd64@sha256:770ca58ab319d8cfde610b0db25c06899ce0af9591c4addfa10d2cb8f0318e65
  local admin_entropy admin_password deadline startup_probe fixture_probe port

  # The shared runner generates an alphanumeric password safe for SQL*Plus and the URI.
  if [[ ! $fixture_password =~ ^[a-zA-Z0-9]+$ ]]; then
    printf 'The Oracle fixture password must be alphanumeric.\n' >&2
    return 1
  fi
  admin_entropy=$(openssl rand -hex 12) || return 1
  admin_password="Gm${admin_entropy}Z9"
  if [[ ${GITHUB_ACTIONS:-false} == true ]]; then
    printf '::add-mask::%s\n' "$admin_password"
  fi
  ORACLE_PWD=$admin_password start_fixture "$image" --platform linux/amd64 \
    --publish 127.0.0.1::1521 --env ORACLE_PWD || return 1

  deadline=$((SECONDS + 600))
  while :; do
    # A TCP login verifies that setup has applied the password and registered the PDB.
    if startup_probe=$(oracle_sql 10 2>/dev/null <<SQL
whenever oserror exit failure
whenever sqlerror exit failure
set heading off
set feedback off
set pagesize 0
connect SYS/"$admin_password"@//127.0.0.1:1521/FREEPDB1 as sysdba
select 'GOMIGRATE_ORACLE_READY' from v\$pdbs where name = 'FREEPDB1' and open_mode = 'READ WRITE';
exit success
SQL
    ) && [[ $startup_probe == *GOMIGRATE_ORACLE_READY* ]]; then
      break
    fi
    if [[ $(timeout --kill-after=2s 5s docker inspect --format '{{.State.Running}}' "$fixture_id" 2>/dev/null) == false ]]; then
      printf 'The Oracle fixture container stopped before FREEPDB1 was ready.\n' >&2
      return 1
    fi
    if (( SECONDS >= deadline )); then
      printf 'Timed out waiting for the Oracle fixture service FREEPDB1.\n' >&2
      return 1
    fi
    sleep 5
  done

  # Conformance provisions and drops isolated users, delegates their migration grants,
  # and queries the server version as the fixture account.
  if ! oracle_sql 30 >/dev/null 2>&1 <<SQL
whenever oserror exit failure
whenever sqlerror exit failure
connect / as sysdba
alter session set container = FREEPDB1;
declare
  existing number;
  datafile varchar2(4000);
begin
  select count(*) into existing from dba_tablespaces where tablespace_name = 'USERS';
  if existing = 0 then
    select regexp_replace(file_name, '[^/]+$', 'gomigrate_users.dbf') into datafile
      from dba_data_files where tablespace_name = 'SYSTEM' and rownum = 1;
    execute immediate 'create tablespace users datafile ' || dbms_assert.enquote_literal(datafile)
      || ' size 16M autoextend on next 8M maxsize 256M';
  end if;
end;
/
alter database default tablespace USERS;
create user GM_FIXTURE identified by "$fixture_password";
grant create user, drop user to GM_FIXTURE;
grant create session, create table, create procedure, create view to GM_FIXTURE with admin option;
grant execute on SYS.DBMS_LOCK to GM_FIXTURE with grant option;
grant select on SYS.V_\$VERSION to GM_FIXTURE;
exit success
SQL
  then
    printf 'Could not create and grant privileges to the Oracle fixture account.\n' >&2
    return 1
  fi
  if ! fixture_probe=$(oracle_sql 10 2>/dev/null <<SQL
whenever oserror exit failure
whenever sqlerror exit failure
set heading off
set feedback off
set pagesize 0
connect GM_FIXTURE/"$fixture_password"@//127.0.0.1:1521/FREEPDB1
select 'GOMIGRATE_ORACLE_FIXTURE' from v\$version where banner like 'Oracle%';
exit success
SQL
  ) || [[ $fixture_probe != *GOMIGRATE_ORACLE_FIXTURE* ]]; then
    printf 'The Oracle fixture account failed its FREEPDB1 login or version check.\n' >&2
    return 1
  fi

  port=$(fixture_port 1521) || return 1
  export GOMIGRATE_TEST_ORACLE_DSN="oracle://GM_FIXTURE:$fixture_password@127.0.0.1:$port/FREEPDB1?protocol=tcp"
}
