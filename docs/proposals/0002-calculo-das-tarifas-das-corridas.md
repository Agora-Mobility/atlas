# Proposta 0002 – Cálculo das tarifas das corridas

- **Estado:** fases 2 e 3 implementadas no Atlas; fase 1 é só do Argus; fase 4 espera pelo mapa próprio
- **Data:** 2026-11-11 (documento original de 2026-11-10)
- **Requisitos:** RF09, RF24
- **ADRs afetados:** 0018

---

## 1. Resumo

A AGORA mantém o **preço fechado antes da corrida**: tarifa base + km + minutos, com
mínimo, taxas de zona, noite e paragens, e comissão em percentagem. A esse preço
junta-se um **factor de procura**: quando numa zona há muitos pedidos para poucos
prestadores livres, o preço sobe, até um tecto. Depois de fechado, o preço só muda por
motivos que o cliente vê: espera depois do tempo grátis, alteração do destino ou aumento
da oferta feito pelo próprio cliente.

Esta proposta acrescenta:

- o factor de procura, que compara em cada área da zona os clientes à espera com os
  prestadores livres, com tecto e modo por zona e categoria (desligado, só observar,
  ligado);
- a regra de **não** recalcular o preço pelos km reais no fim da corrida; os km e minutos
  reais passam a ser gravados como indicador de desvio, para revisão no Argus;
- uma previsão do tempo de viagem mais certa, com as velocidades aprendidas no mapa
  próprio (fase 4, por fazer).

Os valores (Kz por km, comissão, escalões e tecto da procura) e as regras marcadas como
decisão são do cliente. O código implementa o mecanismo, não os valores: os números
dos exemplos são fictícios.

## 2. Como o mercado calcula

Todas partem da mesma fórmula (base + km + minutos, com mínimo); diferem em quem fixa o
preço e em como reagem à procura. Os algoritmos exactos não são públicos: a tabela
resume o comportamento conhecido.

| App | Quem fixa o preço | Procura alta | Muda depois do pedido |
|---|---|---|---|
| Uber | A plataforma, antes da corrida | Multiplicador automático | Rota muito diferente, paragens, espera |
| Bolt | A plataforma, antes da corrida | Multiplicador automático | Rota, paragens, espera |
| Yango | A plataforma, antes da corrida | Coeficiente dinâmico | Destino, paragens, espera |
| Heetch | A plataforma, estimativa fixada no pedido | Aumento automático | Destino, espera |
| inDrive | O cliente propõe, o motorista aceita ou contrapõe | Sem multiplicador: regula-se pela negociação | Só por acordo |

Duas ideias repetem-se em todas: o cliente quer saber o preço antes de pedir, e o ganho
do prestador é o preço menos a comissão da plataforma.

## 3. O que o Atlas já tinha

O Atlas já seguia o modelo de preço fechado da Uber e da Yango, com um aumento opcional
feito pelo cliente, parecido com o inDrive. Nada disto foi reescrito.

| Peça | Onde | O que faz |
|---|---|---|
| Tarifa por serviço e zona | `service_tariffs` | Base, Kz/km, Kz/min, mínimo, taxa de zona, taxa nocturna (22h–6h por omissão), taxa por paragem, espera grátis (180 s por omissão), comissão em pontos base. Nunca se altera: fecha-se e cria-se uma versão nova |
| Cálculo | `ServiceTariff.FareFor` / `FareForRoute` | Aplica a fórmula da secção 4, em cêntimos inteiros |
| Estimativa | `fare_estimates`, `CreateEstimates` | Guarda o preço, a distância, a duração, a origem da rota e a comissão; válida 5 min (`FARE_ESTIMATE_TTL`) |
| Rota | `Routing.Route` | Google; se falhar, uma rota de recurso (`fallback`) |
| Tempo aprendido | `eta_observations` | Duração real das corridas por célula H3 (resolução 8) de origem e destino e por hora; usada só na rota de recurso, com pelo menos 5 amostras |
| Espera | `chargeWaiting` | Cobra os minutos depois da espera grátis ao preço por minuto |
| Aumento da oferta | `Boost` | O cliente sobe o preço em 10% ou 20% enquanto procura prestador (se a política permitir; não em pré-pago) |
| Alteração de rota | `ChangeRoute` | Recalcula com a mesma tarifa e regista a diferença |
| Ajustes | `fare_adjustments` | Cada mudança de preço fica registada com motivo e autor, e aparece no recibo |
| Pré-pagamento | `prepaid_hold` | Cativa o valor fechado antes da corrida |

