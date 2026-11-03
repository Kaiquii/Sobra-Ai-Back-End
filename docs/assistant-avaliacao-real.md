# Avaliacao semantica opt-in

O harness comum nao faz inferencias reais. Para avaliar entendimento, existe
`go test -tags=assistant_eval ./internal/assistant -run '^TestAssistantLiveEvaluation$' -count=1 -timeout=8m -v`.
Exige autorizacao para o orcamento real e estas variaveis:

- `OPENROUTER_API_KEY`: fornecida privadamente pelo ambiente, nunca Git ou APK.
- `ASSISTANT_EVAL_ENABLE=1` e `ASSISTANT_EVAL_MAX_CALLS=1..12`.
- `ASSISTANT_EVAL_CASES`: IDs separados por virgula de `testdata/live_eval.json`.
- `ASSISTANT_EVAL_MODEL`: ID gratuito no catalogo; padrao recomendado do servidor.
- `ASSISTANT_EVAL_OUTPUT`: caminho absoluto novo em `.artifacts`, fora do Git.

O runner usa `buildInterpreterRequest`, o mesmo caminho da API. Verifica planos
e campos, nao texto literal nem somente status 200. Nao conecta a banco, nao
le `.env`, nao acessa VM e nao faz escritas financeiras. A chave precisa ser
carregada externamente sob autorizacao. Chamadas POST, mesmo falhas, contam no
teto; falha do provedor interrompe a rodada sem retry. Uma nova execucao exige
controle do saldo do orcamento total autorizado (o teto e por execucao).

Artefatos registram caso, aprovacao, motivo, modelo, latencia, tokens, hashes de
prompt/schema/request e resposta sintetica. Nunca usar conversas reais neste
corpus. Os hashes permitem verificar se o contrato mudou entre rodadas. O hash
do schema registra sua definicao; JSON object nao significa que ele foi enviado
como schema nativo. Falhas ficam no relatorio e retornam codigo de saida diferente
de zero. Um mock nunca pode substituir o resultado desta avaliacao.

`ASSISTANT_EVAL_BASELINE=1` usa instrucoes congeladas de b4b33d3 e historico sem
demonstracoes. Compara o prompt no adaptador atual; nao restaura o binario antigo
nem suas politicas de formato. As rodadas iniciais de 01/11 foram capturadas antes
da alteracao do adaptador. `ASSISTANT_EVAL_FORMAT=json_object` e um controle de
experimento do teste, nao uma configuracao publica de producao.

No Windows, se o executavel for bloqueado pela politica de aplicativos, nao
desativar a politica. Compilar o teste com GOOS=linux, GOARCH=amd64 e CGO_ENABLED=0
e executa-lo em Linux local e uma alternativa. Credenciais seguem somente pelo
ambiente privado. Producao nao deve servir de laboratorio de conversas reais.

O corpus cobre os erros do video: orcamento anual de categoria, saldo apos
despesas, compra incompleta, recuperacao de compra, troca de assunto global e
cancelamento. Ampliar com variacoes independentes conforme novos comportamentos
forem suportados. A aprovacao desses casos nao e garantia universal de acerto.

## Revisao do video de 01/11 a noite

O corpus v2 tem 18 casos: os seis originais e 12 variacoes de linguagem,
correcao e continuidade. Os novos casos ainda nao foram executados com provedor
real. Nao apresentar o resultado antigo de 6/6 como aprovacao dos 18 casos.
O teste offline de estrutura do corpus nao mede entendimento do modelo.

A expectativa de saldo agora exige monthly_summary sem query sobreposta:
o executor prioriza query quando ela existe, e operation=balance nao pertence
ao contrato de financialReadQuery. O avaliador anterior aceitava essa operacao
nao executavel. O modelo deve produzir uma acao que o servidor suporta.

A qualidade tambem depende da resposta construida depois da interpretacao.
TestBudgetDeliversProposedTargetFromVerifiedHistory verifica a entrega de uma
proposta numerica de planejamento, sua base, arredondamento e ausencia de
gravacao. Esses testes validam o calculo/resposta deterministico, nao o modelo.

O video navega por respostas de 31/10 (saldo e compra) e mensagens de 01/11
(planejamento). Respostas antigas continuam no historico apos publicar codigo;
nao sao evidencia de nova execucao da ultima versao. A nova pergunta no final
fica em Pensando quando a gravacao termina, sem resposta visivel para avaliar.
Nao atribuir esses trechos automaticamente a uma regressao da ultima release.

