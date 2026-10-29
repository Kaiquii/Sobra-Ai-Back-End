# Consultas e orientacao com escopo preservado

Evolucao publicada em 29/10/2026 as 14:02 de Sao Paulo apos os testes do segundo
video. Evidencias e rollback em [publicacao das intencoes](publicacao-intencoes-ia.md).
A publicacao anterior permanece em [registro anterior](publicacao-consultas-ia.md).
Nao altera schema nem contratos HTTP do aplicativo.

## Interpretacao e dados

A unica completion interpreta o pedido; filtros, calculos e orientacoes pessoais
sao executados no Go. O modelo recebe data, nomes de categorias do usuario,
historico compacto e rascunho ativo. O snapshot financeiro anual deixou de ser
enviado antes da interpretacao. Mensagens escritas pelo usuario ainda compoem o
historico; isso nao significa ausencia de dados pessoais no provedor.

Endpoints que anunciam `structured_outputs` recebem JSON Schema estrito com
root `action` e cinco formatos exclusivos: consulta, cadastro, conversa,
resumo mensal e consultas simples/cancelamento. O `anyOf` fica dentro de action,
com objetos fechados e propriedades obrigatorias/nullable por formato. Consulta
nao exige campos ficticios de cadastro e cadastro nao pode conter query. Esse
envelope e interno; o adaptador o remove antes do parser legado. As categorias sao
limitadas ao catalogo autenticado. Nil significa catalogo desconhecido nos testes;
uma lista vazia conhecida permite somente filtro vazio. Essa metadata interna
nao e serializada como campo publico do pedido. O wrapper do schema entra no
orcamento de contexto antes do HTTP.

O adaptador preserva respostas antigas sem envelope e remove campos de
cadastro/reply de `financial_query` mesmo
quando o modelo os preenche. Remove `position` fora de max/min e controles
opcionais nulos. Campos obrigatorios de periodo/query e mascaras de criacao nao
sao apagados para transformar resposta invalida em autorizacao ou escopo maior.
Cadastro acompanhado de query e consultas explicitas iniciadas por qual/quanto/
quais interpretadas como cadastro sao recusados antes de aceitar a resposta do
provedor. O motivo e fixo nos logs, sem expor a mensagem. Modelos sem schema
continuam com JSON object/texto e validacao local. Schema
correto nao comprova equivalencia perfeita com linguagem natural.

## Plano financeiro

`financial_query` usa `query`, sem valores calculados pelo modelo:

- `periods`: ate dois anos, cada um com `year` e `months` obrigatorios. `[]`
  seleciona o ano inteiro; ausencia/null nao amplia silenciosamente o periodo.
- `category_name` e `payment_source`: filtros opcionais, com vazio sem filtro.
  Nome exato tem prioridade; abreviacao so resolve se identificar uma categoria
  unica do dono. Correspondencias ambiguas pedem esclarecimento.
- `group_by`: month/category/expense/installment/purchase ou
  clarify_installment_or_purchase.
- `operation`: list/compare/max/min/sum/average/review/budget.
- `limit`: 1..24. `position`: ordinal 1..24 somente max/min, devolvendo um item;
  segundo maior nao equivale a listar os dois maiores. A posicao fixa limit=1.

Comparacoes mensais incluem todos os meses pedidos. Soma mensal mantem o total
completo e tambem apresenta detalhamento dos meses; apos seis, informa o limite
visual. Ausencia de registros nao e prova de gasto real zero. Rankings nao usam
mes ausente como minimo real. Valores usam centavos, periodo efetivo das despesas
adiantadas e partes da fonte escolhida. Leituras sao do JWT, em snapshot somente
leitura, com timeout de cinco segundos e limites por ano de 10.000 despesas e,
para orientacoes, 5.000 rendas. Excesso falha sem totais parciais.

Parcelas mensais e compras inteiras sao distintas. Compras agrupam somente
series identificadas, somando as parcelas registradas em todos os anos; exclusoes
anteriores podem fazer a soma diferir do preco original. Nao se inferem series
pela descricao.

## Orientacao contextual

`review` com group_by=category responde onde as despesas registradas pesam:
ate cinco categorias e participacoes calculadas no Go, com denominador completo.
Compara despesas/rendas em dinheiro quando ha renda em dinheiro, separando VA/VR.
Nao interpreta valor alto como desperdicio nem uma diferenca positiva como folga
segura. Lacunas de cadastro, periodo, futuro e limite visual ficam explicitos.

