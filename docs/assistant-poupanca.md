# Capacidade mensal de poupar

O protocolo financial_query agora aceita operation=savings com group_by=month,
sem filtros de categoria ou fonte. A consulta usa o mesmo snapshot autenticado
e transacional das orientacoes, com limites de leitura e isolamento por usuario.

O calculo por mes e renda em dinheiro menos todas as despesas em dinheiro.
Splits de pagamento e mes efetivo de despesas adiantadas sao respeitados.
VA/VR nao financiam objetivos em dinheiro. Mes sem renda cadastrada fica
indeterminado; saldo negativo nao produz uma sugestao positiva. Nao ha escrita,
migration, transferencia ou transporte automatico de saldo entre meses.

Perguntar quanto guardar para comprar algo deve selecionar savings, inclusive
ao corrigir uma resposta anterior de budget. O ano da compra nao deve ser
confundido com o periodo historico; sem periodo de contribuicao informado,
consulta-se o mes atual. O resultado informa capacidade pelos cadastros, nao
garante que despesas essenciais estejam completas ou renda recebida.

O valor necessario para atingir uma meta exige preco, valor ja reservado e prazo.
Esta etapa calcula a capacidade mensal cadastrada; ainda nao implementa um
simulador de metas com essas entradas. O corpus inclui a pergunta do carro e a
correcao apos budget de Dividas, pendentes de qualificacao com modelo real.

Uma completion por mensagem permanece. Nao houve publicacao nem inferencias
reais durante esta alteracao local.
