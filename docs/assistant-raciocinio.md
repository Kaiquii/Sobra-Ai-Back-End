# Analise com fatos antes da resposta

Este documento preserva o historico da investigacao de 02/11. A politica de
contexto e raciocinio de 03/11 e sua publicacao estao nas secoes finais; os
resultados historicos nao qualificam automaticamente essa nova politica.

## Base e historico de 02/11

A interpretacao de pedidos abertos estava limitada a escolher uma consulta e
receber uma resposta deterministica. financial_analysis ja existia no executor,
mas nao no schema nativo, e seus fatos eram carregados somente depois da resposta.
Isso impedia o modelo de analisar os dados na sua unica completion.

O fluxo agora carrega um snapshot autenticado antes da geracao. Mantem limites
de dois anos, 1000 categorias, 10000 despesas/ano e 5000 rendas/ano, com transacao
de leitura. O JSON tem teto de 48000 bytes: exceder falha explicitamente, sem
truncar totais. Quando ha somente um ano diferente do atual, inclui tambem o atual
para permitir referencias ao presente durante planejamento e comparacoes.

financial_analysis passa a ser uma acao estruturada disponivel. Recebe os mesmos
fatos usados na validacao posterior, sem nova leitura depois da completion.
Calculations declara operacoes e caminhos numericos: o servidor recalcula e
recusa valores monetarios sem procedencia. Essa validacao prova procedencia
numerica, nao adequacao semantica de toda conclusao. Datas e categorias continuam
dados, nunca instrucoes. Confirmacao e gravacao financeiras permanecem locais.

Consultas exatas continuam pelo executor; pedidos abertos podem analisar o
snapshot sem criar uma operacao nova para cada frase. Perguntas de planejamento
nao devem ser forcadas a media de categoria. Correcoes do usuario substituem a
interpretacao rejeitada. Quando faltam dados, a analise pede as entradas faltantes.

O limite estatico de prompt/schema passa de 9000 para 11000 bytes. O snapshot
aumenta tokens por mensagem e carga de leitura; nao alegar reducao de consumo.
Continua uma completion por mensagem, sem chamadas reais no harness.

Validacao local cobre snapshot antes da geracao, preservacao da correcao atual,
schema analitico, contas verificadas e rejeicao de fabricacoes/contexto excedido.
Testes simulados nao qualificam entendimento do modelo real. A nova politica
de contexto e schema exige qualificacao real antes de recomendar publicacao.
Sob autorizacao posterior, a chave foi lida em memoria na VM para testes locais
com dados ficticios. Nenhuma publicacao ou commit foi feito nesta alteracao.

## Qualificacao real de 02/11

O modelo dots-3-note-preview recebeu seis perguntas abertas com o mesmo snapshot
ficticio. Apenas duas respostas passaram pelo avaliador atual. Houve erro real:
uma sobra de 700 foi descrita como deficit de 600; outra resposta calculou 35%
com caminhos de operandos que nao produziam essa porcentagem. Ambas foram
recusadas. Tambem houve recusas por numeros de enumeracao e uma classificacao
general em uma explicacao conceitual, portanto 2/6 nao equivale sozinho a uma
medicao isolada da inteligencia do modelo. As respostas precisam de revisao
semantica alem da checagem numerica.

Comparacao no catalogo gratuito: nemotron-3-ultra retornou HTTP 404 na primeira
completion e gemma-4-31b-it HTTP 429 na primeira. Ambas as rodadas foram
interrompidas, sem retries. Total: oito tentativas POST nesta qualificacao.
Nao atribuir falhas de disponibilidade a falhas de entendimento nem trocar
automaticamente para um modelo nao qualificado.

Esse resultado inicial nao foi aprovado para publicacao. A producao anterior
permaneceu ativa enquanto a investigacao continuou.