`budget` com group_by=month exige categoria. Mostra primeiro media/maior valor
historico, ou a insuficiencia de dados. Usa ao menos dois meses completos com
registros dessa categoria; mes atual, futuro e ausencia nao completam a amostra.
A referencia considera todo o escopo solicitado; apenas seis meses registrados
recentes aparecem no texto, mantendo todos no JSON. Nao amplia anos em silencio,
inventa teto seguro ou cadastra limite. Um pedido de Uber nao vira resumo de
outras categorias. O protocolo nao aceita ainda metas monetarias hipoteticas.

O resultado HTTP continua `tool_call=financial_analysis`, `status=read_only`,
com query normalizada, metricas e `reply_source=verified_guidance` nas orientacoes.
O intent livre legado `financial_analysis` continua sendo lido para compatibilidade;
seus valores sao verificados apos a completion, sem outra chamada. Uma analise
recusada nao e mais substituida por um resumo anual generico. A alternativa antiga
`verified_summary` foi removida da execucao, mas historicos ja salvos continuam
compativeis na compactacao.

## Continuidade e confirmacao

O plano validado substitui numeros antigos no contexto seguinte; mensagens
originais do usuario prevalecem quando ele rejeita a resposta. Perguntas de
continuidade conservam categoria, periodo e ranking; mudanca de assunto descarta
filtros. A resolucao legada de calendario tambem conserva referencias relativas
como ano passado em um acompanhamento.

Rascunho ativo e confirmacao local exigem o ultimo turno do assistente ser uma
proposta de despesa. Sim/ok depois de ajuda ou relatorio continua a conversa,
sem ressuscitar cadastro antigo. Confirmacao real segue local, transacional e
idempotente; `provided_fields` restringe correcoes inclusive nao vazias.

## Economia e verificacao

Permanecem uma completion por mensagem, zero na confirmacao real, teto de saida
2048 nos modelos compativeis e somente precos zero/privacidade deny. O raciocinio
interno e desativado na interpretacao somente quando o catalogo declara suporte
e nao obrigatoriedade. Nenhuma tentativa adicional repara o mesmo pedido.

Prompt fixo + wrapper nativo com catalogo vazio cabem nos 5.669 bytes anteriores.
Catalogo/historico continuam variaveis; nao se promete percentual fixo de economia.
Logs usam motivos fechados, contadores e IDs publicos, sem corpo, credenciais ou
valores financeiros retornados pelo provedor.

Harness completo local com HTTP simulado e PostgreSQL descartavel aprovado:
formatacao, vet, build, regressao de escopo, isolamento, orientacao e confirmacao.
Esses testes provam a execucao do plano; a selecao pela linguagem natural precisa
de avaliacao real separada, com orcamento explicito e dados sinteticos.

Nesta revisao, as quatro chamadas adicionais autorizadas levaram o total a 14:

- O formato nativo achatado acertou os dois meses, mas misturou alias/ordinal
  inaplicavel e tentou escrever um valor sem dados. A captura foi reprocessada
  offline pelo contrato corrigido, resolvendo a categoria e detalhando cada mes,
  sem outra chamada nem valor inventado.
- A pergunta de segundo maior revelou consulta misturada com cadastro. Isso
  motivou os formatos de acao exclusivos e os bloqueios antes da execucao.
- A comparacao com Gemma 4 31B retornou HTTP 429, sem resposta de qualidade;
  nao houve troca de modelo recomendado com base nesse resultado.
- O modelo atual, com action.anyOf, interpretou corretamente o segundo maior,
  preservando periodo, ausencia de filtros e uma unica linha no ranking. Essa
  completion terminou em 59 tokens, sem prometer economia igual em toda pergunta.

As duas chamadas seguintes, autorizadas separadamente, passaram com o mesmo
modelo e formato final: orcamento de Uber/99 preservou o ano e calculou media
historica de R$ 80,00 e maior mes encerrado de R$ 100,00; maior mes anual
selecionou novembro com R$ 200,00 registrados, incluindo o aviso sobre futuro.
Cada pedido usou uma completion. Os valores sao sinteticos. O total acumulado
foi de 16 tentativas autorizadas, incluindo as falhas descritas acima.

O formato final foi avaliado com tres pedidos reais aprovados (segundo maior,
orcamento e maior mes anual). A pergunta de dois meses teve replay offline da
captura anterior; nao se afirma que recebeu uma nova completion no formato final.
Isso valida estes casos e preserva o limite economico, sem garantir que qualquer
modelo gratuito entenda toda forma de perguntar. A revisao foi publicada com
backup restaurado isoladamente e vinte verificacoes GET aprovadas, sem novas
completions no deploy.
