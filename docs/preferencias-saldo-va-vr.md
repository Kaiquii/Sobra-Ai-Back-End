# Preferências de VA e VR no disponível mensal

Publicado em 04/11/2026 e verificado às 14:47 de São Paulo, fonte `ad3d2d8`,
schema 10. Ambas as opções continuam desativadas por padrão. Ver
[evidências e rollback compatível](publicacao-preferencias-saldo-va-vr.md).

Cada usuário possui duas opções independentes, ambas `false` por padrão:

- `exclude_va_from_available`: excluir o restante de VA do disponível geral.
- `exclude_vr_from_available`: excluir o restante de VR do disponível geral.

A escolha é persistida no backend e vale para todos os meses, anteriores e
futuros. Não altera créditos, despesas, fontes, pagamentos ou recorrências.
Sem preferência cadastrada, a leitura retorna `false`/`false` sem criar linha.
Clientes existentes mantêm o comportamento atual até que o próprio usuário
ative uma opção. A interface deve consultar o usuário sobre a localização dos
controles antes da implementação: [handoff web e Android](handoffs/preferencias-saldo-va-vr.md).

## API autenticada

`GET /api/users/balance-preferences` retorna diretamente:

```json
{
  "exclude_va_from_available": false,
  "exclude_vr_from_available": false
}
```

`PATCH /api/users/balance-preferences` aceita um ou ambos os booleanos:

```json
{"exclude_va_from_available": true}
```

Omitir um campo preserva seu valor. `false` explícito desativa a opção.
`{}` é uma consulta sem alteração. O PATCH retorna o estado completo, com os
dois booleanos. Não enviar mês, ano, IDs, usuário ou os valores financeiros.
O usuário é determinado pelo JWT; nenhuma opção permite alterar outro usuário.
Atualizações parciais concorrentes em opções diferentes não apagam uma à outra.

O corpo deve ser um único objeto JSON de até 4096 bytes. Campos desconhecidos,
duplicados, `null`, strings, números, listas e JSON adicional são rejeitados
com HTTP 400 e `code=invalid_balance_preferences`, sem escrita parcial.
Autenticação é obrigatória; falhas de banco retornam 500 e não são tratadas
como uma preferência desativada. As rotas de perfil existentes não mudam.

## Cálculo e escopo

`GET /api/reports/summary?month=11&year=2026` mantém os campos anteriores e
acrescenta os dois booleanos aplicados. Somente `total_geral_disponivel` depende
dessas opções:

```text
base = total_income - total_expense
restante_va = va - total_gasto_va
restante_vr = vr - total_gasto_vr
disponível = base
se excluir VA: disponível -= restante_va
se excluir VR: disponível -= restante_vr
```

Com alguma exclusão ativa, o resultado é arredondado a centavos. Saldos
negativos conservam seu sinal; não zerar o restante antes de excluí-lo.
Uma despesa dividida usa a parcela efetiva de cada fonte. O período efetivo
dos adiantamentos e a regra de pagamento existente continuam preservados.

Exemplo: salário R$ 1.000 com gasto R$ 200; VA R$ 725 com gasto R$ 600;
VR R$ 300 com gasto R$ 100. Os restantes são R$ 800, R$ 125 e R$ 200:

| Excluir VA | Excluir VR | Disponível geral |
| --- | --- | ---: |
| false | false | R$ 1.125,00 |
| true | false | R$ 1.000,00 |
| false | true | R$ 925,00 |
| true | true | R$ 800,00 |

Se houver apenas VA R$ 725 e despesa R$ 600 paga no VA, excluir VA deixa o
disponível geral em R$ 0 e o restante do VA em R$ 125. Desativar a opção volta
a incluir esses R$ 125. Não subtrair novamente os R$ 725 de crédito.

`total_income`, `total_expense`, valores e restantes de todas as fontes,
percentuais calculados a partir dos totais, categorias, gráficos, comparativos,
totais anuais e exportações CSV/XLSX/PDF permanecem integrais. O assistente não
recebe alteração de prompt ou novas chamadas; planejamento em dinheiro mantém
a separação já existente entre dinheiro e benefícios. Esta preferência afeta
o disponível do resumo mensal, e não transforma todos os relatórios em caixa.

## Persistência e compatibilidade

Migration 10: `add_user_balance_preferences`, checksum
`8f310d0c1fbad8507564d121cd70b17598a66757e2a75866432d31d0af7daa25`.
Cria somente `user_balance_preferences`: `user_id` como PK/FK com exclusão em
cascata, dois booleanos `NOT NULL DEFAULT false` e `updated_at`. Não reescreve
lançamentos nem altera migrations anteriores. A API apenas valida o schema;
`migrate up` pertence à publicação autorizada.

A API anterior conhece somente schema 9 e não pode ser reiniciada diretamente
com schema 10. O rollback deve usar uma compilação compatível da fonte anterior
com apenas a migration 10 e seu registro incorporados. Essa imagem preserva
a tabela e as escolhas, retorna ao comportamento anterior e dispensa restaurar
o banco ou remover registros de migrations. Ambas as imagens devem ser
verificadas no backup restaurado antes de publicar.

## Validação

Testes unitários cobrem a fórmula, as quatro combinações, arredondamento,
restantes negativos, parsing, autenticação e contrato de resposta. A integração
local com PostgreSQL descartável cobre upgrade 9→10, idempotência, defaults,
concorrência do primeiro upsert, isolamento, persistência entre meses,
edição/exclusão de despesas, pagamento, adiantamento e divisão de fontes.
Também compara todos os campos financeiros não afetados e a exportação CSV
antes/depois de mudar preferências. Não usa banco remoto nem inferência real.

`scripts/test.ps1 -Integration` passou com formatação, vet, build, testes
unitários e toda a integração local. Cobertura unitária geral 50,7%; integração
45,9% dos pacotes internos/API. As simulações da OpenRouter foram atualizadas
para cobrir descoberta ZDR e a troca na mesma mensagem já existente; nenhuma
política ou implementação de IA de produção foi alterada nesta tarefa.
