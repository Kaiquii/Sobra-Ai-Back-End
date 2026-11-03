# Assistente IA

**Filtro de provedores em validacao local (03/11/2026):** a revisao seguinte
cruza `/models` com `/endpoints/zdr`, pelo ID exato da variante, para mostrar e
usar apenas modelos com endpoint ativo, preco integralmente zero, politica ZDR
e `safety_retention=false` explicito. Metadados ausentes ou indisponiveis impedem
a selecao. O teste real recebeu HTTP 200, mas falhou no escopo semantico da
pergunta de poupanca. O filtro foi versionado por pedido posterior do usuario;
a publicacao permanece pendente, sem deploy desta revisao.

**Atualizacao de 03/11/2026, 17:12 de Sao Paulo:** fallback gratuito limitado
publicado no commit `adabbda`. Uma falha recuperavel pode tentar outro modelo na
mesma mensagem, com no maximo duas tentativas HTTP. A politica anterior de uma
tentativa descrita nos registros historicos abaixo foi substituida. Detalhes e
recuperacao na secao "Fallback limitado apos falha do provedor".

**Publicada em 03/11/2026, as 11:22 de Sao Paulo, com qualificacao semantica real pendente:** o modelo recomendado
passa a `xingchen-agi/xing4.0-29b-a4b:free`, com schema nativo quando anunciado no
catalogo. Dots permanece disponivel, com sua excecao de JSON object vinculada
ao ID literal desse modelo, independente de qual seja o recomendado. Contexto
compactado, referencias financeiras verificadas e raciocinio opcional desabilitado
mantem uma completion e o teto de 2048 tokens. As duas primeiras chamadas reais,
com Dots, falharam semanticamente. A terceira, com Nvidia, retornou HTTP 404 em
193ms, sem texto ou tokens, e a rodada foi interrompida. Essa falha de provedor
nao qualificou o entendimento e sua causa especifica nao foi diagnosticada.
Com Xing, a quarta tentativa retornou HTTP 429 em 1438ms; a quinta respondeu em
7315ms, mas escolheu o ano da compra em vez do periodo financeiro atual. A sexta
e ultima tentativa recebeu HTTP 429 em 1522ms, sem texto ou tokens reportados.
Foram seis POSTs: tres respostas geradas, todas reprovadas semanticamente, e tres
falhas do provedor. O orcamento de seis foi encerrado. A revisao publicada
introduz `current_savings_capacity` sem argumentos, convertido pelo parser para
o mes atual confiavel; periodos explicitos usam `period_savings`, preservados no
Go, e `savings` legado continua aceito. A fonte final foi implementada e revisada;
o harness completo com integracao e as regressoes opt-in offline passaram.
A ultima chamada nao gerou resposta, portanto a nova gramatica continua sem
validacao semantica real. Apos ser informado dessa limitacao, o usuario deu nova
autorizacao explicita para publicar. A API da fonte `402ddc2`, integrada a main,
foi publicada na imagem `app-financeiro-api:assistant-efficient-20261103` e
conferida as 11:23:42: vinte verificacoes GET HTTP/HTTPS passaram, sem reinicios,
com schema 9 e banco/ambiente preservados. Nao houve inferencia nova no deploy.
A branch do checkpoint foi mantida. Essa autorizacao e a disponibilidade da API
nao aprovam o entendimento do modelo nem oferecem SLA.
[Politica e evidencias](assistant-raciocinio.md#revisao-de-eficiencia-e-uso-dos-fatos-em-0311),
[avaliacao real](assistant-avaliacao-real.md),
[publicacao e rollback](assistant-raciocinio.md#publicacao-autorizada-em-0311).

**Correcao publicada em 03/11/2026, as 09:11:** leitura HTTP interrompida por tempo podia
aparecer como resposta invalida. A correcao alinha a espera HTTP ao orcamento de
45s e distingue timeout, cancelamento e JSON invalido, mantendo uma completion e
2048 tokens. [Diagnostico e limites](assistant-tempo-resposta.md),
[publicacao e rollback](publicacao-timeout-ia.md). O teste real da pergunta de
planejamento ficou para o usuario, sem inferencias no deploy.

**Atualizacao em producao em 01/11/2026:** interpretacao qualificada com modelo
real nos seis cenarios de saldo, orcamento, cadastro e continuidade do video.
O modelo recomendado daquela publicacao, Dots, usa JSON object com validacao no Go;
schema nativo nao foi aprovado para esse modelo. [Evidencias e rollback](publicacao-qualificacao-ia.md),
[metodo de avaliacao](assistant-avaliacao-real.md).

**Roteamento automatico do servidor:** o app deixa de escolher modelos. POST sem
`model_id` ou com string vazia sempre usa roteamento automatico do servidor,
ignorando preferencias manuais antigas sem apaga-las. A escolha legada explicita
continua compativel, com validacao gratuita e os mesmos limites de uso.

**Estado em 28/10/2026:** estilo de respostas, roteamento automatico, OpenRouter e
melhorias de contexto publicados em producao, com catalogo completo e dinamico de
modelos gratuitos. Schema permanece 9. Validacoes e recuperacao atuais:
[publicacao-estilo-respostas-ia.md](publicacao-estilo-respostas-ia.md).
Registros anteriores: [roteamento automatico](publicacao-roteamento-automatico-ia.md),
[catalogo completo](publicacao-catalogo-completo-ia.md) e
[publicacao inicial](publicacao-openrouter.md).

**Historico de 28/10/2026, as 20:29 de Sao Paulo (substituido pelo fallback limitado abaixo):** uma mensagem de
chat permite no maximo uma chamada ao OpenRouter, com teto de 2048 tokens de
saida na interpretacao quando suportado. Consultas financeiras sao formatadas
localmente e a rotacao apos falha vale apenas para a mensagem seguinte.
Validacao e rollback: [publicacao-economia-ia.md](publicacao-economia-ia.md).

**Analises e continuidade de rascunhos:** planos `financial_query` permitem comparar
meses, consultar rankings e elaborar orientacoes. O servidor consulta e calcula
depois da unica inferencia, sem aceitar valores financeiros calculados pelo modelo.
Detalhes, cobertura e publicacao: [publicacao-analises-ia.md](publicacao-analises-ia.md).

**Evolucao de consultas em 29/10/2026:** a mesma
interpretacao pode fornecer um plano de leitura com periodos, filtros e ranking.
O Go executa e formata a consulta, sem confiar em valores ou conclusoes de ranking
escritos pelo modelo. Escopo, compatibilidade e economia:
[assistant-consultas-estruturadas.md](assistant-consultas-estruturadas.md).

## Regra principal

O assistente nao deve depender de padroes fixos de texto para entender perguntas do usuario.

Exemplos do que evitar:

- Se a mensagem contem "quanto gastei", fazer resumo mensal direto.
- Se a mensagem contem "categoria", tentar cortar a frase manualmente.
- Criar uma lista crescente de frases esperadas para cada pergunta.

Esse caminho deixa o assistente engessado e contradiz a proposta do produto.

## Como deve funcionar

O OpenRouter e o unico provedor do assistente. Groq e Gemini nao sao chamados.
A chave `OPENROUTER_API_KEY` fica exclusivamente no ambiente do backend, nunca no
APK, Git, resposta HTTP, manifesto ou logs.

O fluxo correto e:

1. O usuario envia uma mensagem natural.
2. O back-end envia a mensagem, ate 20 mensagens recentes da mesma conversa e o
   rascunho ao modelo gratuito selecionado no OpenRouter. Historico recebido no
   corpo do cliente nao e usado como autoridade; somente registros do servidor
   vinculados ao usuario autenticado sao incluidos, com limite de 24000 bytes.
3. O modelo devolve uma intencao estruturada, por exemplo:
   - resumo mensal
   - gastos por categoria
   - cadastro de despesa
   - duvida sobre o app
   - conversa comum
   - consulta de patrimonio, FGTS ou investimentos
4. O Go executa a acao com seguranca usando o usuario autenticado pelo JWT.
5. Consultas simples continuam formatadas em Go. Analises usam o `reply` da mesma
   inferencia, com fatos atuais e verificacao numerica no servidor. Conversa comum
   e ajuda tambem usam essa unica inferencia. Cadastros e confirmacoes usam textos
   exatos do Go.

Confirmar um cadastro preparado nao usa chamada ao modelo. Interpretar cadastro,
conversa comum, ajuda e consulta financeira usam uma geracao, com uma tentativa
normal e no maximo uma tentativa adicional em outro modelo se o provedor falhar.
Nao ha nova completion para reescrever ou corrigir uma resposta gerada invalida.
O orcamento de uma etapa e de
45 segundos e o chat inteiro tem limite de 55 segundos, menor que os timeouts
padrao do proxy e do app.

Quando um modelo estiver limitado, indisponivel ou devolver JSON invalido, o
backend preserva conversa e dados; a proxima mensagem pode usar outro modelo. Cooldowns
por modelo/conta respeitam Retry-After e o reset informado pelo OpenRouter.
Cota global da conta (ou credito indisponivel) interrompe as tentativas: trocar
modelo nao renova a cota. Retornar `error_code=assistant_quota_exceeded` com
`retry_after_seconds`; nao disfarcar uma falha como resposta concluida.

As alternativas preservam o contrato JSON e nunca podem ser modelos pagos.

## Responsabilidades

### OpenRouter e modelos gratuitos

- Entender a intencao da mensagem.
- Extrair entidades como mes, ano, categoria, valor, fonte de pagamento e descricao.
- Gerar respostas finais apenas quando for conversa comum, ajuda sobre o app ou quando uma resposta deterministica do Go nao for suficiente.

- Selecionar alternativas somente dentro do catalogo aprovado pelo servidor.
- Impor `provider.max_price` zero, `data_collection=deny`, sem plugins pagos.

### Go

- Autenticar o usuario.
- Buscar dados no banco.
- Criar, atualizar ou deletar dados somente com confirmacao quando necessario.
- Garantir que o usuario so acesse os proprios dados.
- Salvar conversas e mensagens.

## Cadastro de despesas e campos pendentes

O cadastro pelo chat e um rascunho persistido na conversa. O servidor valida os
campos independentemente do prompt e devolve `tool_call=create_expense` com:

- `status=needs_information` e `missing_fields` quando faltam dados ou ha valores
  invalidos. A resposta pergunta quais campos precisam ser informados/corrigidos;
  nao grava despesas nem cria categorias.
- `status=needs_confirmation` quando os dados estao completos. A resposta apresenta
  descricao, categoria, fonte, tipo, primeira data e valores antes de pedir confirmacao.
- `status=created` somente depois de confirmar e concluir a transacao.
- `status=cancelled` quando o usuario rejeita/cancela o rascunho.

Campos obrigatorios: descricao nao branca, categoria existente escolhida pelo usuario,
fonte valida (`Salario`, `Adiantamento`, `Renda Extra`, `VA` ou `VR`), valor positivo em centavos,
data completa valida e tipo (`Unica`, `Parcelada` ou `Fixa`). Parceladas exigem de 2 a
120 parcelas. O servidor nao assume dia 1, data atual, categoria, descricao ou fonte.
Datas relativas explicitamente informadas podem ser interpretadas pela IA.
Uma categoria desconhecida ou de outro usuario exige escolher uma categoria propria;
o assistente nao cria categorias automaticamente.

Respostas seguintes complementam o rascunho da mesma `conversation_id`. O modelo
retorna somente os campos alterados e o servidor preserva os demais. Uma nova despesa
independente usa `new_expense=true` na intencao para nao herdar os dados anteriores.
O `confirm` produzido pelo modelo nunca autoriza uma escrita. A confirmacao aceita
apenas afirmacoes isoladas, como `sim` ou `pode cadastrar`; uma frase com negacao ou
correcao nao confirma automaticamente. Confirmar um rascunho incompleto apenas repete
os campos pendentes. Correcoes em uma proposta completa exigem nova confirmacao.

Em despesas parceladas, `amount` no rascunho e o **total da compra**. R$ 600 em 3 vezes
gera tres despesas de R$ 200. O Go distribui centavos restantes nas primeiras parcelas:
R$ 100 em 3 vezes gera R$ 33,34, R$ 33,33 e R$ 33,33. Datas mensais preservam o dia
informado ou o ultimo dia do mes quando necessario (31/jan -> 28/fev -> 31/mar).
Cada parcela tem sua divisao de pagamento persistida e a soma permanece igual ao total.
Para fixa, o valor informado e mensal e sao gerados 120 lancamentos, conforme o cadastro
manual. O cadastro manual continua recebendo o valor **por parcela**; seu contrato de
`amount` nao mudou.

Propostas pendentes antigas exigem informar o valor novamente antes de confirmar,
porque o significado de `amount` em parcelas mudou. Despesas ja gravadas nao sao
alteradas. A confirmacao, as parcelas, as divisoes e a resposta da conversa sao gravadas
em uma transacao; uma falha reverte tudo e repetir a confirmacao nao duplica a compra.

As rotas manuais de despesas tambem rejeitam descricao vazia e categoria ausente,
inexistente ou de outro usuario nos novos cadastros. O PATCH valida esses campos quando
enviados; atualizacoes de outros campos e a leitura de registros legados continuam
permitidas. `GET /api/expenses`, inclusive `period_mode=effective`, preserva os nomes e
tipos JSON de `description` (string) e `category_id` (numero), sem reparar registros antigos.

## Regra para novas features

Antes de adicionar qualquer condicao baseada em frase, perguntar:

"Isso pode ser interpretado pelo modelo em vez de virar mais um padrao no Go?"

Se a resposta for sim, usar o modelo para interpretar e manter o Go como executor seguro.

## Estilo e apresentacao das mensagens

Apresentacao publicada em 28/10/2026: os prompts compartilham um estilo de portugues
brasileiro natural, resposta principal primeiro, paragrafos
curtos com espaco entre blocos, negrito pontual e listas quando ajudam. Emojis sao
opcionais e moderados, de zero a dois; avisos e limites mantem um tom sereno.
Sem tabelas, HTML, blocos de codigo ou encerramentos genericos repetidos.

O contrato continua igual: `reply` e `content` sao strings. Na interpretacao, o
objeto JSON externo permanece puro; Markdown simples e quebras escapadas podem
aparecer apenas na string `reply`. Nao ha nova API, migration ou chamada extra de
IA para decorar uma resposta. Confirmacoes e escritas financeiras continuam no Go.

O app deve renderizar o subconjunto visual nas mensagens do assistente, incluindo
as antigas: negrito, paragrafos e listas. Mensagens do usuario permanecem literais.
Guardar o conteudo original no estado/historico e preservar selecao, acessibilidade
e os valores exatos. Nao executar HTML, buscar imagens nem abrir links por conta
propria. Exibir os asteriscos literalmente nao implementa o negrito visual.

A validacao financeira considera uma copia sem marcadores de formatacao para
impedir que `R$ **2026**` transforme um ano permitido em valor monetario inventado.
Essa normalizacao nao altera o texto retornado ou persistido. Consultas financeiras
preferem marcadores a listas numeradas, evitando introduzir numeros sem origem nos
dados autenticados.

## Roteamento automatico e compatibilidade do catalogo

Todas as rotas abaixo exigem JWT e respeitam o bloqueio de acesso.

- GET `/api/assistant/models`: retorna `models` com `id`, `name`, `description` e
  `recommended`; `selected_model` (vazio para Recomendado), `recommended_model`,
  `effective_model_id` e `automatic_fallback=true`.
- GET `/api/assistant/model-preference`: retorna `model_id`, `effective_model_id`
  e `available`. Uma preferencia removida do catalogo fica `available=false` e
  usa o recomendado sem reescrever silenciosamente a preferencia armazenada.
- PUT `/api/assistant/model-preference`: corpo `{"model_id":"id-retornado"}`;
  `{"model_id":""}` restaura Recomendado. Retorna o mesmo formato do GET.
- POST `/api/assistant/chat` preserva os campos existentes. O campo opcional
  `model_id` explicito continua sendo uma sugestao prioritaria legada para aquela
  mensagem, validada no catalogo gratuito e sujeita a fallback. Ausente ou vazio
  significa SEMPRE automatico: nao le nem depende da preferencia salva. Modelo
  pago/desconhecido enviado explicitamente continua retornando 400.

Somente o dono autenticado pode ler/gravar sua preferencia. Migration
`000009_add_assistant_model_preferences` cria a tabela com FK e exclusao em cascata.
A API valida migrations; executar `migrate up` explicitamente na publicacao.
As rotas GET/PUT de preferencia e a tabela permanecem para clientes antigos;
valores e datas ja salvos nao sao apagados nem regravados pelo chat. A nova regra
substitui intencionalmente a antiga semantica de usar a preferencia quando o campo
era omitido, pois o servidor nao pode distinguir dois clientes que omitem o mesmo
campo. Clientes antigos continuam recebendo os mesmos formatos e erros; apenas
um `model_id` explicito influencia a prioridade da mensagem. O novo app nao precisa
consultar catalogo/preferencia nem realizar PUT de reset ao entrar.

O catalogo lista automaticamente TODOS os modelos de conversa gratuitos retornados
pelo OpenRouter, sem shortlist ou limite de quantidade. `OPENROUTER_MODELS` nao
restringe mais a lista. Na publicacao de 03/11, o preferido passa a
`xingchen-agi/xing4.0-29b-a4b:free`, quando presente e elegivel; sua qualificacao
esta pendente no registro acima. Os demais candidatos priorizam suporte a schema
nativo com limite de saida; entre esses, raciocinio opcional que possa ser
desabilitado. Depois, a ordenacao usa nome e ID. Se o preferido estiver ausente,
o primeiro elegivel recebe a marca de recomendado. Essa prioridade nao exclui
outras opcoes gratuitas nem obriga todos os fallbacks a terem schema nativo.
Recomendado e um atalho, nao um modelo adicional. Modelos novos gratuitos aparecem
na proxima atualizacao e os que passam a cobrar deixam de ser oferecidos.

A excecao de JSON object de Dots e independente da politica de recomendacao.
Outros modelos com `structured_outputs` recebem o schema nativo; modelos com
`response_format` podem usar JSON object, e os demais continuam sujeitos ao
protocolo JSON no prompt e a validacao de Go. Suporte de formato nao garante
entendimento, disponibilidade ou qualidade de todos os candidatos.

Os modelos devem aceitar texto e gerar texto pelo endpoint de conversa. Servicos
de embeddings, reranking e geracao de audio/imagem nao sao opcoes de chat.
Nao se exige suporte nativo a JSON para aparecer: o adaptador envia somente os
parametros suportados, pede JSON no prompt e valida a resposta antes de executar
qualquer acao. Todos os campos de precos, inclusive adicionais e overrides, devem
ser zero; um sufixo `:free` sozinho nao prova gratuidade. Roteadores pagos ou
opacos sao excluidos; `openrouter/free` pode aparecer como roteador exclusivamente
gratuito. Cache de cinco minutos com espera cancelavel e cache curto de falhas;
se nao for possivel revalidar apos expirar, bloquear em vez de arriscar cobranca.

O tamanho da lista nao limita os candidatos do servidor. Cada POST de chat tem um
orcamento compartilhado de no maximo UMA chamada de completions. O orcamento e
verificado na fronteira HTTP do provedor, inclusive se houver mais de um cliente
ou etapa na mesma mensagem. Resposta invalida, truncada, timeout ou erro nao
autorizam outra tentativa dentro desse POST. Modelos em cooldown podem ser
descartados antes de enviar a chamada; uma falha recuperavel pode usar o segundo
modelo na mesma mensagem, dentro do teto total de duas tentativas.
No modo automatico, o servidor reutiliza por ate cinco minutos o ultimo modelo
gratuito que respondeu corretamente para a conta. Erros descartam essa prioridade;
um modelo retirado do catalogo nao volta a ser utilizado por esse estado. Depois
vem o recomendado e as demais alternativas elegiveis. Uma sugestao explicita de
cliente antigo mantem prioridade sem aumentar o orcamento de duas tentativas.
Disponibilidade gratuita pode variar por modelo, capacidade ou politica
de dados; o catalogo nao garante capacidade instantanea. A politica `deny` continua
ativa; nao liberar uso para treinamento automaticamente para fazer uma opcao funcionar.

O novo app remove o seletor e envia somente mensagem e `conversation_id`,
preservando historico e compositor. Nao reenviar POST automaticamente para trocar
modelo. O servidor pode escolher outra opcao na proxima mensagem, respeitando cooldown.
Nao fixar IDs nem colocar chaves no app. Tratar `assistant_quota_exceeded`,
`assistant_user_limit` e demais erros `assistant_*` com `retry_after_seconds`,
preservando leitura de respostas antigas `gemini_quota_exceeded`.

O OpenRouter nao documenta um contador de cota restante por modelo em cada resposta
de sucesso. O roteamento nao inventa previsao: usa cooldown, `Retry-After` e reset
reais quando disponiveis, e rotacao entre mensagens em erro/indisponibilidade. Cota global
da conta e limite por usuario interrompem tentativas em vez de tentar contorna-los.
Referencia: [limites oficiais do OpenRouter](https://openrouter.ai/docs/api_reference/limits).
Nenhuma rotacao apaga contadores, repete mensagens ou confirma cadastros por conta propria.

## Configuracao e publicacao

### Economia de cota

A interpretacao solicita somente o JSON util, sem analise intermediaria, repeticao
do historico ou `reply` redundante nas consultas/cadastros. Nos modelos que aceitam
limite de saida, `assistant_intent` usa no maximo 2048 tokens, respeitando tambem
o contexto menor do modelo. Esse teto nao e uma promessa de economia percentual:
entrada/historico tambem consomem tokens. Resposta truncada falha sem tentar outra
inferencia na mesma mensagem. Conversa comum/ajuda ainda usam o reply dessa chamada.

O harness PowerShell/shell substitui a chave herdada por uma credencial sintetica,
para nao usar acidentalmente a conta real. As suites usam transportes locais/mock e
PostgreSQL descartavel. No PowerShell, a variavel original e restaurada ao finalizar.
Para validar uma publicacao, executar as suites com OpenRouter simulado e usar
smokes de disponibilidade/autenticacao/leitura que nao gerem inferencia real.
Nao enviar baterias de POSTs reais de chat em cada release. Um eventual teste de
qualidade externo exige autorizacao e um orcamento de chamadas declarado antes;
se autorizado, usar no maximo uma mensagem e considerar seu consumo na cota da conta.

### Ambiente

Definir `OPENROUTER_API_KEY` em ambiente privado. As antigas variaveis
`GROQ_API_KEY`, `GROQ_MODEL`, `GEMINI_API_KEY` e `GEMINI_MODEL` nao sao utilizadas.
Cada tentativa efetiva de geracao e limitada por usuario: por padrao 6 por minuto
e 50 por dia UTC. Configuracao privada: `OPENROUTER_USER_RPM` (1 a 60) e
`OPENROUTER_USER_DAILY_LIMIT` (1 a 1000). O contador concorrente e mantido por
processo; reiniciar a API o reinicia. Multiplas replicas exigem contador compartilhado.
Esse limite retorna `assistant_user_limit` com `retry_after_seconds`; confirmacoes
locais, consulta de catalogo e preferencias nao consomem chamadas ao modelo.
Nao configurar fallback pago nem plugins. A cota gratuita da conta e compartilhada
por todos os usuarios e modelos; ela nao aumenta ao selecionar outra opcao.

Esta evolucao de roteamento nao exige nova migration nem altera a configuracao de
provedores; parte do schema 9 ja publicado. Uma publicacao futura requer autorizacao
especifica, harness completo com integracao e verificacao das imagens/recuperacao,
preservando banco, preferencias, ambiente, volumes e proxy. O upgrade 8->9 e a
substituicao de variaveis de provedor pertencem a publicacao inicial historica.

O binario antigo conhece apenas schema 8 e recusa schema 9. O rollback operacional
precisa do binario anterior com o registro imutavel da migration 9, preservando
tabela e preferencias, junto do ambiente anterior privado. Nunca apagar registros
de migration nem restaurar backup sobre novas escritas para reverter so a API.

Os helpers puros de preparacao e parcelamento ficam em `expense_draft.go`,
reutilizados pela criacao/confirmacao em `expense_create.go`, sem funcoes
duplicadas. Ambos e seus testes fazem parte do harness e dos binarios. As
alteracoes locais preexistentes do Compose e configuracoes privadas nao entram
no commit nem no pacote de publicacao.

## Fallback limitado apos falha do provedor — 03/11/2026

A politica atual substitui o limite anterior de uma tentativa HTTP por mensagem:
o fluxo normal continua com uma tentativa; se o provedor/modelo estiver indisponivel
(429 especifico do modelo, 404, 5xx ou falha de rede sem timeout), pode tentar uma
segunda IA do catalogo gratuito. O teto total e de **duas tentativas HTTP por
mensagem**, compartilhado entre clientes e etapas. Uma resposta aceita encerra a
geracao; nao existe segunda chamada para reescrever o resultado. JSON invalido,
resposta truncada ou rejeitada semanticamente nao ganham outra completion no chat.

Cada tentativa conta nos limites por usuario (padrao 6/minuto e 50/dia), inclusive
a falha. Assim, 50 tentativas nao garantem 50 mensagens respondidas quando ha
fallback. Permanecem o teto de 2048 tokens por tentativa, o prazo total de 45s,
o catalogo de preco zero e a politica de privacidade. Nao ha retry ilimitado,
chamada paralela nem consulta de quota por mensagem.

Um 429 de modelo bloqueia esse modelo por pelo menos 15 minutos; outras falhas
recuperaveis por pelo menos 60 segundos, respeitando Retry-After maior. A proxima
tentativa pula os modelos em pausa, e o modo recomendado reutiliza por ate cinco
minutos o substituto que respondeu. Preferencias gravadas do usuario nao mudam.
Estado de saude e cooldown vivem na memoria da API e reiniciam com o processo.

Limite global identificado da OpenRouter, falta de credito, credencial invalida,
limite do usuario, cancelamento e timeout encerram a geracao sem tentar contornar
o bloqueio. Cabecalhos de rate limit com provider_name explicito nao bastam para
classificar o erro como global; sinais explicitos de limite da conta prevalecem.
Um 429 sem causa identificada permite somente a tentativa adicional limitada.
Logs de rejeicao incluem apenas codigo interno, status, global e retry_after;
nao armazenam corpo de erro, credenciais nem texto do usuario.

Os testes usam HTTP local simulado; nao comprovam disponibilidade ou qualidade
semantica atual dos modelos reais. Nenhuma inferencia real faz parte do deploy.

### Validacao e publicacao

O harness completo `scripts/test.ps1` passou (formatacao, vet, build e testes
com cobertura; geral 49,9%, assistente 67,1%). As regressoes verificam fallback
na mesma mensagem, teto de duas tentativas, pausa e reutilizacao de substituto,
limites globais/por usuario, privacidade e impossibilidade de outro cliente ou
etapa gastar mais chamadas da mesma mensagem.

Codigo publicado: `adabbdaf2df58239263ee69b6f235634774d2f82`.
Imagem: `app-financeiro-api:assistant-fallback-20261103`, digest
`sha256:98f212361016fec1b7de99e50fb380c1f779b29a2bbd2e68f8258d01420f9a1b`.
SHA256 do binario API:
`b038ee59c2cb41aa89cd14bbb638fbb1e69a054a71792cd03f3d562d0e0022ef`.

A API iniciou em 03/11/2026 as 17:12:04 de Sao Paulo e foi verificada as
17:12:15, sem reinicios. Passaram 20 verificacoes GET HTTP/HTTPS, autenticadas e
sem autenticacao, e auditoria de configuracao. Banco e schema 9 foram preservados,
sem migration; backup foi restaurado em PostgreSQL isolado e ambas as imagens
passaram nas verificacoes, preservando fingerprints financeiros. Nenhuma
inferencia real foi feita; disponibilidade e qualidade dos modelos continuam
dependentes do provedor, sem promessa de contornar cota global.

Rollback somente da API, preservando novas escritas no banco:

```sh
sudo sh /home/ubuntu/app-financeiro/deploy-assistant-fallback-20261103/rollback.sh
```

O procedimento restaura a imagem anterior
`app-financeiro-api:before-assistant-fallback-20261103`, digest
`sha256:128ad69cc2440ada0013550e45a449690e0a448f60747e9bce8388b7f872bcae`,
com a configuracao preservada no snapshot privado.

## Filtro de preco e politica por endpoint (validacao local)

O catalogo geral indica capacidades e preco do modelo, mas nao prova que o
endpoint gratuito passa na politica de dados. Ate `/models?zdr=true` pode listar
uma variante gratuita por existir um deployment pago compativel na familia.
O servidor cruza `/models` com a lista oficial `/endpoints/zdr`, usando IDs
exatos, status 0, precos conhecidos e todos zero e `safety_retention=false`
explicito. Nao basta o nome do fornecedor nem o sufixo `:free`. Routers opacos
sem endpoint concreto elegivel tambem ficam de fora.

Os dois GETs compartilham o refresh de ate 8s e o cache de 5 minutos. Nao gastam
completions. Se qualquer metadado falhar, o cache expirado nao e reutilizado;
o erro e reavaliado apos 5 segundos. Nao existe whitelist fixa de modelos:
novos endpoints elegiveis entram no proximo refresh. A lista publica de modelos
e as preferencias preservam o JSON existente; uma preferencia retirada do
catalogo passa para a recomendacao disponivel sem alterar o valor salvo.

As chamadas conservam preco maximo zero, `data_collection=deny` e
`require_parameters=true`, acrescentando `zdr=true` e `provider.only` com os
tags dos endpoints aprovados daquele modelo. Isso evita gastar a tentativa em
uma variante sem provedor gratuito compativel. Capacidade, guardrails da conta
e parametros ainda sao validados pela OpenRouter; metadados nao garantem
disponibilidade instantanea. Se houver apenas um modelo elegivel e ele falhar,
nao existe segunda opcao e o servidor encerra sem repetir esse modelo nem
relaxar a politica. O teto continua em duas tentativas, nunca uma meta de gasto.

Fontes oficiais: [lista de endpoints ZDR e diferenca de Safety Retention](https://openrouter.ai/docs/guides/features/zdr)
e [preferencias de roteamento de provedores](https://openrouter.ai/docs/guides/routing/provider-selection).
A lista ZDR pode incluir endpoints de Safety Retention; eles sao excluidos
explicitamente aqui. ZDR nao representa promessa de sigilo absoluto: a politica
oficial tem excecoes legais descritas pela OpenRouter.

### Resultado da verificacao de 03/11/2026, 17:23 de Sao Paulo

O harness completo passou: formatacao, vet, build e testes com cobertura
(geral 50,1%; assistente 67,3%). Uma tentativa real de um maximo autorizado de
duas usou a pergunta `car_exact` com dados sinteticos e o cliente de producao.
O catalogo filtrado continha somente `xingchen-agi/xing4.0-29b-a4b:free`, com
`provider.only=["phala"]`, preco zero, `data_collection=deny` e `zdr=true`.
Nemotron e demais variantes sem endpoint gratuito ZDR verificado nao entraram.

O provedor retornou HTTP 200 em 1648ms (1879ms no fluxo com catalogo),
3775 tokens de entrada e 57 de saida. A resposta foi gerada, mas o avaliador
semantico existente retornou `capacity_scope_mismatch`; disponibilidade nao
equivale a entendimento correto. A validacao real nao passou. A tentativa
restante nao foi usada e nao houve acesso a VM nem publicacao. Depois desse
resultado, o usuario autorizou explicitamente commit e push do filtro, sem deploy.
O release em producao continua sendo o fallback anterior, sem este filtro.
