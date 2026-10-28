# Catalogo completo e automatico de IA gratuita

Correcao publicada em 28/10/2026, em continuidade ao pedido original de oferecer
todas as opcoes gratuitas. Removidos a shortlist de tres, a variavel de restricao
`OPENROUTER_MODELS` e os filtros de JSON nativo/contexto minimo para exibicao.

- Imagem ativa: `app-financeiro-api:openrouter-all-models-20261028`.
- Digest: `sha256:030229b806697bd01b57a2b1666ba81f8c367e79193cd014616186530a4deec2`.
- Catalogo autenticado validado com **17 opcoes**, conjunto identico ao catalogo
  publico de modelos gratuitos de conversa no momento do teste. O numero nao e
  fixado no codigo: novas opcoes entram e as que passam a cobrar saem na renovacao.
- Mesmos contratos HTTP e schema 9; nenhuma migration aplicada e nenhuma variavel
  de ambiente, bind, porta, rede, volume ou politica de reinicio alterada.

## Comportamento automatico

O backend consulta `/api/v1/models` no OpenRouter e valida todos os precos,
inclusive adicionais e overrides. Modelos de entrada textual e resposta textual
com precos zero entram na lista, inclusive sem `:free` ou JSON nativo. Geracao de
audio/imagem, embeddings/reranking e roteadores opacos pagos ficam fora por nao
serem opcoes deste chat. `openrouter/free` e incluido como roteador so de gratuitos.

Cache compartilhado de cinco minutos, renovado sob demanda. A atualizacao nao
exige alterar nomes no codigo, publicar API nem gerar APK. O app precisa buscar
novamente o catalogo; ao abrir o seletor deve renovar uma lista antiga. O app
atual retém a consulta por 60 segundos na navegacao; fechar/abrir limpa esse estado.
Falhas de consulta tem cache curto de cinco segundos e espera cancelavel.

O modelo escolhido e tentado primeiro, seguido do recomendado e de alternativas.
O limite de tres tentativas HTTP por etapa protege cota e latencia; nao limita
o numero de opcoes do menu. Parametros de JSON/temperatura/limite de tokens sao
adaptados ao suporte do modelo. JSON continua validado antes de qualquer acao.
Preco maximo zero e `data_collection=deny` permanecem ativos; disponibilidade
momentanea e politicas de provedores nao sao garantidas pelo catalogo publico.

## Evidencias de validacao

- Harness completo com integracao aprovado na copia isolada dos fontes: Go vet,
  formatacao, build, testes e PostgreSQL descartavel. Integracao final `cmd/api`:
  13,760 s. Rascunhos IA duplicados e Compose preexistentes preservados e excluidos
  dessa copia, conforme documentado na publicacao inicial.
- Testes HTTP cobrem mais de tres opcoes, preco zero sem sufixo, modelos sem JSON
  nativo, parametros opcionais, roteador gratuito, bloqueio de cobranças/routers
  pagos, escolha fora da lista antiga, contexto, autenticacao e isolamento.
- Backup restaurado em PostgreSQL isolado; imagens anterior e nova ensaiadas com
  schema 9, erro controlado sem provedor e fluxos patrimoniais. Fingerprints de
  dados financeiros, conversas, preferencias e registro de migrations identicos.
- Smoke real aprovado em HTTP interno e no HTTPS usado pelo APK: igualdade exata
  do conjunto gratuito, 17 opcoes, preferencia fora dos tres antigos, reset,
  rejeicao de modelo pago/desconhecido, chat e pergunta de continuacao, historico
  preservado, isolamento e resumo mensal inalterado.
- Fixtures e containers descartaveis removidos; auditoria confirmou schema e
  ambiente inteiramente preservados, backup integro e API ativa sem reinicios.
- A aprovacao nao afirma que todos os modelos externos estavam disponiveis para
  gerar respostas ao mesmo tempo; o fallback/adaptador foi coberto por mocks e o
  fluxo real foi aprovado com o recomendado.

## Recuperacao

- Diretorio remoto: `/home/ubuntu/app-financeiro/deploy-openrouter-all-models-20261028/`.
- Backup privado: `rollback/database.dump`, indice e SHA-256 no mesmo diretorio.
- Rollback operacional:
  `sudo sh /home/ubuntu/app-financeiro/deploy-openrouter-all-models-20261028/rollback.sh`.
  Reativa a imagem imediatamente anterior preservada como
  `app-financeiro-api:before-openrouter-all-models-20261028`, mantendo banco,
  preferencias e ambiente. Ela retorna ao catalogo inicial de tres modelos.
- Pacote: `.artifacts/openrouter-all-models-20261028.tar.gz`.
- SHA-256: `39fcb8f6a8a818ecffe8fe815158fed20e38602a8cd0b8aeb110155c4a1b4e8c`.

O manifesto do pacote e um snapshot dos testes anteriores ao deploy; este registro
documenta a execucao posterior. Nao restaurar backup sobre novas escritas para
reverter uma troca de imagem da API.
