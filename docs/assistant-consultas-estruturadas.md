# Consultas estruturadas e continuidade do assistente

Evolucao de 29/10/2026, sem nova migration. Corrige a limitacao
dos campos singulares month/year para perguntas de varios periodos e separa a
interpretacao do pedido dos calculos, filtros e ordenacao dos resultados.
Publicacao e recuperacao: [registro da release](publicacao-consultas-ia.md).

## Contrato interno do modelo

A completion continua unica e devolve JSON com `intent` obrigatoria. A nova
intencao interna `financial_query` usa `query`, sem precisar escrever valores ou
um relatorio em `reply`. O objeto fechado aceita:

- `periods`: ate dois anos, com `year` e `months` obrigatorios em cada periodo.
  `months: []` pede o ano inteiro; omissao/null pede esclarecimento, sem ampliar
  silenciosamente a consulta. Os periodos sao deduplicados e ordenados.
- `category_name`: nome exato de uma categoria do usuario; vazio/ausente inclui
  todas. Campo desconhecido, categoria alheia ou fonte invalida nao removem filtro.
- `payment_source`: opcional, respeitando as fontes e splits cadastrados.
- `group_by`: `month`, `category`, `expense`, `installment`, `purchase` ou
  `clarify_installment_or_purchase` para pedir a distincao entre parcela e compra.
- `operation`: `list`, `compare`, `max`, `min`, `sum` ou `average`.
- `limit`: de 1 a 24; comparacao mensal preserva todos os meses pedidos.

O contrato HTTP permanece igual. O resultado usa `tool_call=financial_analysis`,
ja existente, com `status=read_only`, query normalizada, linhas, totais e cobertura.
Planos invalidos/ambiguos retornam `needs_clarification` como conversa normal,
sem nova inferencia ou escrita. `query:null` nao equivale a ausencia: esclarece,
sem cair no resumo mensal. Intencoes simples legadas continuam aceitas; conselhos
personalizados usam `financial_analysis` sem query e os fatos autenticados atuais.

## Exatidao e continuidade

Leituras exigem o usuario do JWT, timeout de cinco segundos e snapshot somente
leitura. Cada ano permite ate 10.000 despesas; excesso falha sem totais parciais.
O calculo usa centavos, periodo efetivo dos adiantamentos e partes da fonte
selecionada. Rendas e beneficios nao sao confundidos com pagamentos de despesas.

Comparacoes apresentam os meses pedidos, inclusive ausencia de registros. Rankings
nao tratam mes sem cadastro como prova de menor gasto real. Totais completos sao
calculados antes do limite visual; esse limite e informado. Dados futuros sao
descritos como cadastrados, sem afirmar pagamento ou recebimento.

`installment` compara parcelas. `purchase` agrupa parcelas das compras parceladas
com lancamentos no periodo, pelo `series_id`, em todos os anos. Sem identificacao
de serie, pede esclarecimento em vez de juntar compras por descricao. A soma e das
parcelas registradas; nao garante o preco original quando houve exclusoes.

O plano validado fica no resultado persistido. No prompt seguinte ele substitui
relatorios antigos por metadados compactos; nao altera o historico armazenado nem
transforma uma consulta executada em prova de que o pedido foi entendido. O texto
original do usuario prevalece, inclusive quando ele rejeita a resposta anterior.
Conselhos conversacionais sem plano nao sao apagados pela compactacao.

Categoria residual nao muda uma intencao de todas as categorias para uma unica.
`provided_fields`, quando presente, limita de verdade o patch de rascunho, incluindo
campos nao vazios; corrigir categoria nao pode alterar valor/data/descricao por
defaults ou campos extras do modelo. A confirmacao continua local e transacional.
Valores com Markdown em general/app_help tambem passam pela verificacao financeira.

Se uma recomendacao `financial_analysis` nao passar pela verificacao numerica,
o texto do modelo e descartado. Havendo fatos anuais completos e consistentes,
o Go apresenta uma visao dos totais e das maiores categorias registradas, com
escopo e limitacoes explicitos. Nao infere desperdicio, capacidade de corte ou
dinheiro disponivel. O resultado usa `status=read_only`,
`reply_source=verified_summary` e `analysis_status=analysis_not_verified` para
distinguir essa resposta da recomendacao livre aprovada. Sem fatos suficientes,
mantem o esclarecimento. Essa alternativa nao se aplica a `general`/`app_help`.

## Economia e limites de validacao

Permanecem uma completion por mensagem, zero na confirmacao e teto de saida de
2048 tokens quando suportado, sem tentativa extra para reparar JSON/analise.
O usuario autorizou elevar o teto de saida de 1024 para 2048 nesta publicacao;
os limites de chamadas/franquia permanecem iguais. O novo trabalho
de consulta e feito no Go; instrucoes foram compactadas em vez de ampliar contexto.
Um teste fixa o prompt sem fatos dinamicos em ate 5.669 bytes, orcamento anterior.
Isso nao promete numero identico de tokens para toda pergunta/historico possivel.

Na interpretacao, `reasoning: {"enabled": false}` e enviado somente quando o
catalogo publico anuncia suporte a `reasoning` e `reasoning.mandatory` e
explicitamente `false`. Metadados ausentes/null, raciocinio obrigatorio e outros
estagios nao recebem esse controle. Nao ha aumento do teto, tentativa extra ou
remocao de modelos. `exclude: true` apenas esconderia o raciocinio e nao resolveria
seu consumo do teto de saida; veja a
[documentacao oficial](https://openrouter.ai/docs/guides/best-practices/reasoning-tokens).

Diagnosticos de respostas recusadas usam somente motivos fixos, sem registrar
corpo, modelo retornado livre, intencao desconhecida ou erro bruto do provedor.
O codigo publico `assistant_invalid_response` e preservado, com mensagem mais
clara; falha de formato nao e apresentada como prova de falta de dados pessoais.

Testes locais usam HTTP simulado e PostgreSQL descartavel, com dados sinteticos.
Eles comprovam execucao, isolamento e limites; nao comprovam que todo modelo
entendera qualquer frase. Avaliacao real deve ter orcamento explicito e registrar
separadamente falha de transporte/formato e erro de interpretacao. O servidor
verifica o plano recebido, mas nao prova equivalencia perfeita com linguagem
natural nem a correção de toda recomendacao livre.

A avaliacao real desta alteracao usou dez completions autorizadas, somente com
fatos sinteticos. Tres consultas passaram: dois meses de Uber, maior mes anual
e retomada do pedido apos correcao do usuario. O conselho anual inicialmente
esgotou 1024 tokens sem texto; com reasoning desativado, terminou normalmente
em 360 tokens, com zero tokens de reasoning informado pelo provedor. A resposta
foi corretamente recusada por percentuais sem calculos verificaveis. O resumo
local e coberto por regressao e replay offline dessa resposta, sem nova chamada.
A decima chamada validou o prompt final com teto 2048: terminou em 351 tokens,
sem raciocinio, mas o texto livre ainda continha percentuais e valores derivados
sem calculos verificaveis. A resposta entregue usou corretamente o resumo local
verificado. O fluxo protegido passou; a recomendacao livre do modelo nao foi
aprovada. Isso nao equivale a aprovar recomendacoes livres de todos os modelos.

As duplicacoes de rascunho foram consolidadas: preparacao/parcelamento puros em
`expense_draft.go`, chamados por `expense_create.go`, com todos os testes incluidos
no harness executado na pasta original. A confirmacao permanece local e transacional.
