# Publicação das preferências de VA e VR — 04/11/2026

O usuário autorizou implementar, validar, fazer commit/push na main e publicar
na VM com backup e rollback, além de enviar handoffs à web e Android. A
localização dos controles deve ser discutida com ele antes de implementar UI.

## Versão publicada

- Fonte: `ad3d2d853a18c47a951bc8d08487804a5a766e61`.
- Imagem: `app-financeiro-api:benefit-balance-preferences-20261104`.
- Digest: `sha256:0168ae3e951e592db56de4dc434a01406058a8271c7d837f8ae9d96adc87ca64`.
- SHA256 de `main`: `cc6ad5f5ab367193224527deb05a4265c0a562a99f0249d73b9f5603fbe6d83d`.
- Pacote SHA256: `6d6b833532752b0ae65c734bc52093b70786c2cec878d506ebbbc413654cf40f`.
- API iniciada em `2026-11-04T17:46:58Z`, verificada em
  `2026-11-04T17:47:17Z` (14:47 de São Paulo), ativa e com zero reinícios.

O pacote foi compilado do commit aprovado e conferido contra o freeze dos
165 arquivos Go/módulo. Compose, ambiente e credenciais não entraram no commit
nem no pacote. Variáveis, redes, volumes e demais configurações foram
preservados integralmente, incluindo a configuração da OpenRouter existente.

## Migration e validação

Foi aplicada somente a migration 10 `add_user_balance_preferences`, checksum
`8f310d0c1fbad8507564d121cd70b17598a66757e2a75866432d31d0af7daa25`.
Ela cria uma tabela de preferências por usuário, sem escrever em tabelas
financeiras ou reescrever lançamentos. Ambas as flags são `NOT NULL DEFAULT
false`; leitura sem registro retorna `false`/`false` sem criar linha. O
registro das migrations 1–9 permaneceu intacto. O container do banco não foi
substituído nem reiniciado.

O harness completo `scripts/test.ps1 -Integration` passou: formatação, vet,
build, unidades e integração local com PostgreSQL descartável. Cobertura
unitária geral 50,7%; integração 45,9% dos pacotes internos/API. As simulações
antigas de descoberta ZDR e fallback foram alinhadas ao comportamento já
publicado, sem alteração do código ou da política de IA em produção.

Antes de aplicar a migration em produção, um `pg_dump` privado foi restaurado
em um PostgreSQL isolado, sem rede externa. Foram verificados a API original
no schema 9, o upgrade 9→10, a API nova e a imagem de recuperação compatível no
schema 10. Os fingerprints financeiros e o registro legado foram preservados
no clone; preferências padrão, GET sem criar linha, PATCH parcial e isolamento
de usuário também passaram. Os usuários/lançamentos de teste foram removidos
somente do clone.

Depois da ativação, passaram **24 GETs autenticados/não autenticados** por HTTP
interno e HTTPS público: resumo, rendas, despesas, conversas, preferência de
modelo e as novas preferências de saldo. A aceitação não enviou POST/PATCH,
não criou fixtures nem alterou preferências em produção. Ela valida status e
contrato; não exige igualdade entre snapshots de dados de usuários enquanto
o aplicativo continua recebendo uso legítimo. A prova de invariância pertence
ao clone, à integração e ao escopo aditivo do SQL.

**Zero novas inferências reais** foram feitas na implementação, validação e
publicação. Os testes de IA utilizaram apenas respostas simuladas.

## Backup e rollback compatível

Evidências, snapshots e backup privados na VM:
`/home/ubuntu/app-financeiro/deploy-benefit-balance-preferences-20261104`.
Snapshots e dump permanecem privados; não devem ser copiados ao Git ou handoffs.

A API original conhece somente schema 9 e rejeita o registro 10 ao iniciar.
Por isso, a recuperação após a migration utiliza:

- Imagem: `app-financeiro-api:benefit-balance-preferences-rollback-20261104`.
- Digest: `sha256:788a6d89ffb74b54d3540429302821eec9cfd248906da1ce1a907669166cc947`.
- SHA256 de `main`: `e0d13c9be5cbb231883cee751a50bb5a8fdf17403e8df2ea394ecd08ef3e61be`.
- Fonte anterior: `0e6e56d1963f8b3ab8b58ff75196db92ee7ba25e`, acrescida
  exclusivamente de `internal/migrations/user_balance_preferences.go` e
  da linha de registro da migration 10 em `internal/migrations/migrations.go`.

Essa imagem foi verificada no clone atualizado e retorna ao comportamento
anterior sem apagar escolhas, remover migrations ou restaurar o banco. Durante
esse rollback, as rotas novas deixam de existir e o disponível volta à regra
anterior; a tabela de preferências fica preservada para uma publicação futura.
Uma falha de promoção antes da migration usa a imagem original exata no
schema 9; depois dela usa a imagem compatível no schema 10.

O script de promoção contém recuperação automática em falha. Para rollback
manual futuro, obter autorização específica e executar:

```sh
sudo sh /home/ubuntu/app-financeiro/deploy-benefit-balance-preferences-20261104/rollback.sh
```

Não há downgrade ou restauração automática do banco. O backup foi restaurado
e validado no ambiente isolado antes da publicação.
