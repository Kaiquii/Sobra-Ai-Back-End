# Referencia mensal de categoria

O pedido de teto de gasto era interpretado como budget, mas a resposta local
mostrava media/maximo e devolvia ao usuario a tarefa de escolher um teto.
Interpretacao correta nao garantia que a resposta atendesse ao pedido.

Com ao menos dois meses encerrados com registros da categoria no escopo,
a resposta agora propoe a media mensal observada como teto inicial de
planejamento para manter o padrao historico. Calcula em centavos e arredonda
para o centavo mais proximo. Meses sem registros nao viram gasto zero;
o mes corrente e os futuros nao entram nessa media.

O resultado acrescenta suggested_monthly_target e
target_basis=completed_registered_month_average, com
target_is_affordability_limit=false e target_saved=false.
Campos existentes permanecem compativeis. Nao ha migration nem escrita.
Filtros de usuario, categoria, periodo e fonte continuam obrigatorios.

A proposta nao e um calculo de capacidade financeira: nao demonstra que o
valor cabe no saldo atual, que a renda foi recebida ou que despesas essenciais
estao completas. VA/VR permanecem separados do dinheiro. A resposta explicita
a base historica, oferece ajuste conforme o objetivo e nao afirma salvar meta.
Sem historico suficiente, pede mais historico ou um objetivo informado, sem
inventar um valor. Nao aplica percentuais arbitrarios de reducao.

Esta correcao resolve a omissao reproduzivel da resposta de planejamento.
Nao comprova entendimento de toda pergunta nem qualifica um modelo sozinho.
As variacoes do corpus v2 precisam de avaliacao real separada, com orcamento
total previamente limitado; o harness local nunca gasta inferencias reais.

## Publicacao autorizada

Publicado em 01/11/2026 as 23:41 de Sao Paulo.
Imagem: app-financeiro-api:planning-20261101b.
Digest: sha256:d3e98c9f513ad37ab70cdf7b2d0b2b6229a618e41e763841884733d94235bcd7.
SHA256 do binario: 7bfbace5b8a63f69d319ef6a932360f3f71d4b4c9aaa965394a9b0d76ca80032.

Harness completo aprovado: formatacao, vet, build e testes; cobertura geral
47,8%, assistente 63,7%. O teste offline de estrutura do corpus v2 passou.
Nenhuma chamada real ao modelo foi feita nesta publicacao. O resultado antigo
de 6/6 nao qualifica os 18 casos novos e a mudanca nao promete acerto universal.

Backup privado restaurado em PostgreSQL descartavel, sem rede, removido ao
final. As nove migrations continuam aplicadas; executado somente status.
Ambiente efetivo, mounts, portas, redes e politica de reinicio foram comparados
antes/depois. HTTPS e leituras autenticadas de relatorios e conversas passaram.
API ativa, sem reinicios apos a troca. O docker-compose.yml local preexistente
foi preservado e nao faz parte do commit nem foi enviado para a VM.

Diretorio privado: /home/ubuntu/app-financeiro/deploy-planning-20261101b.
Backup: rollback/database.dump, modo 600. Imagem anterior preservada:
app-financeiro-api:before-planning-20261101b,
sha256:c8ecb26a7796c4557133808ae79e90a23237107ead0e753c73d4e253a48b7c47.

Rollback de codigo preservando a configuracao, sem sobrescrever transacoes:

```sh
sudo bash /home/ubuntu/app-financeiro/deploy-planning-20261101b/rollback.sh
```
