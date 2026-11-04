# Auditoria: API Atlas vs. telas da app cliente

- **Data:** 2026-11-02
- **Branch:** `feat/geo-dispatch-realtime`, em `d3002f5`, com alterações por commitar (motivos de rejeição, frota, oficinas, `/admin/requests`). A evidência refere-se a este estado do código.
- **Ambiente:** BD nova na versão 20, `dirty=false`, seed de desenvolvimento aplicado. `/ready`, `/health`, `/docs` e `/docs/openapi.json` responderam 200. O OpenAPI tem 130 rotas.
- **Telas de referência:** as imagens anexadas e o brief.

**Critério dos estados**

- ✅ Funciona como a tela mostra, com a lógica confirmada no código.
- 🟡 Existe, mas falta o que está indicado.
- ❌ Não existe.

Os requisitos só de interface, sem regra no servidor, não contam como trabalho em falta e estão assinalados como tal.

> **Atualizado em 2026-11-02.** As secções 1 a 4, 6 e 8 a 15 descrevem o estado inicial em `d3002f5` e ficam como registo. As secções 5 e 7 foram reescritas com o estado final. O estado final de tudo está na secção 18.

## 1. Resumo

| Fluxo | Requisitos | ✅ | 🟡 | ❌ | Cobertura |
|---|---:|---:|---:|---:|---:|
| Táxi: pedir corrida | 21 | 11 | 7 | 3 | 69% |
| Táxi: cancelar e motivos | 10 | 2 | 2 | 6 | 30% |
| Táxi: avaliação | 10 | 1 | 1 | 8 | 15% |
| Manutenção | 27 | 0 | 7 | 20 | 13% |
| Reboque | 17 | 6 | 5 | 6 | 50% |
| Mudanças | 21 | 2 | 6 | 13 | 24% |
| Pedidos | 12 | 3 | 3 | 6 | 38% |
| Conta | 15 | 5 | 3 | 7 | 43% |
| Notificações e transversais | 8 | 2 | 2 | 4 | 38% |
| Chat e chamadas | 4 | 0 | 0 | 4 | 0% |
| **Total** | **145** | **32** | **36** | **77** | **34%** |

A percentagem é `(✅ + 0,5 × 🟡) / total de requisitos`. É uma medida de cobertura, não de esforço: um ❌ de Multicaixa pesa o mesmo que um ❌ de favoritos.

### Os 5 bloqueios mais graves

1. **Os pagamentos ainda não fecham o ciclo financeiro.** Express, Referência e dinheiro já funcionam (secção 16), mas falta a comissão, a gorjeta, os vouchers, a taxa de cancelamento, a carteira e o recibo.
2. **Erro real de preço com paragens.** O pedido aceita uma estimativa calculada para outra rota, porque as paragens não são comparadas. Na API local, uma corrida com 1 paragem (31 km) ficou a 922,94 Kz, quando a estimativa certa era 4 962,44 Kz. Corrigir antes de tudo. Ver secção 12.
3. **A Manutenção das telas não existe para o cliente.** O modelo atual envia um técnico ao local, por cobertura. As telas mostram um marketplace de oficinas com marcação. As oficinas de hoje só têm rotas de parceiro e admin, sem ligação a pedidos, horários nem capacidade.
4. **Reboque e Mudanças não têm modelo próprio.** Há um único serviço em cada categoria, `details` é JSON livre sem validação e não há catálogo de artigos, volume, ajudantes, extras nem tipos de viatura com preço. As Mudanças funcionam por propostas de prestadores, e as telas mostram preço fixo.
5. **Não há avaliações nem reputação.** Sem estrelas, elogios, média nem contagem de serviços. A app também não recebe rating nem telefone do prestador, por isso "Ligar" e "4,8 · 530 serviços" não têm dados.

### Cobertura que já é sólida

O despacho (H3, ofertas, aceitação concorrente, watcher), o tempo real por SSE com replay, as tarifas por serviço e zona, o agendamento com busca antecipada, as moradas guardadas, a gestão da conta e a revisão de prestadores têm lógica no servidor e testes.

## 2. Táxi: pedir corrida (TX01)

