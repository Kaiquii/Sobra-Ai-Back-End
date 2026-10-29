# Publicacao de intencoes e orientacao da IA

Publicada em 29/10/2026 as 14:02 de Sao Paulo. A autorizacao do usuario condicionou
commit, push e publicacao a dois testes reais adicionais aprovados. Ambos passaram.
Verificacao final as 14:02: API ativa, zero reinicios e binario conferido por hash.

- Codigo: `72f8e90a870d5f427dab0b770577fa73d3df8e72`.
- Imagem: `app-financeiro-api:assistant-actions-20261029`.
- Digest: `sha256:f5b402a50ca746eea056fe90efe7479115fa3700804e6d7276f13813fe40e440`.
- SHA256 do binario API: `4e6f673f1876db81985fcc6faa0d72396786b6871a56342c4cafbfc34017efac`.
- Schema 9 preservado; nenhuma migration aplicada.

## Comportamento

O interpretador recebe categorias autenticadas e usa formatos exclusivos para
consulta, cadastro e conversa quando o modelo oferece schema nativo. Consultas
nao viram propostas de cadastro por campos contraditorios. O Go resolve filtros
e calcula valores; o snapshot anual nao e mais enviado antes da interpretacao.

Segundo maior devolve somente a segunda posicao. Pedidos de dois meses preservam
o detalhamento de cada mes. Abreviacoes como Uber resolvem Uber/99 apenas quando
identificam uma unica categoria do dono. Orientacoes sobre orcamento usam meses
encerrados com registros e declaram lacunas; nao inventam um limite seguro.
Perguntas recusadas nao sao substituidas por um resumo anual de outro escopo.

Confirmacao local exige proposta no ultimo turno, preservando transacao e
idempotencia. Um sim depois de ajuda ou consulta continua a conversa e nao
ressuscita um rascunho antigo. Contratos HTTP do aplicativo permanecem compativeis.
Detalhes em [consultas estruturadas](assistant-consultas-estruturadas.md).

## Validacao e consumo

Harness completo na pasta original com `scripts/test.ps1 -Integration` aprovado:
formatacao, vet, build, testes e PostgreSQL descartavel. Cobertura geral 47,3%;
assistente 62,8%; integracao 42,5%. Hashes das fontes Go testadas conferidos antes
do commit e do pacote. Compose privado e caches preexistentes ficaram fora.

O modelo gratuito atual passou em tres pedidos sinteticos com o formato final:
segundo maior parcelado, planejamento de Uber/99 e maior mes anual da categoria.
Cada pedido usou uma completion. No planejamento, o Go calculou media de R$ 80,00
e maior valor historico de R$ 100,00, excluindo mes atual e futuro da referencia.
Na consulta anual, novembro tinha o maior valor cadastrado, R$ 200,00, com aviso
de que lancamentos futuros nao comprovam pagamento.

A pergunta de dois meses foi validada por replay offline da captura anterior,
descartando o valor inventado pelo modelo e calculando cada mes corretamente.
Nao houve outra chamada real desse caso com o formato final. O total acumulado
de avaliacao foi 16 tentativas autorizadas, incluindo falhas anteriores e um
HTTP 429 de outro modelo. Isso aprova os casos descritos, sem provar que todos
os modelos gratuitos entendem qualquer formulacao.

Permanecem uma completion por mensagem, zero na confirmacao real, teto de saida
2048 e somente modelos com preco zero e privacidade deny. O limite de contexto
inclui o wrapper do schema. Nenhuma chamada de IA foi feita durante a publicacao.

## Producao e recuperacao

Backup privado restaurado em PostgreSQL isolado sem rede externa. Imagens nova
e anterior aprovadas em startup, schema, compatibilidade e escritas isoladas;
fingerprints financeiros restaurados permaneceram iguais ao final. Containers
temporarios removidos antes da publicacao.

A troca afetou somente a API. Vinte verificacoes GET por HTTP interno e HTTPS
publico aprovaram relatorio, rendas, despesas, conversas e preferencia de modelo:
401 sem JWT e 200 com JWT. Sem fixtures em producao ou alteracao da preferencia.
Ambiente, configuracao, portas, volumes, redes e politica de reinicio preservados.
O banco manteve identidade, inicio, configuracao e contador de reinicios.

Diretorio privado: `/home/ubuntu/app-financeiro/deploy-assistant-actions-20261029`.
Backup, snapshots e evidencias permanecem nesse diretorio, fora do Git. A promocao
teve rollback automatico preparado caso a verificacao ou auditoria falhasse.

Rollback somente da API:

```sh
sudo sh /home/ubuntu/app-financeiro/deploy-assistant-actions-20261029/rollback.sh
```

Restaura `app-financeiro-api:before-assistant-actions-20261029`, digest
`sha256:40060dcdfe8bd7d05e1b9f7c89d14df8de01416e8db0c798022ee9ddb7b8d13a`,
com o ambiente e a configuracao anteriores. Nao restaurar o dump sobre novas
transacoes para reverter codigo; esta publicacao nao alterou schema.
