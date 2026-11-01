# Qualificacao da interpretacao e publicacao

Publicado em 01/11/2026 as 09:55 de Sao Paulo.
Imagem: `app-financeiro-api:qualified-20261101`.
Digest: `sha256:c8ecb26a7796c4557133808ae79e90a23237107ead0e753c73d4e253a48b7c47`.
SHA256 da API: `2bd99716a0d24bfb247e773c95ad7b88157115d94a0d591dfef1283fe6fa1714`.

## Problema e comportamento

O video de 31/10 mostrou saldo respondido com gastos, teto de categoria respondido
com listagem e cadastro incompleto tratado como conversa. Os testes HTTP simulados
anteriores nao comprovavam que o modelo interpretava essas mensagens corretamente.

O contrato final usa contexto de tarefa explicito e distingue uma promessa em
texto de um rascunho realmente salvo. Dados de compra informados pelo usuario
podem ser recuperados quando o assistente apenas conversou, sem inventar campos
ausentes. Com rascunho salvo, permanecem mascaras de correcao e confirmacao local.

Para o modelo recomendado, a interpretacao usa JSON object e validacao no Go.
Na qualificacao, o schema nativo com anyOf produziu selecoes erradas de acao,
inclusive cancelar em vez de preparar uma despesa. O catalogo continua refletindo
capacidades anunciadas; capacidade nao equivale a qualidade comprovada. Outros
modelos continuam com seus formatos suportados, sem alegacao de qualificacao.
Envelope action tambem e adaptado quando retornado fora do schema nativo;
envelopes conflitantes continuam recusados, assim como planos/mutacoes misturados.

Uma revisao mensal de UMA categoria compila para a mesma orientacao historica de
orcamento, preservando todos os periodos, fonte e categoria. Revisao mensal sem
categoria nao e ampliada silenciosamente. Isso evita recusar um plano equivalente
apenas por usar review em vez de budget, sem permitir comandos arbitrarios.

Continua uma completion por mensagem e nenhuma para confirmacao valida. Nao
altera schema do banco, chave, modelo recomendado nem contrato HTTP do APK.
Nao promete previsao garantida, limite financeiro seguro ou acerto universal.

## Evidencias

- Avaliacao real final: 6/6 aprovados com o mesmo modelo gratuito, entre 1,3 e
  5,2 segundos, 9.334 tokens. Antes das mudancas: 1/6; contexto sozinho: 2/6.
- Nenhuma expectativa foi substituida por resposta simulada para aprovar o modelo.
  Depois da qualificacao, as respostas reais sinteticas viraram fixtures de replay.
- Harness canonico local completo: formatacao, vet, build e testes aprovados;
  cobertura geral 47,8%. A avaliacao real e opt-in, fora do harness normal.
- Docker Desktop local falhou por um socket invalido do Model Runner. A integracao
  foi executada por binario Linux na VM, com PostgreSQLs descartaveis em tmpfs,
  sem DB_DSN/chave de producao e sem inferencias. Suite completa aprovada: PASS.
- Backup privado restaurado com sucesso em outro PostgreSQL isolado, sem rede,
  removido ao final. O primeiro ensaio detectou uma corrida na criacao do banco;
  a espera foi corrigida para executar SELECT no banco de destino. A API anterior
  permaneceu ativa ate a restauracao passar.
- Nove migrations aplicadas; apenas status executado. Nenhuma migration nova.
- Ambiente, volumes, portas, redes e politica de reinicio comparados antes/depois.
- Leituras autenticadas de relatorios e conversas aprovadas na rede interna e
  HTTPS. Sem chamadas reais de chat no smoke de producao.

Metodo e resultados completos: [confiabilidade](assistant-confiabilidade.md) e
[avaliacao opt-in](assistant-avaliacao-real.md). A investigacao usou apenas a cota
gratuita existente, sem comprar creditos, trocar chaves ou contornar limites.

## Recuperacao

Diretorio privado: `/home/ubuntu/app-financeiro/deploy-qualified-20261101`.
Backup: `rollback/database.dump`, modo 600; configuracoes efetivas privadas fora
do Git. Imagem anterior: `app-financeiro-api:before-qualified-20261101`, digest
`sha256:f31f72a101ed89b46904285753361a3a22b30485cb9281929f6dac11d21deeb7`.

Rollback de codigo, sem restaurar banco sobre novas transacoes:

```sh
sudo bash /home/ubuntu/app-financeiro/deploy-qualified-20261101/rollback.sh
```

O comando preserva ambiente e mounts anteriores. O banco de producao nao foi
alterado pelo deploy; dump e para recuperacao verificada, nao para desfazer codigo.
