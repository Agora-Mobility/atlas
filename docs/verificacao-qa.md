# Verificação de QA independente: API Atlas vs. telas da app

- **Data:** 2026-11-03
- **Estado verificado:** branch `feat/geo-dispatch-realtime`, em `38b5544` (50 commits e 269 ficheiros desde `d3002f5`).
- **Independência:** revisão feita por um revisor que não escreveu o código. As asserções foram escritas de novo a partir do requisito da tela; os helpers de `qa_flows.py` foram reutilizados, mas não as suas verificações.
- **Documento anterior:** a verificação do autor está preservada em `docs/verificacao-qa-autor-2026-11-02.md`. Este documento substitui-o como resultado atual.
- **Ambiente:** API local com recetor de SMS/e-mail (`127.0.0.1:8799`), PostgreSQL/PostGIS, RabbitMQ, Redis e Mapbox reais. BD de desenvolvimento na migração `35`, `dirty=false`.
- **Evidência:** `tests/e2e/evidence/independent-20261103-225448.json` (execução final completa: 31 cenários, 201 verificações), `independent-20261103-224050.json` (segurança, após a correção) e `run-20261103-221011.json` (`qa_flows` completo) e `run-20261103-224421.json` (grupo `orders` repetido depois da correção). Logs em `tests/e2e/evidence/independent-api.log`.

## 1. Resumo

| Execução | Resultado |
|---|---|
| `python3 tests/e2e/independent_verification.py` | **31 passaram, 0 falharam, 0 não testáveis**: 24 cenários funcionais (169 verificações), 6 de segurança (27 verificações) e 5 de contrato |
| `python3 tests/e2e/qa_flows.py` | **42 passaram, 0 falharam, 0 não testáveis**; grupo `orders` repetido depois da correção: 8/8 |
| `go test -tags=integration,e2e -count=1 -v ./...` | **928 passaram, 0 falharam, 0 saltados**, 31 pacotes, `EXIT=0` |
| Cobertura `test/e2e` sobre `usecases`, `infra` e `domain` | **59,8%** das instruções |

**Veredicto:** não foi encontrado nenhum bug crítico, alto ou médio no estado final. O único desvio funcional encontrado (**P2 "Repetir"**) foi corrigido e re-verificado: o endpoint cria agora o pedido novo por omissão e aceita `{"preview": true}` para só devolver a rota e os preços. Restam três observações de desenho/dados (taxa por paragem a 0, capacidade de mudanças por dia e não por janela, paginação/estado permissivos), que não são falhas contra o requisito tal como está escrito.

## 2. Método

1. Fechar os dados de QA e neutralizar pedidos abertos antes de cada grupo (`neutralize_test_data`).
2. Parar a API do Docker e lançar o binário local com `CASTBRICK_BASE_URL` e `RESEND_BASE_URL` apontados ao stub. Isto impede SMS/e-mail reais.
3. Executar os grupos `docs`, `taxi`, `maint`, `tow`, `mov`, `orders`, `sec`, com valores calculados a partir da BD (tarifas, políticas, catálogo), não a partir da resposta da API.
4. No fim, esperar que as filas esvaziem e repor a API do Docker. Se o *drain* falhar, deixar a API parada de propósito.
5. Comparar os resultados com `docs/auditoria-api-vs-telas.md` e com a secção R do documento anterior.

**Correções feitas apenas no script durante a revisão** (antes da correção do produto descrita na secção 11):

| Sintoma | Causa | Correção |
|---|---|---|
| V0 dizia que faltavam 18 rotas | `paths` do OpenAPI tem prefixo `/api/v1` | `removeprefix("/api/v1")` |
| V-T1 esperava 409 onde a API devolve 422 | código de erro trocado no teste | aceitar `422 FARE_ESTIMATE_INVALID` |
| V-M2 duração da marcação vinha 0 | `extract(epoch ...)` sem `/60` | converter para minutos |
| V-R1 não trazia oficina recomendada | faltava `lat`/`lng` | `GET /towing/options?lat&lng` |
| V-D2 total não batia | o total inclui ajudantes e extras | `vehicle + 2*500000 + 1500000` |
| V-D3 nome com acento não batia | `Camião 7 t` | comparação exata com acento |
| V-P3 caixa de entrada vazia | inbox do motorista exige client `PROVIDER` | token com o client correto |

