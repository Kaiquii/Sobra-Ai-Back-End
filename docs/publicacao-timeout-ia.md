# Publicacao da correcao de timeout da IA

Publicada em 03/11/2026 as 09:11 de Sao Paulo, com autorizacao explicita para
commit, push e troca da API. Verificacao final as 09:11: API ativa, sem reinicios
e binario conferido por SHA256. O usuario ficou de testar a pergunta sobre
planejamento para comprar um carro; nao houve inferencia real nesta publicacao.

- Codigo: `d745f55593990c2c6937792f92a45ad9c1b56030`.
- Imagem: `app-financeiro-api:assistant-timeout-20261103`.
- Digest: `sha256:0a449a7fcc655277bb570799557406e56af021aad3b0437f77e2a38a3bf66490`.
- SHA256 da API: `783ea5d6209c1a8e2adb84f9ca71935609b43bca5e1bec07738bb57c923ed1c0`.
- Schema 9 preservado; nenhuma migration aplicada.

## Comportamento

O limite HTTP passa de 18s para os 45s ja previstos na geracao, respeitando o
contexto pai. Timeout de transporte/deadline recebe `assistant_request_timeout`,
status interno 504 e retry de 15s, com mensagem de demora. Nao pede ao usuario
que reformule a pergunta por causa de uma interrupcao de transporte.

Cancelamento real e JSON realmente invalido continuam distintos. Status
explicitos do provedor, como 429, permanecem classificados corretamente.
Nao ha conserto de resposta parcial, retry ou segunda completion. O patch
preserva modelo, prompt, snapshot, politica de raciocinio, preco zero, privacidade
e teto de 2048 tokens. Detalhes em [tempo de resposta](assistant-tempo-resposta.md).

## Evidencias e limites

Reproducao local antes do ajuste: cabecalhos HTTP 200 e corpo atrasado eram
classificados como resposta invalida ao expirar a leitura. Depois do ajuste,
testes verificam headers/corpo/deadline/cancelamento, cota, JSON invalido,
mensagem publica, privacidade e uma unica tentativa. Os testes usam HTTP local.

Harness `scripts/test.ps1 -Integration` aprovado: formatacao, vet, build, testes
e PostgreSQL descartavel. Cobertura geral 48,6%; assistente 65,1%; integracao 43,2%.
Os seis arquivos do patch e o log foram conferidos contra a auditoria por hash.
Compose privado, credenciais e caches preexistentes ficaram fora do commit/pacote.

Os logs de producao consultados antes da troca mostravam HTTP 200 e falha de
decode aos 18s, compativeis com o bug reproduzido. O corpo real nao foi coletado;
isso nao comprova que o modelo ja tinha uma resposta correta. A qualificacao
semantica da pergunta sobre o carro continua pendente do teste do usuario.

## Producao e recuperacao

Backup privado restaurado em PostgreSQL isolado sem rede externa. Imagens nova
e anterior passaram por startup, schema, compatibilidade e escritas isoladas;
fingerprints financeiros restaurados permaneceram iguais. Containers temporarios
removidos antes da promocao. Nao foram criadas fixtures no banco de producao.

A troca afetou somente a API. Vinte verificacoes GET em HTTP interno e HTTPS
publico aprovaram relatorio, rendas, despesas, conversas e preferencia de modelo:
401 sem JWT e 200 com JWT. Nenhuma chamada de IA, preferencia alterada ou migration.
Ambiente, configuracoes, portas, volumes, redes e politica de reinicio preservados.
O banco manteve identidade, inicio, configuracao e contador de reinicios.

Diretorio privado: `/home/ubuntu/app-financeiro/deploy-assistant-timeout-20261103`.
Backup, snapshots e evidencias permanecem fora do Git. A promocao tinha retorno
automatico para a API anterior caso smoke ou auditoria falhassem.

Rollback somente da API:

```sh
sudo sh /home/ubuntu/app-financeiro/deploy-assistant-timeout-20261103/rollback.sh
```

Restaura `app-financeiro-api:before-assistant-timeout-20261103`, digest
`sha256:b2ffb5af6d8f54a2273720d298390a24c76f1b6c8bbbafdc5ffd6fdd35ddeae2`,
com o ambiente e a configuracao anteriores. Nao restaurar dump sobre transacoes
novas para reverter codigo; nao houve alteracao de schema.
