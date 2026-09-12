# SobraAí - API

Back-end em Go do app SobraAí, responsável por autenticação, usuários, despesas, rendas, categorias, relatórios, assistente IA e foto de perfil.

## Resumo Do Projeto

O projeto roda uma API HTTP em Go usando Gin, GORM e PostgreSQL.

Fluxo principal:

1. A API conecta no PostgreSQL usando `DB_DSN`.
2. A API valida se todas as migrations já foram aplicadas, sem alterar o banco durante a inicialização.
3. Rotas públicas ficam em `/api/auth`.
4. Rotas protegidas precisam de `Authorization: Bearer TOKEN`.
5. A API pode salvar foto de perfil em storage local ou Oracle Object Storage.

Principais variáveis:

```txt
PORT=8080
DB_DSN=host=db user=postgres password=4343 dbname=app_financeiro port=5432 sslmode=disable TimeZone=America/Sao_Paulo
JWT_SECRET=sua_chave
GIN_MODE=release
```

## Harnesses De Desenvolvimento

O executor de agentes esta em `cmd/harness`: implementacao, testes, revisao e
tentativas limitadas. Veja [o guia completo](docs/harness/README.md).

```powershell
go run ./cmd/harness -mode run -task "Descreva a tarefa e os criterios de aceite"
```

O repositório possui um harness para agentes em `AGENTS.md`. Ele define o contexto obrigatório,
as regras de implementação, a estratégia de testes e o critério de conclusão. Os comandos
canônicos e o checklist de revisão ficam em `.agents/`.

Para executar o harness completo de testes no Windows:

```powershell
.\scripts\test.ps1
```

No Linux ou macOS:

```bash
sh ./scripts/test.sh
```

O fluxo valida formatação, executa `go vet`, roda toda a suíte sem cache e gera cobertura em
`.artifacts/test/`. Para uma iteração mais rápida, use `-Quick` no PowerShell ou `--quick` no
shell.

## Adiantamento De Despesas

Despesas dos tipos `Única` e `Parcelada` podem ter o impacto financeiro antecipado sem alterar o status visual de pagamento. Despesas `Fixa` não aceitam adiantamento.

`advanced_at` pode ser uma data futura em relação ao dia atual, desde que seja anterior à data prevista da despesa. O valor participa do planejamento do mês/ano de `advanced_at` mesmo antes de o dia informado chegar.

Para marcar uma despesa:

```http
PATCH /api/expenses/:id/advance-status
Authorization: Bearer TOKEN
Content-Type: application/json

{
  "is_advanced": true,
  "advanced_at": "2026-08-19"
}
```

Para remover, envie `{"is_advanced": false}`. A remoção não altera `is_paid` nem `paid_at`.

A consulta padrão de despesas continua usando o mês previsto, preservando o histórico:

```http
GET /api/expenses?month=9&year=2026
```

Para listar as despesas que afetam financeiramente um mês, incluindo lançamentos futuros adiantados para ele, use:

```http
GET /api/expenses?month=8&year=2026&period_mode=effective
```

Relatórios, gráficos, comparações, exportações e o assistente usam automaticamente o período financeiro efetivo.

## Como Subir Na VM Pela Primeira Vez

Na sua máquina local, gere a imagem:

```bash
docker build -t app-financeiro-api .
docker save -o app-financeiro-backend.tar app-financeiro-api
```

Envie para a VM:

```txt
app-financeiro-backend.tar
docker-compose.yml
```

Na VM:

```bash
cd /home/ubuntu/app-financeiro
sudo docker load -i app-financeiro-backend.tar
sudo mkdir -p /var/app-financeiro/uploads
sudo docker compose up -d db
sudo docker compose run --rm --no-deps api ./migrate up
sudo docker compose up -d api
```

Conferir se subiu:

```bash
sudo docker ps
sudo docker logs -f app_financeiro_api
```

Na primeira instalação, o banco precisa estar em execução antes de `migrate up`. A API só deve ser iniciada depois que o comando terminar com sucesso.

## Como Atualizar Somente A API Na VM

Use quando o banco já existe e você quer trocar apenas o back-end.

Na máquina local, gere um novo `.tar`:

```bash
docker build -t app-financeiro-api .
docker save -o app-financeiro-backend.tar app-financeiro-api
```

Envie o novo `app-financeiro-backend.tar` para a VM.

Na VM:

```bash
cd /home/ubuntu/app-financeiro
sudo docker load -i app-financeiro-backend.tar
sudo docker compose up -d --no-deps --force-recreate api
```

Esse comando recria somente o container da API. A inicialização apenas consulta o estado das migrations e não executa DDL nem altera o schema. Se a nova imagem exigir uma migration ainda pendente, a API recusará a inicialização e informará que `./migrate up` precisa ser executado.

Conferir logs:

```bash
sudo docker logs -f app_financeiro_api
```

Importante: não use `docker compose down -v`, porque o `-v` remove volumes e pode apagar os dados do banco.

## Como Atualizar A API Com Migration Na VM

Use este fluxo quando a nova versão do back-end incluir uma migration de banco.

Antes de aplicar a migration, faça e valide um backup do banco de produção. Depois de enviar a nova imagem para a VM, execute:

```bash
cd /home/ubuntu/app-financeiro
sudo docker load -i app-financeiro-backend.tar
sudo docker compose run --rm --no-deps api ./migrate status
sudo docker compose run --rm --no-deps api ./migrate up
sudo docker compose up -d --no-deps --force-recreate api
```

O `docker compose run` usa a imagem e a configuração atuais do serviço em um container temporário, sem substituir a API que já está rodando. A API só é recriada depois que `migrate up` conclui com sucesso.

Confira o estado e os logs:

```bash
sudo docker compose run --rm --no-deps api ./migrate status
sudo docker logs -f app_financeiro_api
```

O comando `./migrate up` é o único fluxo normal autorizado a criar ou alterar o schema. Não edite migrations que já tenham sido aplicadas; crie sempre uma nova versão.