## 3. Contrato e migrações (V0)

| Verificação | Resultado | Evidência |
|---|---|---|
| OpenAPI documenta as rotas usadas | **PASSOU** | 227 caminhos; todas as 18 rotas exigidas presentes, incluindo `/requests/{id}/repeat` e `/requests/{id}/receipt` |
| Migrações aplicadas | **PASSOU** | versão 35, `dirty=false` |
| `dispatch_policies.max_stops` existe | **PASSOU** | coluna presente |
| Preferências de notificação existem | **PASSOU** | colunas em `users` |

## 4. Táxi (T1–T8)

| ID | Requisito | Resultado | Evidência |
|---|---|---|---|
| T1 | Com 2 paragens, valor e rota mudam; estimativa de outra rota é recusada | **PASSOU** | sem paragens 93 319; com 2 paragens 192 945 (exato); rota 4 088 m/407 s → 10 390 m/1 115 s; pedido guarda 192 945 e 2 paragens; estimativa sem paragens no pedido com paragens → `409 FARE_ESTIMATE_INVALID` |
| T2 | Remover paragem recalcula; limite de paragens configurável | **PASSOU** | novo preço 116 592 (exato); ajustamentos somam −76 353, igual ao delta; com `max_stops=1` na BD, 2 paragens → `422`; reposto a 3 volta a aceitar |
| T3 | Agendar amanhã 07:30, *job* 30 min antes, janela de 7 dias | **PASSOU** | `201 scheduled`; `next_action_at = scheduled_for − 1800 s`; 8 dias, passado e 07:37 → `422`; *watcher* despacha; cancelar sem motorista custa 0 |
| T4 | Pedido para outra pessoa: motorista vê nome/telefone; passageiro recebe SMS | **PASSOU** | motorista vê `Maria Neto +244920666121`; SMS no stub com matrícula `QA-644554`, motorista e ligação; cobrança no nome de quem pediu |
| T5 | Cancelamento: custo antes, catálogo de motivos, pré-aviso de 2 min | **PASSOU** | texto livre → `422`; motivo inexistente → `422 REASON_NOT_APPLICABLE`; "Outro" sem texto → `422`; 2 min grátis; depois 50 000; sem confirmar → `409 CANCEL_FEE_CHANGED`; com confirmação `200 50000`; dívida é a taxa |
| T6 | Motorista recusa oferta com motivo e a oferta passa ao seguinte | **PASSOU** | motivo guardado (204); relatório agrupa `too_far`; oferta reatribuída |
| T7 | Avaliação, elogios, média e gorjeta | **PASSOU** | resumo soma 93 244; 0/6/99999999999 estrelas recusadas; duplicado recusado; média `4|1|1`; gorjeta acima do limite → `422 TIP_OUT_OF_RANGE`; 500 Kz no ledger do motorista a 100% |
| T8 | Voucher de 10%, expirado e de outro serviço | **PASSOU** | preview 9 324 de 93 244; total do servidor 83 920; expirado/outro serviço/inexistente recusados; segunda utilização barrada |

**Observação T1:** a coluna `service_tariffs.stop_fee_cents` existe e é lida, mas o valor atual na BD é **0**. A variação de preço com paragens vem apenas da rota. Se o negócio espera cobrar por paragem, falta configurar o valor; não é uma falha de código.

## 5. Manutenção (M1–M6)

