# Harness De Agentes

Este arquivo define o contrato de trabalho para qualquer agente que altere este repositorio.

## Objetivo Do Projeto

O SobraAi Back-End e uma API HTTP em Go para autenticacao, usuarios, despesas, rendas,
categorias, relatorios, assistente de IA, versao do aplicativo e fotos de perfil.

## Contexto Obrigatorio

Antes de alterar codigo:

1. Leia `docs/rules.md`. As restricoes de VM, producao, migrations e conclusao sao obrigatorias.
2. Leia os arquivos diretamente envolvidos e seus testes antes de propor a mudanca.
3. Verifique `git status --short` e preserve mudancas que ja pertencem ao usuario.
4. Consulte `.agents/commands.md` para usar os comandos canonicos do repositorio.

## Mapa Do Repositorio

- `cmd/api`: inicializacao da API HTTP.
- `cmd/migrate`: CLI de migrations (`status`, `up` e `baseline`).
- `internal/<dominio>`: handlers, modelos e regras de negocio por dominio.
- `internal/migrations`: registro imutavel e execucao das migrations.
- `docs`: regras operacionais e documentacao funcional.
- `scripts/test.ps1` e `scripts/test.sh`: entrada oficial do harness de testes.
- `cmd/harness`: executor local implementacao -> testes -> revisao, com tentativas limitadas.
- `docs/harness/README.md`: operacao, perfis e resultados do harness executavel.

Quando estiver sendo executado pelo harness, nao inicie outro harness de agentes.

## Regras De Implementacao

- Mantenha alteracoes pequenas e dentro do pedido do usuario.
- Preserve a organizacao por dominio existente; nao crie abstracoes compartilhadas sem uso real.
- Use `gofmt` em todo arquivo Go alterado.
- Nao registre segredos, tokens, credenciais ou arquivos `.env`.
- Nunca edite uma migration ja aplicada. Crie uma nova versao e mantenha o registro ordenado.
- A inicializacao da API apenas valida migrations. Alteracoes de schema pertencem ao comando
  `migrate up`.
- Mudancas em valores, pagamentos ou listas financeiras devem manter totais consistentes e ser
  transacionais quando envolverem mais de uma escrita.
- Preserve contratos JSON e compatibilidade com dados existentes, salvo mudanca explicitamente
  solicitada e documentada.
- Nao acesse nem altere VM, producao, banco remoto ou deploy sem autorizacao explicita para essa
  operacao especifica.

## Estrategia De Testes

- Coloque testes junto ao pacote, em arquivos `*_test.go`.
- Prefira testes de tabela para regras com varias entradas.
- Use `httptest` para handlers e clientes HTTP; testes unitarios nao devem depender da internet.
- Um bug corrigido deve receber um teste de regressao quando for praticavel.
- Para iterar rapidamente, rode o pacote alterado com `go test ./internal/<pacote>`.
- Antes de concluir, rode o harness completo descrito em `.agents/commands.md`.

## Criterio De Conclusao

Uma tarefa so esta concluida quando:

1. O pedido foi implementado sem sobrescrever trabalho preexistente.
2. Casos de erro, autorizacao e compatibilidade relevantes foram considerados.
3. Testes novos ou atualizados cobrem a mudanca quando aplicavel.
4. O harness completo passa, ou a limitacao e relatada claramente.
5. A documentacao foi atualizada quando contrato, operacao ou configuracao mudou.
6. O diff final foi revisado para segredos, alteracoes acidentais e escopo indevido.

Use `.agents/review-checklist.md` na revisao final.
