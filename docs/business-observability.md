# Business observability

Métricas, spans e logs de **negócio** do Atlas, sobre a stack de observabilidade
que já existe (OpenTelemetry, Prometheus, Loki, Tempo, Grafana, Alertmanager).
Nada de ferramentas novas: as métricas técnicas, os dashboards e os alertas que
já existiam ficam como estavam.

| O quê | Onde |
|---|---|
| Instrumentação (métricas, spans, logs) | `infra/observability/business/` |
| Contrato usado pelos usecases | `domain/services/business_events.go` (`IBusinessEvents`) |
| Emissão só depois do commit | `shared/txhook/` + `infra/database/postgres/tx.go` |
| Recording rules, alertas, SLOs | `infra-prod/support/monitoring/prometheus/rules/business.yml` |
| Testes das regras | `infra-prod/support/monitoring/prometheus/rules/tests/business_test.yml` |
| Dashboard "Atlas – Negócio" (`uid: atlas-business`) | `infra-prod/support/monitoring/grafana/dashboards/atlas-business.json` (cópia em `monitoring/grafana/dashboards/` para o compose local) |

## Como funciona

```
usecase (dentro da transação)
  └─ IBusinessEvents.PaymentSettled(ctx, ...)      ← uma chamada, sem OTel no usecase
       └─ txhook.AfterCommit                        ← fica em fila na transação
            commit OK  → métrica (OTel Metrics API) + atributos no span + log JSON
            rollback   → descartado
```

- **Métricas**: OpenTelemetry Metrics API, meter `github.com/Agora-Mobility/atlas/business`.
  O exporter Prometheus do OTel regista-as no mesmo registry do `client_golang`, por isso
  saem no mesmo `/metrics`. O caminho até ao Prometheus não muda: Alloy faz o scrape → remote write.
  Os exemplars (trace id) seguem no formato OpenMetrics.
- **Depois do commit**: os usecases chamam o recorder dentro da transação, onde o estado muda. O
  `TxManager` só corre a fila depois de `Commit` e descarta-a no rollback. Sem transação
  (por exemplo nos testes com um tx falso), emite logo.
- **Nunca parte o negócio**: cada emissão corre dentro de `recover()`. Não há I/O, chamadas bloqueantes
  nem queries à BD: os dados chegam pelos argumentos (`PaymentEvent`, `RequestTransition`, ...).
- **Cardinalidade**: todos os valores de label passam por um conjunto fechado (`labels.go`).
  Um valor desconhecido vira `other`. IDs, telefones, e-mails e texto livre **nunca** são labels.
  Vão para o span e para o log.
- Sem recorder (testes unitários), os usecases usam `services.NoBusinessEvents`.

## Catálogo de métricas

Os nomes no Prometheus seguem a conversão do OTel: pontos viram `_`, a unidade `s` acrescenta
`_seconds`, os counters acrescentam `_total`, e unidades entre chavetas (`{AOA}`, `{request}`) não
aparecem no nome. Os valores monetários estão **em kwanzas** (cêntimos / 100), com a moeda no label
`currency`.