O fim da corrida não recalcula o preço: `CompleteJob` só valida o código de conclusão.

## 4. O algoritmo

O preço é calculado uma vez, quando o cliente pede a estimativa, e fica fechado:

```text
preço = ( max(B + K × d_km + M × t_min, P_min) + T_zona + T_noite + T_paragem × n_paragens ) × f_procura
```

B = tarifa base, K = Kz por km, M = Kz por minuto, P_min = mínimo, d e t = distância e
duração previstas, f_procura = factor de procura da área da recolha e da categoria no
momento da estimativa: 1 quando há prestadores livres suficientes, maior quando há mais
pedidos do que prestadores, até ao tecto.

Passos, por pedido de estimativa (`CreateEstimates`):

1. Encontrar a zona de serviço da recolha; fora de zona, recusar.
2. Para cada serviço da categoria, ler a tarifa em vigor para serviço + zona (ou a tarifa
   geral do serviço).
3. Calcular a rota: distância e duração previstas, à hora local da recolha (ou da
   marcação).
4. Ler o factor de procura da área da recolha (`demandFor`) e aplicar a fórmula em
   cêntimos inteiros (`ServiceTariff.Quote`), arredondando cada parcela.
5. Calcular a comissão: preço × comissão em pontos base ÷ 10 000 (secção 7).
6. Guardar a estimativa com a versão da tarifa, a rota, o factor, a parte da procura e a
   validade, e devolver o preço com `demand_factor_bps` e `high_demand`.
7. Ao confirmar o pedido, o preço da estimativa passa para a solicitação e a parte da
   procura fica como ajuste `demand`; a partir daí só muda por um ajuste registado.

Duas regras protegem o cliente e o prestador:

- **A tarifa é a da estimativa.** Uma tarifa nova não mexe em corridas já pedidas,
  porque cada estimativa guarda o id da versão. O factor de procura também fica na
  estimativa: uma alteração de rota recalcula com o mesmo factor, não com o do momento.
- **Todo o ajuste tem motivo.** Espera, alteração de rota, aumento da oferta e procura:
  cada um fica em `fare_adjustments` e no recibo (“Procura alta”).

## 5. Procura dinâmica

O preço sobe quando, numa área da zona, há mais pedidos à espera do que prestadores
livres, e volta ao normal quando o equilíbrio regressa. O aumento reserva os prestadores
disponíveis para quem aceita pagar mais e puxa prestadores de áreas vizinhas.

### Áreas

Uma zona de serviço inteira (por exemplo, a província de Luanda) é grande demais para um
só factor. A área de preço é a célula H3 da recolha à resolução
`DEMAND_AREA_RESOLUTION` (7 por omissão, cerca de 5 km²; aceita de 5 a 8). A
configuração (escalões, tecto, modo) é por zona de serviço e categoria; o factor é
por área.

### Cálculo, a cada minuto (`RefreshDemand`)

Corre no housekeeping do vigilante. Um cadeado no Redis (`demand:refresh`) garante que só
uma instância calcula em cada minuto. Para cada zona e categoria em modo `observe` ou
`on`:

1. **Procura (P):** número de **clientes distintos** com uma solicitação em `created`,
   `searching`, `offered` ou `awaiting_choice` com recolha na área (`pickup_h3_r8`
   agregado à área), ou com uma estimativa pedida nos últimos `DEMAND_QUOTE_WINDOW`
   (3 min por omissão), que mostra procura antes de virar pedido. Cada cliente conta uma
   vez: quem pediu estimativa e depois o pedido, ou pediu várias estimativas porque mexeu
   o pin, é um só cliente. Assim um cliente não consegue subir o preço da área sozinho. As
   estimativas agendadas não contam.
2. **Oferta (O):** prestadores online nas células da área (índice
   `live:idx:<categoria>:r<res>:<célula>`), sem reserva (`offer:lock`) e sem corrida
   activa (`live:trip`).