| Tela | Requisito | Estado | Endpoint(s) | Evidência | O que falta |
|---|---|---|---|---|---|
| TX01 | Moradas guardadas Casa, Trabalho e "Guardar" | ✅ | `GET,POST /users/me/addresses`, `PUT,DELETE /users/me/addresses/{id}` | `infra/rest/addresses/router/address-router.go:22-25`; uma só `home` e `work` em `migrations/000007_addresses.up.sql:27`; teste `test/e2e/dispatch_test.go:1090` | |
| TX01 | Destinos recentes | 🟡 | `GET /requests` | `infra/database/sqlc/queries/requests.sql:76` | Não há lista própria. A app teria de deduzir de `/requests`, sem remover repetidos |
| TX01 | Escolher no mapa | ✅ | `POST /fare-estimates`, `POST /requests` | `usecases/dispatch/dtos/dispatch-dtos.go:12-17` | Lat/lng aceites. A geocodificação inversa fica na app, com Mapbox |
| TX01-A | Recolha, destino e paragens no pedido | ✅ | `POST /requests` | `domain/entities/service_request.go:263-282`; teste `test/e2e/dispatch_test.go:1047` | |
| TX01-A | Adicionar e remover paragens depois de pedir | ✅ | `POST /requests/{id}/route` | `usecases/dispatch/requests.go:414-491`; teste `test/e2e/dispatch_test.go:903` | |
| TX01-A | Distância total com paragens na estimativa | ✅ | `POST /fare-estimates` | `usecases/dispatch/fares.go:99-111`; confirmado localmente: 4,1 km vs. 31,3 km | |
| TX01-A | O preço do pedido corresponde às paragens | ❌ | `POST /requests` | `domain/entities/pricing.go:129-137` e `usecases/dispatch/requests.go:179` não comparam paragens; secção 12 | Comparar paragens e recusar estimativa de outra rota |
| TX01-A | Taxa por paragem | ❌ | | `domain/entities/pricing.go:25-42` sem campo | Coluna na tarifa |
| TX01-A | Tempo de espera | 🟡 | `POST /requests/{id}/start` | `usecases/dispatch/offers.go:378-395`; `free_wait_seconds` em `migrations/000017_pricing.up.sql:14` | Só cobra a espera na recolha, não nas paragens intermédias |
| TX01-A | Limite de paragens configurável | 🟡 | | `domain/entities/service_request.go:55`, `dispatch-dtos.go:27,44,207` | Limite de 3 fixo no código e nos DTOs |
| TX02 | Estimativa por serviço (Económico, Conforto, XL) | ✅ | `POST /fare-estimates` | `usecases/dispatch/fares.go:65-92`; teste `test/e2e/dispatch_test.go:58` | |
| TX01-B | Agora ou marcar, com máximo de 7 dias | ✅ | `POST /requests` (`scheduled_for`) | `domain/entities/dispatch_policy.go:131-142`; seed `schedule_days=7`; 8 dias devolveu 422 localmente | |
| TX01-B | Intervalos de 15 min | ❌ | `POST /requests` | Confirmado localmente: `:07` foi aceite | Validar no servidor, com intervalo configurável |
| TX01-B | Despacho 30 min antes | ✅ | watcher | `domain/entities/service_request.go:419-425`, `usecases/dispatch/watcher.go:46-80`; `search_lead_seconds=1800` na migração 000015; teste `test/e2e/dispatch_test.go:734` | |
| TX01-B | Preço marcado usa a hora da viagem | 🟡 | `POST /fare-estimates` | `usecases/dispatch/fares.go:57,74` usa a hora atual | A estimativa não recebe `scheduled_for`, por isso a taxa noturna pode falhar |
| TX01-B | Cancelamento grátis até à confirmação | 🟡 | `POST /requests/{id}/cancel` | `usecases/dispatch/requests.go:304-338` | É grátis porque não há taxa nenhuma. A regra não existe como regra |
| TX01-B | Separador "Marcados" | ✅ | `GET /requests?status=scheduled` | `requests.sql:76-82`; confirmado localmente | |
| TX01-C | Nome e telemóvel de outra pessoa | ✅ | `POST /requests` (`passenger`) | `domain/entities/service_request.go:226-228`; `migrations/000018_service_requests.up.sql:16-17` | Sem testes |
| TX01-C | SMS com os detalhes ao passageiro | 🟡 | evento `request.accepted` | `usecases/notifications/request-events.go:176-178` | Texto fixo "pediram um táxi", sem motorista, matrícula nem seguimento, e usado em qualquer categoria |
| TX01-C | O motorista vê nome e contacto do passageiro | 🟡 | `GET /requests/{id}` | `usecases/dispatch/requests.go:541-544`; `RequestView` sem o campo; confirmado localmente | Vê o nome, mas `passenger_phone` nunca é devolvido |
| TX01-C | Quem pede paga | ✅ | `POST /requests/{id}/payments` | O pagamento fica no cliente do pedido: `usecases/payments/payments.go` (`Pay`); teste `test/e2e/payments_test.go` |  |

## 3. Táxi: cancelar (TX-E2) e catálogo de motivos

| Tela | Requisito | Estado | Endpoint(s) | Evidência | O que falta |
|---|---|---|---|---|---|
| TX-E2 | Cancelar a procurar ou com motorista atribuído | ✅ | `POST /requests/{id}/cancel` | `domain/entities/service_request.go:59-68`; teste `test/e2e/dispatch_test.go:749` | Depois de `in_progress` não se cancela |
| TX-E2 | Motivos do cliente | ❌ | | Confirmado localmente: aceita qualquer texto. `CancelInput` em `dispatch-dtos.go:201-203` | Catálogo e endpoint para a app |
| TX-E2 | "Outro motivo" com texto livre | 🟡 | `POST /requests/{id}/cancel` | `dispatch-dtos.go:201-203` | Só existe texto livre |
| TX-E2 | Taxa conforme o estado | ❌ | | `usecases/dispatch/requests.go:304-338` | Regra e valor. As telas dizem "após 2 min" |
| TX-E2 | Saber o custo antes de confirmar | ❌ | | | Endpoint de pré-visualização |
| Backoffice | Motivos de admin ao rejeitar prestador ou documento | ✅ | `GET,POST /admin/rejection-reasons`, `PUT /admin/rejection-reasons/{id}` | `migrations/000016_providers.up.sql:122-164`, `usecases/providers/providers.go` (`resolveReasons`); teste `test/e2e/providers_test.go:244` | Sem filtro por serviço |
| Backoffice | Motivos de cancelamento do cliente | ❌ | | `rejection_reasons.scope` só tem `document`, `provider` e `suspension` | Novos âmbitos, por ator e serviço |
| Backoffice | Motivos de cancelamento do motorista | ❌ | `POST /requests/{id}/cancel` | Texto livre | Catálogo |
| Backoffice | Motivo ao recusar oferta | ❌ | `POST /offers/{id}/reject` | `usecases/dispatch/offers.go:167`, sem corpo nem motivo | Campo na oferta e catálogo |
| Relatórios | Motivo registado no pedido | 🟡 | `GET /admin/requests/{id}` | `cancel_reason` em `migrations/000018_service_requests.up.sql:35`; histórico `usecases/dispatch/admin_requests.go:106` | Texto, sem ID de motivo, por isso não dá para agrupar |

## 4. Táxi: concluída e avaliação (TX07)