| Nome OTel | Nome no Prometheus | Tipo | Unidade | Labels | Significado | Emitida em |
|---|---|---|---|---|---|---|
| `atlas.business.requests.created` | `atlas_business_requests_created_total` | Counter | `{request}` | `category`, `payment_method`, `mode` | Pedidos criados | `dispatch.announceTransition` (para `created`) |
| `atlas.business.requests.cancelled` | `atlas_business_requests_cancelled_total` | Counter | `{request}` | `category`, `actor_type`, `stage` | Pedidos cancelados, por quem cancela e se já havia prestador | transição para `cancelled` (dispatch e marcações) |
| `atlas.business.requests.fare` | `atlas_business_requests_fare_{bucket,sum,count}` | Histogram | `{AOA}` | `category`, `payment_method`, `currency` | Tarifa final dos pedidos concluídos (valor médio = sum/count) | transição para `completed` |
| `atlas.business.requests.accept.duration` | `atlas_business_requests_accept_duration_seconds_*` | Histogram | `s` | `category` | Tempo do pedido até um prestador aceitar (só pedidos imediatos) | transição para `accepted` |
| `atlas.business.payments.created` | `atlas_business_payments_created_total` | Counter | `{payment}` | `category`, `payment_method`, `kind`, `gateway` | Pagamentos iniciados (inclui dinheiro confirmado e gorjetas) | `payments.Pay`, `Tip`, `ConfirmCash` |
| `atlas.business.payments.settled` | `atlas_business_payments_settled_total` | Counter | `{payment}` | `category`, `payment_method`, `kind`, `gateway`, `status`, `error_reason` | Pagamentos que chegaram a um estado final | `payments.persistChange`, duplicados, `ConfirmCash` |
| `atlas.business.payments.amount` | `atlas_business_payments_amount_*` | Histogram | `{AOA}` | `category`, `payment_method`, `kind`, `currency` | Valor dos pagamentos pagos. **`_sum` é a receita bruta** | pagamento `paid` |
| `atlas.business.payments.settle.duration` | `atlas_business_payments_settle_duration_seconds_*` | Histogram | `s` | `payment_method`, `status` | Do pagamento criado até pago/falhado (só gateway) | pagamento `paid`/`failed` |
| `atlas.business.refunds.amount` | `atlas_business_refunds_amount_*` | Histogram | `{AOA}` | `category`, `payment_method`, `kind`, `source`, `currency` | Valores reembolsados | `payments.refund` |
| `atlas.business.users.registered` | `atlas_business_users_registered_total` | Counter | `{user}` | `role`, `channel` | Registos | `auth.Register`, registo de prestador, motorista de frota |
| `atlas.business.providers.reviewed` | `atlas_business_providers_reviewed_total` | Counter | `{provider}` | `outcome` | Revisões de prestadores | `providers.ReviewProvider` |
| `atlas.business.bookings.transitions` | `atlas_business_bookings_transitions_total` | Counter | `{booking}` | `status`, `actor_type` | Mudanças de estado das marcações de oficina | `bookings.announce` |

### Valores possíveis dos labels

| Label | Valores |
|---|---|
| `category` | slug da categoria (`taxi`, `towing`, `removals`, `maintenance`); catálogo fechado gerido no backoffice, o mesmo label das métricas `atlas_business_*` antigas |
| `payment_method` | `cash`, `wallet`, `gpo`, `reference`, `other` |
| `gateway` | `appypay` (gpo, reference), `none` (cash, wallet), `other` |
| `kind` | `fare`, `cancellation_fee`, `tip`, `extra` |
| `status` (pagamentos) | `paid`, `failed`, `expired`, `duplicate` (pago duas vezes, creditado na carteira; não conta como receita) |
| `error_reason` | `none`, `declined` (o gateway recusou ao criar a cobrança), `charge_failed` (falhou depois), `expired` (o cliente não pagou a tempo) |
| `mode` | `now`, `scheduled` |
| `actor_type` (pedidos) | `customer`, `provider`, `operator`, `system` |
| `actor_type` (marcações) | `customer`, `workshop`, `provider`, `operator`, `system` |
| `stage` | `before_accept`, `after_accept` |
| `channel` | `customer_app`, `provider_app`, `partner_panel`, `backoffice` |
| `role` | `customer`, `provider`, `staff` |
| `outcome` | `approved`, `rejected`, `suspended` |
| `source` (reembolsos) | `admin`, `automatic` |

**Buckets** (kwanzas): 500, 1k, 2k, 3,5k, 5k, 7,5k, 10k, 15k, 25k, 50k, 100k, 250k, 500k, 1M. Cobrem
desde um táxi curto em Luanda até uma mudança ou uma reparação. Pagamento: 5 s a 3 dias (uma
referência pode demorar um dia). Aceite: 5 s a 10 min.

### Métricas que já existiam e são reaproveitadas

`atlas_business_fare_quotes_total{stage="estimate"}` (topo do funil),
`atlas_business_request_transitions_total{status}` (oferta, aceite, concluído) e
`atlas_business_first_offer_delivered_seconds` (SLO 2). Não foram alteradas.

## Spans

Atributos acrescentados ao span que está ativo (o pedido HTTP ou o evento consumido):

