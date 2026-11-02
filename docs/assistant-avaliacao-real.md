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
