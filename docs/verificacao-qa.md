# Verificação de QA: API Atlas vs. telas da app

- **Data:** 2026-11-02
- **Estado verificado:** branch `feat/geo-dispatch-realtime`, em `d3002f5`, mais as alterações por commitar. Nada foi corrigido.
- **Independência:** esta verificação foi feita pelo mesmo agente que escreveu o código e o relatório anterior, por isso não é uma revisão independente. Cada resultado vem de execução real e pode ser repetido com o script abaixo. Convém que outra pessoa ou sessão repita o trabalho.

## 1. Resumo

| | Passou | Falhou | Não testável | Total |
|---|---:|---:|---:|---:|
| Cenários pedidos (T, M, R, D, P) | 2 | 22 | 0 | 24 |
| Segurança e verificações extra (S, X, A, E) | 12 | 2 | 0 | 14 |
| **Total** | **14** | **24** | **0** | **38** |

**Conclusão:** só o táxi base, o Express, o backoffice de frota e a segurança de acesso funcionam de ponta a ponta. Os fluxos de avaliação, voucher, Manutenção, Mudanças, recibo e notificações não existem.

**Como ler os resultados:**

- **FALHOU** inclui funcionalidades que não existem. Um cenário com 5 verificações certas e 1 errada também conta como FALHOU; a observação indica o que correu bem.
- **Nenhum cenário inteiro ficou NÃO TESTÁVEL**, mas há partes que não foi possível provar. Estão na secção 6.

## 2. Preparação

| Verificação | Resultado |
|---|---|
| Commits desde `f694d32` | 33 commits; 168 ficheiros, +44 840 / −5 294 |
| Por commitar | 55 ficheiros alterados, +16 530 / −3 165, e 39 ficheiros novos |
| Migrações novas | 000007, 000015–000021, e outras editadas no local |
| `make migrate-up` e `make docker-up` | Versão 21, `dirty=false`; `/ready` e `/docs` respondem 200 |
| Suite completa (`integration,e2e`) | 798 testes: 798 passam, 0 falham, 0 ignorados |
| Sinais de implementação falsa | Sem TODO, FIXME ou `not implemented` em código de produção. Os 3 `panic` são de arranque ou de transação |
| Valores fixos no código | Limite de 3 paragens (`domain/entities/service_request.go:55`, `dtos/dispatch-dtos.go:27,44,207`), boost 10% e 20% |
| Endpoints com dados fixos | Nenhum encontrado |

### Instabilidade detetada

`TestTaxiFullFlowWithFixedPriceTrackingAndHonoredFare` falhou uma vez em 8 execuções da suite (`no request.offer event`), na execução com cobertura em todos os pacotes. Passou nas 6 repetições só da suite e2e. **A causa não foi determinada.** Pode ser a carga de vários pacotes em paralelo, mas não está provado.

### Qualidade dos testes existentes

| Área | Avaliação |
|---|---|
| Pagamentos (`test/e2e/payments_test.go`) | Testam regras: webhook forjado, valor errado, dívida, retentativa, reembolso acima do pago |
| Despacho (`test/e2e/dispatch_test.go`) | Testam regras: concorrência, ordem de eventos, reconexão |
| Motivos de rejeição | Só caminho principal e `REASON_REQUIRED`. Motivo inexistente, inativo ou de outro âmbito sem teste |
| **Frota, oficinas e `/admin/requests`** | **Nenhum teste permanente**. Só foram verificados por este script |

## 3. Resultados por cenário

**Script:** `tests/e2e/qa_flows.py`. **Evidência completa** (pedido, resposta e BD): `tests/e2e/evidence/run-20261102-083112.json`, e `run-20261102-083204.json` para os cenários E. Os valores em cêntimos são 100 vezes os Kz.

### Táxi