| Tela | Requisito | Estado | Endpoint(s) | Evidência | O que falta |
|---|---|---|---|---|---|
| TX07 | Tarifa do serviço e total | 🟡 | `GET /requests/{id}` | `fare_cents` e `adjustments` em `dispatch-dtos.go:105,116` | Sem discriminação com tarifa base, voucher e total pago |
| TX07 | Voucher | ❌ | | Sem tabela nem código | Secção 10 |
| TX07 | Método de pagamento | ✅ | `POST /requests` (`payment_method`), `PUT /requests/{id}/payment-method` | `migrations/000021_payments.up.sql`; teste `test/e2e/payments_test.go` |  |
| TX07 | Estrelas de 1 a 5 obrigatórias | ❌ | | Sem tabela nem rota; `/requests/{id}/rating` devolveu 404 | Tudo |
| TX07 | Elogios configuráveis por serviço | ❌ | | | Catálogo por serviço |
| TX07 | Gorjeta com valores por serviço | ❌ | | | Valores, registo e pagamento |
| TX07 | A gorjeta chega ao motorista | ❌ | | Sem saldo nem extrato do prestador | Depende da decisão de pagamentos |
| TX07 | Média e nº de serviços do prestador | ❌ | | `ProviderSummary` em `dispatch-dtos.go:64-69`; sem coluna em `providers` | Agregados e exposição |
| TX07 | Só uma vez e só concluídos | ❌ | | | Regra e restrição única |
| PD01 | "Avaliar" no histórico | ❌ | | | Campo `can_rate` ou equivalente |

## 5. Manutenção (MN01–MN07)

Modelo de marcações, com estados próprios. Prova: `tests/e2e/qa_flows.py` M1 a M6 e `test/e2e/bookings_test.go`.

| Tela | Requisito | Estado | Endpoint(s) |
|---|---|---|---|
| MN01 | Lista, pesquisa, categoria, distância, rating, "Aberta até", Ao domicílio, promoção | ✅ | `GET /workshops` |
| MN02 | Detalhe, garantia, Sobre, serviços, avaliações, favoritos | ✅ | `GET /workshops/{id}`, `/reviews`, `POST/DELETE /workshops/{id}/favorite` |
| MN03 | Viaturas, descrição, quilometragem, até 5 fotos | ✅ | `/users/me/vehicles`, `POST /bookings`, `/media/photos`, `/requests/{id}/photos` |
| MN04 | Oficina ou domicílio, Livre/Fechado/Esgotado, horários por capacidade e duração, corrida pelo último lugar | ✅ | `GET /workshops/{id}/availability`, `POST /bookings` |
| MN05 | Desconto automático, Express só depois de aprovar | ✅ | `POST /bookings/{id}/approve`, `POST /requests/{id}/payments` |
| MN06 | Código MK, SMS e calendário | ✅ | `GET /bookings/{id}/calendar` |
| MN07 | Estados sem saltos, hora prevista, orçamento extra, mecânico, chat e chamada, rotas da oficina | ✅ | `/bookings/{id}/...`, `/fleet/bookings/{id}/...`, `/fleet/workshops/{id}/schedule`, `/closures`, `/mechanics` |

## 6. Reboque (RB01–RB06)

| Tela | Requisito | Estado | Endpoint(s) | Evidência | O que falta |
|---|---|---|---|---|---|
| RB01 | Localização por GPS ou pino | ✅ | `POST /requests` | `dispatch-dtos.go:12-17`; teste `test/e2e/dispatch_test.go:604` | |
| RB01 | Aviso de segurança | ✅ | | Só de interface | |
| RB02 | O que aconteceu e se a viatura anda | 🟡 | `POST /requests` (`details`) | `domain/entities/service_request.go:234-237`, JSON livre; chega ao motorista em `usecases/dispatch/requests.go:633` | Catálogo e validação |
| RB02 | Tipo de viatura com preço | ❌ | `GET /catalog` | Confirmado localmente: `towing` só tem 1 serviço. Conflito C1 | Tipos com preço |
| RB02 | Fotos opcionais | ❌ | | | Upload para R2 |
| RB03 | Destino outro endereço | ✅ | `POST /requests` | `requires_destination=true` na migração 000014 | |
| RB03 | Oficina parceira recomendada | ❌ | | Reboque e oficinas não estão ligados | Regra de recomendação |
| RB03 | Preço com distância incluída | 🟡 | `POST /fare-estimates` | `domain/entities/pricing.go:66-78` | Cobra por km. Falta o conceito de km incluídos |
| RB03 | Método de pagamento | ✅ | `POST /requests` (`payment_method`) | Métodos por categoria em `category_payment_methods`, migração 000021 |  |
| RB04 | Contacta N reboques próximos | ✅ | `POST /requests`, `GET /nearby` | Seed `parallel`, lote 3, migração 000015; teste `test/e2e/dispatch_test.go:604` | |
| RB04 | Cancelar grátis nesta fase | ✅ | `POST /requests/{id}/cancel` | `usecases/dispatch/requests.go:304-338` | Grátis porque não há taxa, como em TX-E2 |
| RB05 | ETA | 🟡 | SSE `/events` | `usecases/dispatch/presence.go:202-208` | Estimativa por distância em linha reta e velocidade média, não por Mapbox |
| RB05 | Dados do motorista: rating e nº de serviços | 🟡 | `GET /requests/{id}` | `dispatch-dtos.go:64-69`, `usecases/dispatch/requests.go:576-593` | Tem nome, matrícula e viatura. Falta rating e contagem |
| RB05 | Ligar, mensagem e partilhar | ❌ | | `ProviderSummary` sem telefone | Secção 11 |
| RB05 | Código de entrega das chaves | ❌ | `POST /requests/{id}/complete` | `usecases/dispatch/offers.go:370-376` conclui sem código | Código e validação |
| RB06 | Resumo | 🟡 | `GET /requests/{id}` | Como TX07 | Discriminação |
| RB06 | Avaliação e gorjeta | ❌ | | | Secção 4 |

## 7. Mudanças (MD01–MD07)

Preço fixo por viatura, ajudantes e extras. Prova: `tests/e2e/qa_flows.py` D1 a D3 e `test/e2e/removals_test.go`.

| Tela | Requisito | Estado | Endpoint(s) |
|---|---|---|---|
| MD01 | Moradas com andar e elevador, paragens, Casa ou Escritório | ✅ | `POST /requests` (`removal`) |
| MD02 | Artigos por divisão, volume no servidor | ✅ | `GET /removals/options`, `POST /fare-estimates` (`removal.items`) |
| MD03 | Viatura recomendada, preço promocional, ajudantes e extras | ✅ | `POST /fare-estimates` |
| MD04 | Dia Livre ou Esgotado, janela, duração, contacto e notas | ✅ | `GET /removals/availability`, `POST /requests` |
| MD05 | Seguro informativo; referência só confirma depois de paga | ✅ | `insurance_cents`, `POST /requests/{id}/payments` |
| MD06 | Equipa, responsável, verificados | ✅ | `PUT /requests/{id}/team`, `PUT /fleet/requests/{id}/team` |
| MD07 | Elogios e gorjetas | ✅ | `GET /rating-options?category=removals` |

