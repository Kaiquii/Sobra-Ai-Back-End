# Comandos Canonicos

Execute os comandos a partir da raiz do repositorio.

## Harness Completo

Windows PowerShell:

```powershell
.\scripts\test.ps1
```

Linux e macOS:

```bash
sh ./scripts/test.sh
```

O harness completo valida formatacao, executa `go vet`, roda todos os testes sem cache e gera:

- `.artifacts/test/coverage.out`: perfil de cobertura.
- `.artifacts/test/coverage.txt`: resumo por funcao.

No CI, a mesma suite tambem passa pelo detector de condicoes de corrida do Go.

## Iteracao Rapida

Windows PowerShell:

```powershell
.\scripts\test.ps1 -Quick
```

Linux e macOS:

```bash
sh ./scripts/test.sh --quick
```

O modo rapido executa todos os testes sem gerar cobertura nem rodar analise estatica.

Para testar somente um pacote durante a implementacao:

```bash
go test ./internal/auth -count=1
```

Para executar um teste especifico:

```bash
go test ./internal/auth -run '^TestNomeDoTeste$' -count=1
```

## Build Local

```bash
go build ./cmd/api
go build ./cmd/migrate
```

## Migrations

Estes comandos precisam de `DB_DSN` apontando para um banco explicitamente escolhido:

```bash
go run ./cmd/migrate status
go run ./cmd/migrate up
```

Nunca execute `up` contra producao sem a autorizacao exigida por `docs/rules.md`.