## Qualificacao de rapidez e uso dos fatos

`TestAssistantFastEvaluation`, tambem sob a tag `assistant_eval`, usa o mesmo
`interpreterRequestWithFacts` da API. O corpus sintetico fixo fica em
`internal/assistant/testdata/fast_eval.json`. Cada execucao escolhe somente um
caso e pode enviar somente um POST. Nao usar a chave em argumento ou arquivo
versionado; ela entra privadamente no ambiente. Exige:

- `ASSISTANT_FAST_EVAL_ENABLE=1`.
- `ASSISTANT_FAST_EVAL_CASE`: um ID do corpus.
- `ASSISTANT_FAST_EVAL_LEDGER`: caminho absoluto de um ledger existente em
  `.artifacts`, criado uma unica vez para o orcamento autorizado.
- `ASSISTANT_FAST_EVAL_OUTPUT`: caminho absoluto novo de um JSON em `.artifacts`.
- `ASSISTANT_FAST_EVAL_MAX_LATENCY_MS`: limite de aprovacao de 1 a 10000ms;
  omitir usa 10000ms. Nao altera os 45s do transporte de producao.
- `ASSISTANT_FAST_EVAL_MODEL`: opcional; omitir usa o recomendado do servidor.

Comando para um caso selecionado:

```powershell
go test -tags=assistant_eval ./internal/assistant -run '^TestAssistantFastEvaluation$' -count=1 -v -timeout=2m
```

O ledger usa bloqueio exclusivo e reserva a tentativa antes do POST. Conta erros
do provedor no `authorized_total` escolhido explicitamente para aquela
autorizacao, incluindo tentativas de perfis anteriores. A ferramenta aceita um
total de um a seis e recusa uma tentativa acima do autorizado; seis e um teto
defensivo da ferramenta, nao permissao para consumir seis chamadas. Os quatro
casos do corpus sao independentes desse orcamento. Caso duplicado, qualificacao
alterada, resultado pendente ou rodada interrompida bloqueiam outra inferencia.
Falha de HTTP, latencia ou semantica marca a rodada como interrompida. Nao apagar,
zerar ou trocar o ledger para repetir sem nova decisao do usuario; uma retomada
autorizada deve preservar o historico e a contagem global ja consumida.

Corpus, avaliador, modelo e limite de latencia entram no hash de qualificacao.
O artefato registra tambem hashes de prompt, snapshot e request, tamanho do
contexto, raciocinio solicitado/reportado, tokens, tentativas, resposta sintetica,
resposta renderizada e motivo de aprovacao ou recusa. O resultado e persistido
antes de liberar a reserva como concluida. Falha de persistencia nao vira caso
aprovado sem evidencia revisavel.

Antes de selecionar o proximo caso, revisar manualmente a resposta inteira e o
resultado de Go. Verificar periodo, dinheiro versus VA/VR, uso dos dados ja
fornecidos, perguntas sobre informacoes realmente ausentes, ausencia de escrita
e resposta ao objetivo. O avaliador automatico complementa essa revisao; nem o
validador numerico nem a meta de 10s provam adequacao semantica sozinhos. Nao
alterar expectativas para aprovar uma resposta capturada que estava errada.

Em 03/11, a primeira chamada deste protocolo com Dots recebeu HTTP 200 em 2974ms,
uma completion, 2734 tokens de entrada, 90 de saida e zero de raciocinio reportados.
Foi reprovada porque pediu renda ja fornecida. A rodada foi interrompida. O
usuario autorizou retomar com as tres restantes, sem ultrapassar quatro no total
e novamente parando na primeira falha. A segunda chamada, apos corrigir a
distincao entre capacidade e meta, levou 3367ms, com 3280 tokens de entrada e 53
de saida. Foi reprovada por escolher soma com filtro na categoria inexistente
`Dividas`; a rodada parou novamente. Ambas as falhas continuam no historico.

O usuario autorizou depois as duas chamadas restantes para a pergunta do carro
e sua parafrase, parando na primeira falha. A terceira tentativa, primeira com
`nvidia/nemotron-3.5-super-vl:free` e schema nativo, retornou HTTP 404 em 193ms,
sem texto ou tokens. O ledger foi interrompido novamente. A tentativa conta no
orcamento, mas nao avaliou entendimento. A causa especifica do 404 nao foi
diagnosticada, e esse candidato nao sera publicado nesta revisao.