## 8. Pedidos (PD01–PD02)

| Tela | Requisito | Estado | Endpoint(s) | Evidência | O que falta |
|---|---|---|---|---|---|
| PD01 | Em curso, Histórico e Marcados | ✅ | `GET /requests?status=` | `infra/rest/dispatch/controllers/dispatch-controller.go:377-391` | A app escolhe os estados de cada separador |
| PD01 | Vários pedidos ativos em simultâneo | 🟡 | `GET /requests/active` | `requests.sql:64-68`, `LIMIT 1` | Só devolve um. Com filtro por estado a lista funciona |
| PD01 | Filtro por serviço | ✅ | `GET /requests?category=` | `requests.sql:80` | Sem pesquisa de texto |
| PD01 | Título e dados da linha | 🟡 | `GET /requests` | `usecases/dispatch/requests.go:493-504` | `service_name` não vem na lista do cliente |
| PD01 | Ações Avaliar, Recibo e Repetir | ❌ | | | Secções 4 e seguintes |
| PD02 | Estado, trajeto e horas | 🟡 | `GET /requests/{id}` | `dispatch-dtos.go:119-122` | Horas de recolha e fim existem. Falta hora por paragem |
| PD02 | Código legível (TX-48271) | ❌ | | Só UUID | Sequência |
| PD02 | Prestador e viatura | ✅ | `GET /requests/{id}` | `usecases/dispatch/requests.go:576-593` | |
| PD02 | A avaliação dada | ❌ | | | Secção 4 |
| PD02 | Recibo PDF | ❌ | | `/requests/{id}/receipt` devolveu 404 | Geração |
| PD02 | Repetir | ❌ | | | Endpoint, ou a app refaz estimativa e pedido |
| PD02 | Ajuda e reportar problema ligados ao pedido | ❌ | | | Tickets |

## 9. Conta (CO01–CO03)

| Tela | Requisito | Estado | Endpoint(s) | Evidência | O que falta |
|---|---|---|---|---|---|
| CO01 | Perfil e editar | ✅ | `GET,PATCH /users/me` | `infra/rest/auth/router/auth-router.go:89-90`; testes em `usecases/auth/profile-usecase_test.go` | Sem foto de perfil |
| CO01 | Convida amigos | ❌ | | `invite` só existe para staff; `referral` não existe | Códigos e recompensa |
| CO01 | Pagamentos | 🟡 | `GET /payment-methods` | `usecases/payments/payments.go` (`Methods`) | Cartões e números guardados |
| CO01 | Moradas guardadas | ✅ | `/users/me/addresses` | Secção 2 | |
| CO01 | As minhas viaturas | ❌ | | Secção 5 | |
| CO02 | Toggles de notificações | ❌ | | Sem preferências em `users`; o SMS não consulta nada | Preferências |
| CO02 | Idioma, aspeto e moeda | ❌ | | | Preferências. O aspeto pode ficar só na app |
| CO02 | Partilhar localização só durante pedidos | 🟡 | | O cliente não envia GPS à API | Sem envio contínuo não há risco. Falta guardar a preferência |
| CO02 | Alterar palavra-passe | ✅ | `PUT /users/password` | `auth-router.go:96` | |
| CO02 | Alterar telemóvel | ✅ | `POST /users/me/phone/verification-code`, `PUT /users/me/phone` | `auth-router.go:93-94` | |
| CO02 | Eliminar conta com SMS | ✅ | `POST /users/me/verification-code`, `DELETE /users/me` | `usecases/auth/profile-usecase.go:154-163,200`; teste `profile-usecase_test.go:220` | |
| CO03 | Pesquisa e temas de ajuda | ❌ | | `/help/topics` devolveu 404 | Conteúdo |
| CO03 | Pedido recente | 🟡 | `GET /requests` | | Sem vínculo com ajuda |
| CO03 | Chat com o apoio | ❌ | | | Secção 11 |
| CO03 | Linha de apoio | ❌ | | | Número configurável |

## 10. Notificações (NT01) e transversais

| Tela | Requisito | Estado | Endpoint(s) | Evidência | O que falta |
|---|---|---|---|---|---|
| NT01 | Caixa de entrada | ❌ | | `/notifications` devolveu 404. A tabela `notifications` da 000019 é um registo de envios, sem texto | Tabela e rotas |
| NT01 | Lidas e "Marcar todas" | ❌ | | | Estado de leitura |
| NT01 | Deep link | 🟡 | push FCM | `usecases/notifications/request-events.go:166` envia `request_id` | Só no push, não numa caixa de entrada |
| Transversal | Vouchers por serviço com validade | ❌ | | `/vouchers` devolveu 404 | Motor |
| Transversal | Carteira e reembolsos | ❌ | | `/wallet` devolveu 404 | Ledger |
| Transversal | Tempo real em todos os serviços | ✅ | `GET /events`, FCM | `usecases/realtime/projector.go:18-26`, tracking por política; testes `test/e2e/dispatch_test.go:146-208` | Falta estados próprios de Manutenção |
| Transversal | Multicaixa Express | ✅ | `POST /requests/{id}/payments`, `POST /webhooks/appypay` | `infra/payment/appypay/client.go`; ambiente de teste real via `make test-appypay` | Falta configurar o webhook no portal; produção por testar |
| Transversal | Cash | 🟡 | `POST /requests/{id}/payments/cash` | `usecases/payments/payments.go` (`ConfirmCash`); teste `test/e2e/payments_test.go` | Falta decidir como se cobra a comissão ao motorista |

## 11. Chat e chamadas (telas TX-E3, TX05, RB05, MN07, CO03)

O brief refere estas ações em várias telas. Ficam aqui uma só vez.

