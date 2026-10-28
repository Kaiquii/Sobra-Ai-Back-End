# Apresentacao das respostas de IA em 28/10/2026

Publicacao explicitamente autorizada pelo usuario, concluida e auditada.
Somente a API foi substituida. Banco, proxy, volumes, ambiente efetivo, portas,
redes e politica de reinicio foram preservados. Nenhuma migration foi aplicada.

## Estado publicado

- Commit da implementacao: `d66079d`, enviado para `main`.
- Imagem: `app-financeiro-api:openrouter-response-style-20261028`.
- Digest: `sha256:01bd54f35ccf7723aaba8545685cfb34f1d89c3308006c920141e2f06ae7b984`.
- Ativacao: 16:22:27 de Sao Paulo, schema 9 preservado.
- Hash do binario API: `00bdbf108c0644bc44ca0769e7ad5012a993f672ae5f25fb047d9acc79d1d64d`.

Os prompts passam a compartilhar orientacoes de resposta principal primeiro,
paragrafos curtos, negrito pontual, listas quando ajudam e emojis ocasionais,
opcionais e moderados. O estilo nao adiciona chamadas de IA nem altera cotas,
roteamento, confirmacoes ou escritas financeiras.

O contrato continua `reply`/`content` como string. A interpretacao devolve JSON
externo puro, inclusive em modelos sem JSON nativo, com formatacao apenas na string
visivel. Texto original, emojis e quebras de linha permanecem no historico.
O app renderiza Markdown simples; o backend nao transforma essas strings em HTML.
A exibicao visual exige a versao do app com o renderer comum Android/iOS.

A validacao financeira usa uma copia sem marcadores e com espacos/sinais
normalizados. Negrito nao permite transformar ano/ID em dinheiro nem inverter o
sinal de um valor. A resposta visivel original nao e reescrita pela validacao.

## Validacoes aprovadas

- Harness completo `scripts/test.ps1 -Integration`: formatacao, vet, build,
  testes sem cache com shuffle/cobertura e PostgreSQL descartavel. Pacote
  assistant: 24,666 s; integracao final: 34,011 s.
- Regressao de JSON/resposta/historico preserva negrito, emojis, aspas e quebras
  de linha com uma unica inferencia para ajuda geral. Guardas financeiros cobrem
  moeda dentro/fora do negrito, ano/ID, espacos Unicode e sinais negativos.
- Os 101 fontes Go conferidos correspondem a copia testada e compilada para
  Linux amd64. Compose, cache e os rascunhos duplicados `expense_draft*.go`
  preexistentes foram preservados e excluidos dessa copia.
- Backup restaurado em PostgreSQL sem rede externa. Imagens anterior e nova
  ensaiadas sequencialmente com limites de RAM, schema 9, respostas controladas
  sem provedor e fluxos patrimoniais. Fingerprints financeiros, conversas,
  preferencias e migrations identicos apos o ensaio.
- Smoke autenticado real aprovado no HTTP interno e no HTTPS usado pelo app:
  ajuda geral formatada, consulta patrimonial com valores sinteticos exatos,
  continuidade, quatro mensagens alternadas por conversa sem duplicacao,
  historico identico aos replies originais, preferencia/data intactas e
  resumo mensal/isolamento preservados. Confirmacoes locais sem rascunho nao
  criaram despesas. Nao houve reenvio automatico de POST.
- Auditoria final: fixtures e containers descartaveis removidos, ambiente e
  configuracao preservados, API sem reinicios/OOM, schema 9, CDI/padroes e backup
  integro. Hashes de API/migrate no container iguais aos do pacote testado.

Testes externos usaram apenas dados sinteticos. O estilo e orientado por prompt;
nao se afirma que toda resposta de todo modelo tera formato ou emojis identicos.
Compilacao/testes do app nao substituem conferencia visual em aparelhos fisicos.

## Pacote e recuperacao

- Pacote local: `.artifacts/openrouter-response-style-20261028.tar.gz`.
- SHA-256: `79422627728e95cd64c409b5ef111a6f851f6dc918739fdbff2b014b6dfc9143`.
- Diretorio remoto privado:
  `/home/ubuntu/app-financeiro/deploy-openrouter-response-style-20261028/`.
- Backup: `rollback/database.dump`, indice e SHA-256 no mesmo diretorio.
  Credenciais e snapshot privado permanecem na VM, fora do Git e do pacote.
- Rollback operacional:
  `sudo sh /home/ubuntu/app-financeiro/deploy-openrouter-response-style-20261028/rollback.sh`.
  Reativa somente a imagem anterior preservada como
  `app-financeiro-api:before-openrouter-response-style-20261028`, de digest
  `sha256:c5bd87e5a14784c2bcb3803d4ce5d8ae0423503ca77ed038d9bad7e081200395`.
  Mantem o roteamento automatico e o schema 9. Nunca restaurar o dump sobre novas
  escritas para reverter apenas a imagem da API.

O manifesto do pacote registra a preparacao; este registro descreve a execucao
posterior. Futuras operacoes remotas exigem autorizacao especifica.
