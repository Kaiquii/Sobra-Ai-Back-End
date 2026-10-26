# Patrimonio: FGTS e investimentos

Acompanhamento manual do saldo atual, separado de receitas, despesas, fontes de
pagamento, saldo mensal, relatorios, exportacoes e consultas do assistente IA.
Nao ha historico mensal, calculo de rendimento, estimativa pelo salario ou
integracao bancaria. Nenhum servico financeiro externo e necessario.

**Status da evolucao de categorias:** publicada em producao em 26/10/2026, com
migration `000008_add_investment_category_cdi` aplicada e 15 categorias padrao,
incluindo **CDI**. Catalogo autenticado,
gerenciamento pessoal e investimentos por `category_id` validados no HTTPS usado
pelo APK. O contrato por texto continua aceito para clientes anteriores.

O handoff do aplicativo confirmou o dropdown e o gerenciamento implementados no
APK, com fallback legado quando o catalogo retorna 404. O pacote de publicacao e
o procedimento estao em [publicacao-categorias-investimentos.md](publicacao-categorias-investimentos.md).
A operacao foi autorizada explicitamente pelo usuario e concluida. O mesmo APK
pode usar o dropdown apos consultar novamente o catalogo; forcar a atualizacao ou
fechar e abrir o app caso ainda apresente a lista anterior em cache.

## Contrato HTTP

Todas as rotas exigem `Authorization: Bearer <token>` e respeitam bloqueio de acesso.
Cada usuario acessa apenas seus registros; IDs de outro usuario retornam 404.

| Metodo | Rota | Corpo / resultado |
| --- | --- | --- |
| GET | `/api/patrimony/` | Saldo FGTS, investimentos e totais |
| PUT | `/api/patrimony/fgts` | `{"amount":2500.50}` cria ou substitui o unico saldo FGTS |
| DELETE | `/api/patrimony/fgts` | Remove o saldo FGTS informado |
| POST | `/api/patrimony/investments` | `{"name":"Reserva","category_id":1,"amount":5000}` |
| PATCH | `/api/patrimony/investments/:id` | Um ou mais campos: `name`, `category_id`, `amount` |
| DELETE | `/api/patrimony/investments/:id` | Remove o investimento |
| GET | `/api/patrimony/investment-categories` | Categorias padrao e pessoais do usuario |
| POST | `/api/patrimony/investment-categories` | `{"name":"Minha categoria"}` cria uma categoria pessoal |
| PATCH | `/api/patrimony/investment-categories/:id` | `{"name":"Novo nome"}` renomeia uma categoria pessoal |
| DELETE | `/api/patrimony/investment-categories/:id` | Exclui categoria pessoal sem investimentos vinculados |

GET retorna:

```json
{
  "fgts": {"amount": 2500.50, "updated_at": "2026-10-26T12:00:00Z"},
  "investments": [
    {"id": 1, "name": "Reserva", "category": "CDB", "category_id": 1, "amount": 5000,
     "created_at": "2026-10-26T12:00:00Z", "updated_at": "2026-10-26T12:00:00Z"}
  ],
  "total_fgts": 2500.50,
  "total_investments": 5000,
  "total_patrimony": 7500.50
}
```

Sem registros: `fgts: null`, `investments: []`, totais numericos iguais a zero.
FGTS cadastrado com zero permanece um objeto; remocao o torna `null`.
PUT/PATCH retornam 200 e `{"data": objeto}`; POST retorna 201 e o mesmo envelope.
DELETE retorna 200 com `message`, ou 404 se ja removido. Nao ha rota individual de
consulta: a tela utiliza GET do patrimonio. Lista ordenada por ID crescente.

Valores em reais como numero JSON decimal, sem aspas ou notacao cientifica, entre
0 e 9999999999.99, com ate duas casas decimais. Armazenamento e somas em centavos
inteiros. Campos ausentes/nulos, valores negativos, precisao excedente, PATCH
vazio e campos desconhecidos retornam 400. Nome: 1 a 120 caracteres; categoria:
1 a 80 caracteres, ambos sem espacos nas pontas. A categoria pertence ao catalogo
de investimentos, independente das categorias de despesas. O campo `category` de
texto continua aceito para clientes anteriores, resolvendo uma categoria visivel
ou criando uma categoria pessoal quando ausente. Enviar exatamente um entre `category` e
`category_id` ao definir a categoria; os clientes novos devem usar `category_id`.
Respostas preservam `category` e acrescentam `category_id`. Datas sao geradas
pelo servidor em formato RFC3339.
Cada atualizacao substitui o saldo, nao soma um aporte.

