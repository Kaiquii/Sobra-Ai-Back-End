# Confiabilidade e continuidade do assistente

Estado: contrato final QUALIFICADO nos seis cenarios reais em 01/11/2026.
Registro de publicacao: [publicacao](publicacao-qualificacao-ia.md).

## Avaliacao real em 01/11/2026

Foram autorizadas e usadas exatamente 12 completions com dados ficticios,
seis no prompt de b4b33d3 e seis na revisao, mesmo corpus e mesmo modelo
`dots-studio/dots-3-note-preview:free`. Nenhuma escrita ou publicacao na VM.
Chave lida somente em memoria, sem exposicao. Windows bloqueou o executavel
opt-in; a avaliacao foi executada no Linux local (WSL), sem alterar politicas.

| Caso | Anterior | Revisao |
| --- | --- | --- |
| Orcamento anual de Uber | Falhou | Falhou |
| Saldo apos consulta de despesas | Falhou | Passou |
| Cadastro incompleto de chocolate | Falhou | Falhou |
| Continuacao: unica, comprei hoje | Falhou | Falhou |
| Orientacao global apos Uber | Falhou | Falhou |
| Cancelar rascunho | Passou | Passou |

Todas receberam HTTP 200. Resultado: 1/6 contra 2/6, insuficiente para aprovar.
Nao atribuir automaticamente todas as falhas ao provedor: a avaliacao mede o
conjunto modelo, prompt e protocolo; nao prova que outro modelo resolveria.
O corpus preserva falas do video. As expectativas nao foram afrouxadas depois
dos resultados. Artefatos detalhados ficam em `.artifacts/eval-*-20261101.json`.
Nao houve novas tentativas apos consumir o orcamento autorizado.

O usuario autorizou em seguida continuar a correcao e publicar. A investigacao
posterior usou somente modelos gratuitos e confirmou diferenca entre formatos:
o mesmo modelo/casos de cadastro falhou com schema nativo e passou com JSON
object. Isso nao isola o unico fator causal de todas as respostas anteriores.
O contrato final combina formato qualificado, contexto explicito, demonstracao
de rascunho incompleto e equivalencia tipada de orientacao mensal de categoria.
O validador Go permanece obrigatorio, sem permitir SQL, categorias alheias,
totais do modelo ou gravacao sem confirmacao.

Rodada final real: 6/6 aprovados, uma completion por mensagem, 1,3 a 5,2 segundos.
Consumo da rodada final: 9.334 tokens (entrada e saida), custo monetario zero.
O historico completo de investigacao tem 51 tentativas POST, incluindo 12 da
primeira autorizacao e indisponibilidades 404/429/503, 59.316 tokens reportados.
Esses totais sao da investigacao, nao uma meta de consumo por conversa.
Nenhum credito foi comprado, nenhuma chave foi trocada e nenhuma cota contornada.

Mais instrucoes sozinhas aumentaram tokens sem ganho suficiente de qualidade.
A primeira revisao nao qualificou deploy. A investigacao posterior comparou candidatos e
formatos com o mesmo corpus, aprovar fluxos criticos completos e medir latencia,
erros e consumo; escolher por resultado, nao por disponibilidade/ordem do catalogo.
Nao mudar o modelo recomendado com base em suposicao nem chamar mocks de prova
de entendimento. Manter a confirmacao financeira transacional independentemente
do modelo escolhido. Nao ha garantia de acerto universal.

O video de 31/10 mostrou pedido de orcamento respondido com soma/lista,
saldo respondido com despesas e compra incompleta tratada como conversa,
perdendo seus dados nas continuacoes. Sem captura do plano real nao se atribui
uma causa unica a cada resposta do video.

O contexto agora distingue conversa sem acao, consulta executada, esclarecimento,
rascunho incompleto e proposta de confirmacao. Um plano anterior e referencia,
nao instrucao para repetir a operacao. Texto do assistente nao comprova que existe
rascunho. Se ele so prometeu cadastrar, a interpretacao seguinte pode reconstruir
a compra usando os dados informados pelo usuario. Um rascunho real continua
recebendo apenas os campos novos/corrigidos e depende de confirmacao transacional.

Producao e avaliacao usam o mesmo construtor de request. Os testes simulados
continuam verificando execucao, isolamento e contrato; nao medem entendimento
de linguagem natural. A avaliacao opt-in separada mede a intencao/escopo do
modelo real com dados ficticios, tentativas limitadas e relatorio por caso.
Uma resposta HTTP 200 nao aprova a qualidade da interpretacao.

Mantem-se uma completion por mensagem. Instrucoes fixas + schema ocupam 8.255
bytes com catalogo vazio no contrato final (antes 5.669). O teto explicito passa a
9.000 bytes para nao remover regras necessarias apenas para satisfazer um limite
de tamanho. Contexto dinamico e limitado separadamente. Nao ha promessa de
economia de tokens nem de acerto universal: ambos precisam de medicao.

Schema, credenciais e modelo recomendado permanecem iguais. A publicacao deve
manter backup e rollback, descritos no registro da release. Falhas continuam
visiveis no relatorio, nunca substituidas por mocks.
