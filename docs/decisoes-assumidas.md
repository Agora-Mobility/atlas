# Decisões assumidas

Decisões tomadas sem te perguntar, para rever. Os valores ficam na BD e mudam-se no backoffice, salvo indicação.

## Decisões do brief

| Tema | Decisão |
|---|---|
| Pagamento | Cobra-se no fim do serviço, com o valor final. A AppyPay não tem pré-autorização. Em Manutenção, o pedido de pagamento por Express só sai depois de o cliente aprovar o serviço concluído |
| Referência | Desligada em Táxi e Reboque. Em Mudanças e Manutenção, a marcação só confirma depois de paga |
| Reembolso de referência | Vai para a Carteira. A AppyPay só reembolsa Express |
| Dinheiro | A comissão fica como dívida do motorista no ledger, com liquidação gerida no backoffice |
| Gorjeta | 100% para o motorista, sem comissão, por método digital, registada no ledger. Possível em corrida paga a dinheiro |
| Cancelamento | Sem motorista, custo 0. Com motorista, grátis nos primeiros 2 min, depois taxa por serviço. Se o motorista cancelar, o cliente não paga e o cancelamento conta para o motorista. Taxa por pagar entra na dívida do cliente |
| Marcadas | Grátis até ao despacho (30 min antes). Mudanças: grátis até 24 h antes, depois taxa |
| Chamadas | O número não é partilhado. Interface de chamada mascarada por env; sem fornecedor, 503 `CALLS_UNAVAILABLE`. Chat só durante o pedido, com respostas rápidas, por SSE |
| Manutenção | Aposenta o técnico ao local (C2). Modelo próprio de marcações. O mecânico é um registo da oficina, sem conta. Capacidade por oficina, com dias bloqueáveis |
| Mudanças | Aposenta as propostas (C3). Preço fixo por viatura, ajudantes e extras, fechado na marcação. Equipa como registo informativo |
| Reboque | Um serviço `towing` com tipos de viatura e preços (C1). Km incluídos por tipo, default 10, com preço por km a mais |
| Reboque e Mudanças | Só prestadores de parceiros (`self_signup=false`) |
| Seguro de bens | Informativo, valor configurável, sem fluxo de sinistro |
| Vouchers e convites | Pagos pela AGORA. A recompensa do convite vai para a Carteira quando o amigo concluir e pagar o primeiro pedido |
| Valores | Em cêntimos, como estão |

## Decisões tomadas durante a implementação