| Tela | Requisito | Estado | Endpoint(s) | Evidência | O que falta |
|---|---|---|---|---|---|
| TX-E3 | Chat com motorista e respostas rápidas | ❌ | | `chat` não aparece no código | Mensagens e eventos SSE |
| TX05 | Ligar com número privado | ❌ | | `ProviderSummary` sem telefone | Decisão: número mascarado ou aberto |
| TX05 | Partilhar viagem | ❌ | | | Ligação pública temporária |
| TX06 | SOS | ❌ | | | Registo e alerta. A chamada ao 113 fica na app |

## 12. Verificação na API local

Chamadas feitas com o seed de desenvolvimento e o Mapbox real, com o cliente `agora-customer-app`.

**Paragens e preço**

```
POST /api/v1/fare-estimates  {category: taxi, pickup, destination}
201  taxi 92294 cêntimos, 4088 m

POST /api/v1/fare-estimates  {category: taxi, pickup, destination, stops: [1]}
201  taxi 496244 cêntimos, 31288 m

POST /api/v1/requests  {fare_estimate_id: <sem paragens>, stops: [1], scheduled_for: ...:07, passenger: {...}}
201  status=scheduled, fare_cents=92294, stops=1, passenger_phone ausente
```

O pedido ficou com a paragem e com o preço da rota sem paragem. A diferença foi de 4 039,50 Kz, e o motorista veria a paragem no pedido.

**Regras**

| Pedido | Resposta |
|---|---|
| `GET /requests?status=scheduled` | 200, total 1 |
| `POST /requests` a 8 dias | 422, "A data está para lá do permitido" |
| `POST /requests` com 4 paragens | 422 |
| `POST /requests/{id}/cancel` com `{"reason":"texto qualquer que não existe em catálogo"}` | 200, gravado tal como foi enviado |

**Rotas sem resposta (404)**

`/notifications`, `/users/me/vehicles`, `/vouchers`, `/wallet`, `/workshops`, `/requests/{id}/receipt`, `/requests/{id}/rating`, `/payment-methods`, `/cancellation-reasons`, `/help/topics`. São nomes prováveis. A conclusão de que faltam vem da pesquisa no código e no OpenAPI.

**Por verificar**

- `/admin/requests` e `/fleet/requests` só correram sem corridas na resposta.
- Os SMS e push dos eventos não foram vistos a chegar a um telefone real nesta auditoria.
- O FCM não está configurado no `.env` local.

## 13. Conflitos com o modelo atual

Uma correção ao brief: o `service_id` único do prestador não vem da 000020. Está em `providers.service_id` (`migrations/000016_providers.up.sql:6`). A 000020 só cria as oficinas.

| # | Conflito | Modelo atual | As telas pedem | Proposta |
|---|---|---|---|---|
| C1 | Tipos de reboque vs. serviço único | Motorista presta 1 serviço. Oferta filtrada por `service_id` (`test/e2e/dispatch_test.go:1015`) | Ligeiro, SUV e Motociclo com preços diferentes | Um serviço `towing` e **tipos com preço** dentro dele, com a capacidade na viatura. Se virassem 3 serviços, cada motorista só receberia um tipo |
| C2 | Manutenção | `call_out`, cobertura, técnico ao local, sem destino (migrações 000014, 000015; `test/e2e/dispatch_test.go:638`) | Oficina, marcação, orçamento e estados próprios | Modelo próprio de **marcações**, fora da máquina de estados das corridas, e aposentar o fluxo atual se confirmares |
| C3 | Mudanças | `quote`: prestadores propõem e o cliente escolhe (`test/e2e/dispatch_test.go:680`) | Preço fixo calculado por viatura, ajudantes e extras | Passar a `fixed`, com preço por itens. O motor de propostas fica sem uso |
| C4 | Equipa | Um `provider_id` por pedido | Responsável e ajudantes verificados | Tabela de membros da equipa, apenas informativa na V1 |
| C5 | Estados | 11 estados de corrida, comuns a todos | Viatura recebida, Em serviço, Pronta | Não misturar com a máquina de corridas. Ver C2 |
| C6 | `details` JSON livre | Sem validação | Formulários com regras e preço | Colunas ou tabelas por categoria. `details` só para dados sem regra |
| C7 | Motivos | `rejection_reasons` com 3 âmbitos fixos | Por ator e por serviço | Generalizar a mesma tabela, com `actor` e `service_id` opcional. Evita um segundo catálogo |
| C8 | Valores monetários | Tudo em cêntimos: `fare_cents`, `price_cents` | O brief pede "Kz guardados como inteiros" | Manter cêntimos, que já são inteiros, e formatar na app. Pergunta 6 |
| C9 | Migrações | O teu pedido anterior foi editar as existentes. O brief diz para não alterar as aplicadas | | Pergunta 7 |
| C10 | Sem receita associada | Comissão calculada, nunca cobrada | Dinheiro, Multicaixa e carteira | Ledger como base única de pagamentos, gorjetas, reembolsos e comissões |

## 14. Ordem de desenvolvimento proposta

Contesto a ordem sugerida em dois pontos:

- **A correção do preço passa para primeiro**, porque subfatura hoje.
- **Pagamentos passam para antes da gorjeta**. Gorjeta, taxa de cancelamento, voucher, recibo e carteira assentam todos no mesmo registo financeiro. Se vierem antes, teriam de ser refeitos.

| # | Bloco | Esforço | Depende de |
|---|---|---|---|
| 0 | Corrigir preço com paragens, passenger_phone e SMS ao passageiro | P | |
| 1 | Catálogo de motivos e cancelamento com pré-visualização de custo, ainda sem cobrar | M | |
| 2 | Paragens: taxa, limite configurável, intervalos de 15 min e preço à hora marcada | P | 0 |
| 3 | Fundação de pagamentos: método no pedido, dinheiro, ledger, discriminação do total | G | Perguntas 1 e 2 |
| 4 | Avaliação, elogios, reputação, gorjeta | M | 3 |
| 5 | Taxa de cancelamento efetiva, vouchers | M | 1, 3 |
| 6 | Pedidos: código legível, recibo PDF, repetir, ajuda ligada ao pedido | M | 3, 4 |
| 7 | Multicaixa Express, com pré-autorização se o fornecedor permitir | G | 3 e pergunta 2 |
| 8 | Chat, chamada, partilha e SOS | M | Pergunta 5 |
| 9 | Reboque: tipos, km incluídos, formulário, fotos, código de entrega, oficina recomendada | M | 3, 4 e C1 |
| 10 | Viaturas do cliente | P | |
| 11 | Manutenção: leitura pública, horários, marcação, estados, orçamento extra | G | 3, 4, 7, 10 e C2 |
| 12 | Mudanças: artigos, volume, preço, janelas, equipa, seguro | G | 3, 4 e C3 |
| 13 | Conta e notificações: caixa de entrada, preferências, carteira, convites, centro de ajuda | M | 3 |