| ID | Requisito | Resultado | Evidência |
|---|---|---|---|
| M1 | Lista, filtro por categoria, pesquisa, raio e "aberta até" | **PASSOU** | oficina 201; categoria+pesquisa devolve; outra categoria e fora do raio não; cartão com domicílio/aberta/até; favoritos |
| M2 | 2 serviços: duração total, capacidade e dia fechado | **PASSOU** | duração 120+45 = 165 min; marcação ocupa 165; segundo no mesmo horário → `409 BOOKING_SLOT_FULL`; dia fechado aparece `closed`; marcar → `422 BOOKING_SLOT_INVALID`; outro cliente não vê |
| M3 | Ao domicílio: taxa e promoção de 15% uma vez por cliente | **PASSOU** | taxa+desconto no total (570 000 → 3 230 000); promoção só uma vez; oficina sem domicílio → `422 HOME_SERVICE_UNAVAILABLE` |
| M4 | Fotos: 5 aceites, 6.ª recusada, PDF recusado | **PASSOU** | 5 anexadas (200); 6.ª → `422 PHOTO_LIMIT`; PDF recusado |
| M5 | Estados, orçamento extra aprovado/recusado | **PASSOU** | salto de estado recusado; extra antes de receber recusado; com viatura na oficina não cancela; sem resposta não fica pronta nem aprova; aprovado entra no total (4 700 000); recusado não; histórico `confirmed,vehicle_received,in_service,ready` |
| M6 | Só cobra depois de aprovado; ledger da oficina | **PASSOU** | não concluído/pronta sem aprovação → `409 REQUEST_NOT_PAYABLE`; aprovar conclui e deve 35 000 Kz; `amount_cents=1` ignorado; ledger da oficina 3 150 000 (total − comissão); recibo PDF |

## 6. Reboque (R1–R3)

| ID | Requisito | Resultado | Evidência |
|---|---|---|---|
| R1 | Preço por tipo de viatura e km a mais, ao cêntimo | **PASSOU** | Ligeiro 2 500 000; SUV/Pick-up 3 200 000; Motociclo 1 500 000; km a mais 2 917 350 (todos exatos); incidentes+oficina recomendada; "o que aconteceu" obrigatório; pedido guarda tipo/avaria |
| R2 | Cancelar durante a procura é grátis | **PASSOU** | pré-aviso 0; cancelado sem custo |
| R3 | Código de entrega de 4 dígitos | **PASSOU** | 4 dígitos na BD; motorista não vê o código na oferta; código errado → `422 COMPLETION_CODE_WRONG`; certo conclui |

## 7. Mudanças (D1–D3)

| ID | Requisito | Resultado | Evidência |
|---|---|---|---|
| D1 | Volume vem dos artigos; viatura recomendada | **PASSOU** | catálogo 10 divisões; volume do servidor 9 200; recomendada `Camião 3,5 t`; carrinha recusada para 9,2 m³; app não impõe volume/preço; preço fechado 8 500 000; duração/seguro 272 min / 50 000 000 |
| D2 | Total = viatura + ajudantes + extras | **PASSOU** | catálogo 4 500 000 / 8 500 000 / 14 000 000; totais 7 000 000 / 11 000 000 / 16 500 000; `Camião 3,5 t` = 110 000 Kz; máximo de ajudantes; extra inexistente recusado; catálogo e rota não alteram o preço fechado |
| D3 | Janela/dia esgotado, 24 h e concorrência | **PASSOU** | a menos de 24 h taxa 15 000 (sem confirmar → `409 CANCEL_FEE_CHANGED`); a mais de 24 h grátis; fora da janela → `422 REMOVAL_WINDOW_INVALID`; capacidade 2 sob concorrência `[201,409,409,201,409]`; disponibilidade `full`; cancelar liberta |

**Observação D3:** a capacidade é **por viatura e por dia** (`catalog_items.attrs.daily_capacity`), não por janela. O calendário devolve `days[].status` e esconde as janelas quando o dia enche. Se a tela precisar de janelas independentes (manhã cheia, tarde livre), o modelo atual não o representa. É uma decisão de desenho a confirmar, não um erro detetado.

## 8. Pedidos, conta e notificações (P1–P4)