### Categorias de investimentos

Lista padrao compartilhada, criada pela migration e disponivel para usuarios novos
e existentes: **CDB, CDI, RDB, Tesouro Direto, LCI, LCA, CRI, CRA, Poupanca, Fundos de
investimento, Acoes, Fundos imobiliarios, ETFs, Previdencia privada e Outros**.
Os nomes com acentos sao retornados normalmente pela API. Essas categorias nao
podem ser renomeadas nem excluidas por usuarios. Categorias pessoais sao visiveis
e gerenciaveis somente por quem as criou. FGTS permanece uma secao propria.

CDI integra a lista padrao protegida, conforme solicitado pelo usuario. Categorias
pessoais preexistentes chamadas "CDI" preservam seus IDs, proprietarios e vinculos.
Escritas legadas por texto priorizam a categoria padrao; enviar explicitamente o
`category_id` de uma categoria pessoal continua preservando essa escolha.
"Reserva de emergencia" e um exemplo de nome do investimento; o usuario seleciona
o tipo correspondente no dropdown.

GET do catalogo retorna 200, com padroes primeiro e nomes ordenados:

```json
{
  "total": 16,
  "categories": [
    {"id": 1, "name": "CDB", "is_default": true},
    {"id": 16, "name": "Minha categoria", "is_default": false}
  ]
}
```

O exemplo abrevia a lista. Os IDs sao obtidos desse endpoint; nao os fixar no app.
POST retorna 201 e PATCH retorna 200 com `{"data":{"id":16,"name":"Minha categoria","is_default":false}}`.
DELETE retorna 200 com `message`. Somente o
campo `name` pode ser enviado ao cadastrar ou editar uma categoria; `user_id` e
`is_default` sao determinados pelo servidor. Nomes tem 1 a 80 caracteres e sao
normalizados removendo espacos nas pontas. Duplicatas sao verificadas sem
diferenciar maiusculas/minusculas, inclusive em relacao aos padroes.

Erros relevantes, em JSON com `error` (e `code` nas regras abaixo):

- 400: entrada invalida, ID zero/nulo/negativo ou envio simultaneo de texto e ID.
- 403 `default_category`: tentativa de editar ou excluir categoria padrao.
- 404: categoria pessoal de outro usuario, ID inexistente ou categoria removida.
- 409 `category_exists`: nome ja existente no catalogo visivel.
- 409 `category_in_use`: categoria vinculada a investimentos; trocar a categoria
  desses investimentos antes de excluir. Nao apagar investimentos automaticamente.

Renomear uma categoria pessoal atualiza atomicamente o texto de todos os
investimentos vinculados, preservando IDs, valores e datas de atualizacao dos
saldos. A associacao e sempre validada para o usuario autenticado. Operacoes que
criam uma categoria por compatibilidade e depois falham ao gravar o investimento
sao revertidas integralmente.

Validacao local da evolucao: testes unitarios, harness completo e integracao HTTP
com PostgreSQL descartavel aprovados. Inclui upgrades 6 para 7 e 7 para 8, idempotencia,
protecao dos padroes, isolamento pessoal, duplicatas, associacao por ID,
compatibilidade do texto legado, exclusao em uso, rollback de renomeacao e
criacao, exclusao de usuario e resumo mensal preservado. A regressao de CDI cobre
preservacao de categoria pessoal homonima e seus investimentos, selecao do padrao
por texto, selecao pessoal por ID e protecao contra edicao/exclusao. Execucao em copia
isolada dos fontes, pelo conflito preexistente de rascunhos IA descrito abaixo.

## Interface Android/iOS

