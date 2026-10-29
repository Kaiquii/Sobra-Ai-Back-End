# Publicacao de consultas e contexto da IA

Publicada em 29/10/2026 as 11:30 de Sao Paulo, com autorizacao explicita do usuario.
Verificacao final as 11:32: API ativa, sem reinicios, HTTP e HTTPS aprovados.

- Codigo: `3bf212d562a69df5054418b345b09a93ed3f050f`.
- Imagem: `app-financeiro-api:assistant-semantics-20261029`.
- Digest: `sha256:40060dcdfe8bd7d05e1b9f7c89d14df8de01416e8db0c798022ee9ddb7b8d13a`.
- SHA256 do binario API: `e1c36824a36bcdf8ba7e8c12ea133d6ef38733f95f10cdb173ba19040d96c404`.
- Schema 9 preservado; nenhuma migration aplicada.

## Comportamento

Consultas de despesas usam planos fechados de periodos, categorias, agrupamentos
e operacoes. O Go faz filtros e calculos autenticados; o historico preserva o
pedido do usuario e compacta resultados antigos. Parcelas e totais de compras
sao distintos. Correcao de rascunho respeita `provided_fields`.

Duplicacoes foram consolidadas: os helpers puros de rascunho/parcelamento em
`expense_draft.go` sao usados por `expense_create.go`, sem excluir arquivos dos
testes ou da release. Confirmacao permanece local e transacional.

O usuario escolheu elevar o teto de saida de 1024 para 2048 tokens. Continuam
uma completion por mensagem, zero na confirmacao e modelos gratuitos, com teto
de preco zero e preferencias de privacidade. Raciocinio interno e desativado
na interpretacao apenas quando o catalogo declara suporte e nao obrigatoriedade.

## Validacao

Harness completo executado na pasta original com `scripts/test.ps1 -Integration`:
formatacao, vet, build, testes e PostgreSQL descartavel aprovados. Cobertura geral
44,6%; integracao 41,8%. Fontes e binarios conferidos por SHA256; nenhum segredo
incluido no commit/pacote. Compose privado e cache preexistentes ficaram fora.

A unica chamada real adicional autorizada usou dados sinteticos e levou o total
da avaliacao a dez. O modelo terminou em 351 tokens, sem reasoning, mas a
recomendacao livre continha percentuais/valores derivados sem calculos verificaveis.
O backend recusou esse texto e entregou corretamente o resumo local verificado.
O fluxo protegido foi aprovado; isso nao aprova a recomendacao livre do modelo.
Detalhes em [consultas estruturadas](assistant-consultas-estruturadas.md).

Antes da troca, backup privado restaurado em PostgreSQL isolado, sem rede externa.
Imagens nova e anterior passaram por startup, schema e verificacoes de cadastro,
compatibilidade e isolamento; fingerprints financeiros restaurados permaneceram
iguais. Containers de verificacao removidos ao terminar.

Apos a troca, vinte verificacoes GET em HTTP interno e HTTPS publico cobriram
relatorio, rendas, despesas, conversas e preferencia do modelo: 401 sem JWT e 200
com JWT. Nenhuma fixture criada em producao e nenhuma chamada de IA no deploy.
Ambiente, configuracoes, volumes, portas, redes e politica de reinicio preservados.
O container do banco manteve identidade, inicio, configuracao e contador de reinicios.

## Recuperacao

Diretorio privado: `/home/ubuntu/app-financeiro/deploy-assistant-semantics-20261029`.
Backup, snapshots de configuracao e evidencias ficam nesse diretorio, fora do Git.

Rollback somente da API:

```sh
sudo sh /home/ubuntu/app-financeiro/deploy-assistant-semantics-20261029/rollback.sh
```

Restaura `app-financeiro-api:before-assistant-semantics-20261029`, digest
`sha256:f44135021789b76aba713658c037c79ecc25efff9c49023cfb8961fd599e3e27`,
com o ambiente/configuracao preservados. Nao restaurar dump sobre novas transacoes
para reverter codigo: esta publicacao nao alterou schema.