| ID | Requisito | Resultado | Evidência |
|---|---|---|---|
| P1 | Separadores Em curso/Histórico/Marcados e filtro por serviço | **PASSOU** | Em curso inclui reboque; mudança em `scheduled`; Histórico tem a concluída; Marcados junta corrida agendada e oficina; filtro por serviço |
| P2 | Código legível, recibo PDF e **Repetir cria pedido novo** | **PASSOU** | código `TX-11275`; PDF com total 932,44 Kz e código; outro cliente não vê recibo; `{"preview": true}` devolve `201` com estimativas e não cria (1 → 1); repetir sem body devolve `201` com o pedido novo e as estimativas (1 → 2), com o mesmo trajeto e serviço; problema ligado ao pedido |
| P3 | Notificações criadas, lidas e *deep link* | **PASSOU** | eventos geram 4 notificações; *deep link* para o pedido; outro cliente não marca; marcar lida; uma menos por ler; todas lidas; motorista recebe oferta com *deep link* |
| P4 | Eliminar conta por SMS e preferências persistidas | **PASSOU** | sem confirmação recusa; código sai por SMS no stub; código errado recusa; conta eliminada na BD; sessão termina; toggles guardados/lidos; idioma inválido recusado |

## 9. Segurança (S1–S6)

| ID | Requisito | Resultado | Evidência |
|---|---|---|---|
| S1 | IDOR entre clientes e motoristas | **PASSOU** | 15 tentativas: ver/cancelar/rota/boost/método/pagar de outro cliente → `404`; motorista estranho em *en-route/arrive/start/complete/cash* recusado; cliente não acede a `/admin/payments`; parceiro não acede a `/admin/requests` |
| S2 | Motorista não aprovado/suspenso/sem viatura não opera | **PASSOU** | sem perfil aprovado não fica *online*; não aprovado e suspenso não aceitam; sem viatura não fica *online* |
| S3 | Entradas inválidas nunca dão 5xx | **PASSOU** | JSON malformado → 400; 31 casos sem nenhum 5xx |
| S4 | Preço e estado vêm do servidor | **PASSOU** | payload não impõe preço/estado: 93 319 / `searching` |
| S5 | Duplo clique não duplica pedidos nem pagamentos | **PASSOU** | 5 pedidos com a mesma `Idempotency-Key` → 1 na BD (`[201,409,409,409,409]`); a mesma estimativa com 3 chaves diferentes → 1 pedido (`[409,201,409]`); 5 pagamentos → 1 (`[201,200,200,200,200]`) |
| S6 | Dois motoristas aceitam ao mesmo tempo | **PASSOU** | aceites simultâneos `[200,409]` |

**Observações S3 (aceites, mas permissivos):** `GET /requests?page=-1&size=99999999` → `200`; `GET /admin/requests?status=%27%3B%20DROP%20TABLE%20users` → `200` (parâmetro tratado como valor, não como SQL). Não são falhas de segurança, mas deviam ser `400`/`422` por validação.

## 10. Onde o relatório anterior dizia ✅

| Item | Relatório anterior | Esta verificação |
|---|---|---|
| **PD02 Repetir** | A secção R dá `42/42` e o `qa_flows.py` aceita `201` com estimativas; a auditoria base dizia "Endpoint, ou a app refaz estimativa e pedido" | **Corrigido e verificado**: `POST /requests/{id}/repeat` cria agora um pedido novo com o mesmo trajeto; `{"preview": true}` mantém o comportamento antigo (só rota e preços). Ver secção 11 |
| Limite de paragens (T2) | Auditoria: 🟡 "limite de 3 fixo no código" | **Corrigido e verificado**: vem de `dispatch_policies.max_stops`; com 1, 2 paragens → `422`; reposto a 3 aceita |
| Preço com paragens (T1) | Auditoria: ❌ crítico, estimativa de outra rota era aceite | **Corrigido e verificado**: estimativa sem paragens num pedido com paragens → `409 FARE_ESTIMATE_INVALID`; valor e rota exatos |
| Taxa por paragem (T1) | Auditoria: ❌ "sem campo na tarifa" | **Implementado, mas inerte**: `stop_fee_cents` existe e é lido; está a **0** na BD. Variação vem só da rota |
| Janelas de mudanças (D3) | Auditoria/QA: ✅ | **Funciona, mas a capacidade é por dia/viatura**, não por janela. Confirmar expectativa da tela |
| SMS com matrícula/motorista (T4) | Reportado como corrigido | **Confirmado** no stub: matrícula, motorista e ligação |
| Ledger da oficina (M6) | Reportado como corrigido | **Confirmado**: 3 150 000 = total − comissão |