Na continuacao foram executados mais cinco casos com o mesmo modelo gratuito,
apos reforcar uso dos fatos atuais, sinal do saldo e perguntas apenas sobre
entradas ausentes. Duas respostas passaram imediatamente. A revisao das demais
detectou falsos negativos concretos: R$ 2.000 era interpretado como dois reais,
e marcadores de lista eram interpretados como numeros financeiros inventados.
O validador agora entende agrupamento brasileiro de milhares sem centavos e
marcadores de listas; continua verificando todos os valores monetarios antes
de remover marcadores. Testes cobrem tambem valores inventados nas listas.

As cinco respostas dessa rodada e a resposta de correcao da primeira rodada
foram capturadas com dados ficticios. Seu replay pelo validador final passou
6/6. Isso NAO representa uma nova rodada ao vivo de seis casos nem aprovacao de
todas as perguntas possiveis. A revisao semantica verificou uso dos saldos
fornecidos, objetivos pendentes e abandono da media de categoria; tambem mostrou
que o modelo pode usar linguagem mais forte que a evidencia de cadastros permite.
As instrucoes explicitam essa limitacao, sem garantir obediencia universal.

Total da investigacao: 13 tentativas POST, 23525 tokens reportados. Nenhum credito
comprado, chave trocada ou retry para contornar limite de provedor. As comparacoes
indisponiveis nao qualificaram outro modelo; recomendado permanece o mesmo.
Os artefatos completos ficam em .artifacts/eval-reasoning*-20261102.json e as
capturas sinteticas em testdata/reasoning_captured_20261102.json.

## Revisao final antes de publicar

A revisao manual recusou aprovar open_capacity: embora sem numeros inventados,
o modelo pediu renda e despesas ja disponiveis. Portanto o replay numerico 6/6
nao e aprovacao semantica. A publicacao permaneceu retida, mesmo apos o harness
completo e a integracao HTTP/PostgreSQL isolada passarem. Os testes de integracao
agora exigem fatos autenticados antes da geracao; preservam isolamento de outros
usuarios, notas privadas e historico forjado.

Uma tentativa adicional com inkling gratuito retornou 403 e foi interrompida.
Foi encontrado o controle de raciocinio desabilitado do antigo caminho de
interpretacao: pedidos com snapshot agora solicitam enabled=true, mantendo uma
completion e teto de saida. A chamada exploratoria resultante nao qualificou o
fluxo: referiu 2027, ausente dos metadados do snapshot ficticio, e pediu detalhes
do objetivo sem explicar a sobra. Ha lacuna na avaliacao de datas futuras e na
utilidade da resposta. Uma tentativa final com Xing gratuito retornou 429 e
foi interrompida. Total final: 16 tentativas, 25916 tokens reportados nos retornos
com usage. Nao apresentar essas rodadas como sucesso nem comprar creditos.

Estado de entrega: checkpoint em Git solicitado pelo usuario, sem deploy desta
revisao; producao anterior preservada. A qualificacao semantica continua pendente.

## Retomada

- Corrigir o avaliador para exigir utilidade semantica: perguntar renda ja
  fornecida deve falhar mesmo sem numeros inventados. Nao afrouxar expectativas
  para aprovar o modelo. Replay numerico nao qualifica raciocinio.
- Resolver datas do objetivo na avaliacao sem confundir capacidade financeira
  atual com ano futuro. O snapshot ficticio e congelado, o real inclui o ano
  atual quando solicitado somente outro ano.
- Revisar a politica de raciocinio habilitado e o custo do snapshot antes de
  publicar. A alteracao final teve suite do assistente aprovada em Linux local;
  harness completo e integracao isolada passaram antes desse ultimo controle.
- Comparar modelos somente com corpus e criterios fixos, controle total de
  chamadas e revisao das respostas. Nao repetir modelos com limite ativo nem
  comprar creditos. Consultar os artefatos ignorados antes de novas inferencias.
- Rodar harness e integracao finais, qualificar a resposta inteira e so entao
  preparar nova publicacao com backup e rollback. O diretorio remoto
  deploy-reasoning-20261102 contem artefatos de testes, nao uma release ativa.