| Evento | Atributos |
|---|---|
| Pedido | `request.id`, `request.category`, `request.status.from`, `request.status.to`, `actor.type`, `request.fare_cents`, `request.currency` |
| Pagamento | `payment.id`, `request.id`, `payment.method`, `payment.kind`, `payment.gateway`, `payment.status`, `payment.amount_cents`, `payment.currency`, `payment.error_reason`, `payment.failure_code` |
| Reembolso | `payment.id`, `refund.amount_cents`, `refund.source` |
| Registo | `user.id`, `user.role` |
| Prestador | `provider.id`, `provider.review` |
| Marcação | `booking.id`, `booking.status.from`, `booking.status.to` |

Spans novos: `payment.charge` (chamada ao gateway para cobrar) e `payment.reconcile` (consulta do
estado da cobrança). Telefones e e-mails nunca vão para spans.

> O sampler só começa traces nas bordas (HTTP e eventos consumidos). Um pagamento confirmado pelo
> **reconciler** em background não tem trace; confirmado pelo webhook ou pela API, tem.

## Logs (Loki)

Um log JSON por evento, com `message="business event"` e o nome no campo **`event`**. O `event` vai no
corpo, **não** é label do Loki: os streams continuam só com `level`, `service_name` e `environment`.
`trace_id` e `span_id` são acrescentados pelo `logger.FromContext` quando há span; o Alloy põe-nos
em structured metadata e o Grafana mostra o botão "Ver trace".

Eventos: `request_created`, `request_accepted`, `request_completed`, `request_cancelled`,
`request_expired`, `payment_created`, `payment_paid`, `payment_failed`, `payment_expired`,
`payment_duplicate`, `payment_refunded`, `user_registered`, `provider_reviewed`,
`booking_confirmed`, `booking_cancelled`, `booking_completed`.

```json
{"level":"info","message":"business event","event":"payment_failed","payment_id":"01a1…","request_id":"01a1…",
 "category":"taxi","payment_method":"gpo","kind":"fare","gateway":"appypay","amount_cents":450000,"currency":"AOA",
 "status":"failed","source":"api","error_reason":"declined","failure_code":"4012","trace_id":"…","span_id":"…"}
```

Os logs só têm IDs; nunca telefones, e-mails nem dados de cartão.

## Exemplos de queries

PromQL:

```promql
# Pedidos por minuto, por categoria
sum by (category) (rate(atlas_business_requests_created_total[5m])) * 60

# Pedidos agora vs. a mesma hora da semana passada (1 = igual)
sum(rate(atlas_business_requests_created_total[1h])) / sum(rate(atlas_business_requests_created_total[1h] offset 1w))

# Receita bruta por hora, em kwanzas
sum(rate(atlas_business_payments_amount_sum[1h])) * 3600

# Valor médio por serviço concluído
sum(rate(atlas_business_requests_fare_sum[1h])) / sum(rate(atlas_business_requests_fare_count[1h]))

# Taxa de sucesso por gateway e método (expirados e duplicados ficam de fora)
sum by (gateway, payment_method) (rate(atlas_business_payments_settled_total{status="paid"}[10m]))
  / sum by (gateway, payment_method) (rate(atlas_business_payments_settled_total{status=~"paid|failed"}[10m]))

# Principais motivos de falha nas últimas 24h
topk(5, sum by (error_reason, payment_method) (increase(atlas_business_payments_settled_total{status=~"failed|expired"}[24h])))

# p95 do tempo até um prestador aceitar
histogram_quantile(0.95, sum by (le, category) (rate(atlas_business_requests_accept_duration_seconds_bucket[15m])))
```

LogQL:

```logql
# Todos os pagamentos falhados, com o código do gateway
{service_name="atlas"} | json | event="payment_failed" | line_format "{{.payment_method}} {{.error_reason}} {{.failure_code}} {{.payment_id}}"

# Tudo o que aconteceu a um pedido
{service_name="atlas"} | json | event=~"request_.*|payment_.*" | request_id="<uuid>"

# Logs de um trace (vindo de um exemplar ou do Tempo)
{service_name="atlas"} | trace_id="<trace id>"

# Cancelamentos depois do aceite, por minuto
sum(count_over_time({service_name="atlas"} | json | event="request_cancelled" | stage="after_accept" [1m]))
```