Criar tela **Patrimonio**, com secoes FGTS e Investimentos, totais separados e total
geral. Permitir cadastrar/atualizar/remover FGTS e adicionar/editar/excluir
investimentos. Exibir ultima atualizacao de cada item e estados vazio, carregando,
erro e tentativa novamente. Ao salvar/excluir, recarregar GET para atualizar totais.
Confirmar exclusoes e aceitar saldo zero. Converter a entrada monetaria pt-BR
para numero JSON decimal com ponto e ate duas casas. Nao usar saldo mensal nem
fonte de pagamento e nao exibir variacao como rendimento.

Nos formularios de investimento, substituir o campo livre de categoria por
dropdown alimentado pelo GET de categorias. Oferecer "Nova categoria" e acesso a
"Gerenciar categorias"; permitir renomear/excluir somente itens pessoais. Marcar
os padroes como protegidos, sem acao de exclusao/edicao, e tratar a protecao tambem
quando vier erro da API. Selecionar o ID criado apos "Nova categoria". Ao editar
um investimento, selecionar seu `category_id`. Recarregar catalogo e patrimonio
apos renomear ou excluir para evitar nomes e selecoes desatualizados.

### Handoff atualizado: acesso flutuante e APK de teste

O usuario autorizou a implementacao no front-end. Patrimonio e a evolucao de
categorias estao publicados e validados no backend de producao. O app deve usar o
catalogo e `category_id` e aceitar o legado apenas para ambientes cuja consulta
de catalogo retorne 404. Erros 401, 403 e de rede nao indicam catalogo ausente.
Validacao visual e uso em aparelho sao responsabilidade da verificacao do app;
o smoke HTTP do backend nao substitui esses testes.

O acesso principal a tela Patrimonio deve ser um botao flutuante na tela Inicio,
no canto inferior direito, acima da navegacao inferior, seguindo a referencia de
posicionamento dos botoes flutuantes do WhatsApp enviada pelo usuario. Usar as
cores e os componentes do SobraAi, icone que represente patrimonio e identificacao
acessivel "Patrimonio". Respeitar area segura, gestos e tamanho de toque, sem
encobrir os totais, os itens da tela ou a navegacao. Se ja existir um botao
flutuante nesse canto, organizar os atalhos verticalmente com espacamento,
preservando a funcao atual de cada um. O toque deve abrir diretamente a tela de
Patrimonio; voltar deve retornar ao Inicio preservando a navegacao existente.

Implementar no front-end os formularios e fluxos descritos nesta documentacao,
mantendo a consistencia Android/iOS conforme a estrutura do projeto. Considerar
concluida a entrega do front-end somente apos:

1. Validar abertura pelo botao flutuante, retorno e ausencia de sobreposicao em
   telas pequenas e nas areas seguras.
2. Validar consulta, cadastro, edicao e exclusao de FGTS/investimentos, dropdown de
   categorias, criacao/renomeacao/exclusao pessoal, protecao dos padroes, categoria
   em uso, saldo zero, formatacao monetaria, erros e persistencia ao reabrir.
3. Confirmar atualizacao dos totais e que o saldo mensal permanece inalterado.
4. Executar as verificacoes e o build Android previstos no repositorio do app.
5. Gerar um APK instalavel para teste e entregar ao usuario o arquivo com link
   clicavel para o caminho absoluto, indicando a variante gerada e como instalar.

O pedido inclui explicitamente a entrega do APK apos a implementacao. Nao basta
informar que o build passou ou fornecer apenas o codigo-fonte. Se houver bloqueio
real de build ou assinatura, informar a causa e o passo necessario para destravar,
sem declarar o APK entregue.

## Migration e publicacao

Migration `000008_add_investment_category_cdi`: insere CDI como categoria padrao
compartilhada e protegida, de forma idempotente. Mantem a migration 7 imutavel e
preserva categorias pessoais, IDs, associacoes e saldos existentes. A API continua
apenas validando migrations na inicializacao.

### Correcao CDI publicada em 26/10/2026

- Imagem ativa: `app-financeiro-api:investment-cdi-20261026`.
- Digest: `sha256:b554ffd28bd05929567aed15e57f47d90c69b237a67845429b45f9c94395417e`.
- Migration 8 aplicada as 12:34:32 de Sao Paulo; migrations anteriores preservadas.
- Catalogo autenticado com 15 padroes, incluindo CDI, confirmado no HTTP interno
  e em `https://137-131-247-103.nip.io`, endereco usado pelo APK.
