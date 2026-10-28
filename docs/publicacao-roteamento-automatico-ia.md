# Roteamento automatico da IA em 28/10/2026

Registro da etapa anterior. O roteamento permanece ativo na evolucao de
[estilo das respostas](publicacao-estilo-respostas-ia.md), que e a imagem atual.

Publicacao explicitamente autorizada pelo usuario, concluida e auditada.
Somente a API foi substituida; banco, proxy, volumes e configuracao permaneceram
preservados. Ativacao as 15:36:50 de Sao Paulo, sem migration nova.

## Estado publicado

- Commit da implementacao: `08a611e`, enviado para `main`.
- Imagem: `app-financeiro-api:openrouter-auto-routing-20261028`.
- Digest: `sha256:c5bd87e5a14784c2bcb3803d4ce5d8ae0423503ca77ed038d9bad7e081200395`.
- Schema 9 e registro completo das migrations preservados.
- Ambiente efetivo, binds, portas, redes e politica de reinicio identicos ao
  snapshot anterior. Nenhuma chave ou variavel de provedor foi substituida.

POST `/api/assistant/chat` sem `model_id` ou com string vazia delega a escolha ao
servidor, ignorando preferencias manuais antigas sem apagar valores ou datas.
O app nao precisa consultar catalogo/preferencia nem enviar PUT de reset.
Selecao explicita legada continua validada e prioritaria por mensagem, sujeita
a fallback gratuito. GET/PUT de preferencias continuam compativeis.

O servidor reutiliza o ultimo modelo gratuito bem-sucedido da conta por ate cinco
minutos, sem renovar indefinidamente esse prazo. Falhas e retirada do catalogo
removem a prioridade. Preco maximo zero, privacidade `deny`, limites de tentativas
e cotas permanecem ativos. Uma requisicao observa o bloqueio global identificado
por outra antes de iniciar nova tentativa. Chamadas ja em voo podem terminar.
Estado de saude e limitadores locais continuam em memoria por processo; nao ha
previsao inventada de cota futura nem tentativa de contornar o limite da conta.

## Validacoes aprovadas

- Harness completo `scripts/test.ps1 -Integration`: formatacao, vet, build,
  testes sem cache com shuffle/cobertura e PostgreSQL descartavel. Integracao
  final: 16,370 s; pacote assistant: 12,873 s.
- Fontes Go e hashes conferidos entre a arvore local, a copia testada e o pacote
  Linux amd64. Compose, cache e os dois rascunhos `expense_draft*.go` preexistentes,
  que duplicam codigo versionado, foram preservados e excluidos dessa copia.
- Regressao com barreiras concorrentes confirma bloqueio global antes do
  proximo fallback/reserva. Testes tambem cobrem TTL fixo, catalogo alterado,
  override legado, cota pessoal compartilhada e confirmacao sem nova chamada IA.
- Backup restaurado em PostgreSQL isolado sem rede externa. Imagens anterior e
  nova ensaiadas sequencialmente com limites de memoria, schema 9, respostas
  controladas sem provedor e fluxos patrimoniais. Fingerprints de dados
  financeiros, conversas, preferencias e migrations identicos apos o ensaio.
- Smoke autenticado real aprovado em `http://127.0.0.1:8080` e
  `https://137-131-247-103.nip.io`: mensagens sem/vazio e explicita legada,
  patrimonio seguido de FGTS na mesma conversa, preferencia antiga e `updated_at`
  intactos, exatamente tres pares de mensagens, isolamento e resumo mensal
  inalterado. Confirmacoes repetidas sem rascunho nao criaram despesas.
- Usuarios e dados sinteticos removidos; containers descartaveis removidos.
  Auditoria confirmou ambiente/configuracao integralmente preservados, API sem
  reinicios, schema 9, CDI/padroes e backup integro.

Os testes externos usaram somente fixtures sinteticas. Nao foi provocada exaustao
real da cota. A identidade/prioridade de cada tentativa e a reutilizacao do modelo
saudavel foram verificadas com mocks; a resposta publica nao expoe o modelo
efetivo. Nao se afirma disponibilidade continua de todos os modelos externos.

## Pacote e recuperacao

- Pacote local: `.artifacts/openrouter-auto-routing-20261028.tar.gz`.
- SHA-256: `4f390b1d3a2b7cfc908054e52f97a1e24628f8db15b90380f929cb07cfc58e5e`.
- Diretorio remoto privado:
  `/home/ubuntu/app-financeiro/deploy-openrouter-auto-routing-20261028/`.
- Backup: `rollback/database.dump`, com indice e SHA-256 no mesmo diretorio.
  Snapshot de configuracao e credenciais permanecem privados na VM.
- Rollback operacional:
  `sudo sh /home/ubuntu/app-financeiro/deploy-openrouter-auto-routing-20261028/rollback.sh`.
  Reativa somente a imagem anterior preservada como
  `app-financeiro-api:before-openrouter-auto-routing-20261028`, de digest
  `sha256:030229b806697bd01b57a2b1666ba81f8c367e79193cd014616186530a4deec2`.
  Mantem o catalogo completo e retorna a semantica anterior de preferencias.
  Nunca restaurar o dump sobre novas escritas para reverter a imagem da API.

O manifesto dentro do pacote registra a preparacao; este documento registra o
resultado posterior. Futuras operacoes remotas exigem autorizacao especifica.