| ID | Resultado | Evidência | Observação |
|---|---|---|---|
| T1 | FALHOU | Sem paragens 97 569 (4 088 m); com 2 paragens 202 120 (10 390 m). A conta bate: `30000 + 13000×km + 1500×min`. Estimativa sem paragens + 2 paragens: `201`, `fare_cents=97569` | O cálculo está certo. Falha pelo bug B1 |
| T2 | FALHOU | `POST /route` com 1 paragem: 202 120 → 121 017, igual à estimativa; BD `route_change -81103`. 4 paragens: 422 nas 3 rotas | Só falha o limite configurável: é uma constante |
| T3 | FALHOU | Amanhã 07:30: `201 scheduled`; BD `next_action_at` 1 800 s antes. 8 dias e passado: 422. Ao simular a hora, o motorista recebeu a oferta. 07:37: `201` | Só falham os intervalos de 15 min |
| T4 | FALHOU | Motorista vê `Maria Neto`, mas `passenger_phone` e `contact_phone` vêm vazios. SMS: "AGORA: pediram um táxi para ti. O motorista está a caminho." BD: `payments.customer_id` é quem pediu | Nome e cobrança certos. Bug B4 |
| T5 | FALHOU | Sem motorista: 200, motivo na BD, sem campo de custo. Sem texto: 422. `GET /cancellation-reasons`: 404. Com motorista: 200 com texto livre, sem custo. Sem pré-aviso | Não há catálogo, taxa nem pré-aviso |
| T6 | FALHOU | Recusa `204`; `request_offers` não tem coluna de motivo. O outro motorista recebeu a oferta | A reoferta funciona. O motivo enviado é descartado em silêncio |
| T7 | FALHOU | `/requests/{id}/rating`: 404; sem rotas nem tabelas de avaliação ou gorjeta | Não existe |
| T8 | FALHOU | `voucher_code=TAXI10`: `201`, `fare_cents` igual; sem rota de vouchers | Não existe; o campo é ignorado em silêncio |

### Manutenção

| ID | Resultado | Evidência | Observação |
|---|---|---|---|
| M1 | FALHOU | Parceiro cria oficina e serviços: 201. Admin lista: 200. `GET /workshops` do cliente: 404. Horário é texto | Sem rotas para o cliente e sem "aberta até" calculado |
| M2 | FALHOU | Disponibilidade e marcação: 404; sem tabela de marcações | Não existe |
| M3 | FALHOU | A taxa existe na oficina, mas nada a soma | Não existe |
| M4 | FALHOU | `/requests/{id}/photos/upload-url`: 404 | O R2 só serve documentos de prestadores |
| M5 | FALHOU | `service_requests.status` sem estados de oficina; sem orçamento extra | Não existe |
| M6 | FALHOU | Pedido não concluído: `409 REQUEST_NOT_PAYABLE`. Sem passo de aprovação | Só a primeira parte funciona |

### Reboque

| ID | Resultado | Evidência | Observação |
|---|---|---|---|
| R1 | FALHOU | 4 088 m: 97 569; 15 792 m: 277 496. Serviços: só `Reboque` | Depende da distância, mas não há tipos de viatura |
| R2 | PASSOU | `201 searching`, depois `200 cancelled` | Grátis porque não existe taxa nenhuma |
| R3 | FALHOU | `POST /complete {"code":"0000"}`: `200 completed` | Não há código de entrega |

### Mudanças

| ID | Resultado | Evidência | Observação |
|---|---|---|---|
| D1 | FALHOU | `details.volume_m3=999` guardado como enviado; sem catálogo | O volume vem da app |
| D2 | FALHOU | `POST /fare-estimates` ignora ajudantes e extras e devolve 97 569; política `quote` | Conflito C3 do relatório |
| D3 | FALHOU | Cancelar a 12 h: 200, sem taxa; sem janelas | Não existe |

### Pedidos, conta e notificações

| ID | Resultado | Evidência | Observação |
|---|---|---|---|
| P1 | PASSOU | Em curso junta reboque, mudanças e manutenção; Histórico tem a concluída; Marcados só a marcada; `category=towing` só o reboque | |
| P2 | FALHOU | `/receipt` e `/repeat`: 404 | Não existe |
| P3 | FALHOU | `/notifications` e `/read-all`: 404. A tabela só regista envios (`request_status/fcm/skipped`) | Não há caixa de entrada |
| P4 | FALHOU | Sem confirmação: 422. Código errado: 422. Código certo: 204, `deleted_at` preenchido, sessão 401. `PATCH` com toggles: 200, sem colunas | A eliminação funciona. Os toggles são ignorados em silêncio |

### Segurança

