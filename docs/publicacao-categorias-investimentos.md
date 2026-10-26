# Publicacao das categorias de investimentos

**Estado atual em 26/10/2026: correcao CDI publicada em producao.** A API
`investment-cdi-20261026` esta ativa com migration 8 e 15 categorias padrao,
incluindo CDI protegido contra edicao e exclusao. Catalogo e smoke autenticados
aprovados no HTTP interno e HTTPS do APK. A imagem de fallback foi testada em
copia restaurada e isolada, sem necessidade de ativa-la em producao.

## Correcao CDI: registro atual e recuperacao (schema 8)

- Pedido original atendido com CDI padrao; criacao e exclusao de categorias
  pessoais continuam disponiveis. IDs, categorias pessoais homonimas e
  investimentos existentes preservados.
- Nova migration imutavel `000008_add_investment_category_cdi`, aplicada as
  12:34:32 de Sao Paulo; migration 7 preservada. Escritas por texto priorizam o
  padrao CDI; `category_id` explicito continua respeitando a escolha pessoal.
- Release: `app-financeiro-api:investment-cdi-20261026`.
- Digest: `sha256:b554ffd28bd05929567aed15e57f47d90c69b237a67845429b45f9c94395417e`.
- Pacote: `.artifacts/investment-cdi-20261026.tar.gz`, SHA-256
  `a2add0b7cda179b7f1f030b3af5a4b81feb593adb0160da81ec9f7ef31d74733`.
- Diretorio remoto: `/home/ubuntu/app-financeiro/deploy-investment-cdi-20261026/`.
- Harness completo com integracao aprovado na copia isolada dos fontes, conforme
  limitacao de rascunhos IA preexistentes registrada em `patrimonio.md`.
- Backup completo restaurado com sucesso em PostgreSQL isolado; fingerprints
  financeiros preservados apos migration 8. Release e fallback passaram nos
  testes autenticados sobre essa copia antes de publicar.
- Publicacao alterou somente a API. Smoke autenticado interno e HTTPS aprovado:
  15 padroes incluindo CDI, gravacao por ID/texto, protecao de CDI 403, CRUD
  pessoal, duplicatas, exclusao em uso, isolamento, autorizacao, FGTS e resumo
  mensal preservados. Fixtures e containers descartaveis removidos.
- Auditoria confirmou configuracao preservada, API ativa sem reinicios e hash do
  backup integro. Banco, volumes e proxy preservados.
- Rollback atual:
  `sudo sh /home/ubuntu/app-financeiro/deploy-investment-cdi-20261026/rollback.sh`.
  Ativa `app-financeiro-api:investment-cdi-rollback-20261026`, compativel com
  schema 8 e escritas por ID/texto. Nao remove categorias/dados nem restaura
  backup. Desativa apenas as rotas do catalogo.

As imagens e os procedimentos da publicacao inicial descritos a seguir sao
historicos e valem para schema 7. Nao executar seus rollbacks sobre schema 8:
as imagens antigas recusam a migration desconhecida ao iniciar.

## Procedimento historico da publicacao inicial (schema 7)

Escopo da aprovacao: consultar a versao/schema/rotas efetivos, transferir o pacote,
preservar imagem/configuracao, gerar e restaurar backup em ambiente descartavel,
aplicar migration 7 e substituir somente a API; executar smoke autenticado com
fixtures sinteticas e eventual rollback compativel. Banco e volumes permanecem;
proxy e demais servicos nao serao reconfigurados ou reiniciados. A troca inclui
uma parada breve da API. Conforme [regras do projeto](rules.md), a aprovacao deve
ocorrer antes de qualquer acesso a VM e vale somente para esta operacao.

## Artefatos e verificacao local

- Diretorio de trabalho: `.artifacts/investment-categories-bundle/`.
- Pacote local: `.artifacts/investment-categories-20261026.tar.gz`.
- Release: `app-financeiro-api:investment-categories-20261026`.
- Fallback: `app-financeiro-api:investment-categories-rollback-20261026`.
- Diretorio remoto proposto:
  `/home/ubuntu/app-financeiro/deploy-investment-categories-20261026/`.

Antes de transferir, fechar o pacote e conferir seu SHA-256, manifesto dos fontes
e hashes dos binarios `main`, `main-compatible` e `migrate`. Nao incluir `.env`,
chaves, configuracao com credenciais, dados reais, caches ou arquivos alheios a
entrega. Registrar os comandos locais efetivamente aprovados e suas limitacoes,
incluindo qualquer uso de copia isolada para preservar trabalho preexistente.

