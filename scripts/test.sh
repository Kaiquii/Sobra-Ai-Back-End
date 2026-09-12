#!/usr/bin/env sh
set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_cache_directory="$repository_root/.codex-go-cache"
artifact_directory="$repository_root/.artifacts/test"
coverage_profile="$artifact_directory/coverage.out"
coverage_summary="$artifact_directory/coverage.txt"

cd "$repository_root"
mkdir -p "$go_cache_directory"
export GOCACHE="$go_cache_directory"

if [ "$#" -eq 1 ] && [ "$1" = "--quick" ]; then
    echo "[test] Executando suite rapida..."
    go test ./... -count=1 -timeout=2m
    echo "[test] Suite rapida concluida."
    exit 0
fi

integration=false
if [ "$#" -eq 1 ] && [ "$1" = "--integration" ]; then
    integration=true
elif [ "$#" -gt 0 ]; then
    echo "Uso: $0 [--quick|--integration]" >&2
    exit 2
fi

echo "[test] Validando formatacao..."
unformatted_files=$(gofmt -l .)
if [ -n "$unformatted_files" ]; then
    echo "Arquivos Go sem formatacao:" >&2
    echo "$unformatted_files" >&2
    exit 1
fi

echo "[test] Executando go vet..."
go vet ./...

echo "[test] Compilando todos os comandos..."
go build ./...

mkdir -p "$artifact_directory"

echo "[test] Executando testes com cobertura..."
go test ./... \
    -count=1 \
    -shuffle=on \
    -timeout=2m \
    -covermode=atomic \
    -coverprofile="$coverage_profile"

echo "[test] Gerando resumo de cobertura..."
go tool cover -func="$coverage_profile" > "$coverage_summary"
tail -n 1 "$coverage_summary"
if [ "$integration" = true ]; then
    go test -tags=integration ./cmd/api -count=1 -timeout=5m -coverpkg=./internal/...,./cmd/api -coverprofile="$artifact_directory/integration.out"
fi
echo "[test] Harness completo concluido. Artefatos em $artifact_directory"