| ID | Resultado | Evidência | Observação |
|---|---|---|---|
| S1 | PASSOU | Cliente B: 404 em ver, cancelar, rota, boost, método, pagamentos e pagar. Motorista estranho: 404 em todos os passos. Cliente em `/admin/payments` e parceiro em `/admin/requests`: 403 | Avaliar não testável porque não existe |
| S2 | PASSOU | Sem aprovação: online 403, aceitar 404. Suspenso: aceitar 403. Sem viatura: online 403 | |
| S3 | PASSOU | 51 entradas inválidas e JSON malformado: nenhum 5xx. `DROP TABLE` e `%'` na query: 200 sem efeito | |
| S4 | PASSOU | `fare_cents=1`, `status=completed`, `provider_id`: ignorados, fica 97 569 `searching` | Também em X1 com `amount_cents=1` |
| S5 | FALHOU | Mesma chave, 5 envios: `[201, 409×4]`, 1 pedido. 5 pagamentos: 1 linha. 5 confirmações de dinheiro: 1 linha. **Chaves diferentes: `[201,201,201]`, 3 pedidos ativos** | Bug B3 |
| S6 | PASSOU | Dois motoristas aceitam em simultâneo: `[200, 409]` | |
| S7 | FALHOU | `capacity_kg`, `display_order` e `duration_minutes` com 99 999 999 999: `201`, BD guarda 1 215 752 191 | Bug B2 |

### Verificações extra

| ID | Resultado | Evidência | Observação |
|---|---|---|---|
| X1 | PASSOU | AppyPay real: pago, reembolso acima do pago 422, parcial 200, 2 totais em simultâneo `[200, 422]`, reembolsado igual ao valor pago | |
| X2 | PASSOU | Referência real `809811583`, entidade `00348`. Webhook forjado `204`, continua `pending` | Pagar a referência até ao fim não é possível no ambiente de teste |
| A1 | PASSOU | Parceiro cria reboque e motorista, SMS, palavra-passe. Sem aprovação: 403. Corrida real: `organization_id` correto, listas, resumo e detalhe do parceiro e do admin | A aprovação foi feita por SQL |
| E1 | PASSOU | Casa 201, segunda Casa 4xx, intruso 404 | |
| E2 | PASSOU | Nome, palavra-passe e telemóvel, com erros 4xx | |
| E3 | PASSOU | SSE: estados, posição e pagamento | Só no táxi. Na primeira execução a posição não chegou. O script enviava um salto de 44 m, provavelmente rejeitado como implausível, mas isso não foi confirmado. Com movimentos pequenos, os 4 pontos foram aceites e a posição chegou |
| E4 | PASSOU | `size=99999999` fica limitado | |

## 4. Onde o relatório disse ✅

| Marca | Item do relatório | Resultado |
|---|---|---|
| 🔴 | Backoffice: "Motivos de admin" ✅ | `display_order` grande é aceite e gravado errado (B2). O resto funciona |
| 🟠 | "Multicaixa Express" ✅ | Express real funciona, mas webhook real e referência paga não foram provados. Devia ser 🟡 |
| 🟠 | "Tempo real em todos os serviços" ✅ | Só provado aqui no táxi. Os outros serviços dependem dos testes e2e existentes |
| 🟠 | "Nome e telemóvel de outra pessoa" ✅ | Grava-se, mas o telefone nunca é devolvido. O relatório regista isso noutra linha, mas este ✅ engana |

Os outros ✅ exercitados bateram certo. Não foram exercitados aqui: paragens em Mudanças, "Prestador e viatura" (coberto só por teste e2e) e o aviso de segurança (só de interface).

## 5. Bugs por gravidade

Só constam erros em código que existe. O que falta construir está na secção 3.

### B1. Crítico: pedido com paragens paga o preço da rota sem paragens

1. `POST /fare-estimates` sem paragens.
2. `POST /requests` com esse `fare_estimate_id` e `stops` com 2 paragens.
3. Resposta `201`, `fare_cents=97569`, quando a rota real custa 202 120.

**Causa:** `domain/entities/pricing.go:129-137` e `usecases/dispatch/requests.go:179` não comparam paragens. O valor segue para a cobrança real.

### B2. Alto: números grandes são gravados errados, sem erro

1. `POST /fleet/vehicles` com `capacity_kg: 99999999999`.
2. Resposta `201`; a BD guarda 1 215 752 191.

Também em `display_order` e `duration_minutes`, onde a resposta devolve um valor e a BD guarda outro. **Causa:** conversões para `int32` sem limite, em `infra/repository/fleet-repository.go` e `provider-document-repository.go`, marcadas `#nosec` como se estivessem validadas.

### B3. Alto: a mesma estimativa cria várias corridas ativas