## Recording rules

| Regra | O quê |
|---|---|
| `business:hours:active` | 1 em horário de operação (WAT), ausente fora dele |
| `business:requests_created:rate5m` / `:rate15m` | pedidos/s |
| `business:revenue:rate1h` | receita/s (× 3600 = por hora) |
| `business:request_fare_avg:rate1h` | tarifa média dos concluídos |
| `business:payments_settled:rate5m` | pagamentos finais/s por estado e motivo |
| `business:payment_success_ratio:rate5m` | pagos / (pagos + falhados), por gateway e método |
| `business:gateway_payment_success_ratio:rate10m` / `business:gateway_payment_failure_ratio:rate10m` | o mesmo em 10 min, para os alertas |
| `business:slo_payments:error_ratio_rate{5m,30m,1h,2h,6h,1d,3d}` | SLO 1 |
| `business:slo_first_offer:error_ratio_rate{5m,30m,1h,2h,6h,1d,3d}` | SLO 2 |

### Limites configuráveis

Cada limite é uma recording rule no grupo `atlas-business-config` de `business.yml`. Para mudar um limite,
altere o valor do `vector(...)`, corra os testes (secção "Validação") e faça deploy. O valor atual
pode ser consultado no Prometheus pelo nome da regra.

| Regra | Valor | Usada por |
|---|---|---|
| `business:config:payment_success_ratio_min` | 0.95 | AtlasBusinessPaymentSuccessLow |
| `business:config:gateway_failure_ratio_max` | 0.10 | AtlasBusinessGatewayFailing |
| `business:config:payments_min_10m` | 5 | volume mínimo dos dois alertas de pagamentos |
| `business:config:requests_drop_ratio` | 0.6 (40% abaixo) | AtlasBusinessRequestsBelowLastWeek |
| `business:config:requests_min_last_week_15m` | 5 | idem, volume mínimo da semana anterior |
| `business:config:no_requests_min_last_week_30m` | 3 | AtlasBusinessNoRequests |
| `business:config:hours_start_wat` / `hours_end_wat` | 6 / 23 | horário de operação (Luanda) |
| `business:config:slo_min_events_1h` | 10 | volume mínimo para as páginas dos SLOs |

A janela de 30 min de `AtlasBusinessNoRequests` e as janelas dos SLOs ficam na expressão, porque o
PromQL não aceita intervalos vindos de uma série.

## Alertas e runbooks

Todos têm `summary`, `description`, `severity`, `dashboard: atlas-business` e `runbook_url` a
apontar para esta secção. `critical` vai para o Telegram e repete de hora a hora; `warning` notifica
uma vez e repete de 4 em 4 horas (configuração do Alertmanager que já existia).

### AtlasBusinessPaymentSuccessLow

**Dispara** quando a taxa de sucesso dos pagamentos por gateway (pagos / pagos + falhados) fica abaixo de
95% durante 10 min, com pelo menos 5 pagamentos nos últimos 10 min. **Severidade**: critical.

1. No dashboard, painel "Principais motivos de falha": `declined` aponta para o pedido de cobrança
   (dados do cliente, limites, gateway a recusar); `charge_failed` aponta para falhas depois de criada a cobrança.
2. Loki: `{service_name="atlas"} | json | event="payment_failed"`. Veja o `failure_code` e abra o trace.
3. Painel "Erros 5xx das rotas críticas" e os spans `payment.charge`: o AppyPay está a responder? há timeouts?
4. Se o problema for do gateway, avise o suporte da AppyPay e a operação, que pode sugerir dinheiro ou carteira.

### AtlasBusinessGatewayFailing

**Dispara** quando um gateway e método (por exemplo `appypay/gpo`) falha mais de 10% dos pagamentos
durante 10 min, com volume mínimo. **Severidade**: warning.

1. Isole o método: GPO (pagamento no telemóvel) e referência falham por motivos diferentes.
2. Siga os passos 2 e 3 do alerta anterior, filtrando `payment_method`.
3. Se for só um método, a operação pode desativá-lo temporariamente para a categoria (backoffice, métodos de pagamento).