- Backup completo restaurado em PostgreSQL descartavel com rede isolada;
  fingerprints de FGTS e investimentos identicos antes/depois da migration 8.
  Imagens release e fallback testadas sobre a copia restaurada.
- Harness completo com integracao aprovado na copia isolada dos fontes; smoke
  remoto aprovado para CDI por ID/texto, protecao 403, CRUD pessoal, isolamento,
  autorizacao, FGTS, saldos/datas e resumo mensal preservados.
- Fixtures e containers descartaveis removidos; backup integro, configuracao da
  API preservada e nenhum reinicio na auditoria. Banco e proxy nao reiniciados.
- Pacote: `.artifacts/investment-cdi-20261026.tar.gz`, SHA-256
  `a2add0b7cda179b7f1f030b3af5a4b81feb593adb0160da81ec9f7ef31d74733`.
- Recuperacao, backup, inspect privado e manifesto:
  `/home/ubuntu/app-financeiro/deploy-investment-cdi-20261026/`.
- Rollback compativel com schema 8:
  `sudo sh /home/ubuntu/app-financeiro/deploy-investment-cdi-20261026/rollback.sh`.
  Usa `app-financeiro-api:investment-cdi-rollback-20261026`, preservando schema,
  categorias, investimentos e escritas por ID/texto; desativa apenas as rotas do
  catalogo. As imagens anteriores que conhecem somente migration 7 recusam
  schema 8 ao iniciar; nao usar os rollbacks historicos abaixo nessa versao.

### Historico do catalogo inicial (migration 7)

Migration `000007_add_investment_categories`: cria o catalogo, insere 14 padroes,
acrescenta `investments.category_id` com FK e indice, e relaciona os textos legados
sem mudar nomes, valores, FGTS ou datas dos saldos. Categorias legadas nao presentes
nos padroes sao pessoais. A inicializacao da API continua apenas validando schema.
Validar upgrade a partir da migration 6 e manter clientes antigos usando `category`.

A publicacao desta evolucao foi autorizada e concluida em 26/10/2026. Procedimento:
preservar imagem/configuracao, realizar e testar backup, preparar uma imagem de
rollback compativel com migration 7 e com a resolucao de categorias nas escritas
legadas, executar `./migrate status` e `./migrate up` na imagem nova com a conexao
explicitamente escolhida, e trocar apenas a API. Verificar catalogo, protecoes,
CRUD pessoal e investimentos com `category_id` e `category` por HTTPS autenticado.
A imagem anterior de migration 6 recusa a migration 7 e nao preenche a nova coluna
obrigatoria; nao usa-la diretamente como rollback nem retirar registros de
migrations ou categorias para forcar uma reversao. Preferir imagem corrigida
compativel, preservando todas as categorias e investimentos. O rollback documentado
na publicacao inicial abaixo e historico e vale apenas para schema ate versao 6.

### Publicacao inicial das categorias em 26/10/2026 (schema 7)

- Imagem publicada nessa etapa: `app-financeiro-api:investment-categories-20261026`.
- Digest: `sha256:6733ee06f48bbb8b2fc39a4b8a401009a238cbb266e1c9f4e2a584f1846a578f`.
- Migration 7 aplicada as 12:15 de Sao Paulo; migrations 1 a 6 preservadas.
- Diagnostico anterior confirmado na VM: API/schema 6, catalogo 404 interno e
  publico, proxy apontando corretamente para a API. A pendencia era publicacao.
- Backup completo restaurado em PostgreSQL descartavel com rede isolada;
  fingerprints de FGTS e investimentos identicos antes/depois da migration.
- Imagens release e fallback validadas na copia restaurada: inicializacao,
  texto legado, `category_id`, FGTS, isolamento e resumo mensal preservados.
- Smoke autenticado aprovado em `http://127.0.0.1:8080` e
  `https://137-131-247-103.nip.io`: catalogo 200, 14 padroes para novas contas,
  CRUD pessoal, protecao dos padroes, duplicatas, categoria em uso, isolamento,
  contratos novo/legado, datas/saldos, zero, centavos e bloqueio de acesso.