## Revisao de eficiencia e uso dos fatos em 03/11

O snapshot bruto continua limitado a 48000 bytes. Antes da unica completion,
uma projecao de no maximo 16000 bytes preserva os totais mensais, a separacao
entre dinheiro e beneficios e os indices dos caminhos usados nas contas. Nao
altera os dados originais. Mostra um prefixo de ate oito categorias por ano;
se necessario, reduz esse prefixo para respeitar o teto e marca
`categories_complete=false`, mantendo `represented_category_count`. Uma consulta
exata ou exaustiva continua pelo executor `financial_query`, sem tratar esse
prefixo como todas as categorias.

`current_month` resume somente o fluxo em dinheiro do mes atual cadastrado.
`latest_registered_month` e uma referencia datada, nunca um mes futuro nem uma
promessa de capacidade atual. As flags `cash_income_registered` e
`expenses_registered` distinguem ausencia de cadastro de valor zero registrado;
receber apenas VA/VR nao significa ter renda em dinheiro registrada. Metadados
de calendario autorizam datas, nunca operandos monetarios.

O prompt distingue a capacidade de poupar do valor necessario para uma meta.
Perguntar quanto pode separar depois das obrigacoes consulta a capacidade, mesmo
mencionando uma compra futura. Na gramatica local final implementada,
`current_savings_capacity` nao recebe argumentos: o parser compartilhado usa a
data confiavel do servidor para montar `financial_query/savings` do mes atual,
sem filtros. Periodos financeiros explicitamente pedidos usam `period_savings`,
normalizado para `savings` sem alterar o escopo; consultas legadas `savings`
continuam aceitas. O modelo escolhe a tarefa, sem uma lista de frases no Go.
Detalhes: [capacidade mensal](assistant-poupanca.md).
Perguntar quanto precisa para atingir um objetivo ou explicar
hipoteses usa `financial_analysis`; preco e prazo ausentes podem ser pedidos,
mas renda e despesas ja disponiveis devem ser usadas. Demonstracoes de protocolo
ensinam essa distincao sem reconhecer uma lista fixa de frases do usuario.

Nas analises, `{{fact:/current_month/total_cash_balance}}` referencia um fato
monetario autenticado. Para uma conta, o modelo declara `calculations` com
operacao e caminhos e escreve `{{calc:0}}` para o primeiro resultado. O Go calcula
e formata `R$` ou `%`; nao se deve repetir simbolos no texto. Por exemplo:

```json
{
  "intent": "financial_analysis",
  "reply": "As despesas representam {{calc:0}} da renda em dinheiro cadastrada.",
  "calculations": [
    {
      "operation": "percentage",
      "paths": [
        "/current_month/total_cash_expense",
        "/current_month/total_cash_income"
      ]
    }
  ]
}
```

`fact` aceita dinheiro; percentual exige um resultado `percentage` de `calc`.
Caminho inexistente, referencia malformada, operando sem procedencia ou divisao
por zero sao recusados. A resposta renderizada ainda passa pelo validador
numerico. Datas, IDs e nomes nao autorizam dinheiro ou percentuais; percentuais
exigem procedencia percentual explicita. Respostas legadas continuam sujeitas
ao mesmo validador, sem dispensar verificacao por nao usarem referencias.

O raciocinio opcional e solicitado como desabilitado, respeitando as capacidades
do modelo. Permanecem uma completion por mensagem, saida de ate 2048 tokens,
orcamento de transporte/geracao de 45s, preco zero e politica de privacidade.
Nao ha segunda chamada para reformular ou reparar uma resposta. O teto de 45s
protege a requisicao; nao e uma meta de rapidez nem garantia de latencia.

No fixture de compactacao, o JSON passou de 13893 para 8285 bytes, preservando os
totais testados. Isso mede esse contexto ficticio, nao uma reducao universal de
tokens, tempo ou quota. A qualidade semantica precisa de avaliacao real separada:
procedencia numerica, status 200 e testes locais nao provam entendimento.