### AtlasBusinessRequestsBelowLastWeek

**Dispara** quando os pedidos dos últimos 15 min ficam 40% abaixo da mesma janela da semana anterior,
durante 15 min, em horário de operação. A semana anterior tem de ter pelo menos 5 pedidos na janela.
**Severidade**: warning.

1. Confirme no painel "Pedidos/min — agora e semana passada" (a linha tracejada é a semana anterior).
2. Feriado, chuva forte ou um evento explicam a queda? Se sim, silencie o alerta no Alertmanager.
3. Procure o ponto de falha no funil: estimativas a cair → app ou mapas; estimativas normais mas
   pedidos a cair → `POST /api/v1/requests` (latência, 5xx, tarifas), ou o cliente não aceita o preço.
4. Veja as anotações de deploy: houve um deploy pouco antes?

### AtlasBusinessNoRequests

**Dispara** quando ninguém cria um pedido durante 30 min, em horário de operação, e na mesma janela da
semana anterior houve pelo menos 3. **Severidade**: critical.

1. Teste o fluxo completo na app de cliente: estimativa → pedido.
2. `AtlasPublicEndpointDown` ou `AtlasHighErrorRate` também estão a disparar? Comece por eles.
3. Loki: `{service_name="atlas", level="error"}` nos últimos 30 min, e as rotas `/api/v1/fare-estimates` e `/api/v1/requests`.
4. Se a API estiver bem, o problema pode estar na app (release nova, versão bloqueada) ou nas tarifas e zonas (backoffice).

### AtlasSLOPaymentsBurnFast

**SLO 1**: 99,5% dos pagamentos por gateway são pagos (orçamento de erro: 0,5%). Dispara quando as
falhas consomem o orçamento a 14,4× (janelas de 1h e 5m) ou a 6× (6h e 30m), com pelo menos 10
pagamentos na última hora. **Severidade**: critical. Siga o runbook de `AtlasBusinessPaymentSuccessLow`.

### AtlasSLOPaymentsBurnSlow

Consumo a 3× (1d e 2h) ou a 1× (3d e 6h). **Severidade**: warning. Não é urgente: veja a tendência
dos motivos de falha nesta semana e abra um ticket (gateway, validação do telefone, janela de GPO).

### AtlasSLOFirstOfferBurnFast

**SLO 2**: 95% dos pedidos recebem a primeira oferta em 21 s ou menos (orçamento: 5%). Dispara a 14,4× ou
6×, com volume mínimo. **Severidade**: critical.

1. Dashboard "Dispatch": há prestadores online? A pesquisa encontra candidatos?
2. Ligações SSE e canal FCM: `AtlasOfferChannelFailing` está a disparar?
3. Uma zona sem prestadores à hora de ponta é um problema de oferta, não técnico: avise a operação.

### AtlasSLOFirstOfferBurnSlow

Consumo a 3× ou 1× nas janelas longas. **Severidade**: warning. Reveja as políticas de despacho (raios,
lotes) e a cobertura de prestadores por zona e hora.

## SLOs

| SLO | Objetivo | Bom / total | Orçamento (30 d) |
|---|---|---|---|
| Pagamentos | 99,5% | `settled{gateway!="none", status="paid"}` / `settled{gateway!="none", status=~"paid\|failed"}` | 0,5% |
| Primeira oferta | 95% em ≤ 21 s | `first_offer_delivered_seconds_bucket{le="21"}` / `_count` | 5% |

Alertas de burn rate (Google SRE Workbook, cap. 5):

| Severidade | Janela longa | Janela curta | Burn rate |
|---|---|---|---|
| critical | 1h | 5m | 14,4× |
| critical | 6h | 30m | 6× |
| warning | 1d | 2h | 3× |
| warning | 3d | 6h | 1× |

Os pagamentos expirados (o cliente não pagou) e os duplicados não contam para o SLO 1: não são
falhas do sistema. As recusas do gateway (`declined`) contam, mesmo quando a causa é do cliente.

## Dashboard "Atlas – Negócio"

Secções:

- **Visão geral**: pedidos/min, comparação com a semana anterior, receita/hora, valor médio,
  sucesso dos pagamentos e cancelamentos. Os gráficos de pedidos e receita mostram a semana anterior a tracejado.
- **Funil e pedidos**: estimativa → pedido → com oferta → aceite → concluído → pago; tempo até
  aceitar; cancelamentos por ator e fase; registos, marcações e revisões de prestadores.
- **Pagamentos**: sucesso por gateway e método, motivos de falha, tempo até pago, receita e reembolsos.
- **Correlação técnico ↔ negócio**: latência p95 e 5xx das rotas críticas ao lado de pedidos e falhas,
  com exemplars que abrem o trace no Tempo e um painel de logs de negócio com o botão "Ver trace".
- **SLOs**: orçamento restante em 30 dias e taxa de erro face ao objetivo.

Variáveis: `ds_prometheus`, `ds_loki` (datasources por variável, sem IDs de ambiente), `job`
(serviço), `category`, `payment_method`, `channel` e `event` (logs). Anotações: deploys e reinícios da
API (`changes(process_start_time_seconds[2m]) > 0`).

## Como acrescentar uma métrica de negócio

1. **Evento**: se for um evento novo, acrescente um método a `services.IBusinessEvents` e um struct com os
   dados (só tipos simples) em `domain/services/business_events.go`, e o método vazio em `NoBusinessEvents`.
2. **Instrumento**: crie-o em `newInstruments` (`infra/observability/business/recorder.go`). Nome
   `atlas.business.<domínio>.<coisa>`, unidade (`s`, `{AOA}`, `{request}`) e descrição. Para valores
   ou durações, use um histograma com buckets à medida do negócio.
3. **Labels**: só valores de um conjunto fechado; acrescente o conjunto a `labels.go` e passe sempre por
   `oneOf`. IDs vão para o span e para o log, nunca para o label.
4. **Emissão**: no recorder, dentro de `r.after(ctx, ...)` (depois do commit), com a métrica, os
   atributos no span e uma linha `emit(ctx, "<evento>", ...)`.
5. **Usecase**: chame o método no ponto em que o estado muda, dentro da transação, através de
   `services.BusinessOrNoop(u.Business)`. Se o usecase ainda não tiver o recorder, acrescente
   `Business services.IBusinessEvents` aos `Deps` e passe `c.Business` no container.
6. **Testes**: em `recorder_test.go`, com o `ManualReader`, verifique os atributos no sucesso e na falha
   e acrescente o nome Prometheus a `TestPrometheusNames`.
7. **Documentação**: acrescente a linha ao catálogo acima; se houver alerta, uma regra em `business.yml`,
   um teste em `rules/tests/business_test.yml` e um runbook nesta página.

## Validação local

```bash
make test                                   # recorder, txhook, usecases
make lint
go test -tags=integration -run TestAfterCommit ./infra/database/postgres/   # commit/rollback com Postgres real

# Regras (no repositório infra-prod, em support/monitoring/prometheus)
docker run --rm -v "$PWD":/w -w /w --entrypoint promtool prom/prometheus:v3.16.0 check rules rules/business.yml
docker run --rm -v "$PWD":/w -w /w --entrypoint promtool prom/prometheus:v3.16.0 test rules rules/tests/business_test.yml
```

Para ver o dashboard a encher com o compose local (Prometheus e Grafana; o compose local não tem Loki,
Tempo nem as regras):

1. `make docker-up`. Se os portos 5432 ou 5672 estiverem ocupados, use
   `POSTGRES_PORT=55432 RABBITMQ_PORT=55672 RABBITMQ_MANAGEMENT_PORT=55673 docker compose up -d`.
2. Gere tráfego com o cenário de carga: `test/load/run.sh` sobe uma stack própria. Contra o compose,
   faça o seed de uma base vazia com `test/load/seed` e corra
   `k6 run -e BASE_URL=http://127.0.0.1:8700 -e FIXTURES=fixtures.json -e REQUEST_RATE=2 test/load/dispatch.js`
   com `RATE_LIMIT_REQUESTS` alto na API.
3. Confirme em `curl -s localhost:8700/metrics | grep atlas_business_` e abra
   http://localhost:3000/d/atlas-business.
