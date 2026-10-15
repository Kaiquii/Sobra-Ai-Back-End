# Assistente IA

## Regra principal

O assistente nao deve depender de padroes fixos de texto para entender perguntas do usuario.

Exemplos do que evitar:

- Se a mensagem contem "quanto gastei", fazer resumo mensal direto.
- Se a mensagem contem "categoria", tentar cortar a frase manualmente.
- Criar uma lista crescente de frases esperadas para cada pergunta.

Esse caminho deixa o assistente engessado e contradiz a proposta do produto.

## Como deve funcionar

O Groq deve ser usado como provedor principal para interpretar a mensagem livre do usuario. O Gemini deve ser usado como fallback quando o Groq falhar ou estiver indisponivel.

O fluxo correto e:

1. O usuario envia uma mensagem natural.
2. O back-end envia essa mensagem para o Groq interpretar.
3. O Groq devolve uma intencao estruturada, por exemplo:
   - resumo mensal
   - gastos por categoria
   - cadastro de despesa
   - duvida sobre o app
   - conversa comum
4. O Go executa a acao com seguranca usando o usuario autenticado pelo JWT.
5. O Go monta uma resposta curta e clara usando o resultado consultado.

Para economizar cota, o fluxo financeiro deve usar apenas uma chamada ao provedor principal sempre que possivel: a chamada de interpretacao da intencao. Depois disso, o Go executa a acao e responde com base nos dados retornados.

Quando o Gemini for usado como fallback e retornar limite de cota, o back-end deve respeitar o tempo de retry informado pela API e evitar novas chamadas ao Gemini ate esse periodo acabar. Durante esse cooldown, a API deve responder com `error_code=gemini_quota_exceeded` e `retry_after_seconds`.

O fallback para o Gemini deve preservar o mesmo contrato: interpretar a mensagem em JSON estruturado e deixar o Go executar a acao com seguranca.

## Responsabilidades

### Groq

- Entender a intencao da mensagem.
- Extrair entidades como mes, ano, categoria, valor, fonte de pagamento e descricao.
- Gerar respostas finais apenas quando for conversa comum, ajuda sobre o app ou quando uma resposta deterministica do Go nao for suficiente.

### Gemini (fallback)

- Assumir a mesma responsabilidade do Groq apenas quando o provedor principal falhar ou estiver indisponivel.

### Go

- Autenticar o usuario.
- Buscar dados no banco.
- Criar, atualizar ou deletar dados somente com confirmacao quando necessario.
- Garantir que o usuario so acesse os proprios dados.
- Salvar conversas e mensagens.

## Cadastro de despesas e campos pendentes

O cadastro pelo chat e um rascunho persistido na conversa. O servidor valida os
campos independentemente do prompt e devolve `tool_call=create_expense` com:

- `status=needs_information` e `missing_fields` quando faltam dados ou ha valores
  invalidos. A resposta pergunta quais campos precisam ser informados/corrigidos;
  nao grava despesas nem cria categorias.
- `status=needs_confirmation` quando os dados estao completos. A resposta apresenta
  descricao, categoria, fonte, tipo, primeira data e valores antes de pedir confirmacao.
- `status=created` somente depois de confirmar e concluir a transacao.
- `status=cancelled` quando o usuario rejeita/cancela o rascunho.

Campos obrigatorios: descricao nao branca, categoria existente escolhida pelo usuario,
fonte valida (`Salario`, `Adiantamento` ou `Renda Extra`), valor positivo em centavos,
data completa valida e tipo (`Unica`, `Parcelada` ou `Fixa`). Parceladas exigem de 2 a
120 parcelas. O servidor nao assume dia 1, data atual, categoria, descricao ou fonte.
Datas relativas explicitamente informadas podem ser interpretadas pela IA.
Uma categoria desconhecida ou de outro usuario exige escolher uma categoria propria;
o assistente nao cria categorias automaticamente.

Respostas seguintes complementam o rascunho da mesma `conversation_id`. O modelo
retorna somente os campos alterados e o servidor preserva os demais. Uma nova despesa
independente usa `new_expense=true` na intencao para nao herdar os dados anteriores.
O `confirm` produzido pelo modelo nunca autoriza uma escrita. A confirmacao aceita
apenas afirmacoes isoladas, como `sim` ou `pode cadastrar`; uma frase com negacao ou
correcao nao confirma automaticamente. Confirmar um rascunho incompleto apenas repete
os campos pendentes. Correcoes em uma proposta completa exigem nova confirmacao.

Em despesas parceladas, `amount` no rascunho e o **total da compra**. R$ 600 em 3 vezes
gera tres despesas de R$ 200. O Go distribui centavos restantes nas primeiras parcelas:
R$ 100 em 3 vezes gera R$ 33,34, R$ 33,33 e R$ 33,33. Datas mensais preservam o dia
informado ou o ultimo dia do mes quando necessario (31/jan -> 28/fev -> 31/mar).
Cada parcela tem sua divisao de pagamento persistida e a soma permanece igual ao total.
Para fixa, o valor informado e mensal e sao gerados 120 lancamentos, conforme o cadastro
manual. O cadastro manual continua recebendo o valor **por parcela**; seu contrato de
`amount` nao mudou.

Propostas pendentes antigas exigem informar o valor novamente antes de confirmar,
porque o significado de `amount` em parcelas mudou. Despesas ja gravadas nao sao
alteradas. A confirmacao, as parcelas, as divisoes e a resposta da conversa sao gravadas
em uma transacao; uma falha reverte tudo e repetir a confirmacao nao duplica a compra.

As rotas manuais de despesas tambem rejeitam descricao vazia e categoria ausente,
inexistente ou de outro usuario nos novos cadastros. O PATCH valida esses campos quando
enviados; atualizacoes de outros campos e a leitura de registros legados continuam
permitidas. `GET /api/expenses`, inclusive `period_mode=effective`, preserva os nomes e
tipos JSON de `description` (string) e `category_id` (numero), sem reparar registros antigos.

## Regra para novas features

Antes de adicionar qualquer condicao baseada em frase, perguntar:

"Isso pode ser interpretado pelo Gemini em vez de virar mais um padrao no Go?"

Se a resposta for sim, usar o Gemini para interpretar e manter o Go apenas como executor seguro.
