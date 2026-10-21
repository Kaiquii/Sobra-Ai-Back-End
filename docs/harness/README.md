# Harness executavel

Requisitos: Go conforme go.mod, Git e Codex CLI autenticado para executar agentes.
Os comandos devem ser executados na raiz do repositorio. Os testes e a preparacao
de tarefas nao precisam de autenticacao, banco ou servicos de IA.

```powershell
# Validacao completa: formatacao, vet, build e testes com cobertura
go run ./cmd/harness -mode verify

# Preparar tarefa e registrar estado inicial sem chamar IA
go run ./cmd/harness -mode prepare -task "Corrigir X; aceitar quando Y passar"

# Conferir dependencias e interface do Codex
go run ./cmd/harness -mode doctor

# Implementar, validar, revisar e corrigir em ate duas tentativas
go run ./cmd/harness -mode run -task "Corrigir X; aceitar quando Y passar"
```

Se Codex nao estiver no PATH, informe `-codex "C:/caminho/codex.exe"`.
No Windows, o executor tambem procura automaticamente o CLI empacotado pelo Codex
em `.codex/plugins/.plugin-appserver/codex.exe` dentro da pasta do usuario.
O modelo e a autenticacao sao os configurados no CLI. O harness nao escolhe outro
modelo, instala ferramentas ou usa chaves de producao. As chamadas de IA consomem
os limites da conta autenticada. Use `codex login` se a autenticacao estiver ausente.

O fluxo usa os perfis implementer.md e reviewer.md desta pasta. O implementador
recebe sandbox workspace-write; o revisor recebe read-only. Os testes sao executados
fora do agente pelo processo local, portanto rode apenas codigo de repositorios confiaveis.
O harness nao representa isolamento do banco ou de segredos ja existentes no checkout.

Cada execucao tem uma pasta exclusiva em `.artifacts/agents/run-*`, contendo a tarefa,
status Git inicial, logs por etapa, respostas JSON e `result.json`. O status complete
exige implementacao, validacao automatizada e revisao aprovadas. Falhas de processo,
resposta invalida, cancelamento e limite de tentativas retornam codigo diferente de zero.
O modo prepare retorna prepared: isso nao e aprovacao de uma implementacao.

`-attempts 1` a `5` controla as tentativas; `-timeout 30m` limita a duracao total.
Uma falha de validacao ou revisao alimenta a proxima implementacao. Falhas do CLI
ou autenticacao encerram a execucao com erro. Nao ha loops ilimitados.

Um lock impede duas execucoes mutantes simultaneas no mesmo checkout. Depois de uma
interrupcao forcada, confirme que nenhum harness esta ativo antes de remover manualmente
`.artifacts/agents/active.lock`. Mudancas ficam no checkout para revisao, inclusive
quando a execucao falha. Nenhum commit, push, deploy ou descarte automatico e realizado.

Teste do proprio orquestrador: `go test ./cmd/harness -count=1 -v`. Esses testes usam
um executor simulado para verificar falhas e transicoes sem gastar chamadas de IA;
nao devem ser confundidos com uma execucao autenticada do modelo.

O projeto nao possui workflow de CI no GitHub Actions. As validacoes sao locais.
Para incluir integracao com PostgreSQL real, use `./scripts/test.ps1 -Integration` ou
`sh scripts/test.sh --integration`. Docker local em modo Linux deve estar ativo.
O teste cria um container postgres:15-alpine com nome unico, armazenamento temporario
e porta local aleatoria, e remove esse container ao terminar. Nunca le DB_DSN ou .env.
Falha explicitamente se Docker estiver indisponivel; nao pula a validacao.

A suite HTTP usa o mesmo roteador da API sobre uma porta TCP real: migrations
em banco vazio e reaplicacao, login, token ausente, bloqueio de acesso, isolamento
entre usuarios, CRUD de rendas e categorias, despesas, pagamento, adiantamento e
reflexo nos relatorios. Nao exercita o app Android, provedores externos ou deploy.
O perfil separado `.artifacts/test/integration.out` mede essa cobertura; nao se deve
somar percentuais de perfis. O startup do processo main nao e exercitado pelo servidor
HTTP de testes, mas o roteamento, middleware, handlers e persistencia sao reais.

Integracao baseada no [modo nao interativo oficial do Codex](https://developers.openai.com/codex/noninteractive/).