3. **Razão:** `r = (P + 1) ÷ (O + 1)`, em pontos base (10 000 = 1,0). O +1 evita saltos
   quando os números são pequenos.
4. **Suavização:** a razão nova mistura-se com a do minuto anterior: sobe depressa (70%
   da diferença) e desce devagar (25%). Uma área sai quando a razão volta a 1,0 e a
   suavizada fica abaixo de 1,01.
5. **Factor:** a razão suavizada converte-se pelos escalões da zona e categoria, com
   tecto.
6. **Gravação:** os factores ficam no Redis (`demand:factor:<zona>:<categoria>`) com
   validade `DEMAND_FACTOR_TTL` (3 min por omissão); sem valor válido, o factor é 1. Cada
   área fora do normal grava uma linha em `demand_snapshots` (histórico do Argus),
   apagada ao fim de `DEMAND_HISTORY_RETENTION` (30 dias por omissão).

Na estimativa, o factor do momento entra no preço fechado e fica guardado com ela. O
cliente vê o preço já com o aumento e `high_demand: true` antes de confirmar. Durante os
5 min de validade da estimativa, o preço não muda.

### Escalões

Um escalão aplica-se quando a razão suavizada está **acima** de `above_ratio_bps`.
Exemplo **fictício**:

| Razão pedidos / prestadores livres | Factor | Escalão |
|---|---|---|
| até 1,0 | 1,0 (preço normal) | — |
| 1,0 a 1,5 | 1,1 | `{"above_ratio_bps": 10000, "factor_bps": 11000}` |
| 1,5 a 2,0 | 1,2 | `{"above_ratio_bps": 15000, "factor_bps": 12000}` |
| acima de 2,0 | 1,3 (tecto) | `{"above_ratio_bps": 20000, "factor_bps": 13000}` |

Validação: no máximo 10 escalões, razões a subir, factores que não descem, nenhum acima
do tecto, e tecto entre 1,0 e 3,0.

### Modos

| Modo | Mede a procura | Cobra o factor | Uso |
|---|---|---|---|
| `off` | Não | Não | Por omissão. Passar a `off` apaga logo os factores da zona no Redis (cheias, falhas de energia) |
| `observe` | Sim: conta, calcula o factor e grava o histórico | Não: as estimativas saem ao preço normal | Calibrar os escalões com dados reais de cada zona antes de cobrar |
| `on` | Sim | Sim | Depois de calibrar |

O caminho recomendado para cada zona é `observe` durante algumas semanas, rever o
histórico (`/admin/demand-pricing/history`) e só depois `on`, com um tecto baixo no início
(por exemplo 1,2).

### Regras

- **Tecto e modo por zona e categoria.** Baixar o tecto ou mudar o modo aplica-se logo às
  estimativas novas.
- **Corridas agendadas usam o factor 1**, porque o preço é fixado muito antes da hora (a
  confirmar). Mudanças (preço por itens, sempre agendadas) também ficam de fora.
- **Falha fechada no preço normal.** Se o Redis ou a configuração falharem, a estimativa
  sai com factor 1 e regista um aviso; a procura nunca impede um pedido.
- **O aumento da oferta (10% ou 20%) soma-se por cima** do preço com procura, e continua
  a servir quando o factor está em 1.
- Só se aplica a categorias despachadas a prestadores online (`candidate_source = live`):
  táxi e reboque.
- Os prestadores podem ver as áreas com procura alta pelo mapa de calor do mapa próprio,
  para se deslocarem (fora deste trabalho).

## 6. Fim da corrida

O fim da corrida **não** recalcula o preço pelos km reais; o GPS real serve para
aprender, não para cobrar. Recalcular tiraria ao cliente o preço que aceitou,
desacertaria a cativação do pré-pagamento e faria cada desvio do prestador custar ao
cliente.

| Situação | O que acontece ao preço |
|---|---|
| Corrida normal, mesmo com trânsito | Fica o preço fechado |
| Espera na recolha além do tempo grátis | Soma a espera |
| Cliente muda o destino ou acrescenta paragens | Recalcula com a mesma tarifa e o mesmo factor de procura |
| Percurso real muito mais longo sem pedido do cliente | Fica o preço fechado; a corrida vai para revisão no Argus |
| Percurso real muito mais curto (corrida terminada cedo) | Fica o preço fechado e vai para revisão (a decidir se recalcula para baixo) |