O utilitario `http-probe` tambem e incluido e verificado por SHA-256; e copiado
somente para containers descartaveis de verificacao. Nao e instalado na API de
producao. O harness completo com integracao passou na copia isolada. Os binarios
Linux release/fallback foram ensaiados em Docker local com PostgreSQL descartavel,
incluindo inicializacao, catalogo 200/404, gravacao por ID/texto, FGTS, isolamento,
saldo mensal e IDs padrao preservados. Depois da autorizacao, tambem foram
aprovados a restauracao do backup real, os testes na copia migrada e o smoke na
VM. As verificacoes locais e remotas estao registradas separadamente abaixo.

O fallback deve conservar o resolvedor de categoria por ID/texto e o registro
exato da migration 7, desativando apenas as rotas do catalogo. Assim o APK recebe
404 no catalogo e usa seu formulario legado, enquanto as escritas continuam
preenchendo `category_id`. Validar essa propriedade no binario empacotado.

## 1. Conferir o estado efetivo, depois da aprovacao

1. Identificar o container da API, ID/digest da imagem, estado/reinicios, redes,
   portas, volumes e configuracao efetiva. Preservar o inspect em arquivo privado;
   nao imprimir variaveis ou segredos em logs/chat. Conferir tambem banco e proxy.
2. Consultar `./migrate status` e o registro de migrations diretamente no banco
   escolhido; conferir a presenca real de `investment_categories` e
   `investments.category_id`, sem alterar schema.
3. Consultar a rota de catalogo no HTTP interno e no HTTPS ja usado pelo APK,
   primeiro sem autenticacao e depois com fixture autenticada autorizada.
   Distinguir 404 de 401/403 e erro de conexao. Conferir encaminhamento do proxy.
4. Se a API/schema 7 ja estiverem presentes, corrigir o diagnostico e evitar
   republicacao ou migration desnecessaria. Se o problema for proxy, interromper
   a publicacao e apresentar a correcao especifica para aprovacao; este escopo
   nao inclui alterar proxy. Se o estado divergir do esperado, reavaliar antes de
   prosseguir.

## 2. Preparar recuperacao e testar restauracao

No diretorio remoto aprovado, conferir hashes novamente. O `prepare.sh` do pacote
deve preservar a imagem atual como
`app-financeiro-api:before-investment-categories-20261026`, o inspect privado e o
backup custom completo em `rollback/database.dump`, com indice e SHA-256. Nao
sobrescrever uma preparacao anterior; usar diretorio exclusivo se ja existir.

Executar `sudo sh ./prepare.sh` somente apos revisar o pacote. A preparacao deve:

1. Construir release e fallback sobre a imagem preservada, sem mudar a API ativa.
2. Restaurar o backup em PostgreSQL descartavel, sem portas publicadas, sem volume
   de producao e com rede isolada. Verificar restauracao, contagens e integridade.
3. Aplicar migration 7 somente na copia restaurada; conferir versoes 1 a 6
   preservadas, 14 padroes, backfill dos IDs e valores/datas existentes intactos.
4. Iniciar as duas imagens sobre essa copia, com configuracao de teste sem envio
   de emails e sem integracoes externas, e executar `verify_images.py` para os
   fluxos autenticados abaixo, incluindo legado/ID e fallback.
5. Remover apenas os containers descartaveis criados nesta operacao. Manter
   backup/inspect/imagens privados e recuperaveis.

O marcador `preparation.ok` somente pode existir depois desses gates passarem.
Listar um dump ou receber 401 em uma rota nao substitui restauracao nem smoke
autenticado. Falha em qualquer gate bloqueia a publicacao.

## 3. Publicar com parada breve somente da API

Reconfirmar que imagem/container/configuracao/schema nao mudaram apos o snapshot
e que hashes e backup permanecem validos. Executar `sudo python3 ./publish.py`
somente com o pacote revisado e a preparacao aprovada. O procedimento deve:

1. Parar somente `app_financeiro_api` antes do commit da migration 7.
2. Executar `./migrate up` da imagem nova com a conexao explicitamente conferida.
3. Recriar somente a API com a configuracao, portas, redes e binds preservados,
   usando a imagem release. Conferir inicializacao e `./migrate status`.
4. Executar `sudo python3 ./smoke.py` para smoke autenticado no HTTP interno e no
   mesmo HTTPS do APK, e `sudo python3 ./audit.py` para a auditoria da release.

