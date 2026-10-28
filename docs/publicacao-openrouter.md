# Publicacao OpenRouter e contexto em 28/10/2026

Registro da publicacao inicial. Evolucoes posteriores:
[catalogo completo](publicacao-catalogo-completo-ia.md) e
[roteamento automatico ativo](publicacao-roteamento-automatico-ia.md).
Os tres modelos e a imagem abaixo representam a etapa inicial, nao a lista atual.

Publicacao explicitamente autorizada pelo usuario, concluida e auditada.
Somente a API foi substituida; banco, volumes e proxy permaneceram ativos.

## Estado publicado

- Imagem: `app-financeiro-api:openrouter-20261028`.
- Digest: `sha256:d44a91434b4ef02b1543e16eb67b812f5b42852d6ad8ea1995c2d1da9b19e250`.
- Migration 9 `add_assistant_model_preferences`, aplicada as 12:50:43 de Sao Paulo.
  Migrations 1 a 8 preservadas, inclusive CDI e os 15 padroes de investimentos.
- OpenRouter e o unico provedor ativo. Variaveis Groq/Gemini foram removidas da
  configuracao efetiva da release. A chave nova fica em ambiente privado, fora
  da imagem, Git, APK, pacote, manifesto publico e logs.
- Modelo recomendado: `dots-studio/dots-3-note-preview:free`; alternativas:
  `google/gemma-4-31b-it:free` e `google/gemma-4-26b-a4b-it:free`.
- Preferencia persistida por usuario e endpoints de catalogo/preferencia ativos.
  Contexto do servidor chega a interpretacao; patrimonio/FGTS/investimentos sao
  consultados com isolamento e sem alterar o saldo mensal.

## Validacoes realizadas

- Harness canonico completo com `-Integration`: formatacao, vet, build, testes
  sem cache e HTTP com PostgreSQL descartavel aprovados. Integracao final:
  `cmd/api` 23,729 s. Fontes e hashes conferidos antes de empacotar.
- Execucao em copia isolada, preservando os rascunhos preexistentes
  `expense_draft.go`/`expense_draft_test.go` que duplicam funcoes versionadas e
  as alteracoes anteriores de Compose. Esses arquivos nao entraram na imagem.
  O conflito da pasta original ainda depende da conciliacao desses rascunhos.
- Teste sintetico real do modelo recomendado aprovado: portugues informal,
  referencia a categoria/periodo no historico e JSON estruturado. Nenhum dado
  financeiro real de usuario foi usado nos testes externos.
- Tentativas reais mostraram que um candidato recusava a politica de dados;
  ele foi substituido sem relaxar `data_collection=deny`. Outras alternativas
  apresentaram limite temporario do provedor. Nao se afirma disponibilidade
  continua ou aprovacao de qualidade de todos os modelos.
- Alternancia entre modelos, JSON invalido, cota global, cancelamento, precos,
  ausencia de vazamento de credenciais e limites por usuario testados com HTTP
  simulado. Nao foi provocada exaustao real da cota do projeto.
- Backup completo restaurado em PostgreSQL isolado e migration 8->9 aprovada.
  Fingerprints de despesas, rendas, rateios, categorias, FGTS, investimentos,
  conversas/mensagens e migrations anteriores permaneceram iguais.
- Release e recuperacao compativel testadas sobre a copia restaurada, incluindo
  inicializacao, comportamento controlado sem provedor, contratos antigos,
  categorias/IDs, FGTS e isolamento.
- Smoke autenticado real aprovado em `http://127.0.0.1:8080` e
  `https://137-131-247-103.nip.io`: catalogo gratuito, preferencia/reset,
  rejeicao de modelo pago/desconhecido, bloqueio, usuarios isolados, consulta
  de patrimonio e pergunta seguinte de FGTS na mesma conversa, saldos exatos
  no resultado estruturado e resumo mensal inalterado.
- Fixtures e containers descartaveis removidos. Auditoria confirmou configuracao,
  binds, portas, redes e politica de reinicio preservados; backup integro e API
  ativa sem reinicios. Validacao visual do seletor depende da entrega do app.

## Pacote e recuperacao

- Pacote local: `.artifacts/openrouter-20261028.tar.gz`.
- SHA-256: `46d6ea53d3d11d6f9bf312bf90f27970230bc522f6ab73811317e4d3b0fd163b`.
- Diretorio remoto privado: `/home/ubuntu/app-financeiro/deploy-openrouter-20261028/`.
- Backup: `rollback/database.dump`, com indice e SHA-256. Snapshot de configuracao,
  chave e marcadores de verificacao nesse diretorio sao privados e nao devem
  ser copiados para documentacao publica, Git ou respostas do assistente.
- Recuperacao operacional:
  `sudo sh /home/ubuntu/app-financeiro/deploy-openrouter-20261028/rollback.sh`.
  Usa `app-financeiro-api:openrouter-rollback-20261028`, com o binario anterior
  compatibilizado com o registro imutavel da migration 9 e ambiente anterior
  preservado. Preferencias e dados financeiros permanecem no banco; rotas novas
  de modelos nao ficam expostas no fallback. O antigo provedor existe somente
  como recuperacao operacional, sem participar da release ativa.
- Nunca usar o binario puro de schema 8 sobre schema 9, apagar migrations ou
  restaurar o dump sobre escritas novas para reverter apenas a API.

O manifesto dentro do pacote registra a preparacao/testes anteriores ao deploy;
este documento registra o resultado efetivo posterior. Futuras operacoes remotas
continuam sujeitas a autorizacao especifica das regras do repositorio.