Ao concluir (`recordCompletion`), o Atlas grava em `request_trip_metrics`:

- a distância e a duração previstas (da estimativa);
- a distância real, somando os troços entre as amostras de GPS da corrida
  (`provider_location_samples`, uma a cada 30 s, desde o início da corrida), com PostGIS;
- a duração real (do início ao fim da corrida);
- o desvio de cada uma em pontos base face à previsão;
- o número de amostras de GPS;
- `needs_review` quando o desvio da distância passa `ROUTE_REVIEW_ABOVE_PERCENT` (30%
  por omissão) e o cliente não mudou a rota. Um percurso **mais longo** vai sempre para
  revisão, porque falhas no GPS só encurtam a distância medida. Um percurso **mais curto**
  só vai quando as amostras cobrem pelo menos 60% da duração da corrida; abaixo disso a
  app do prestador esteve sem enviar posições (segundo plano, sem rede) e a distância
  medida não é fiável.

O Argus lista as corridas a rever com `GET /admin/requests?route_review=true`; cada linha
traz `distance_deviation_bps` e `route_review`, e o detalhe traz o bloco `trip`. O “preço
por km” do diagrama do mapa próprio deve ser lido como este indicador, não como
cobrança.

### Limites conhecidos

- **Bordos das áreas.** Um prestador a poucos metros, mas noutra célula, não conta como
  oferta da área ao lado; duas ruas vizinhas podem ter factores diferentes. A suavização
  e o tecto limitam o efeito; se for um problema, a oferta pode passar a incluir o anel
  de células vizinhas.
- **Arranque com poucos prestadores.** Com 0 livres, 1 cliente já dá razão 2,0. Por isso
  o modo `observe` e um tecto baixo no início.

## 7. Ganho do prestador e comissão

O prestador recebe o preço menos a comissão da AGORA. A comissão é calculada uma vez,
sobre o preço da tarifa. Os ajustes posteriores ficam inteiros para o prestador:

| Parcela | Comissão da AGORA |
|---|---|
| Preço da tarifa (base, km, minutos, zona, noite, paragens) | Sim, pela percentagem da tarifa |
| Aumento da oferta (10% ou 20%, escolhido pelo cliente) | Não |
| Espera além do tempo grátis | Não |
| Alteração de rota | Sim, recalculada sobre o novo preço sem o aumento |
| Gorjeta | Não (paga à parte) |
| Aumento pela procura (factor acima de 1) | Configurável por zona e categoria: `commission_on_surge`, desligado por omissão (todo o aumento vai para o prestador) |

Esta divisão é uma regra financeira: tem de ser confirmada pelo cliente e revista pelo
responsável técnico, como pede o Documento de Gestão.

## 8. Exemplo

Uma corrida de 8,4 km e 22 min às 23h, com uma paragem, custa 2 700 Kz fechados e chega
a 3 050 Kz com aumento e espera; o prestador recebe 2 510 Kz. **Todos os valores da
tarifa são fictícios.**

Tarifa fictícia: base 500 Kz, 150 Kz/km, 20 Kz/min, mínimo 1 000 Kz, taxa nocturna
300 Kz, 200 Kz por paragem, espera grátis 3 min, comissão 20%.

| Passo | Cálculo | Kz |
|---|---|---|
| Base | | 500 |
| Distância | 8,4 km × 150 | 1 260 |
| Tempo | 22 min × 20 | 440 |
| Subtotal | acima do mínimo de 1 000 | 2 200 |
| Taxa nocturna | 23h está entre 22h e 6h | 300 |
| Paragem | 1 × 200 | 200 |
| **Preço fechado** | | **2 700** |
| Comissão da AGORA | 20% de 2 700 | 540 |
| Aumento da oferta | cliente escolhe 10% | 270 |
| Espera | 7 min na recolha − 3 grátis = 4 × 20 | 80 |
| **Total pago pelo cliente** | | **3 050** |
| **Ganho do prestador** | 3 050 − 540 | **2 510** |