| Tema | Decisão | Porquê |
|---|---|---|
| Estimativa | Uma estimativa serve um único pedido, mesmo que esse pedido seja cancelado. Repetir pede preço novo | É a forma mais simples de impedir corridas duplicadas com a mesma estimativa |
| Duplicados | Recusa com `DUPLICATE_REQUEST` outro pedido ativo do mesmo cliente, no mesmo serviço, com a mesma rota e a mesma hora. Rotas ou serviços diferentes continuam permitidos, porque a PD01 mostra vários pedidos ativos | Protege o duplo clique sem impedir pedidos legítimos |
| Pedido marcado | A estimativa tem de ter o mesmo `scheduled_for` do pedido. Uma estimativa "para agora" não serve para mais tarde | O preço depende da hora: taxa noturna |
| Taxa por paragem | Soma-se depois do mínimo da tarifa, `stop_fee_cents`, por serviço | A tarifa mínima não deve absorver a paragem |
| Dinheiro em pedido digital | O motorista não pode confirmar dinheiro num pedido cujo método é Express ou Referência (`CASH_NOT_EXPECTED`). O cliente pode mudar para dinheiro antes de concluir | Hoje isto apaga a dívida do cliente, e um motorista não deve poder fazê-lo sozinho |
| `details` | `volume_m3`, `fare_cents`, `price_cents` e `total_cents` são retirados de `details` antes de gravar | São dados do servidor |
| Motivos | O texto livre deixa de ser aceite no cancelamento do cliente e do prestador. "Outro motivo" exige `note` | O motivo é guardado por ID, para relatórios. O operador continua a usar texto livre |
| Recusa de oferta | O motivo é obrigatório | "Recusa de oferta com motivo registado" |
| Taxas de cancelamento | Valores iniciais na migração: Económico 500 Kz, Conforto 750 Kz, XL 1 000 Kz, Reboque 2 000 Kz, Mudanças 15 000 Kz, 2 min grátis com motorista. **São palpites**: revê no backoffice em `PUT /admin/cancellation-rules/{serviceId}` | O brief só diz que são configuráveis |
| Pré-aviso | Com taxa, o cancelamento exige `expected_fee_cents` igual à taxa atual; senão responde 409 `CANCEL_FEE_CHANGED`, sem cancelar | Garante que ninguém paga uma taxa que não viu, mesmo se o prazo terminar entre o aviso e a confirmação |
| Motorista que cancela | Conta em `providers.cancelled_requests`, sem penalização automática | O brief só pede que conte |
| Carteira como método | `wallet` é um método de pagamento e paga o valor inteiro, sem pagamento parcial. Sem saldo responde `WALLET_INSUFFICIENT` | O saldo de reembolsos e convites tem de poder ser gasto |
| Taxa de cancelamento | Vai para o motorista atribuído, menos a mesma percentagem de comissão da corrida. Sem motorista, fica na plataforma | O motorista deslocou-se. **Rever** |
| Pagamento em duplicado | Um segundo pagamento confirmado do mesmo pedido vai para a Carteira do cliente | Antes ficava só em log |
| Liquidação | `payout` paga o que a AGORA deve, `collect` recebe a dívida. Não pode passar do saldo. A mesma `reference` só conta uma vez | Evita pagar duas vezes o mesmo documento |
| Taxa da AppyPay | Fica `NULL` até alguém a registar a partir do extrato. Não é descontada ao motorista | Vimos 90 Kz em 100 Kz, mas o valor não está confirmado, por isso não é assumido |
| Catálogos | Elogios, gorjetas, respostas rápidas, tipos de reboque, artigos, extras e temas de ajuda partilham a tabela `catalog_items`, com `attrs` validado por tipo | Evita uma tabela e um CRUD por catálogo, mantendo tudo gerido no backoffice |
| Gorjeta | Uma por pedido, por Express ou Carteira. Em corrida a dinheiro, o método por omissão é Express. Máximo em `max_tip_cents`, inicial 50 000 Kz (**palpite**) | O brief não indica limite |
| Gorjetas do Táxi | 200, 500 e 1 000 Kz; Manutenção 500, 1 000 e 2 000 Kz (**palpite**, as telas só indicam Reboque e Mudanças) | Valores do catálogo, alteráveis |
| Voucher | `fare_cents` mantém o valor sem desconto; o que o cliente paga está em `amount_due_cents`. O desconto deixa sempre 1 Kz por pagar, porque a AppyPay não aceita cobranças de zero | Comissão e relatórios assentam na tarifa |
| TAXI10 | Criado no seed de desenvolvimento, não na migração | Em produção é a operação que decide os vouchers |
| Repetir | `POST /requests/{id}/repeat` devolve a rota e novos preços. O pedido só é criado quando o cliente confirma | O preço pode ter mudado, e uma estimativa só serve um pedido |
| Recibo PDF | Gerado no servidor com um escritor próprio (`shared/simplepdf`), sem biblioteca nova | Regra de não trazer frameworks; basta uma página de texto |
| Ligação partilhada | Dura `share_ttl_minutes` (inicial 240). Na BD fica só o hash do token. Mostra estado, motorista, matrícula, posição e ETA, nunca telefones | Dados mínimos para seguir uma viagem |
| Chamadas | Contrato HTTP simples: `POST {CALLS_BASE_URL}/sessions`, que devolve `proxy_number`. Quando houver fornecedor real pode precisar de um adaptador | Não existe fornecedor escolhido |
| Reboque: valores | Preço por km a mais: Ligeiro 500 Kz, SUV 600 Kz, Motociclo 300 Kz. SUV exige viatura com 2 500 kg (**palpites**, alteráveis em `/admin/catalog/towing_vehicle`) | O brief só indica os preços base e os 10 km |
| Código de entrega | 4 dígitos, 5 tentativas (`completion_code_attempts`). Depois disso só o operador pode concluir | 4 dígitos sem limite adivinham-se. O brief só pede que o código errado recuse |
| Oficina recomendada | A oficina ativa mais próxima da recolha | O brief não define outro critério |
| Marcação e pedido | Cada marcação tem estados próprios em `bookings` e um pedido associado em `service_requests`, só para faturação e listas. Esse pedido nunca é despachado (`next_action_at` nulo) e não usa a máquina de estados das corridas | Pagamentos, ledger, recibo, avaliação, voucher e histórico já assentam em pedidos. Duplicar isso para marcações era mais arriscado. **Rever** se preferires marcações sem pedido associado |
| `/orders` | Não criei esta rota. `GET /requests` já junta corridas e marcações, com filtros por estado e serviço. A marcação aparece em Marcados até a viatura chegar, depois em Em curso | Duas rotas para o mesmo dado só traziam duplicação |
| Manutenção: métodos | Dinheiro desligado em Manutenção. Só Express, Referência e Carteira, e o método por omissão é Express | O brief pede Express depois de aprovar. Dinheiro precisaria de confirmação da oficina, que não existe |
| Marcação: cancelar | Grátis até a viatura chegar, por `POST /bookings/{id}/cancel`, com motivo. Depois disso já não se cancela. O pedido associado não se cancela à parte (`BOOKING_MANAGED`) | Regra `maintenance` com taxa 0 na migração 000023 |
| Oficina no ledger | Conta própria `workshop`, com comissão da tarifa do serviço `maintenance`. Liquidação em `/admin/workshops/{id}/settlements` | A oficina não tem conta de utilizador, por isso não cabe na conta `provider` |
| Referência paga | O valor pago em adiantado confirma a marcação. Se cancelar, vai para a Carteira. Se expirar sem pagamento, a marcação é cancelada e o horário fica livre. Se pagar depois de cancelada, volta para a Carteira | Coerente com "Reembolso de referência vai para a Carteira" |
| Extra com referência | O extra aprovado numa marcação paga em adiantado é cobrado à parte depois de aprovar (`payments.kind = extra`) | O pagamento inicial já ficou fechado |
| Promoção de oficina | Um voucher com `workshop_id` e `auto_apply`, opcionalmente ligado a um serviço. Aplica-se o maior desconto válido. Criada no backoffice em `/admin/vouchers` | Reaproveita validade, limite por cliente e libertação ao cancelar |
| Horários | Passo de 30 min (`booking_slot_minutes`) e até 30 dias (`booking_max_days`) (**palpites**). A capacidade é o número de viaturas em simultâneo, por oficina | O brief não indica valores |
| Chat da marcação | O cliente fala com a oficina, sem ligação a um utilizador concreto. A oficina lê por `/fleet/bookings/{id}/messages`, sem SSE para o painel | O mecânico não tem conta |
| Técnico ao local | Os pedidos de `maintenance` respondem `USE_BOOKINGS`, por `dispatch_policies.booking_only`. `TestMaintenanceByCoverage...` passa a desligar este valor no teste, para continuar a provar a cobertura, que as Mudanças também usam | Decisão C2 do brief |
| Mudanças: catálogo | 10 artigos com volume, 3 viaturas com capacidade (6, 18 e 35 m³), duração base e por m³, capacidade diária (6, 4 e 2) e 3 janelas (**palpites**, exceto os preços do brief). Alteráveis em `/admin/catalog/removal_*` | O brief só indica preços |
| Mudanças: viatura | Cada viatura com lugar para o volume recebe uma estimativa. Viaturas pequenas de mais ficam de fora. A menor que serve vem com `recommended` | O cliente pode preferir uma maior, nunca uma menor |
| Mudanças: dia cheio | A capacidade é por viatura e por dia, não por janela. Cancelar liberta o lugar | A tela mostra o dia como Livre ou Esgotado |
| Mudanças: referência | Só começa a procurar prestador depois de paga. Se expirar, o pedido é cancelado sem taxa. Se cancelar paga, volta para a Carteira, menos a taxa | "A marcação só confirma depois de paga" |
| Mudanças: rota | Depois de marcada, a rota não muda (`ROUTE_CHANGE_FORBIDDEN`) | O preço está fechado |
| Equipa | Registo informativo. O motorista regista, mas só o parceiro pode marcar `verified` | Verificado é um privilégio, não pode vir do motorista |
| Propostas | O motor continua no código e nos testes (`TestRemovalsQuote...`, `TestExpiredQuote...`, que ligam o modo `quote` no próprio teste), mas nenhuma política o usa | Decisão C3. Remover o código era um passo à parte |
| Preferências | `notify_status` e `notify_sms` são respeitadas no envio. O código de identidade e as ofertas ao motorista não dependem delas. `notify_promotions`, `share_location`, `language` e `currency` só ficam guardadas: ainda não há envio de promoções, textos noutras línguas nem outra moeda | **Por provar no envio.** Não há comportamento para estas 4 |
| Caixa de notificações | Tabela nova `inbox_items`, criada nos eventos de pedido, oferta, marcação, pagamento, ticket e convite, com ligação para abrir. Não depende das preferências | `notifications` continua a ser o registo de envios |
| Convite | Código criado no primeiro pedido de consulta, usado uma vez e antes do primeiro pedido pago. Prémio inicial de 1 000 Kz em `referral_reward_cents`, pago na Carteira | Do brief |
| Linha de apoio | `support_phone` inicial `+244923000000` (**palpite, substitui pelo número real**) | O brief não indica o número |
| Reserva sem pagamento | Uma marcação ou mudança por referência sem pagamento em curso expira em 60 min (`prepay_hold_minutes`, **palpite**) e liberta o lugar | Sem isto, um cliente podia esgotar uma oficina sem pagar |
| Mudança nunca paga | Cancelar uma mudança por referência ainda por pagar não custa nada, mesmo a menos de 24 h | Nunca chegou a confirmar. **Rever** |
| Mudança paga em adiantado | O dinheiro fica na plataforma até concluir. Só aí o motorista recebe a tarifa menos a comissão. Se o cliente cancelar tarde, a taxa fica e vai para o motorista atribuído, menos a comissão | Antes de concluir o motorista não ganhou nada |
| Método de pagamento | Depois de marcada, não se muda para Referência, e uma marcação por Referência não muda de método (`PAYMENT_METHOD_LOCKED`) | A confirmação depende do método escolhido na marcação |
| Reembolso | `POST /admin/payments/{id}/refund` passa a exigir `Idempotency-Key`. **O backoffice tem de a enviar** | Dois cliques devolviam dinheiro duas vezes |
| Limites de validação | Quantidades, tamanho da equipa, andares e dias do calendário ficam no código | São limites de entrada, não regras de negócio. **Rever** se preferires no backoffice |
| Taxa acima do pago | Se o cliente pagou menos do que a taxa, por voucher ou reembolso, fica só o que foi pago e não há dívida adicional | Nunca se cobra uma segunda vez um pedido cancelado. **Rever** |
| Mudanças: espera e aumento | Não há taxa de espera nem aumento de preço em Mudanças | "Preço fechado na marcação" |
