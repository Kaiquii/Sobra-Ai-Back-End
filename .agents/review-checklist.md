# Checklist De Revisao

## Git: Somente Main

- [ ] O trabalho e os commits/pushes autorizados ocorreram na `main`.
- [ ] Nenhuma branch temporaria, auxiliar ou de tarefa foi criada pelo agente,
  harness ou subagentes, inclusive por worktree/checkout alternativo.
- [ ] Nenhuma branch foi trocada ou removida para contornar a politica.
- [ ] O estado preexistente foi preservado; divergencias de branch foram relatadas.

## Codigo

- [ ] A mudanca atende somente ao escopo solicitado.
- [ ] O diff nao sobrescreve alteracoes preexistentes do usuario.
- [ ] Erros retornam mensagens e status coerentes.
- [ ] Contratos JSON e dados legados continuam compativeis.
- [ ] Operacoes financeiras com multiplas escritas sao atomicas.
- [ ] Nenhum segredo ou dado sensivel foi adicionado.

## Banco E Migrations

- [ ] Uma alteracao de schema tem uma migration nova.
- [ ] Migrations aplicadas nao foram editadas.
- [ ] Compatibilidade, backup e rollback foram considerados.
- [ ] A API continua apenas validando migrations na inicializacao.

## Qualidade

- [ ] Arquivos Go estao formatados com `gofmt`.
- [ ] O bug ou comportamento novo tem cobertura automatizada quando aplicavel.
- [ ] O harness completo foi executado.
- [ ] Documentacao e exemplos foram atualizados quando necessario.

## Entrega

- [ ] O resultado informa o que mudou e quais validacoes passaram.
- [ ] Qualquer teste nao executado ou risco restante esta explicito.
- [ ] Nenhuma acao de deploy ou producao foi inferida.