A API 6 nao pode continuar servindo apos a migration: nao preenche o novo
`category_id` obrigatorio e rejeita migration 7 ao reiniciar. A inicializacao da
API nova apenas valida migrations; nao aplica alteracoes de schema.

## 4. Aceitacao autenticada e fallback

Com dois usuarios sinteticos exclusivos desta execucao, validar:

- Catalogo 200 com IDs reais e os 14 padroes tambem para conta nova; nenhum ID
  fixado no APK ou no teste.
- CRUD pessoal, duplicata 409 `category_exists`, padrao imutavel 403
  `default_category`, categoria em uso 409 `category_in_use` e isolamento 404.
- Investimentos criados/editados tanto com `category_id` quanto com texto legado;
  retorno preservando `category` e `category_id`; FGTS, zero e centavos.
- Renomeacao propagando rotulos sem mudar saldos/datas; totais e resumo mensal
  preservados; sem token 401 e usuario bloqueado 403.

Na copia restaurada, ativar o fallback e repetir FGTS/investimentos/isolamento e
escritas legadas. O catalogo deve retornar 404; dados, categorias e schema 7 devem
permanecer intactos. Confirmar que reativar a release devolve o catalogo e seus
mesmos IDs. Testar essa recuperacao antes da publicacao, sem alternar producao
desnecessariamente.

Se a release falhar apos migration 7, executar `sudo sh ./rollback.sh` para ativar
`investment-categories-rollback-20261026`, preservando o banco, e repetir o smoke
compativel. Se a migration falhar, conferir seu estado transacional: somente com
schema ainda 6 e versoes verificadas a imagem original pode ser reativada.
Estado incerto exige diagnostico antes de reiniciar.

Nunca usar diretamente o antigo rollback de patrimonio/schema 6 sobre schema 7,
apagar registros de migration/categorias ou restaurar backup sobre escritas novas.
Restauracao de desastre exige procedimento e aprovacao especificos.

## 5. Integridade, limpeza e registro final

Conferir imagem ativa, estabilidade/reinicios, migrations, configuracao preservada
e SHA-256 do backup. O `audit.py` deve refletir a imagem efetivamente escolhida;
nao usar uma auditoria exclusiva da release para declarar fallback aprovado.
Remover somente fixtures identificadas por prefixo/IDs exclusivos desta execucao
e verificar ausencia de usuarios, categorias e investimentos de teste. Preservar
imagens, backup, manifesto e procedimentos de recuperacao; nao imprimir tokens,
dados pessoais nem o inspect privado.

Atualizar `docs/patrimonio.md` com o resultado real: imagem/digest, migration,
testes locais e remotos separados, limpeza e caminho do rollback. Informar ao
usuario se foi publicada release, ativado fallback ou mantido ambiente anterior.
## Registro da execucao de 26/10/2026

- Autorizacao explicita recebida neste chat para publicar e enviar handoff ao
  Android ao concluir. O procedimento acima foi executado sem alterar o proxy.
- Estado anterior: imagem `patrimony-20261026`, migrations 1 a 6; tabela de
  categorias e coluna `category_id` ausentes; rota 404 interna e publica.
- Pacote verificado na VM: SHA-256
  `269c5ed159a47fc7005d04d80384c1724aac302398ddbcd4ab7520704cf797b9`.
- Backup completo restaurado com sucesso. Fingerprints financeiros da copia
  restaurada permaneceram identicos apos migration 7. Release e fallback passaram
  nos testes autenticados sobre essa copia; containers de verificacao removidos.
- API parada brevemente durante migration/troca; banco e proxy mantidos ativos.
- Imagem publicada: `app-financeiro-api:investment-categories-20261026`;
  digest `sha256:6733ee06f48bbb8b2fc39a4b8a401009a238cbb266e1c9f4e2a584f1846a578f`.
- Migration 7 aplicada as 12:15 de Sao Paulo, preservando versoes anteriores.
- Catalogo, CRUD, protecoes, IDs/texto legado, datas/saldos, FGTS, isolamento e
  bloqueio validados por HTTP interno e HTTPS. Fixtures removidas, backup integro,
  configuracao preservada e API sem reinicios na auditoria.
- Diretorio remoto: `/home/ubuntu/app-financeiro/deploy-investment-categories-20261026/`.
  Recuperacao: `sudo sh /home/ubuntu/app-financeiro/deploy-investment-categories-20261026/rollback.sh`.
- O manifesto incluido no pacote e um snapshot da preparacao local. Este registro
  e os metadados finais de publicacao documentam a execucao posterior autorizada.