- Usuarios e dados sinteticos removidos; containers descartaveis removidos.
  Configuracao, binds, redes, portas e politica de reinicio preservados. API
  ativa sem reinicios na auditoria; banco e proxy nao foram reiniciados.
- Recuperacao, inspect privado, backup e manifesto em
  `/home/ubuntu/app-financeiro/deploy-investment-categories-20261026/`.
- Backup: `rollback/database.dump`, com indice e SHA-256.
- Rollback compativel: `sudo sh /home/ubuntu/app-financeiro/deploy-investment-categories-20261026/rollback.sh`.
  Usa `app-financeiro-api:investment-categories-rollback-20261026`, mantem schema,
  dados e escritas legadas/por ID, desativando apenas as rotas de catalogo. Nao
  usar o rollback historico da publicacao inicial de patrimonio em schema 7.
- Testes locais: harness completo com integracao em copia isolada, ensaio Linux
  das duas imagens e sintaxe dos scripts aprovados. Rascunhos IA e compose locais
  preexistentes continuam preservados e fora da imagem, como documentado abaixo.

### Historico do schema inicial (migration 6)

Migration `000006_add_patrimony`: cria apenas `fgts_balances` e `investments`,
com FK de usuario, exclusao em cascata, restricoes de valor e indice de usuario.
Nao altera tabelas ou registros financeiros existentes. A API continua somente
validando migrations na inicializacao; executar `migrate up` explicitamente.

Antes do deploy: harness completo, testes HTTP com PostgreSQL descartavel,
preservacao da imagem/configuracao anterior, backup completo e restauracao de
teste. Publicar somente API, preservando banco, volumes, proxy e configuracao.

Rollback: o binario anterior recusa a migration 6 desconhecida. Portanto, guardar
tambem uma imagem de compatibilidade com o codigo anterior e o registro da
migration 6 (sem expor as rotas novas). Essa imagem permite rollback operacional
sem apagar saldos nem restaurar o banco. Nao remover a migration do registro nem
restaurar backup sobre dados novos para reverter apenas a API. Backup e recuperacao
de desastre, com parada de escritas e procedimento especifico.

Validacao: `./scripts/test.ps1` e `./scripts/test.ps1 -Integration` (Docker local).
Os cenarios verificam CRUD, isolamento entre usuarios, centavos, saldo zero,
validacoes, exclusao em cascata e invariancia do resumo mensal.

### Publicacao de 26/10/2026

- Imagem publicada: `app-financeiro-api:patrimony-20261026`.
- Migration 6 aplicada explicitamente; versoes 1 a 5 preservadas.
- Harness completo e integracao aprovados em copia isolada do commit `03f9699`
  com os arquivos desta entrega. A pasta original contem os arquivos preexistentes
  nao versionados `internal/assistant/expense_draft.go` e `expense_draft_test.go`,
  que conflitam com funcoes de `expense_create.go` recebido pelo pull. Esses
  arquivos e as alteracoes locais de `docker-compose.yml` foram preservados e nao
  foram incluidos na imagem publicada. O harness na pasta original continua
  dependendo da conciliacao desse trabalho preexistente.
- Backup completo restaurado com sucesso em PostgreSQL descartavel e isolado na
  VM; migration e inicializacao das imagens nova e de rollback validadas nessa
  copia. Containers descartaveis removidos ao final.
- Smoke autenticado aprovado por HTTP interno e HTTPS publico: cadastro, consulta,
  edicao, exclusao, saldo zero, centavos, autorizacao, bloqueio de acesso e saldo
  mensal preservado. Usuarios e registros temporarios removidos.
- Imagem anterior, configuracao efetiva, backup e procedimentos preservados em
  `/home/ubuntu/app-financeiro/deploy-patrimony-20261026/`.
- Backup: `rollback/database.dump`, com indice e SHA-256 nesse diretorio.
- Rollback operacional, sem apagar patrimonio ou restaurar banco:
  `sudo sh /home/ubuntu/app-financeiro/deploy-patrimony-20261026/rollback.sh`.
  Usa `app-financeiro-api:patrimony-rollback-20261026`, testada com migration 6.
- Manifesto com hashes dos fontes e exclusoes preexistentes:
  `patrimony-release-manifest.json`, no mesmo diretorio da publicacao.
