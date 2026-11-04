# Handoff — preferências separadas de VA e VR

Backend publicado e verificado em 04/11/2026 às 14:47 de São Paulo, fonte
`ad3d2d8`, schema 10. [Contrato](../preferencias-saldo-va-vr.md) e
[publicação/rollback](../publicacao-preferencias-saldo-va-vr.md).

## Condição explícita do usuário para web e Android

**Antes de implementar a interface, conversar com o usuário para decidir onde
ficarão os dois controles. Não escolher nem implementar a localização por conta
própria.** Apresentar as opções de localização compatíveis com a plataforma e
aguardar a decisão dele. O backend foi publicado independentemente dessa
escolha; a existência das rotas não autoriza criar a UI sem a conversa.

Controles propostos:

- Excluir VA do disponível geral.
- Excluir VR do disponível geral.

Ambos são independentes e desativados por padrão. Não ativar automaticamente
para usuários antigos, nem deduzir a escolha a partir de valores cadastrados.

## Contrato comum

Consultar [a especificação completa](../preferencias-saldo-va-vr.md).

- `GET /api/users/balance-preferences`: retorna diretamente os dois booleanos.
- `PATCH /api/users/balance-preferences`: enviar somente a opção alterada,
  como `{"exclude_va_from_available":true}` ou
  `{"exclude_vr_from_available":false}`; a resposta traz o estado completo.
- Campo omitido é preservado. As opções são por usuário e valem para todos os
  meses; não são uma configuração por dispositivo ou por mês.
- Recarregar `GET /api/reports/summary?month=...&year=...` após salvar. Usar
  `total_geral_disponivel` já calculado pelo backend. **Não subtrair VA/VR
  novamente no cliente.** O resumo inclui os dois booleanos aplicados.
- Manter crédito, gasto e restante dos cartões de VA/VR, Total Recebido,
  Total Gasto, percentuais e demais relatórios como estão.
- A exclusão retira o restante da fonte, não seu crédito inteiro. VA R$ 725
  menos despesa R$ 600 continua mostrando R$ 125 no cartão; com VA excluído,
  esses R$ 125 ficam fora do disponível geral.
- Explicar na apresentação aprovada que uma ou ambas as fontes estão fora do
  disponível; com exclusão ativa, o cartão pode diferir de recebido menos gasto.
- Respeitar loading, erro e a confirmação do PATCH; não transformar falha de
  leitura em gravação de `false`/`false`. Separar estado por conta e invalidar
  o cache do resumo após mudança. Uma preferência não deve apagar a outra.

## Front-end web

Primeiro discutir com o usuário **a localização dos parâmetros na web**,
considerando desktop e mobile. Só depois implementar conforme a decisão e os
padrões/componentes existentes. Não alterar a organização da visão mensal
como parte automática deste handoff.

Após a aprovação, integrar leitura e PATCH parcial autenticados, recarregar o
resumo e validar as quatro combinações, troca de mês e persistência entre
sessões. Exportações e relatórios integrais continuam usando os totais atuais.

## Android

Primeiro discutir com o usuário **a localização dos parâmetros no aplicativo**.
Não assumir que a posição adotada na web define a posição nativa. Só depois
implementar conforme a decisão e os componentes do app.

Após a aprovação, integrar os dois booleanos nos DTOs/estado, salvar via PATCH
parcial autenticado e atualizar o resumo. Tratar valores ausentes de clientes
legados como `false` sem efetuar gravação automática. Se houver cálculo local
do disponível pela soma dos cartões, passar a usar o campo fornecido pela API
para refletir a preferência e evitar desconto duplo. Validar persistência e
troca de conta/mês; gerar APK para teste após concluir a implementação aprovada.

## Checklist de aceite das duas plataformas

- Novo usuário e usuário existente sem preferência: ambos os controles desligados.
- Ligar apenas VA, apenas VR, ambos e desligar cada um sem mudar o outro.
- VA R$ 725 com despesa R$ 600: cartão R$ 125; disponível exclui somente R$ 125.
- Gastos divididos, benefício totalmente gasto e restante negativo.
- Confirmar que fontes, Total Recebido, Total Gasto e registros não mudaram.
- Confirmar que recarregar a tela e trocar de mês conserva a preferência do usuário.
- Erro de salvamento não pode deixar a UI afirmando uma opção que não foi salva.