**Com procura alta.** Se, à hora do pedido, a área tivesse 18 pedidos à espera para 10
prestadores livres, a razão seria (18 + 1) ÷ (10 + 1) ≈ 1,73 e o factor (fictício) 1,2.
O preço fechado passaria de 2 700 para 3 240 Kz (ajuste `demand` de 540 Kz). Com
`commission_on_surge` desligado, a comissão fica em 540 Kz; ligado, passa a 648 Kz. O
aumento da oferta e a espera somam-se por cima, como acima. Estes números estão nos
testes de `domain/entities/demand_pricing_test.go`; o teste ponta a ponta
`test/e2e/demand_test.go` faz o mesmo pela API (estimativa, pedido, ajuste `demand`,
comissão, modo `observe` e desligar).

## 9. Contratos

### Estimativa

`POST /api/v1/fare-estimates` passa a devolver, em cada item:

| Campo | Significado |
|---|---|
| `total_cents` | Preço fechado, já com o factor de procura |
| `demand_factor_bps` | Factor aplicado; 10000 é o preço normal |
| `high_demand` | `true` quando o factor está acima de 1, para a app avisar antes de confirmar |

### Argus (permissão `services:manage`)

| Método e caminho | O que faz |
|---|---|
| `GET /api/v1/admin/demand-pricing` | Configuração de cada zona e categoria |
| `PUT /api/v1/admin/demand-pricing` | Cria ou altera escalões, tecto, modo (`off`, `observe`, `on`) e `commission_on_surge` de uma zona e categoria; fica na auditoria |
| `GET /api/v1/admin/demand-pricing/areas?zone_id=&category=` | Factor e razão actuais por área, com o centro da célula para o mapa; em `observe` mostra o factor que se cobraria |
| `GET /api/v1/admin/demand-pricing/history?zone_id=&category=&from=` | Histórico por área e minuto (24 h por omissão) |
| `GET /api/v1/admin/requests?route_review=true` | Corridas com desvio para rever |
| `GET /api/v1/admin/requests/:id` | Detalhe com o bloco `trip` (previsto vs real) |

Exemplo de `PUT /api/v1/admin/demand-pricing`:

```json
{
  "zone_id": "00000000-0000-0000-0000-0000000000c1",
  "category": "taxi",
  "mode": "observe",
  "max_factor_bps": 13000,
  "commission_on_surge": false,
  "tiers": [
    { "above_ratio_bps": 10000, "factor_bps": 11000 },
    { "above_ratio_bps": 15000, "factor_bps": 12000 },
    { "above_ratio_bps": 20000, "factor_bps": 13000 }
  ]
}
```

### Variáveis de ambiente

| Variável | Omissão | Uso |
|---|---|---|
| `DEMAND_AREA_RESOLUTION` | 7 | Resolução H3 da área de preço (5 a 8) |
| `DEMAND_QUOTE_WINDOW` | 3m | Janela das estimativas que contam como procura |
| `DEMAND_FACTOR_TTL` | 3m | Validade do factor no Redis; sem cálculo novo, volta a 1 |
| `DEMAND_HISTORY_RETENTION` | 720h | Quanto tempo fica o histórico de procura |
| `ROUTE_REVIEW_ABOVE_PERCENT` | 30 | Desvio de distância a partir do qual a corrida vai para revisão; 0 desliga |

### Dados

| Tabela ou chave | Conteúdo |
|---|---|
| `demand_pricing` | Escalões, tecto, modo e `commission_on_surge` por zona e categoria |
| `demand_snapshots` | Histórico por área e minuto: pedidos, livres, razão, factor |
| `fare_estimates.demand_factor_bps`, `demand_cents`, `demand_commissioned` | O factor fechado na estimativa |
| `fare_adjustments.reason = 'demand'` | A parte da procura no recibo |
| `request_trip_metrics` | Previsto vs real, amostras de GPS e marca de revisão |
| Redis `demand:quotes:*`, `demand:ratio:*`, `demand:factor:*`, `demand:refresh` | Estimativas recentes por área e cliente, razões suavizadas, factores em vigor, cadeado do cálculo |

