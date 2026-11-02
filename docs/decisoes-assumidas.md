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