## 15. Perguntas antes de desenvolver

**Pagamentos**

1. No dinheiro, como cobramos a comissão? Dívida do motorista, pré-carregamento ou liquidação semanal? Esta decisão já estava em aberto.
2. Que fornecedor e que contrato temos para Multicaixa Express? Ele permite pré-autorização? O MN05 ("só pagas depois de aprovar") depende disto. Sem ela, proponho pedir o pagamento só no fim.
3. A gorjeta fica 100% com o motorista, sem comissão? Quando é paga, se a corrida foi em dinheiro?

**Regras das telas**

4. Taxa de cancelamento: qual o valor por serviço, e conta a partir de quando? As telas dizem "após 2 min" com motorista atribuído. E quando o motorista cancela, há penalização? Se o cliente tiver dinheiro como método e não pagar a taxa, fica em dívida na próxima corrida?
5. Chamadas: número mascarado (exige fornecedor e tem custo) ou número aberto? A tela TX-E3 diz "O teu número não é partilhado". Chat pode ser só durante o pedido?

**Modelo**

6. Mantemos os valores em cêntimos? O brief diz "Kz como inteiros". Recomendo manter, porque o código, as tarifas e os testes já usam cêntimos.
7. As migrações: a partir de agora usamos só novas (000021 em diante), ou ainda edito as existentes enquanto não há produção? Recomendo novas só para o que for adicionado nesta fase, e fica a teu critério quando os motivos precisarem de mudar.
8. Manutenção: posso aposentar o fluxo atual de técnico ao local (C2)? E o mecânico é uma conta na plataforma ou só um nome registado pela oficina?
9. Mudanças: posso desativar as propostas (C3)? O preço é fixo mesmo se o volume real for maior no dia?
10. Reboque: quantos km estão incluídos em "Distância incluída", e quanto custa por km a mais?
11. Reboque e Mudanças só com prestadores de parceiros (a regra atual `self_signup=false`), ou também independentes?
12. Oficinas: quem define a capacidade (elevadores, mecânicos em simultâneo) e a oficina pode bloquear dias?
13. Seguro de bens até 500 000 Kz: há seguradora, ou é a AGORA que assume? Se houver sinistro, que fluxo é necessário?
14. Vouchers e convites: quem paga o desconto, a AGORA ou o prestador? Os 1 000 Kz do convite vão para a Carteira?

**Pendentes**

15. As alterações de motivos, frota, oficinas e `/admin/requests` continuam por commitar. Faço o commit antes de começar?

## 16. Pagamentos implementados

Atualizado em 2026-11-02. Migração `000021_payments`, módulo `usecases/payments`, cliente `infra/payment/appypay`.

**Endpoints novos**

| Método | Rota | Quem |
|---|---|---|
| `GET` | `/payment-methods?category=` | App |
| `PUT` | `/requests/{id}/payment-method` | Cliente |
| `GET` | `/requests/{id}/payments` | Cliente e prestador do pedido |
| `POST` | `/requests/{id}/payments` | Cliente (`gpo` ou `reference`) |
| `POST` | `/requests/{id}/payments/cash` | Prestador do pedido |
| `POST` | `/webhooks/appypay` | AppyPay |
| `GET`, `PUT` | `/admin/payment-methods` | `services:manage` |
| `GET` | `/admin/payments`, `/admin/payments/{id}` | `finance:read` |
| `POST` | `/admin/payments/{id}/refund` | `refunds:approve` |

**Endpoints alterados:** `POST /requests` aceita `payment_method`. Pedidos e ofertas devolvem o método, e os pedidos também o estado do pagamento (`payment_method`, `payment_status`).

**Regras em vigor**

- O pagamento digital acontece depois de o pedido estar concluído, com o valor do próprio pedido.
- Um pedido concluído por pagar com Express ou Referência impede o cliente de fazer outro (`PAYMENT_OUTSTANDING`).
- Depois de concluído, o método não pode mudar para dinheiro.
- A Referência está desligada no Táxi e no Reboque, e ligada na Manutenção e nas Mudanças. Altera-se no backoffice.
- O webhook é só um aviso: o estado vem sempre de `GET /charges/{id}`, e valor e código têm de coincidir.
- Um reconciliador consulta os pagamentos pendentes, por isso o fluxo funciona mesmo sem webhook.
- Reembolso só em Express, total ou parcial.

**Verificação**

Testes end-to-end com simulador em `test/e2e/payments_test.go`, e testes reais com `make test-appypay`. Na API local com a AppyPay de teste: Express aprovado ficou `paid`, rejeitado voltou a `unpaid` e o novo pagamento ficou `paid`.

**Por verificar:** o webhook com a AppyPay real (precisa de URL público), a Referência paga até ao fim (em teste fica `Pending`) e a produção.

## 17. Evolução por fase

Cada fase lista o que deixou de faltar nas secções acima. As secções 2 a 11 descrevem o estado inicial; o estado final está na secção 18.

### Fase 1: bugs B1 a B7