## 10. Distância e tempo previstos (fase 4, por fazer)

A qualidade do preço fechado depende da previsão da duração: se a corrida demora o dobro
do previsto, o prestador perde; se demora metade, o cliente paga a mais. A proposta é
usar o mapa próprio para melhorar a duração, mantendo o Google para a distância.

| Fase | Distância | Duração |
|---|---|---|
| Hoje | Google; rota de recurso se falhar | Google; na rota de recurso, `eta_observations` com ≥ 5 amostras |
| Com o mapa próprio | Igual | Google, corrigida pela velocidade real por zona e hora (`velocidades_zona`) quando houver amostras suficientes |

Cuidados:

- **Termos do Google.** Usar a distância e a duração do Google para o preço é o uso
  normal da API; guardar a rota em `fare_estimates` como prova do preço deve ser
  confirmado nos termos.
- **Correcção limitada.** A correcção pela velocidade real deve ter um limite (por
  exemplo, nunca mais de ±30% sobre o Google) até haver confiança nos dados. O limite é
  um valor a decidir.
- **Mesma fonte para tudo.** A estimativa, a alteração de rota e a ETA do
  `request.position` devem usar a mesma previsão, para o cliente não ver tempos
  diferentes no preço e no mapa.

Os km e minutos reais que a fase 3 já grava (`request_trip_metrics`) são a base para
medir o erro médio da previsão quando esta fase começar.

## 11. Implementação por fases

Em todas as fases os cálculos ficam em cêntimos inteiros, com testes unitários por
parcela e por arredondamento, e o código passa pela revisão do responsável técnico
(módulo financeiro).

| Fase | Atlas | Argus | Estado |
|---|---|---|---|
| 1. Tarifas no painel | Nada novo: endpoints de tarifas existentes | Ecrã de tarifas por serviço e zona, com histórico de versões (RF24) | Só Argus |
| 2. Procura dinâmica | Cálculo do factor por área e categoria a cada minuto, por clientes distintos; factor na estimativa, em `fare_estimates` e como ajuste `demand`; tecto e modo `off`/`observe`/`on` por zona | Escalões, tecto e modo por zona; factor actual por área e histórico | **Feito no Atlas**; falta o ecrã no Argus, calibrar em `observe` e os valores do cliente |
| 3. Desvio por corrida | Gravar km e minutos reais no fim; calcular o desvio face à estimativa | Coluna e filtro de desvio nas solicitações; detalhe na solicitação | **Feito no Atlas**; falta o ecrã no Argus |
| 4. ETA aprendida | Corrigir a duração do Google com `velocidades_zona`, com limite | Indicador de erro médio da previsão (previsto vs real) | Espera pelo processo nocturno 3 do mapa próprio |

## 12. Questões em aberto para o cliente

- [ ] Valores de cada tarifa por serviço e zona: base, Kz/km, Kz/min, mínimo, taxas de
      zona, noite e paragem, espera grátis.
- [ ] Comissão da AGORA por serviço, e se incide também sobre o aumento da oferta e a
      espera.
- [ ] Preço fechado sem recálculo no fim (implementado), ou recálculo pelos km reais.
- [ ] Corrida terminada antes do destino: manter o preço (implementado) ou recalcular
      para baixo.
- [ ] Procura dinâmica: escalões por zona e categoria, tecto, se a comissão incide sobre
      o aumento (`commission_on_surge`), e se as corridas agendadas ficam de fora
      (implementado: ficam).
- [ ] Tamanho da área de preço (`DEMAND_AREA_RESOLUTION`).
- [ ] Limite da correcção da duração pelas velocidades aprendidas.
- [ ] A partir de que desvio entre o percurso previsto e o real a corrida vai para
      revisão no Argus (`ROUTE_REVIEW_ABOVE_PERCENT`, 30% por omissão).
- [ ] Confirmação legal: regras angolanas sobre preços de transporte e termos do Google
      para guardar a rota da estimativa.

Fonte do estado anterior: código do Atlas na `develop` (`service_tariffs`, `fares.go`,
`pricing.go`, `requests.go`, `offers.go`) e o documento *AGORA Mobility — Arquitectura
do mapa próprio* (v2).
