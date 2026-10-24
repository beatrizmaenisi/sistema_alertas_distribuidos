#!/usr/bin/env bash
# Sobe 3 nós localmente, sem Docker. Ctrl+C derruba todos.
# Para derrubar só um durante a demo: kill $(cat /tmp/alertas-no2.pid)
set -euo pipefail
cd "$(dirname "$0")/.."
go build -o bin/ ./cmd/...

pids=()
for i in 1 2 3; do
  peers=()
  for j in 1 2 3; do [[ $j != "$i" ]] && peers+=("http://localhost:808$j"); done
  ./bin/no -id "no$i" -porta "808$i" -peers "$(IFS=,; echo "${peers[*]}")" "$@" &
  pids+=($!)
  echo $! > "/tmp/alertas-no$i.pid"
done

trap 'kill "${pids[@]}" 2>/dev/null' EXIT
echo "painéis: http://localhost:8081  http://localhost:8082  http://localhost:8083"
wait
