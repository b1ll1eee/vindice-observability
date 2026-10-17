#!/usr/bin/env sh
# Reproduce docs/benchmark.md. Needs Docker with Compose, Go and Python 3. Run from bench/.
# Each system runs alone; everything it creates is removed at the end.
set -eu
cd "$(dirname "$0")"
umask 077
printf 'VINDICE_ADMIN_PASSWORD=%s\nVINDICE_SESSION_SECRET=%s\n' "$(openssl rand -hex 16)" "$(openssl rand -hex 32)" > bench.env
umask 022
(cd loadgen && go build -o ../loadgen-bin .)

docker compose -f vindice.compose.yml up -d --wait
. ./bench.env
curl -sf -c .cookies -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$VINDICE_ADMIN_PASSWORD\"}" http://localhost:18080/api/v1/auth/login >/dev/null
curl -sf -b .cookies -H 'Content-Type: application/json' -d '{"name":"bench"}' http://localhost:18080/api/v1/keys \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["secret"])' > .vkey
python3 bench.py vindice > /dev/null
docker compose -f vindice.compose.yml down -v

docker compose -f loki.compose.yml up -d --wait
until [ "$(curl -s http://localhost:13100/ready)" = ready ]; do sleep 2; done
python3 bench.py loki > /dev/null
docker compose -f loki.compose.yml down -v

rm -f bench.env .cookies .vkey loadgen-bin
echo "results in bench/results/"
