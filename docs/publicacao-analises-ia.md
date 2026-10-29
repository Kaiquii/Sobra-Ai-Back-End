# Analises financeiras com uma inferencia

Versao: `intelligence-20261028`. Nao altera schema, migrations nem contratos
existentes de cadastro/confirmacao. Acrescenta `financial_analysis` ao `tool_call`.

Publicado em 28/10/2026 as 21:47 de Sao Paulo. Imagem final:
`sha256:f44135021789b76aba713658c037c79ecc25efff9c49023cfb8961fd599e3e27`.
SHA256 do binario da API:
`abd815f517c869f7eb7e0ee51798f247e704ebbd0fc531da9b46662228cc3be9`.
Harness completo aprovado: cobertura geral 40,3%, assistente 47,6% e integracao
38,0%. Nove migrations continuam aplicadas. Leituras autenticadas internas e
HTTPS aprovadas; ambiente, volumes, portas, redes e politica de reinicio
preservados. Nenhuma inferencia real foi usada nos testes desta publicacao.

## Comportamento

- Leitura autenticada antes da inferencia: rendas/despesas mensais, categorias de
  janeiro a dezembro, maior lancamento/parcela e maiores compras parceladas
  agrupadas por `series_id`. As parcelas registradas de uma compra nao garantem
  seu valor original caso lancamentos tenham sido removidos.
- Snapshot consistente, somente leitura, com timeout de cinco segundos e filtro
  de usuario em todas as consultas. Valores seguem o periodo efetivo das despesas
  adiantadas e fontes divididas. VA/VR ficam fora dos totais de dinheiro livre.
- Analises podem usar todos os meses disponiveis, em vez de repetir um resumo
  mensal. Lancamentos futuros sao identificados como registrados, sem afirmar
  recebimento, pagamento ou capacidade segura de gasto futura.
- A interpretacao e a resposta analitica compartilham a mesma completion.
  JSON opcional `calculations` declara soma, subtracao, media, minimo, maximo ou percentual
  sobre caminhos JSON Pointer de valores monetarios autenticados. O servidor
  calcula e valida; IDs, anos, contagens e operandos arbitrarios nao viram dinheiro.
- Valores monetarios sem lastro resultam em esclarecimento, sem nova inferencia.
  Numeros em nomes cadastrados, como `Uber/99`, nao viram valores financeiros
  nem impedem uma resposta valida; valores e percentuais continuam verificados.
  Essa verificacao numerica nao prova que toda conclusao em linguagem natural esta
  correta: o modelo ainda pode interpretar mal uma pergunta.
- Categoria sugerida somente entre categorias existentes, a pedido do usuario,
  aparece explicitamente na conferencia. Nenhum cadastro e confirmado pelo modelo.
- Patch de rascunho preserva campos conhecidos quando o modelo envia defaults
  vazios. `provided_fields` permite distinguir uma correcao explicita invalida
  (como valor zero) de um campo ausente. `new_expense=true` inicia compra independente.
- Resumos antigos do servidor viram referencias compactas de periodo/categoria;
  dados atuais prevalecem. Mensagens do usuario e analises conversacionais sao
  preservadas. Rascunhos nao repetem listas de parcelas derivadas no prompt.
- Logs OpenRouter incluem apenas `prompt_tokens`, `completion_tokens` e
  `total_tokens`, quando fornecidos. Nao incluem mensagens, credenciais ou fatos.

## Limites e consumo

Cada contexto cobre no maximo dois anos mencionados, ou o ano corrente. Expressoes
de calendario relativas usuais selecionam o ano correspondente. Um periodo fora
dessa cobertura deve ser esclarecido, nunca preenchido com dados inventados.
Por ano: ate 5.000 rendas e 10.000 despesas. Contextos maiores falham sem enviar
totais parciais. Categorias detalhadas: as 40 de maior valor, com indicador de
cobertura parcial; totais mensais continuam completos. Compras: cinco maiores
series registradas em todo o historico, identificadas separadamente do ano.

Continua valendo no maximo uma completion por mensagem e nenhuma para confirmar
um rascunho completo. Nao ha garantia de menos tokens em toda pergunta: analises
recebem fatos adicionais para melhorar qualidade. A economia deve ser medida
pelos novos contadores e comparacoes equivalentes, sem cortar contexto necessario.

## Validacao e recuperacao

Entrada canonica: `./scripts/test.ps1 -Integration`. PostgreSQL descartavel local
e provedor HTTP simulado; os testes nao usam franquia real. Cobertura inclui
comparacao de meses e soma verificada, periodo adiantado, separacao VA/VR,
continuacao do chocolate, sugestao de categoria, autorizacao e confirmacao sem
duplicacao. Casos de falha numerica e contexto compactado tambem sao verificados.

Deploy: imagem `app-financeiro-api:intelligence-20261028`, preservando ambiente,
volumes, redes, portas e politica de reinicio efetivos. Somente API e recriada.
Diretorio da release: `/home/ubuntu/app-financeiro/deploy-intelligence-20261028`.
Backup privado do banco e identidade do container ficam em `rollback/` nesse
diretorio, fora do Git. Nao restaurar dump sobre novas transacoes para reverter
codigo: nao ha migration nesta release.

Rollback apos publicacao: `sudo bash /home/ubuntu/app-financeiro/deploy-intelligence-20261028/rollback.sh`.
Imagem anterior: `app-financeiro-api:before-intelligence-20261028` (economy).
Validacao pos-deploy: status de migrations, disponibilidade HTTPS e leituras
autenticadas, sem inferencias reais e sem escrever dados financeiros.
