# SPDX-License-Identifier: Apache-2.0
# @author Admilson B. F. Cossa
# Sourced by the native conformance fixture runner.

db2_fixture_owner() {
  local limit=$1
  shift
  timeout "$limit" docker exec --user db2inst1 --interactive "$@" "$fixture_id" bash -ec '
    HOME=$(getent passwd db2inst1 | cut -d: -f6)
    export HOME
    . "$HOME/sqllib/db2profile"
    export LD_LIBRARY_PATH="$HOME/sqllib/lib64/gskit${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
    exec bash -es
  '
}

start_db2() {
  if [[ ${CI_DB2_ACCEPT_LICENSE:-} != Y ]]; then
    printf 'Db2 conformance requires explicit CI_DB2_ACCEPT_LICENSE=Y.\n' >&2
    return 1
  fi

  local image=icr.io/db2_community/db2:12.1.5.0@sha256:e48ca934c29ab72d9e8b602929e6a52b64841cb7f8ef9426ee8cf5e1bc3e5fb3
  local ca="$fixture_dir/db2-ca.pem" ca_path port deadline keystore_password
  ca_path=$ca
  if command -v cygpath >/dev/null; then
    ca_path=$(cygpath -am "$ca")
  fi
  if [[ ! $ca_path =~ ^[a-zA-Z0-9_./:-]+$ ]]; then
    printf 'The Db2 fixture CA path must be URL-safe.\n' >&2
    return 1
  fi
  keystore_password=$(openssl rand -hex 24)
  if [[ ${GITHUB_ACTIONS:-false} == true ]]; then
    printf '::add-mask::%s\n' "$keystore_password"
  fi

  # Replace both image volumes with temporary mounts. Db2 requires setuid on /database.
  DB2INST1_PASSWORD=$fixture_password start_fixture "$image" \
    --platform linux/amd64 --privileged --tmpfs /database:rw,exec,suid,size=3g \
    --tmpfs /hadr:rw,size=32m --env HADR_ENABLED=false \
    --publish 127.0.0.1::50001 --env LICENSE=accept --env DB2INSTANCE=db2inst1 \
    --env DB2INST1_PASSWORD --env DBNAME=GTMTEST --env PERSISTENT_HOME=true \
    --env AUTOCONFIG=false --env ARCHIVE_LOGS=false
  port=$(fixture_port 50001)

  deadline=$((SECONDS + 600))
  until timeout 10s docker logs "$fixture_id" 2>&1 | grep -F 'Setup has completed.' >/dev/null; do
    if ((SECONDS >= deadline)) || [[ $(timeout 10s docker inspect -f '{{.State.Running}}' "$fixture_id") != true ]]; then
      printf 'Db2 image initialization failed or timed out.\n' >&2
      return 1
    fi
    sleep 3
  done

  # GSKit 9 loads the OS ICU libraries. The image's private Db2 ICU build
  # has distinct library names and cannot supply this dependency.
  timeout 180s docker exec --user root "$fixture_id" \
    dnf -q -y --disablerepo='*' --enablerepo=ubi-9-baseos-rpms --enablerepo=ubi-9-appstream-rpms \
    --setopt=install_weak_deps=false install libicu

  timeout 30s docker exec --user root --interactive "$fixture_id" bash -ec '
    useradd --no-create-home --shell /sbin/nologin gtmapp
    exec chpasswd
  ' <<< "gtmapp:$fixture_app_password"

  # Generate the private server key in Db2's own keystore and sign only its CSR.
  db2_fixture_owner 60s --env "KEYSTORE_PASSWORD=$keystore_password" <<'DB2'
umask 077
store=/tmp/gomigrate-db2-tls
mkdir "$store"
gsk="$HOME/sqllib/gskit/bin/gsk9certutil_64"
"$gsk" -keydb -create -db "$store/server.p12" -type pkcs12 -pqc false -pw "$KEYSTORE_PASSWORD" -stash
"$gsk" -certreq -create -db "$store/server.p12" -stashed -label gomigrate \
  -dn 'CN=gomigrate-db2' -size 2048 -sigalg SHA256_WITH_RSA -target "$store/server.csr"
DB2
  (
    umask 077
    cd "$fixture_dir"
    timeout 30s docker exec --user db2inst1 "$fixture_id" cat /tmp/gomigrate-db2-tls/server.csr > db2-server.csr
    openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 2 -subj '/CN=go-migrate Db2 CI CA' \
      -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' -addext 'keyUsage=critical,keyCertSign' \
      -keyout db2-ca.key -out db2-ca.pem
    cat > db2-server.ext <<'EXTENSIONS'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=IP:127.0.0.1
EXTENSIONS
    openssl x509 -req -sha256 -days 1 -in db2-server.csr \
      -CA db2-ca.pem -CAkey db2-ca.key -CAcreateserial \
      -extfile db2-server.ext -out db2-server.pem
  )
  timeout 30s docker exec --user db2inst1 --interactive "$fixture_id" \
    bash -ec 'umask 077; cat > /tmp/gomigrate-db2-tls/ca.pem' < "$ca"
  timeout 30s docker exec --user db2inst1 --interactive "$fixture_id" \
    bash -ec 'umask 077; cat > /tmp/gomigrate-db2-tls/server.pem' < "$fixture_dir/db2-server.pem"

  db2_fixture_owner 240s <<'DB2'
store=/tmp/gomigrate-db2-tls
gsk="$HOME/sqllib/gskit/bin/gsk9certutil_64"
"$gsk" -cert -add -db "$store/server.p12" -stashed -file "$store/ca.pem" -label gomigrate-ca
"$gsk" -cert -receive -db "$store/server.p12" -stashed -file "$store/server.pem"
db2 update dbm cfg using SSL_SVR_KEYDB "$store/server.p12" SSL_SVR_STASH "$store/server.sth" \
  SSL_SVR_LABEL gomigrate SSL_SVCENAME 50001 SSL_VERSIONS TLSV12,TLSV13
db2set DB2COMM=SSL
db2stop force
db2start
db2 connect to GTMTEST
tablespace=$(db2 -x "SELECT COUNT(*) FROM SYSCAT.TABLESPACES WHERE TBSPACE = 'SYSTOOLSPACE'")
if [[ ${tablespace//[[:space:]]/} == 0 ]]; then
  db2 'CREATE TABLESPACE SYSTOOLSPACE IN IBMCATGROUP MANAGED BY AUTOMATIC STORAGE USING STOGROUP IBMSTOGROUP EXTENTSIZE 4'
fi
db2 'GRANT CONNECT, CREATETAB ON DATABASE TO USER GTMAPP'
# The released history DDL needs at least 8 KiB pages in a UTF-8 database.
db2 'CREATE BUFFERPOOL GM_FIXTURE_BP SIZE 256 PAGESIZE 16K'
db2 'CREATE TABLESPACE GM_FIXTURE_DATA PAGESIZE 16K MANAGED BY AUTOMATIC STORAGE BUFFERPOOL GM_FIXTURE_BP'
db2 'CREATE SYSTEM TEMPORARY TABLESPACE GM_FIXTURE_TEMP PAGESIZE 16K MANAGED BY AUTOMATIC STORAGE BUFFERPOOL GM_FIXTURE_BP'
db2 'GRANT USE OF TABLESPACE GM_FIXTURE_DATA TO USER GTMAPP'
db2 'GRANT EXECUTE ON MODULE SYSIBMADM.DBMS_LOCK TO USER GTMAPP'
db2 connect reset
db2 terminate
DB2

  # Look up the hostname used by the negative test, but verify the server's IP.
  if ! (cd "$fixture_dir" && timeout 15s openssl s_client -connect "localhost:$port" -verify_ip 127.0.0.1 \
      -verify_return_error -CAfile db2-ca.pem </dev/null >/dev/null); then
    printf 'Db2 TLS endpoint is unavailable through the fixture hostname or failed certificate verification.\n' >&2
    return 1
  fi

  export GOMIGRATE_TEST_DB2_DSN="db2://db2inst1:$fixture_password@127.0.0.1:$port/GTMTEST?tls=true&ssl_ca=$ca_path"
  export GOMIGRATE_TEST_DB2_APP_DSN="db2://gtmapp:$fixture_app_password@127.0.0.1:$port/GTMTEST?tls=true&ssl_ca=$ca_path"
  export GOMIGRATE_TEST_DB2_TLS_MISMATCH_HOST=localhost
}