Os restantes ✅ exercitados bateram certo. Não foram re-exercitados aqui: chat/SSE além do P3, R2/storage, chamadas mascaradas e push.

## 11. Desvios e bugs por gravidade

### B1. Médio (corrigido): "Repetir" não criava pedido novo

**Antes:** `POST /requests/{id}/repeat` devolvia `201` com a lista de estimativas (`fare_estimate_id`, `total_cents`, `route_source`) e nenhum pedido novo, apesar de o requisito da tela pedir "Repetir cria um pedido novo com o mesmo trajeto".

**Correção aplicada:**
1. `usecases/dispatch/orders.go` (`RepeatRequest`) passou a criar o pedido por omissão, reutilizando `CreateRequest` para manter a validação, o preço do servidor e a idempotência.
2. `dtos.RepeatInput` aceita `{"preview": true}` (só rota e preços), `payment_method` e `scheduled_for`; sem body o comportamento é criar.
3. A idempotência usa o header `Idempotency-Key`; se vier vazio, gera uma chave nova. Repetir duas vezes com a mesma chave não duplica.
4. A reconstrução cobre táxi, reboque (`incident_code`, tipo de viatura) e mudanças (artigos, extras e imóvel por código/rótulo), porque `CreateRequest` precisa da mesma entrada do pedido original.
5. O controller (`infra/rest/dispatch/controllers/orders-controller.go`) lê o body opcional e devolve `201` com `RepeatView` (`request` preenchido, ou só `estimates` em pré-visualização). OpenAPI regenerado.

**Verificação:** `independent_verification.py` V-P2: pré-visualização não cria (1 → 1), repetir cria (1 → 2) e o pedido novo mantém trajeto e serviço; `qa_flows.py` P2: as duas asserções passam; `V-S5` confirma que a mesma estimativa com chaves diferentes só origina um pedido. Os testes Go `TestRepeatCreatesNewRequest`, `TestRepeatTowingCreatesNewRequest` (reboque, com `incident_code` e preço iguais) e `TestRemovalRepeatCreatesNewRequest` (mudanças, com `property`, ajudantes e extras) passam.

### Observações (não são falhas contra o requisito)

| Observação | Efeito | Onde |
|---|---|---|
| `stop_fee_cents = 0` | Não há taxa por paragem; o preço muda só pela rota | `service_tariffs` (dados) |
| Capacidade de mudanças por dia | Uma janela cheia esgota o dia inteiro | `usecases/dispatch/removals.go` (`CountOfDay`), `attrs.daily_capacity` |
| Paginação/estado permissivos | `page=-1&size=99999999` e `status` arbitrário devolvem `200` | validação de *query* |

## 12. Simulado, por provar e mocks

| Integração | Estado nesta revisão | Antes de produção |
|---|---|---|
| SMS (Castbrick) | **Simulado**: recetor local; nenhum SMS real foi enviado | Enviar para um telefone real |
| E-mail (Resend) | **Simulado**: recetor local; não exercitado a fundo | Teste real |
| Push (FCM) | **Não configurado**: `FCM_*` vazios; BD regista `fcm/skipped` | Credenciais e dispositivo real |
| AppyPay Express/Referência | **Real, conta de teste**, via `qa_flows` X1/X2; o script independente só exercitou ledger/dinheiro | Webhook real e produção |
| Números AppyPay simulados | `900000000` OK, `900000003` REJECT | — |
| Mapbox | **Real** (`route_source: mapbox` confirmado) | — |
| R2 (fotos) | **Não exercitado** | Fotos de pedidos |
| Chamadas mascaradas | **Não configurado**: `Unavailable` → `503 CALLS_UNAVAILABLE` | Fornecedor e contrato |
| SSE/Redis/RabbitMQ | **Real** | — |
| Oficinas recomendadas (reboque) | Dados de catálogo, não external | — |