As continuacoes preservam a contagem global de tres tentativas ja usadas; nao
reiniciam a franquia de avaliacao. O caso equivalente de capacidade permite
`savings` no periodo atual ou analise corretamente ancorada, com as mesmas
exigencias de periodo, valores e dinheiro versus beneficios. Essa expectativa
foi ajustada antes de qualquer inferencia da parafrase, com revisao independente;
nao passou a aceitar nenhuma das duas respostas reais reprovadas.

O candidato seguinte foi `xingchen-agi/xing4.0-29b-a4b:free`. O usuario autorizou
ate seis tentativas no total, incluindo as tres ja usadas, permitindo ate tres
chamadas com Xing. A continuacao preservou os tres ledgers anteriores
interrompidos e carregou `used=3` para o teto global de seis. A regressao offline
verifica esse carregamento e a recusa de uma setima tentativa. A quarta tentativa
recebeu HTTP 429 em 1438ms, sem texto ou tokens, com erro local
`assistant_quota_exceeded`, e interrompeu a rodada. O status nao diagnostica qual
limite especifico foi atingido. Uma consulta posterior mostrou saldo diario
restante, sem excluir outros limites do provedor.

Apos nova autorizacao de retomada, a quinta tentativa recebeu HTTP 200 em 7315ms,
com 3705 tokens de entrada, 58 de saida e zero de raciocinio reportados. Acertou
`operation=savings`, mas consultou `year=2027, months=[]`, o ano do objetivo,
em vez do mes atual de novembro de 2026 do fixture. Foi reprovada por
`capacity_scope_mismatch`, e o ledger interrompeu com `used=5`. Nao alterar os
criterios ou o corpus para aceitar esse plano.

A nova gramatica local usa `current_savings_capacity` sem argumentos. O mesmo
parser usado pela API e pela avaliacao converte essa tarefa em consulta `savings`
do mes/ano confiaveis do servidor, sem filtros. Periodos financeiros explicitos
usam `period_savings`, normalizado sem alterar os periodos; `savings` legado
permanece aceito. Isso altera a forma de expressar a tarefa, nao os resultados
esperados nem o verificador semantico. Ver [contrato de capacidade](assistant-poupanca.md).

A sexta e ultima tentativa, apos a correcao do protocolo e a decisao de retomada,
recebeu HTTP 429 em 1522ms, sem texto e com zero tokens reportados. O ledger
interrompeu com `used=6`. A causa especifica do limite nao foi diagnosticada.
O artefato e `.artifacts/fast-eval-capacity-car_exact-20261103T135858Z.json`.
O procedimento continua um caso por vez, com revisao manual e interrupcao na
primeira falha; nao houve repeticao automatica depois desse erro.

Estado final: seis POSTs consumidos, tres textos gerados nas tentativas 1, 2 e 5,
todos semanticamente reprovados, e tres falhas do provedor nas tentativas 3, 4
e 6. O orcamento de seis esta encerrado. A nova gramatica foi implementada e
revisada localmente, mas seu ultimo POST nao gerou texto para verificar
entendimento. Nao houve qualificacao semantica real final.

O harness completo final com PostgreSQL local descartavel passou; a integracao
de `cmd/api` levou 26,234s. As regressoes offline opt-in passaram em 0,585s.
Esses resultados nao substituem a resposta real ausente. A publicacao foi
inicialmente retida em checkpoint na branch
`codex/assistant-efficient-grounding-20261103`. Depois de ser informado da falha
429 e da qualificacao pendente, o usuario deu nova autorizacao explicita para
publicar. A fonte `402ddc2` foi integrada a main e a API foi publicada em
03/11/2026 as 11:22:21 de Sao Paulo, conferida as 11:23:42. Vinte verificacoes
GET HTTP/HTTPS passaram, com zero reinicios e sem nova inferencia no deploy.
Schema 9, banco e ambiente foram preservados; a branch do checkpoint foi mantida.
[Identificadores, recuperacao e rollback](assistant-raciocinio.md#publicacao-autorizada-em-0311).

Publicacao autorizada nao e aprovacao semantica: continuam seis POSTs reprovados,
zero avaliacoes reais aprovadas e nenhuma resposta gerada no perfil final.
Uma futura retomada exige orcamento explicito e nova revisao semantica, sem zerar
o historico de tentativas. Esta publicacao nao autoriza chamadas reais adicionais.
Uma eventual aprovacao de casos de capacidade nao qualificara automaticamente
todo o corpus ou modelos alternativos. Cobertura offline e verificacao real
continuam identificadas separadamente.
