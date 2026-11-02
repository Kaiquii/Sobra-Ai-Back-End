# Analise com fatos antes da resposta

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