A primeira chamada sintetica desta revisao, com Dots, levou 2974ms, com 2734
tokens de entrada, 90 de saida e zero tokens de raciocinio reportados. Ela falhou:
pediu a renda ja fornecida. A rodada foi interrompida, e a captura ganhou
regressao offline para continuar reprovando esse comportamento. Apos corrigir a
distincao entre capacidade e meta e obter autorizacao para retomar, a segunda
chamada levou 3367ms, com 3280 tokens de entrada e 53 de saida. Tambem falhou:
escolheu soma na categoria `Dividas`, inexistente no catalogo ficticio, em vez
da capacidade mensal em dinheiro. A rodada foi interrompida novamente. Resposta
rapida e numericamente bem formada nao compensam intencao ou filtro errados.

A revisao seguinte tentou `nvidia/nemotron-3.5-super-vl:free`, com schema nativo.
A terceira tentativa da investigacao retornou HTTP 404 em 193ms, sem texto ou
tokens, e interrompeu a rodada. Ela conta como POST consumido, mas nao mede
entendimento do modelo. A causa especifica do 404 nao foi diagnosticada; nao
atribuir a falha a privacidade, capacidade ou formato apenas por esse status.
Esse candidato nao sera publicado nesta revisao.

O candidato preferido local passa a `xingchen-agi/xing4.0-29b-a4b:free`. Leituras
de catalogo e endpoints indicam schema nativo, limite de saida e raciocinio
opcional; o endpoint Phala aparece tambem na listagem de ZDR. Essas leituras
nao qualificam a geracao nem garantem disponibilidade da completion com a
politica de privacidade do projeto.

A quarta tentativa, primeira com Xing, retornou HTTP 429 em 1438ms, sem texto
ou tokens, e interrompeu a rodada. O erro local foi `assistant_quota_exceeded`;
isso nao identifica sozinho a causa especifica do limite. Uma consulta posterior
informou quatro de cinquenta requisicoes diarias usadas e quarenta e seis
restantes. Esse saldo nao exclui limites de taxa, de endpoint ou outros controles
do provedor, nem qualifica entendimento.

Apos autorizacao de retomada, a quinta tentativa com Xing recebeu HTTP 200 em
7315ms, com 3705 tokens de entrada e 58 de saida, total 3763, sem raciocinio
reportado. Escolheu `savings`, mas pediu todo o ano de 2027, o ano da compra, em
vez de novembro de 2026, periodo financeiro atual do fixture. Foi reprovada por
`capacity_scope_mismatch`; o ledger parou com cinco tentativas consumidas. O
avaliador e o corpus nao foram afrouxados para aceitar essa resposta.

A acao sem argumentos descrita acima reduz essa ambiguidade na gramatica de
interpretacao. O resultado anterior continua falha; testes do parser nao provam
que o modelo real selecionara a nova acao. Depois de implementar e revisar esse
protocolo, a sexta e ultima tentativa com Xing recebeu HTTP 429 em 1522ms, sem
texto e com zero tokens reportados. O ledger foi interrompido com `used=6`.
Essa ultima tentativa nao gerou resposta para avaliar semanticamente; a nova
gramatica permanece sem qualificacao real. A causa especifica do 429 nao foi
diagnosticada.

A excecao historica de JSON object de Dots fica vinculada ao seu ID literal,
sem acompanhar a constante de modelo preferido. Dots e as demais opcoes
gratuitas continuam disponiveis. A prioridade das alternativas considera schema
nativo com limite de saida e, entre essas, possibilidade de desabilitar
raciocinio opcional. Isso nao exige schema nativo em todos os fallbacks nem
qualifica automaticamente a qualidade semantica de cada modelo.