| Bug | Correção | Prova |
|---|---|---|
| B1 | A estimativa compara paragens e hora marcada (`MatchesRoute`) | `TestARequestWithStopsCannotUseThePriceOfARouteWithout`, T1 |
| B2 | Limites no binding e na entidade para `capacity_kg`, `display_order`, `duration_minutes`, ano e lugares | S7 |
| B3 | Estimativa de uso único (`used_at`), `DUPLICATE_REQUEST` com bloqueio por cliente | `TestAnEstimateServesOnlyOneRequest`, S5 |
| B4 | O motorista vê `passenger_phone`. SMS com motorista e matrícula, texto por categoria em `service_categories.passenger_sms` | `TestThePassengerOfAnotherPersonIsKnownToTheDriverAndGetsTheDetails`, T4 |
| B5 | `volume_m3` e preços retirados de `details`. O resto (motivo da recusa, voucher, preferências) é tratado nas fases 3, 6 e 13 | `TestServerFieldsInDetailsAreNotKept` |
| B6 | `dispatch_policies.max_stops` e `schedule_slot_minutes`, com `service_tariffs.stop_fee_cents` | `TestStopFeeAndStopLimitComeFromTheDatabase`, T2, T3 |
| B7 | **Causa:** no arranque, o relay podia publicar antes de as filas estarem ligadas e o evento ia para a fila de não encaminhados. O relay espera agora pela topologia | `TestEventsPublishedBeforeTheBindingAreParkedAndAfterItArrive` |
| Dinheiro em Express | `CASH_NOT_EXPECTED` | `TestADriverCannotConfirmCashOnARequestPaidDigitally` |

**Endpoints:** nenhum novo. `POST /fare-estimates` aceita `scheduled_for`. Respostas novas: `409 FARE_ESTIMATE_USED`, `409 DUPLICATE_REQUEST`, `409 CASH_NOT_EXPECTED`.

### Fase 2: testes permanentes

`test/e2e/fleet_test.go` cobre frota (limites, IDOR entre parceiros, parceiro nas rotas da plataforma), oficinas (taxas, duração, IDOR), `/admin/requests` e `/fleet/requests` (corrida real, filtros que filtram, resumo com a soma certa, telefone do cliente escondido) e motivos (inexistente, de outro âmbito, inativo, nota obrigatória).

**Endpoints:** nenhum novo.

### Fase 3: motivos por ator e cancelamento com custo

`rejection_reasons` passou a ter os âmbitos `customer_cancel`, `provider_cancel` e `offer_reject`, por categoria (C7). `cancellation_rules` guarda a taxa e os prazos por serviço. O cancelamento guarda `cancel_reason_id` e `cancel_fee_cents`, e a taxa por pagar entra em `PAYMENT_OUTSTANDING`.

**Endpoints novos:** `GET /cancellation-reasons`, `GET /requests/{id}/cancellation`, `GET /admin/cancellation-rules`, `PUT /admin/cancellation-rules/{serviceId}`, `GET /admin/reports/cancellations`.

**Alterados:** `POST /requests/{id}/cancel` passa a exigir `reason_id` e, quando há taxa, `expected_fee_cents`. `POST /offers/{id}/reject` passa a exigir `reason_id`. `POST /admin/rejection-reasons` aceita `category_id`.

### Fase 4: ledger, comissão, Carteira e discriminação do total

`ledger_transactions` e `ledger_entries`: cada transação soma zero, com contas de prestador, carteira, plataforma e externa. O lançamento é feito na mesma transação do pagamento, com chave de idempotência por pagamento.

| Caso | Lançamento |
|---|---|
| Express ou Referência | Externa −valor; prestador +(tarifa − comissão); plataforma +(comissão − desconto) |
| Dinheiro | O motorista já tem o dinheiro: prestador fica com −comissão, em dívida |
| Carteira | Como Express, com a carteira no lugar da conta externa |
| Reembolso | Express volta pela AppyPay; Referência e Carteira voltam à Carteira. Prestador e plataforma devolvem na proporção recebida |
| Pagamento em duplicado | Vai para a Carteira |
| Taxa real da AppyPay | Só quando registada no backoffice, a partir do extrato; custo da plataforma, sem assumir valor |

**Endpoints novos:** `GET /wallet`, `GET /providers/me/ledger`, `GET /admin/ledger/summary`, `GET /admin/providers/{id}/ledger`, `POST /admin/providers/{id}/settlements`, `PUT /admin/payments/{id}/gateway-fee`.

**Alterados:** `POST /requests/{id}/payments` e `PUT /requests/{id}/payment-method` aceitam `wallet`. `GET /requests/{id}` traz `breakdown`. `POST /admin/payments/{id}/refund` passa a aceitar Referência e Carteira, para a Carteira.

**Env nova:** `LEDGER_CURRENCY`.

### Fase 5: avaliação, elogios, reputação e gorjeta

`catalog_items` guarda os catálogos do backoffice (elogios, gorjetas sugeridas e, nas fases seguintes, respostas rápidas, tipos de reboque, artigos e extras). `platform_settings` guarda os limites. `ratings` tem uma avaliação por pedido, com média e contagem em `providers`.

**Endpoints novos:** `GET /rating-options`, `POST /requests/{id}/rating`, `POST /requests/{id}/tips`, `GET /providers/me/ratings`, `GET,POST /admin/catalog/{kind}`, `PUT /admin/catalog/{kind}/{id}`, `GET /admin/settings`, `PUT /admin/settings/{key}`.

**Alterados:** `GET /requests` e `GET /requests/{id}` trazem `can_rate`, `rating` e, no prestador, `rating`, `rating_count` e `completed_requests`.

### Fase 6: vouchers

`vouchers` e `voucher_redemptions`. A AGORA paga o desconto: a comissão continua a incidir sobre a tarifa e o motorista recebe como se não houvesse voucher. Cancelar devolve o voucher. O desconto é recalculado se a rota mudar.

**Endpoints novos:** `GET /vouchers`, `POST /vouchers/preview`, `GET,POST /admin/vouchers`, `PUT /admin/vouchers/{id}`.

**Alterados:** `POST /requests` aceita `voucher_code` e recusa os inválidos, em vez de os ignorar (B5). Os pedidos trazem `discount_cents`, `voucher_code` e `amount_due_cents`.

### Fase 7: pedidos

**Novos no pedido:** `code` (TX-48271, com prefixo por categoria), `service_name`, `arrived_at` por paragem, `requests` em `/requests/active`.