1. Uma estimativa e 3 `POST /requests` com `Idempotency-Key` diferentes.
2. Três `201` e 3 pedidos ativos para o mesmo cliente, com motoristas procurados para os três.

A chave protege o duplo clique se a app a reutilizar, mas o servidor não impede isto.

### B4. Médio: passageiro de outra pessoa

- O motorista não recebe `passenger_phone` (`usecases/dispatch/requests.go:541-544`).
- O SMS tem texto fixo, sem motorista nem matrícula, e diz "táxi" em qualquer categoria (`usecases/notifications/request-events.go:177`). O texto em categorias que não táxi foi visto no código, não executado.

### B5. Médio: dados da app aceites ou ignorados em silêncio

- `details.volume_m3` é guardado sem validação.
- O motivo em `POST /offers/{id}/reject`, `voucher_code` e os toggles em `PATCH /users/me` respondem sucesso e são descartados, por isso a app julga que funcionou.

### B6. Médio: regras das telas não validadas

Horários fora dos 15 min são aceites. O limite de 3 paragens não é configurável.

### B7. Baixo: teste instável

Ver secção 2.

### Visto no código, não executado

Em `usecases/payments/payments.go` (`ConfirmCash`), um motorista pode confirmar dinheiro num pedido de Express por pagar, o que apaga a dívida do cliente. Pode ser intencional, mas precisa de decisão.

## 6. O que está simulado ou por provar

| Integração | Estado neste QA | Antes de produção |
|---|---|---|
| SMS (Castbrick) | **Simulado:** recetor local, para não enviar SMS reais a números inventados. O conteúdo foi capturado | Enviar para um telefone real |
| Push (FCM) | **Não configurado:** `FCM_*` vazios; a BD regista `fcm/skipped` | Credenciais e um dispositivo real |
| E-mail (Resend) | Apontado para o recetor local; não exercitado | Teste real |
| AppyPay Express | **Real**, ambiente de teste, números simulados | Webhook real e produção |
| AppyPay Referência | **Real** até à criação; não paga | Pagamento real e webhook |
| Mapbox | **Real** | |
| R2 | Não exercitado neste QA | Fotos de pedidos não existem |
| Tempo real (SSE, Redis, RabbitMQ) | **Real** | |
| Provedores no script | Aprovação por SQL em vez de envio de documentos | Coberto por `TestPedroCunga...` |

## 7. Cobertura dos ficheiros novos

Medida com `integration` e `e2e` juntos. A execução teve a falha descrita na secção 2, por isso os valores são aproximados. Não inclui o script de QA, por isso frota e `/admin/requests` aparecem quase a zero.

| Ficheiro | Cobertura |
|---|---|
| `domain/entities/payment.go` | 92,2% |
| `infra/payment/appypay/client.go` | 83,8% |
| `infra/repository/payment-repository.go` | 83,3% |
| `usecases/payments/payments.go` | 74,8% |
| `infra/rest/payments/controllers/payment-controller.go` | 70,4% |
| `infra/payment/appypay/unavailable.go` | 40,0% |
| `usecases/providers/providers.go` | 70,0% |
| `infra/repository/provider-document-repository.go` | 67,9% |
| `domain/entities/rejection_reason.go` | 52,4% |
| `infra/repository/fleet-repository.go` | 9,2% |
| `domain/entities/fleet.go` | 6,1% |
| `usecases/fleet/fleet.go` | 0,4% |
| `infra/rest/fleet/controllers/fleet-controller.go` | 0,4% |
| `usecases/fleet/workshops.go` | 0,0% |
| `usecases/dispatch/admin_requests.go` | 0,0% |

## 8. Efeitos deste QA no ambiente

- **BD de desenvolvimento:** ficou com utilizadores, pedidos, pagamentos, viaturas e oficinas de teste (121 utilizadores, 79 pedidos, 17 pagamentos à data). Motivos `qa_*` ficaram inativos.
- **MFA do `admin@agora.ao` e do `parceiro@agora.ao`:** foi reiniciado pelo script. É preciso configurar de novo no próximo login.
- **Conta de teste da AppyPay:** ficou com cobranças, reembolsos e referências pendentes de teste.
- **API do Docker:** esteve parada durante as execuções e foi reposta.
- **Scripts:** o brief pediu `tests/e2e/`, mas o projeto já usa `test/e2e/` para os testes em Go. Ficaram os dois.

**Repetir:** `python3 tests/e2e/qa_flows.py [grupo ...]`, a partir da raiz do atlas.
