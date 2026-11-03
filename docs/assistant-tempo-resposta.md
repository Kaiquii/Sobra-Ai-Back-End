# Tempo de resposta do provedor

Correcao local de 03/11/2026; ainda nao publicada nem avaliada com inferencia real.

## Diagnostico

Uma consulta autorizada somente da versao e dos logs tecnicos da VM encontrou
`app-financeiro-api:reasoning-20261102`, digest
`sha256:b2ffb5af6d8f54a2273720d298390a24c76f1b6c8bbbafdc5ffd6fdd35ddeae2`,
ativa e sem reinicios. Portanto o registro anterior de checkpoint sem deploy nao
descreve sozinho o estado observado. O nome da imagem nao comprova seu commit.

Os logs mostraram duas falhas de `assistant_intent`: HTTP 200 apos 18,002s e
18s, seguidos de `response_decode_failed`, em 02/11 as 00:36 e 03/11 as 08:41
de Sao Paulo. Nao foram consultados mensagens, credenciais ou dados do banco.
Nao foi enviada nenhuma completion. Corpo e erro de transporte brutos nao foram
coletados; os tempos sao compativeis com o timeout HTTP de 18s existente no codigo.

A reproducao local envia cabecalhos HTTP 200 e atrasa o restante do JSON. Antes
da correcao, a expiracao da leitura virava `assistant_invalid_response`, igual
a JSON realmente invalido. Isso explica como uma demora pode ser apresentada
como falha de entendimento; nao comprova que o modelo tinha uma resposta correta.

## Correcao

O cliente HTTP usa agora o mesmo limite de 45s ja aplicado a geracao. O prazo
efetivo respeita tambem o contexto pai do chat, de 55s; nao ha espera ilimitada.
O timeout HTTP cobre inclusive a leitura do corpo, depois dos cabecalhos 200.

Timeout de transporte/deadline retorna `assistant_request_timeout`, status
interno 504 e `retry_after_seconds=15`. A resposta do chat continua no contrato
existente, com `error_code`, `reply` e `retry_after_seconds`. O texto informa
demora sem pedir para reformular a pergunta. Cancelamento real permanece
`assistant_request_cancelled`; JSON realmente invalido permanece
`assistant_invalid_response`. Status explicitos de erro, como 429, conservam
a classificacao do provedor mesmo se a leitura de seu corpo falhar.

Nao ha conserto de JSON parcial, segunda completion ou retry automatico. Timeout
encerra a geracao; outro cliente na mesma mensagem tambem nao ganha uma tentativa.
Logs usam somente fase e motivo fechados, sem corpo ou mensagem bruta do erro.
Confirmacao, valores financeiros, prompt, snapshot, politica de raciocinio,
modelo recomendado, preco zero e teto de 2048 tokens nao foram alterados.

## Limites da verificacao

Testes HTTP locais reproduzem a falha e verificam timeout de cabecalhos/corpo,
cancelamento, JSON invalido, status do provedor, privacidade e uma unica tentativa.
O harness `scripts/test.ps1 -Integration` passou: formatacao, vet, build, testes
com cobertura e integracao em PostgreSQL descartavel. Cobertura geral 48,6%;
assistente 65,1%; integracao 43,2%. O primeiro ensaio encontrou Docker Desktop
local desligado; apos inicia-lo, o harness completo passou. Nenhum teste foi
movido para a VM de producao.
Eles nao qualificam o raciocinio sobre compra de carro, dividas ou outros objetivos.
Esse entendimento continua dependendo de avaliacao real separada e autorizada.
Uma resposta que ultrapasse os 45s ainda falhara, com a causa adequada.
