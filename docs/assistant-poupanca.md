# Capacidade mensal de poupar

O protocolo financial_query agora aceita operation=savings com group_by=month,
sem filtros de categoria ou fonte. A consulta usa o mesmo snapshot autenticado
e transacional das orientacoes, com limites de leitura e isolamento por usuario.

## Gramatica local em revisao em 03/11

A interpretacao passa a distinguir a tarefa de capacidade atual de uma consulta
com periodo financeiro explicito. Para a capacidade atual, o modelo seleciona
uma acao sem argumentos:

```json
{"action":{"intent":"current_savings_capacity"}}
```

O parser compartilhado da API e da avaliacao converte essa acao em
`financial_query`, `operation=savings`, `group_by=month`, sem categoria/fonte e
com mes/ano definidos pelo `now` confiavel do servidor. Datas, filtros, valores
ou texto extras do modelo nao determinam essa consulta. Isso impede que o ano
de uma compra futura vire o periodo dessa tarefa. O modelo continua escolhendo
a tarefa semantica; nao ha roteamento por palavras-chave da mensagem.

Quando o usuario pede explicitamente um periodo financeiro, o contrato do
provedor usa `financial_query` com `operation=period_savings` e os periodos
pedidos. O Go normaliza essa operacao para `savings`, preservando o escopo em vez
de substituir os meses pelo atual. O formato legado `operation=savings` continua
aceito com seu escopo original. Esta e uma gramatica interna de interpretacao,
sem novo endpoint, migration ou escrita financeira.

A mudanca foi implementada e revisada localmente. O harness completo com
integracao e as regressoes offline passaram. A quinta tentativa anterior foi
recusada por usar o ano do objetivo; esse resultado continua falha e nao foi
aprovado por alterar o corpus ou o criterio de avaliacao. A sexta e ultima
tentativa, ja com a nova gramatica, recebeu HTTP 429 em 1522ms, sem texto para
avaliar entendimento. O teto de seis POSTs foi consumido. Portanto a qualificacao
semantica real permanece pendente e esta revisao nao foi publicada; main e
producao permanecem inalteradas. Testes do parser nao provam a escolha correta
da tarefa pelo modelo real.

## Calculo e significado do resultado

O calculo por mes e renda em dinheiro menos todas as despesas em dinheiro.
Splits de pagamento e mes efetivo de despesas adiantadas sao respeitados.
VA/VR nao financiam objetivos em dinheiro. Mes sem renda cadastrada fica
indeterminado; saldo negativo nao produz uma sugestao positiva. Nao ha escrita,
migration, transferencia ou transporte automatico de saldo entre meses.

Perguntar quanto PODE guardar/separar depois das obrigacoes consulta a capacidade,
inclusive ao mencionar uma compra futura ou corrigir uma resposta anterior de
budget. Perguntar quanto PRECISA guardar para atingir um preco/prazo e outra
tarefa, de planejamento da meta. Preco e prazo do objetivo nao sao necessarios
para consultar a capacidade atual. O ano da compra nao deve ser confundido com
o periodo financeiro; sem periodo explicito para os registros, consulta-se o mes
atual. O resultado informa capacidade pelos cadastros, sem garantir que despesas
essenciais estejam completas ou que a renda tenha sido recebida.

O valor necessario para atingir uma meta exige preco, valor ja reservado e prazo.
Esta etapa calcula a capacidade mensal cadastrada; ainda nao implementa um
simulador de metas com essas entradas. O corpus inclui variacoes de capacidade
e planejamento. Seus resultados reais devem ser registrados separadamente dos
testes do calculo e da resposta deterministica.

Uma completion por mensagem permanece. O protocolo de qualificacao e seus limites
estao em [avaliacao real](assistant-avaliacao-real.md).