Foram usados seis POSTs nesta investigacao. As tentativas 1, 2 e 5 geraram texto,
e os tres textos falharam semanticamente. As tentativas 3, 4 e 6 falharam no
provedor, sem texto. O teto global autorizado de seis foi consumido e encerrado;
nao ha tentativa restante nesta autorizacao. Os ledgers e capturas ficam
preservados. A ferramenta exige o total explicitamente autorizado no ledger,
entre um e seis; seis e seu teto defensivo, nunca autorizacao por si so.

O harness final `scripts/test.ps1 -Integration` passou, com formatacao, vet,
build, cobertura e PostgreSQL local descartavel. A integracao de `cmd/api` levou
26,234s; cobertura geral 49,8%, assistente 67,0% e integracao 43,4%. As regressoes
offline com a tag `assistant_eval` passaram em 0,585s. O log final esta em
`.artifacts/assistant-efficient-validation-capacity-final-20261103.log`; a ultima
resposta do provedor esta em
`.artifacts/fast-eval-capacity-car_exact-20261103T135858Z.json`.

Esses testes verificam os comportamentos locais cobertos, sem comprovar o
entendimento da nova gramatica pelo modelo. O trabalho foi inicialmente retido
em checkpoint na branch `codex/assistant-efficient-grounding-20261103`, pois a
verificacao real nao passou. Depois de ser informado da falha 429 e dessa
pendencia, o usuario autorizou explicitamente publicar mesmo assim. Essa nova
decisao substitui a retencao operacional; nao muda nenhum resultado de teste.
A pendencia continua sendo obter uma resposta real e revisar seu entendimento,
com novo orcamento explicito antes de qualquer inferencia adicional. Nao ha
aprovacao de qualidade universal nem SLA.
Ver [operacao da avaliacao](assistant-avaliacao-real.md).

## Publicacao autorizada em 03/11

A API iniciou em 03/11/2026 as 11:22:21 de Sao Paulo e foi conferida as 11:23:42.
A fonte foi integrada a main; a branch do checkpoint foi preservada. A publicacao
e a auditoria passaram com vinte verificacoes GET HTTP/HTTPS e zero reinicios.
Nao houve POST de inferencia durante preparacao, ativacao ou verificacao.

| Identificador | Valor |
| --- | --- |
| Fonte da API | `402ddc2904f7fee7165305524bf2b6bb981dc79f` |
| Imagem ativa | `app-financeiro-api:assistant-efficient-20261103` |
| Digest da imagem | `sha256:128ad69cc2440ada0013550e45a449690e0a448f60747e9bce8388b7f872bcae` |
| SHA-256 do binario | `a16607409b8b76da375b1db02220ebe5683284f9e61bc1faac727b2c62b06829` |
| SHA-256 do pacote enviado | `f664569e80f3959b6029011477fd2f6f34cd9507c5e4f3e3bdd2a84864ea9d88` |
| Schema | `9`, sem `migrate up` ou alteracao de schema |

Container do banco, configuracoes, ambiente, rede e volumes foram preservados.
O backup foi restaurado em clone isolado e verificado com ambas as imagens;
os fingerprints financeiros permaneceram iguais. Essa verificacao de recuperacao
e disponibilidade nao executou uma conversa real nem qualificou o modelo.

O rollback da API usa o snapshot privado e a imagem anterior, sem restaurar o
backup sobre novas escritas de producao. Procedimento preparado na VM:

```sh
sudo sh /home/ubuntu/app-financeiro/deploy-assistant-efficient-20261103/rollback.sh
```

Os metadados sanitizados estao em
`.artifacts/assistant-efficient-publication-20261103.json`. A decisao de publicar
com a limitacao conhecida tem SHA-256
`17b09c964049e8dd19e1f342db570eb59169d2057b7736c8e522eafc49a9e2ae`.
O registro mantem `passed_real_evaluations=0`, `real_validation_pending=true`,
seis POSTs historicos e zero inferencias no deploy. A autorizacao de publicacao
nao reabre o orcamento de inferencias nem transforma as seis falhas em aprovacao.