**Endpoints novos:** `GET /requests/{id}/receipt` (PDF), `POST /requests/{id}/repeat`, `POST /requests/{id}/stops/{position}/arrive`, `GET /users/me/recent-destinations`, `POST /requests/{id}/tickets`, `GET,POST /support/tickets`, `GET /support/tickets/{id}`, `POST /support/tickets/{id}/messages`, `GET /admin/support/tickets`, `GET /admin/support/tickets/{id}`, `POST /admin/support/tickets/{id}/messages`, `POST /admin/support/tickets/{id}/close`.

### Fase 8: chat, chamada, partilha e SOS

**Endpoints novos:** `GET,POST /requests/{id}/messages`, `GET /quick-replies`, `POST,DELETE /requests/{id}/share`, `GET /public/trips/{token}`, `GET /requests/{id}/eta`, `POST /requests/{id}/call`, `POST /requests/{id}/sos`, `GET /admin/sos`, `POST /admin/sos/{id}/resolve`.

O SMS ao passageiro leva agora a ligação de seguimento. SSE novo: `request.message`.

**Env novas:** `CALLS_BASE_URL`, `CALLS_API_KEY`, `CALLS_TIMEOUT`, `SHARE_BASE_URL`.

### Fase 9: viaturas do cliente e fotos no R2

**Endpoints novos:** `GET,POST /users/me/vehicles`, `PUT,DELETE /users/me/vehicles/{id}`, `POST /media/photos`, `GET,POST /requests/{id}/photos`. Limite, tamanho e tipos em `platform_settings`. Antes de ligar a foto ao pedido, o servidor confirma no R2 que o ficheiro existe com o tipo e tamanho anunciados.

### Fase 10: Reboque

Um único serviço `towing` com tipos de viatura em `catalog_items` (C1): Ligeiro 25 000, SUV/Pick-up 32 000 e Motociclo 15 000 Kz, 10 km incluídos e preço por km a mais. O motorista só recebe ofertas que a viatura consegue carregar (`min_capacity_kg`). O pedido exige o incidente e se a viatura anda. A entrega exige o código que só o cliente vê, com limite de tentativas.

**Endpoints novos:** `GET /towing/options`.

**Alterados:** `POST /fare-estimates` na categoria `towing` devolve uma opção por tipo. `POST /requests` aceita `towing`. `POST /requests/{id}/complete` aceita `code`.

### Fase 11: Manutenção

Modelo próprio de marcações (C2), com estados `confirmed`, `vehicle_received`, `in_service`, `ready`, `completed`, `awaiting_payment` e `cancelled`. Cada marcação tem um pedido associado, só para faturação e listas. Migração 000031.

**Endpoints novos:** `GET /workshops`, `GET /workshops/{id}`, `GET /workshops/{id}/reviews`, `GET /workshops/{id}/availability`, `POST,DELETE /workshops/{id}/favorite`, `GET,POST /bookings`, `GET /bookings/{id}`, `GET /bookings/{id}/calendar`, `POST /bookings/{id}/cancel`, `POST /bookings/{id}/approve`, `POST /bookings/{id}/extras/{extraId}/answer`, `GET,POST /bookings/{id}/messages`, `POST /bookings/{id}/call`, `GET,POST /admin/workshops/{id}/...` para ledger e liquidação. Parceiro, em `/fleet` e `/partners/{orgId}/fleet`: `PUT /workshops/{id}/schedule`, `POST /workshops/{id}/closures`, `DELETE /workshops/{id}/closures/{date}`, `GET,POST /workshops/{id}/mechanics`, `DELETE /workshops/{id}/mechanics/{mechanicId}`, `GET /workshops/{id}/bookings`, `GET /bookings/{id}`, `POST /bookings/{id}/status`, `PUT /bookings/{id}/assignment`, `POST /bookings/{id}/extras`, `POST /bookings/{id}/cancel`, `GET,POST /bookings/{id}/messages`.

**Alterados:** `POST /requests` em `maintenance` responde `USE_BOOKINGS`. `POST /admin/vouchers` aceita `workshop_id`, `workshop_service_id` e `auto_apply`. Serviços de oficina aceitam `category_id`.

### Fase 12: Mudanças

Preço fixo por viatura, ajudantes e extras (C3). Migração 000032.

**Endpoints novos:** `GET /removals/options`, `GET /removals/availability`, `PUT /requests/{id}/team`, `PUT /fleet/requests/{id}/team`.

**Alterados:** `POST /fare-estimates` aceita `removal` e devolve uma opção por viatura. `POST /requests` aceita `removal`.

### Fase 13: conta e notificações

Migração 000033.

**Endpoints novos:** `GET,PUT /users/me/preferences`, `GET,PUT /users/me/avatar`, `GET /users/me/referral`, `POST /users/me/referral/redeem`, `GET /notifications`, `POST /notifications/read-all`, `POST /notifications/{id}/read`, `GET /help`.

**Env novas nas fases 11 a 13:** nenhuma. Os valores estão em `platform_settings` e `catalog_items`.

## 18. Estado final

| Área | Estado | Prova | Parcial ou por provar |
|---|---|---|---|
| Táxi (TX01 a TX07) | ✅ | T1 a T8, L1, L2 | Chamada só com simulador |
| Manutenção (MN01 a MN07) | ✅ | M1 a M6, `bookings_test.go`, `prepaid_test.go` | Referência paga só com simulador |
| Reboque (RB01 a RB06) | ✅ | R1 a R3, `towing_test.go` | |
| Mudanças (MD01 a MD07) | ✅ | D1 a D3, `removals_test.go`, `prepaid_test.go` | Seguro é informativo, por decisão |
| Pedidos (PD01, PD02) | ✅ | P1, P2 | |
| Conta (CO01, CO03) | ✅ | P4, P5, E1, E2 | |
| Preferências (CO02) | 🟡 | P4, `account_test.go` | `notify_promotions`, `share_location`, `language` e `currency` só ficam guardadas |
| Notificações (NT01) | ✅ | P3, `account_test.go` | Push real sem credenciais |
| Transversal | ✅ | T8, L3, X1, X2, E3 | Ver `docs/verificacao-qa.md`, secção "Por provar" |
