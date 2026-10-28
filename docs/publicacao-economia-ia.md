# Publicacao de economia de chamadas em 28/10/2026

Publicacao explicitamente autorizada pelo usuario e concluida as 20:29 de Sao Paulo.

## Versao e comportamento

- Imagem: `app-financeiro-api:economy-20261028`.
- Digest: `sha256:de39e362c6303b1424e33c246d650b5863ecd929da2595fb242262ac99e2e3de`.
- SHA-256 do binario API: `194e26d64979c1a208a8b168106c264d1c2a8c9a69d3e678f671eeca7678a1a7`.
- Uma mensagem de chat permite no maximo uma chamada de completions, incluindo
  tentativas com resposta invalida, truncada ou erro. Confirmacoes locais usam zero.
- Consultas financeiras usam formatacao do servidor apos a unica interpretacao;
  conversa comum e ajuda usam o reply retornado nessa mesma interpretacao.
- Interpretacao limitada a 1024 tokens de saida nos modelos que suportam limite,
  respeitando contextos menores. Limite interno padrao: 6 chamadas/minuto e 50/dia UTC.
- A cota gratuita do OpenRouter pertence a conta e continua compartilhada entre
  usuarios e testes externos; o limite local nao garante 50 respostas por usuario.
- Harness passa a substituir a chave herdada por credencial sintetica. As suites
  interceptam chamadas do provedor e nao consomem a franquia real.

## Validacao

- Harness completo com `scripts/test.ps1 -Integration` aprovado: formatacao, vet,
  build, testes com cobertura e integracao HTTP/PostgreSQL descartavel.
- Regressoes verificam orcamento compartilhado entre clientes/etapas, concorrencia,
  falhas sem segunda tentativa, ausencia de segunda chamada na consulta financeira,
  50 mensagens independentes e rotacao somente entre mensagens.
- Nove migrations existentes aplicadas; somente `migrate status` foi executado.
  Nao houve alteracao de schema.
- Smokes autenticados de leitura no HTTP interno e HTTPS publico aprovaram o
  resumo financeiro e a listagem de conversas. Nenhum POST de inferencia foi enviado.
- Ambiente efetivo, mounts, portas, redes e politica de reinicio foram comparados
  antes/depois e permaneceram identicos. API ativa, sem reinicios no encerramento.

## Configuracao e recuperacao

A verificacao inicial detectou que os arquivos Compose anteriores nao reproduziam
o ambiente efetivo do container ativo. A operacao foi interrompida antes da troca.
O ambiente ativo foi preservado em override privado `runtime-environment.yml`,
usando `environment: !override` (Compose 2.40.3), em diretorio privado na VM e
permissao 600. O arquivo contem credenciais: nao copiar para Git, logs ou pacotes.
Os mounts foram conferidos antes e o estado relevante foi comparado apos a troca.

Diretorio remoto: `/home/ubuntu/app-financeiro/deploy-economy-20261028/`.
Backup: `rollback/database.dump`, com indice de restauracao validado por pg_restore.
Configurações anteriores e imagem preservadas. Rollback operacional:

```sh
sudo bash /home/ubuntu/app-financeiro/deploy-economy-20261028/rollback.sh
```

Reativa `app-financeiro-api:before-economy-20261028`, de digest
`sha256:01bd54f35ccf7723aaba8545685cfb34f1d89c3308006c920141e2f06ae7b984`,
mantendo o ambiente efetivo preservado. Nao restaurar o dump sobre novas escritas
para reverter apenas a imagem. O rollback restaura tambem o comportamento anterior
de consumo de IA; o override privado precisa continuar disponivel nas operacoes
futuras de Compose, pois os arquivos antigos isoladamente nao refletem a producao.

As alteracoes locais de `docker-compose.yml` preexistentes foram preservadas e nao
foram enviadas. Nenhum credito foi comprado e nenhuma chave foi criada ou trocada.
