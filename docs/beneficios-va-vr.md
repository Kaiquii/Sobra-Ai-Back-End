# VA e VR

VA (vale-alimentação) e VR (vale-refeição) são fontes independentes, com a mesma
regra mensal de Salário e Adiantamento. Não há transferência de saldo entre meses.
O saldo pode ficar negativo, como nas fontes existentes; não se bloqueia uma despesa
por falta de crédito registrado. Valores de VA/VR participam dos totais gerais,
exatamente como as demais rendas, e também são discriminados por fonte.

## Rendas

As rotas existentes continuam iguais, autenticadas e restritas ao usuário do JWT.
Os valores canônicos de `source` são `VA` e `VR`. Também são aceitos os nomes
por extenso (com/sem acentos, espaço, hífen ou underscore). `VA/VR` não é uma fonte.

`POST /api/incomes/`:

```json
{"source":"VA","amount":600,"month":10,"year":2026,"type":"Fixa"}
```

`type=Fixa` cria 60 lançamentos mensais a partir do período informado, inclusive,
com virada de ano, como Salário/Adiantamento. `type=Única` (ou omitido) cria somente
um mês. `repeat_future` mantém sua regra legada específica de Renda Extra.

`GET /api/incomes/?month=10&year=2026` devolve os registros e seus IDs normalmente.

`PATCH /api/incomes/:id`:

```json
{"amount":650,"update_future":false}
```

`false`/omitido altera somente o registro selecionado. `true` altera o período
selecionado e todos os registros futuros da mesma fonte e usuário, incluindo outros
anos. VA nunca atualiza VR, salário ou adiantamento. Não se criam novos meses com PATCH.
O filtro usa a fonte/período original, inclusive quando `source` é editado.
Criação recorrente e atualização são transacionais: falha não deixa escrita parcial.

PATCH aceita somente `amount`, `source`, `month`, `year`, `update_future`.
Rejeita valores não positivos, fonte desconhecida, período inválido e tentativa de
alterar `user_id`/`id`. `update_future` deve ser booleano; não se pode mover `month` ou
`year` junto com `update_future=true`, evitando colocar vários meses no mesmo período.

`DELETE /api/incomes/:id` remove somente o registro; `?delete_future=true` remove
esse mês e os posteriores da mesma fonte/usuário. Despesas permanecem existentes;
o saldo é recalculado a partir dos créditos e despesas remanescentes.

## Despesas

`payment_source` aceita `VA` e `VR` em criação/edição, nos tipos Única, Fixa e
Parcelada. O contrato e as regras existentes de recorrência e adiantamento permanecem.
Pagamentos divididos também aceitam as novas fontes:

```json
{"payment_splits":[{"payment_source":"VA","amount":100},{"payment_source":"VR","amount":50}]}
```

Não enviar `payment_source` junto com `payment_splits`. As divisões devem somar o
valor da despesa e não podem repetir uma fonte, inclusive usando nomes alternativos.
Editar/excluir despesas recalcula os saldos; marcar como paga não desconta duas vezes.
Despesas adiantadas continuam usando o período efetivo, como nas fontes legadas.

## Resumo e relatórios

`GET /api/reports/summary?month=10&year=2026` mantém todos os campos anteriores e
acrescenta números (zero se não houver registros):

```json
{"va":600,"vr":400,"total_gasto_va":100,"total_gasto_vr":50,"restante_va":500,"restante_vr":350}
```

Cada restante é o crédito do mês menos as despesas daquela fonte no período efetivo.
`total_income`, `total_expense` e `total_geral_disponivel` incluem VA e VR.
Comparativos, gráficos e totais anuais incluem os benefícios; exportações CSV/XLSX
discriminam crédito, gasto e restante de cada fonte. O assistente IA reconhece VA/VR
nas consultas, resumos, filtros e cadastro com confirmação.

## Banco, validação e publicação

Não há mudança de schema nem migration nova: fontes são colunas TEXT existentes.
Não se alteram registros antigos. Clientes antigos continuam usando os contratos
legados; devem tolerar fontes adicionais quando forem cadastradas por outro cliente.

Executar `./scripts/test.ps1 -Integration` para testes unitários, build, vet, cobertura
e cenários HTTP com PostgreSQL descartável (nunca o banco remoto). Os cenários cobrem
recorrência, update/delete futuros, virada de ano, autorização, validações, rollback,
saldo mensal sem acúmulo, divisão e edição/exclusão de despesas.

Deploy: guardar imagem anterior e configuração efetiva, fazer backup do banco,
validar migrations sem aplicá-las, substituir somente a imagem da API e verificar
HTTPS/autenticação/campos do resumo. Rollback operacional restaura a imagem anterior.
Após usuários gravarem VA/VR, uma versão antiga deixa de discriminar esses saldos;
por isso um rollback duradouro exige uma versão corrigida que preserve o suporte.
Não apagar os lançamentos financeiros para reverter uma publicação.

### Publicação de 25/10/2026

- Imagem ativa: `app-financeiro-api:benefits-20261025`.
- Harness local completo aprovado (build, vet, testes e cobertura).
- Docker Desktop local apresentou falha de inicialização. A suíte de integração
  foi compilada para Linux e aprovada na VM com PostgreSQL descartável e isolado,
  removido automaticamente ao concluir; não usou o banco de produção.
- Smoke autenticado aprovado na rede interna e pelo HTTPS público, verificando os
  seis campos novos do resumo. API ativa, sem reinícios após a publicação.
- Banco com as cinco migrations existentes aplicadas; nenhuma migration executada.
- Backup em `/home/ubuntu/app-financeiro/deploy-benefits-20261025/rollback/database.dump`,
  com índice de restauração validado. Configurações anteriores e imagem preservadas.
- Rollback operacional: executar com sudo
  `/home/ubuntu/app-financeiro/deploy-benefits-20261025/rollback.sh`.
- O commit base referenciava funções de cadastro IA sem o arquivo de implementação.
  `internal/assistant/expense_create.go` foi reconstituído conforme o contrato
  documentado e validado pelos testes existentes de confirmação, parcelamento,
  concorrência, autorização e rollback, além dos novos testes VA/VR.
- Handoff enviado ao chat `Projeto` do projeto `Sobra-Ai-Android`, incluindo a
  solicitação do usuário para gerar um APK instalável após a implementação do app.
