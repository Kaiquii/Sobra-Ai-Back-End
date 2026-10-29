# Publicacao de compatibilidade e escopo da IA

Publicada em 29/10/2026 as 14:44 de Sao Paulo. Verificacao final as 14:45: API
ativa, zero reinicios e binario conferido por SHA256. O usuario autorizou commit,
push e publicacao, deixando a interpretacao pelo modelo real para seu teste apos
a renovacao da cota. Essa verificacao continua pendente; nao foi alegada aprovacao
do modelo real para esta revisao.

- Codigo: `adcb81e339a939117cb3373118ad9e78e380f7dd`.
- Imagem: `app-financeiro-api:assistant-query-compat-20261029`.
- Digest: `sha256:f31f72a101ed89b46904285753361a3a22b30485cb9281929f6dac11d21deeb7`.
- SHA256 do binario API: `4a15f813cbcce425c98ea1ae4756b2a8b82eac322697d1db3f1293526cc54bc6`.
- Schema 9 preservado; nenhuma migration aplicada.

## Ajustes

Consultas `financial_query` nos formatos JSON object/texto recebem a mesma
normalizacao de controles opcionais do formato nativo. Limit/position nulos e
ordinal inaplicavel nao fazem uma consulta equivalente falhar apenas pela troca
de modelo. Campos obrigatorios, filtros, periodos, isolamento e mascaras de
cadastro continuam estritos. A adaptacao adicional so vale para consultas;
cadastros em JSON object/texto mantem seu tratamento anterior.

As instrucoes de prompt e schema distinguem consulta global de continuidade,
separam expansao de periodo de ranking e orientam a executar uma consulta clara
em vez de apenas prometer analise. Rejeicoes de consulta registram somente um
motivo fechado, sem dados do pedido, valores ou erros brutos.

Detalhes e limites em [consultas estruturadas](assistant-consultas-estruturadas.md).

## Validacao e consumo

Harness completo aprovado na pasta original com `scripts/test.ps1 -Integration`:
formatacao, vet, build, regressao e PostgreSQL descartavel. Cobertura geral 47,4%;
assistente 62,9%; integracao 43,0%. Regressao de paridade entre tres formatos,
inclusive orcamento, preserva filtros, isolamento e autoridade de correcao de
rascunho. Hashes das nove fontes/documentacao conferidos com a auditoria local.
Compose privado, credenciais e caches preexistentes ficaram fora do commit/pacote.

Nao houve nova completion nesta revisao ou durante o deploy. Testes com HTTP
simulado comprovam comportamento do protocolo e do backend; nao comprovam a
interpretacao semantica do modelo. O manifesto registra explicitamente a
validacao real pendente por decisao do usuario. Os testes reais de releases
anteriores nao foram reutilizados como aprovacao do prompt atual.

Permanecem uma completion por mensagem, zero na confirmacao reconhecida, teto de
saida 2048, modelos gratuitos, preco zero e privacidade deny. Limites de cota e
orcamento fixo de entrada mantidos. Nenhuma preferencia de modelo foi alterada.

## Producao e recuperacao

Backup privado restaurado em PostgreSQL isolado sem rede externa. Imagens atual
e nova passaram por startup, schema e verificacoes de compatibilidade/escritas
isoladas; fingerprints financeiros restaurados permaneceram iguais. Containers
temporarios foram removidos antes da troca.

A publicacao trocou somente a API. Vinte verificacoes GET em HTTP interno e
HTTPS publico aprovaram relatorio, rendas, despesas, conversas e preferencia:
401 sem JWT e 200 com JWT. Nenhuma fixture criada em producao ou chamada de IA.
Ambiente, configuracoes, portas, volumes, redes e politica de reinicio preservados.
O banco manteve identidade, inicio, configuracao e contador de reinicios.

Diretorio privado:
`/home/ubuntu/app-financeiro/deploy-assistant-query-compat-20261029`.
Backup, snapshots e evidencias permanecem nesse diretorio, fora do Git. A promocao
tinha rollback automatico preparado caso a verificacao ou auditoria falhasse.

Rollback somente da API:

```sh
sudo sh /home/ubuntu/app-financeiro/deploy-assistant-query-compat-20261029/rollback.sh
```

Restaura `app-financeiro-api:before-assistant-query-compat-20261029`, digest
`sha256:f5b402a50ca746eea056fe90efe7479115fa3700804e6d7276f13813fe40e440`,
com o ambiente/configuracao anteriores. Nao restaurar dump sobre transacoes novas
para reverter codigo; esta publicacao nao alterou schema.