**Mocks no produto:** nenhum. Os únicos `domain/repository/mocks/*` são gerados para testes; nenhum ficheiro não-teste os importa. Não há `TODO`/`FIXME`/`HACK` em código de produção. Os únicos `panic` são legítimos: `infra/database/postgres/tx.go:42` (re-panic), `infra/rest/apidocs/apidocs.go:60` (falha ao renderizar docs) e `config/logger/logger.go:44` (falha ao iniciar o logger).

## 13. Cobertura dos ficheiros novos

Medida com `go test -tags=integration,e2e -coverpkg=./usecases/...,./infra/...,./domain/... ./test/e2e/...` (59,8% no total). **Não inclui** o script independente nem o `qa_flows.py`; os números reais de exercício são mais altos. Ficheiros que são só interfaces, DTOs ou mocks não têm instruções executáveis e aparecem a 0 blocos.

**Lacuna principal:** os pacotes novos `usecases/bookings`, `usecases/experience`, `usecases/fleet` e `usecases/payments` **não têm testes unitários** (`*_test.go`); são cobertos apenas por e2e e pelo script independente. Isso torna regras de calendário, *holds* de reserva, vouchers e reembolsos dependentes de cenários e2e longos.

| Ficheiro | Cobertura e2e |
|---|---|
| `usecases/payments/payments.go` | 71,3% |
| `usecases/payments/ledger.go` | 77,7% |
| `usecases/dispatch/orders.go` | 76,2% (o caminho "criar pedido" não existe) |
| `usecases/dispatch/removals.go` | 64,0% |
| `usecases/dispatch/towing.go` | 76,9% |
| `usecases/dispatch/breakdown.go` | 72,9% |
| `usecases/dispatch/live.go` | 74,1% |
| `usecases/bookings/bookings.go` | 43,7% |
| `usecases/experience/experience.go` | 76,5% |
| `usecases/experience/vouchers.go` | 47,6% |
| `usecases/experience/account.go` | 64,6% |
| `usecases/fleet/fleet.go` | 40,9% |
| `usecases/fleet/workshops.go` | 70,8% |
| `infra/rest/bookings/controller.go` | 27,4% |
| `infra/rest/experience/voucher-controller.go` | 31,2% |
| `infra/rest/fleet/controllers/fleet-controller.go` | 49,7% |
| `infra/repository/booking-repository.go` | 43,8% |
| `infra/repository/booking-repository.go` (mecânicos) | `AddMechanic`/`DeactivateMechanic` 0% |
| `infra/payment/appypay/client.go` | 70,5% |
| `infra/payment/appypay/unavailable.go` | 0% (fallback não usado com gateway configurado) |
| `domain/entities/booking.go` | 64,1% |
| `domain/entities/removal.go` | 68,8% |
| `domain/entities/towing.go` | 75,0% |
| `shared/simplepdf/simplepdf.go` | não instrumentado (fora do `-coverpkg`) |

Ficheiros de ligação (routers/containers) ficam perto de 100%; os *controllers* de experiência e frota são os mais baixos.

## 14. Efeitos no ambiente

- **BD de desenvolvimento:** ficou com utilizadores, pedidos, marcações, pagamentos e oficinas de QA. Os dados são neutralizados no início de cada execução.
- **MFA do `admin@agora.ao` e do `parceiro@agora.ao`:** foi reiniciado pelo script. É preciso configurar de novo no próximo login.
- **Conta de teste AppyPay:** o `qa_flows.py` (X1/X2) deixou cobranças, reembolsos e referências de teste.
- **SMS/e-mail reais:** nenhum. O binário local usou o stub; a API do Docker não esteve a correr durante os testes.
- **API do Docker:** reposta no fim; confirmado `api running` depois das duas execuções.
- **Scripts:** `tests/e2e/independent_verification.py` (novo) e `tests/e2e/qa_flows.py` (existente). Os testes Go continuam em `test/e2e/`.

**Repetir:**

```bash
cd atlas
python3 tests/e2e/independent_verification.py docs taxi maint tow mov orders sec
python3 tests/e2e/qa_flows.py
go test -tags=integration,e2e -count=1 ./...
```

Não correr o script com a Castbrick real configurada. O arranque local já força o stub.
