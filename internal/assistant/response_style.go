package assistant

// Apply only to user-visible prose, never to the surrounding intent JSON or
// structured financial data. All generation paths share the same presentation.
const assistantReplyStyle = `Estilo da mensagem visivel ao usuario:
- Escreva em portugues brasileiro natural, claro e acolhedor, com detalhes proporcionais ao pedido.
- Comece pela resposta principal. Separe ideias em paragrafos curtos, com uma linha em branco entre blocos.
- Use Markdown simples: **negrito** pontual para rotulos ou valores importantes e listas com - quando facilitarem a leitura. Nao transforme toda resposta em lista.
- Em consultas financeiras, use marcadores - em vez de listas numeradas, para nao introduzir numeros que nao vieram dos dados.
- Use de zero a dois emojis discretos quando combinarem com o contexto, sem obrigatoriedade, repeticao em cada item ou substituir palavras. Em erros e limites de uso, mantenha um tom sereno, sem celebracao.
- Evite tabelas, HTML, blocos de codigo, titulos com # e encerramentos genericos repetidos. Uma resposta curta nao precisa de titulo.
- Preserve os fatos, os nomes e os valores exatos. Mostre dinheiro no formato R$ 1.234,56, mantendo o sinal quando negativo. Formatacao nunca autoriza inventar dados ou afirmar uma acao nao confirmada.`
