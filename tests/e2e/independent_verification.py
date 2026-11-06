#!/usr/bin/env python3
"""Verificacao QA independente dos fluxos pedidos ao Atlas.

Diferenca face a tests/e2e/qa_flows.py: as asserções sao reescritas de forma
propria, exactas (sem tolerancias grandes) e cobrem requisitos que o script
anterior nao exercia (duracao de 2 servicos, marcacao em dia fechado,
limite de paragens configuravel na BD, "Repetir" a criar pedido, capacidade
de mudancas por janela/dia).

Uso (com a stack postgres/redis/rabbitmq levantada):
    python3 tests/e2e/independent_verification.py [taxi maint tow mov orders sec docs]

Para nunca enviar SMS/e-mail reais, para a API do Docker, arranca um binario
local com CASTBRICK_BASE_URL/RESEND_BASE_URL a apontar para um stub HTTP e,
no fim, repoe a API do Docker.
"""
import datetime as dt
import http.server
import json
import os
import random
import re
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
import uuid

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import qa_flows as Q

BASE = Q.BASE
STUB_PORT = Q.STUB_PORT
PICKUP, DEST, STOPS = Q.PICKUP, Q.DEST, Q.STOPS
CUSTOMER, PROVIDER, BACKOFFICE, PARTNER = Q.CUSTOMER, Q.PROVIDER, Q.BACKOFFICE, Q.PARTNER
SIMULATED_OK = Q.SIMULATED_OK
PASSWORD = Q.PASSWORD
PARTNER_ORG = Q.PARTNER_ORG

Scenario, chk, info, code = Q.Scenario, Q.chk, Q.info, Q.code
call, sql, new_user, staff, estimate, create = Q.call, Q.sql, Q.new_user, Q.staff, Q.estimate, Q.create
go_online, go_offline, wait_offer, accept, advance = Q.go_online, Q.go_offline, Q.wait_offer, Q.accept, Q.advance
completed_ride, wait_payment, sms_to, otp_for, random_phone = Q.completed_ride, Q.wait_payment, Q.sms_to, Q.otp_for, Q.random_phone
cancel, reason_id, fetch_raw = Q.cancel, Q.reason_id, Q.fetch_raw
workshop_fixture, new_vehicle, book, slot_of, next_weekday = Q.workshop_fixture, Q.new_vehicle, Q.book, Q.slot_of, Q.next_weekday
removal_options, removal_estimate, removal_request, window_at = Q.removal_options, Q.removal_estimate, Q.removal_request, Q.window_at
tow_option, towing_details = Q.tow_option, Q.towing_details

ADMIN = [None]
PARTNER_TOKEN = [None]
OPENAPI = []


def close(ok, label):
    chk(ok, label)


def exact(label, want, got, tol=0):
    chk(abs(want - got) <= tol, f"{label}: esperado {want}, API/BD {got} (tolerancia {tol})")


def db_one(query):
    return sql(query).splitlines()[0] if sql(query) else ""


# --------------------------------------------------------------------------
# Documentacao / contrato
# --------------------------------------------------------------------------
def docs():
    with Scenario("V0", "Contrato OpenAPI e migracoes"):
        with urllib.request.urlopen(BASE + "/docs/openapi.json") as res:
            spec = json.load(res)
        paths = [p.removeprefix("/api/v1") for p in spec["paths"]]
        chk(len(paths) >= 120, f"{len(paths)} caminhos documentados")
        required = [
            "/requests", "/requests/{id}/repeat", "/requests/{id}/receipt", "/requests/{id}/rating",
            "/requests/{id}/cancellation", "/fare-estimates", "/workshops", "/workshops/{id}/availability",
            "/bookings", "/bookings/{id}/approve", "/bookings/{id}/extras/{extraId}/answer",
            "/removals/options", "/removals/availability", "/towing/options",
            "/notifications", "/notifications/{id}/read", "/users/me/preferences", "/users/me/verification-code",
        ]
        missing = [p for p in required if p not in paths]
        chk(not missing, f"rotas exigidas no OpenAPI: faltam {missing}")
        version, dirty = sql("SELECT version||'|'||dirty FROM schema_migrations").split("|")
        chk(int(version) >= 35 and dirty == "false", f"migracoes: versao {version}, dirty={dirty}")
        chk(sql("SELECT count(*) FROM information_schema.columns WHERE table_schema='atlas' AND table_name='dispatch_policies' AND column_name='max_stops'") == "1", "max_stops existe em dispatch_policies")
        chk(sql("SELECT count(*) FROM information_schema.columns WHERE table_schema='atlas' AND table_name='users' AND column_name='notify_sms'") == "1", "preferencias existem em users")


# --------------------------------------------------------------------------
# Taxi
# --------------------------------------------------------------------------
def taxi(admin):
    with Scenario("V-T1", "Estimativa com e sem 2 paragens (valor exacto)"):
        customer = new_user("customer", "Cliente V-T1")
        plain, stopped = estimate(customer), estimate(customer, STOPS[:2])
        want_plain, tariff = Q.expected_fare(plain["service_slug"], plain["distance_m"], plain["duration_s"], 0)
        want_stops, _ = Q.expected_fare(stopped["service_slug"], stopped["distance_m"], stopped["duration_s"], 2)
        exact("tarifa sem paragens (base+km+min+minimo+zona)", want_plain, plain["total_cents"], 1)
        exact("tarifa com 2 paragens (inclui stop_fee)", want_stops, stopped["total_cents"], 1)
        chk(stopped["distance_m"] > plain["distance_m"] and stopped["duration_s"] >= plain["duration_s"], f"paragens alteram a rota: {plain['distance_m']}m/{plain['duration_s']}s -> {stopped['distance_m']}m/{stopped['duration_s']}s")
        info(f"stop_fee configurado na BD = {tariff[6]} (0 = a variacao vem so da rota)")
        status, created = create(customer, stopped, STOPS[:2])
        chk(status == 201 and created["fare_cents"] == stopped["total_cents"], f"pedido usa o total da estimativa com paragens: {status} {created.get('fare_cents')}")
        walks = sql(f"SELECT count(*) FROM service_request_stops WHERE request_id='{created['id']}'")
        chk(walks == "2", f"2 paragens guardadas na BD: {walks}")
        cheap = estimate(customer)
        status, res = create(customer, cheap, STOPS[:2])
        chk(400 <= status < 500 and code(res) == "FARE_ESTIMATE_INVALID", f"estimativa sem paragens serve pedido com paragens? {status} {code(res)}")
        cancel(customer["tok"], created["id"])


    with Scenario("V-T2", "Remover paragem recalcula exactamente e limite configuravel"):
        customer = new_user("customer", "Cliente V-T2")
        est = estimate(customer, STOPS[:2])
        status, created = create(customer, est, STOPS[:2])
        rid = created["id"]
        status, changed = call("POST", f"/requests/{rid}/route", {"destination": DEST, "stops": STOPS[:1]}, customer["tok"])
        one = estimate(new_user("customer", "Cliente V-T2b"), STOPS[:1])
        chk(status == 200, f"rota aceite: {status}")
        exact("novo preco == estimativa com 1 paragem", one["total_cents"], changed.get("fare_cents", -1), 1)
        delta = int(changed.get("fare_cents", 0)) - int(est["total_cents"])
        adj = sql(f"SELECT COALESCE(sum(amount_cents),0) FROM fare_adjustments WHERE request_id='{rid}'")
        exact("soma dos ajustamentos == delta", delta, int(adj))
        chk(len(changed.get("stops", [])) == 1, f"1 paragem depois da alteracao: {len(changed.get('stops', []))}")
        for where, body in (("rota", ("POST", f"/requests/{rid}/route", {"destination": DEST, "stops": STOPS[:4]})),
                            ("estimativa", ("POST", "/fare-estimates", {"category": "taxi", "pickup": PICKUP, "destination": DEST, "stops": STOPS[:4]})),
                            ("pedido", None)):
            if body is None:
                status, res = create(customer, est, STOPS[:4])
            else:
                status, res = call(body[0], body[1], body[2], customer["tok"])
            chk(400 <= status < 500, f"4 paragens na {where}: {status} {code(res)}")
        policy = sql("SELECT p.id||'|'||p.max_stops FROM dispatch_policies p JOIN service_categories c ON c.id=p.category_id WHERE c.slug='taxi' ORDER BY p.zone_id NULLS FIRST LIMIT 1")
        pid, original = policy.split("|")
        try:
            sql(f"UPDATE dispatch_policies SET max_stops=1 WHERE id='{pid}'")
            status, res = call("POST", "/fare-estimates", {"category": "taxi", "pickup": PICKUP, "destination": DEST, "stops": STOPS[:2]}, customer["tok"])
            chk(status == 422 and code(res) == "VALIDATION_ERROR", f"max_stops=1 na BD recusa 2 paragens: {status} {code(res)}")
        finally:
            sql(f"UPDATE dispatch_policies SET max_stops={int(original)} WHERE id='{pid}'")
        status, res = call("POST", "/fare-estimates", {"category": "taxi", "pickup": PICKUP, "destination": DEST, "stops": STOPS[:2]}, customer["tok"])
        chk(status == 201, f"restaurado max_stops={original} volta a aceitar 2: {status}")
        cancel(customer["tok"], rid)


    with Scenario("V-T3", "Agendamento: amanha 07:30, job 30 min antes, janela 7 dias" ):
        customer, driver = new_user("customer", "Cliente V-T3"), new_user("provider", "Motorista V-T3", "taxi")
        luanda = dt.timezone(dt.timedelta(hours=1))
        tomorrow = (dt.datetime.now(luanda) + dt.timedelta(days=1)).replace(hour=7, minute=30, second=0, microsecond=0)
        est = estimate(customer, scheduled_for=tomorrow.isoformat())
        status, created = create(customer, est, scheduled_for=tomorrow.isoformat())
        chk(status == 201 and created.get("status") == "scheduled", f"amanha 07:30 fica agendado: {status} {created.get('status')}")
        rid = created["id"]
        lead = sql(f"SELECT extract(epoch FROM scheduled_for-next_action_at) FROM service_requests WHERE id='{rid}'")
        chk(lead.startswith("1800"), f"next_action_at = scheduled_for - 1800s (BD): {lead}")
        base = dt.datetime.now(luanda).replace(minute=0, second=0, microsecond=0)
        for label, when in (("8 dias", base + dt.timedelta(days=8)),
                            ("passado", base - dt.timedelta(hours=1)),
                            ("fora da grelha 07:37", tomorrow.replace(minute=37))):
            status, res = call("POST", "/fare-estimates", {"category": "taxi", "pickup": PICKUP, "destination": DEST, "scheduled_for": when.isoformat()}, customer["tok"])
            chk(status == 422, f"estimativa {label}: {status} {code(res)}")
        go_online(driver)
        sql(f"UPDATE service_requests SET next_action_at=now() WHERE id='{rid}'")
        offer = wait_offer(driver, rid, 30)
        chk(offer is not None, "o watcher despacha quando next_action_at vence")
        status, res = cancel(customer["tok"], rid)
        chk(status == 200 and res.get("cancel_fee_cents") == 0, f"cancelar agendado sem motorista e gratis: {status} {res.get('cancel_fee_cents')}")
        go_offline(driver)


    with Scenario("V-T4", "Pedido para outra pessoa" ):
        customer, driver = new_user("customer", "Cliente V-T4"), new_user("provider", "Motorista V-T4", "taxi")
        passenger = random_phone()
        go_online(driver)
        est = estimate(customer)
        status, created = create(customer, est, passenger={"name": "Maria Neto", "phone": passenger}, payment_method="gpo")
        rid = created["id"]
        offer = wait_offer(driver, rid)
        accept(driver, offer)
        _, seen = call("GET", f"/requests/{rid}", None, driver["tok"], PROVIDER)
        chk(seen.get("passenger_name") == "Maria Neto" and seen.get("passenger_phone") == passenger, f"motorista ve nome e telefone: {seen.get('passenger_name')} {seen.get('passenger_phone')}")
        time.sleep(3)
        msgs = sms_to(passenger)
        chk(msgs, "passageiro recebe SMS pelo stub")
        if msgs:
            text = msgs[-1]
            plate = sql(f"SELECT plate FROM vehicles WHERE provider_id='{driver['id']}'")
            chk(plate in text and "Motorista V-T4" in text, f"SMS com matricula e motorista: {plate} / {text[-120:]}")
        advance(driver, rid)
        status, payment = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"])
        owner = sql(f"SELECT customer_id FROM payments WHERE request_id='{rid}'")
        chk(payment.get("amount_cents") == est["total_cents"] and owner == customer["id"], f"cobranca no nome de quem pediu: {status} {owner}")
        wait_payment(customer, rid, "paid")
        go_offline(driver)


    with Scenario("V-T5", "Cancelamento: custo antes, motivos e pre-aviso de 2 min"):
        customer = new_user("customer", "Cliente V-T5")
        est = estimate(customer)
        status, created = create(customer, est)
        rid = created["id"]
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason": "texto livre"}, customer["tok"])
        chk(400 <= status < 500, f"texto livre nao substitui catalogo: {status} {code(res)}")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": str(uuid.uuid4())}, customer["tok"])
        chk(code(res) == "REASON_NOT_APPLICABLE", f"motivo inexistente: {status} {code(res)}")
        other = reason_id(customer["tok"], "customer", "taxi", "other")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": other}, customer["tok"])
        chk(status == 422, f"'Outro motivo' exige texto: {status} {code(res)}")
        status, preview = call("GET", f"/requests/{rid}/cancellation", None, customer["tok"])
        chk(status == 200 and preview.get("fee_cents") == 0, f"sem motorista o custo e 0: {preview.get('fee_cents')}")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": other, "note": "Mudei de ideias"}, customer["tok"])
        stored = sql(f"SELECT cancel_reason_id||'|'||cancel_reason FROM service_requests WHERE id='{rid}'")
        chk(status == 200 and stored.startswith(other) and "Mudei de ideias" in stored, f"motivo por ID + texto na BD: {stored}")
        rider, driver = new_user("customer", "Cliente V-T5b"), new_user("provider", "Motorista V-T5", "taxi")
        go_online(driver)
        _, created = create(rider, estimate(rider))
        rid = created["id"]
        accept(driver, wait_offer(driver, rid))
        _, preview = call("GET", f"/requests/{rid}/cancellation", None, rider["tok"])
        chk(preview.get("fee_cents") == 0 and preview.get("free_until"), f"primeiros 2 min gratis: {preview.get('fee_cents')} ate {preview.get('free_until')}")
        sql(f"UPDATE service_requests SET accepted_at=now()-interval '121 seconds' WHERE id='{rid}'")
        fee = int(sql("SELECT r.fee_cents FROM cancellation_rules r JOIN services s ON s.id=r.service_id WHERE s.slug='taxi'"))
        status, preview = call("GET", f"/requests/{rid}/cancellation", None, rider["tok"])
        chk(preview.get("fee_cents") == fee and fee > 0, f"depois de 2 min custo {preview.get('fee_cents')} = regra BD {fee}")
        reason = reason_id(rider["tok"], "customer", "taxi", "changed_plans")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": reason}, rider["tok"])
        chk(code(res) == "CANCEL_FEE_CHANGED", f"sem confirmar custo nao cancela: {status} {code(res)}")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": reason, "expected_fee_cents": fee}, rider["tok"])
        chk(status == 200 and res.get("cancel_fee_cents") == fee, f"com custo confirmado cancela: {status} {res.get('cancel_fee_cents')}")
        status, payment = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, rider["tok"])
        chk(status == 201 and payment.get("amount_cents") == fee, f"a divida e a taxa, nao a tarifa: {payment.get('amount_cents')}")
        wait_payment(rider, rid, "paid")
        go_offline(driver)


    with Scenario("V-T6", "Motorista recusa oferta com motivo"):
        customer, first, second = new_user("customer", "Cliente V-T6"), new_user("provider", "Motorista V-T6a", "taxi"), new_user("provider", "Motorista V-T6b", "taxi")
        go_online(first)
        go_online(second, {"lat": -8.8420, "lng": 13.2380, "accuracy_m": 10, "age_s": 0})
        _, created = create(customer, estimate(customer))
        rid = created["id"]
        offer = wait_offer(first, rid, 15)
        who, other = (first, second) if offer else (second, first)
        offer = offer or wait_offer(second, rid, 15)
        reason = reason_id(who["tok"], "offer", "taxi", "too_far", PROVIDER)
        status, res = call("POST", f"/offers/{offer['id']}/reject", {"reason_id": reason, "note": "Do outro lado"}, who["tok"], PROVIDER)
        stored = sql(f"SELECT reject_reason_id||'|'||reject_note FROM request_offers WHERE id='{offer['id']}'")
        chk(200 <= status < 300 and stored == f"{reason}|Do outro lado", f"motivo do motorista na oferta: {status} {stored}")
        chk(any(i["code"] == "too_far" for i in call("GET", "/admin/reports/cancellations?scope=offer_reject&category=taxi", None, admin, BACKOFFICE)[1].get("items", [])), "relatorio agrupa too_far")
        chk(wait_offer(other, rid, 30) is not None, "a oferta passa ao outro motorista")
        cancel(customer["tok"], rid)
        go_offline(first, second)


    with Scenario("V-T7", "Avaliacao (estrelas+elogios), media e gorjeta"):
        customer, driver, rid = completed_ride("cash")
        call("POST", f"/requests/{rid}/payments/cash", None, driver["tok"], PROVIDER)
        fare, commission = map(int, sql(f"SELECT fare_cents,commission_cents FROM service_requests WHERE id='{rid}'").split("|"))
        _, view = call("GET", f"/requests/{rid}", None, customer["tok"])
        bd = view.get("breakdown", {})
        chk(bd.get("total_cents") == fare and sum(i["amount_cents"] for i in bd.get("items", [])) == fare, f"resumo soma o total: {fare} {bd.get('items')}")
        status, options = call("GET", "/rating-options?category=taxi", None, customer["tok"])
        codes = {c["code"]: c["id"] for c in options.get("compliments", [])}
        chk(status == 200 and "clean_car" in codes and len(options.get("tips", [])) == 3, f"elogios/gorjetas do taxi: {list(codes)}")
        chk(call("POST", f"/requests/{rid}/rating", {"stars": 5}, new_user("customer", "Intruso V-T7")["tok"])[0] == 404, "outro cliente nao avalia")
        for bad in (0, 6, 99999999999):
            chk(call("POST", f"/requests/{rid}/rating", {"stars": bad}, customer["tok"])[0] == 422, f"{bad} estrelas recusadas")
        chk(call("POST", f"/requests/{rid}/rating", {}, customer["tok"])[0] == 422, "estrelas obrigatorias")
        status, res = call("POST", f"/requests/{rid}/rating", {"stars": 4, "compliment_ids": [codes["clean_car"], codes["safe_driving"]], "comment": "Boa"}, customer["tok"])
        chk(status == 201, f"avaliar 4 estrelas + 2 elogios: {status}")
        chk(code(call("POST", f"/requests/{rid}/rating", {"stars": 4}, customer["tok"])[1]) == "ALREADY_RATED", "nao avalia duas vezes")
        stats = sql(f"SELECT rating_sum||'|'||rating_count||'|'||completed_requests FROM providers WHERE user_id='{driver['id']}'")
        chk(stats == "4|1|1", f"media do motorista (soma|n|servicos): {stats}")
        active = new_user("customer", "Cliente V-T7b")
        _, created = create(active, estimate(active))
        chk(code(call("POST", f"/requests/{created['id']}/rating", {"stars": 5}, active["tok"])[1]) == "RATING_NOT_ALLOWED", "pedido nao concluido nao avalia")
        cancel(active["tok"], created["id"])
        status, res = call("POST", f"/requests/{rid}/tips", {"amount_cents": 100000000, "phone_number": SIMULATED_OK}, customer["tok"])
        chk(code(res) == "TIP_OUT_OF_RANGE", f"gorjeta acima do limite: {status} {code(res)}")
        status, tip = call("POST", f"/requests/{rid}/tips", {"amount_cents": 50000, "phone_number": SIMULATED_OK}, customer["tok"])
        chk(status == 201 and tip.get("amount_cents") == 50000, f"gorjeta 500 Kz: {status}")
        end = time.time() + 60
        while time.time() < end and sql(f"SELECT status FROM payments WHERE id='{tip['id']}'") != "paid":
            time.sleep(1)
        balance = int(sql(f"SELECT COALESCE(sum(amount_cents),0) FROM ledger_entries WHERE account_type='provider' AND account_id='{driver['id']}'"))
        chk(balance == 50000 - commission, f"gorjeta 100% para o motorista (ledger {balance})")


    with Scenario("V-T8", "Voucher 10% no total; expirado e de outro servico recusados"):
        now = dt.datetime.now(dt.timezone.utc)
        def voucher(**extra):
            value = "QA%06d" % random.randint(0, 999999)
            body = {"code": value, "discount_bps": 1000, "valid_from": (now - dt.timedelta(hours=1)).isoformat(), "valid_until": (now + dt.timedelta(hours=1)).isoformat(), "per_customer_limit": 1}
            body.update(extra)
            status, _ = call("POST", "/admin/vouchers", body, admin, BACKOFFICE, log=False)
            assert status == 201
            return value
        taxi_cat = sql("SELECT id FROM service_categories WHERE slug='taxi'")
        tow_cat = sql("SELECT id FROM service_categories WHERE slug='towing'")
        customer = new_user("customer", "Cliente V-T8")
        est = estimate(customer)
        fare = est["total_cents"]
        chk(code(create(customer, est, voucher_code="NAOEXISTE")[1]) == "VOUCHER_NOT_FOUND", "voucher inexistente nao e ignorado")
        expired = voucher(valid_from=(now - dt.timedelta(days=3)).isoformat(), valid_until=(now - dt.timedelta(days=1)).isoformat(), category_id=taxi_cat)
        chk(code(create(customer, est, voucher_code=expired)[1]) == "VOUCHER_EXPIRED", "voucher expirado recusado")
        wrong = voucher(category_id=tow_cat)
        chk(code(create(customer, est, voucher_code=wrong)[1]) == "VOUCHER_NOT_APPLICABLE", "voucher de outro servico recusado")
        good = voucher(category_id=taxi_cat)
        status, preview = call("POST", "/vouchers/preview", {"code": good, "fare_estimate_id": est["fare_estimate_id"]}, customer["tok"])
        chk(status == 200 and preview.get("discount_cents") == round(fare * 0.1), f"preview 10%: {preview.get('discount_cents')} de {fare}")
        status, created = create(customer, est, voucher_code=good, discount_cents=99999, payment_method="gpo")
        chk(status == 201 and created.get("discount_cents") == round(fare * 0.1) and created.get("amount_due_cents") == fare - round(fare * 0.1), f"desconto do servidor no total: {created.get('discount_cents')} / {created.get('amount_due_cents')}")
        chk(code(create(customer, estimate(customer, STOPS[:1]), STOPS[:1], voucher_code=good)[1]) == "VOUCHER_USED", "segunda utilizacao barrada")
        cancel(customer["tok"], created["id"])


# --------------------------------------------------------------------------
# Manutencao
# --------------------------------------------------------------------------
def maint(admin, partner):
    with Scenario("V-M1", "Lista, filtro por categoria, pesquisa, distancia e 'aberta ate'"):
        customer = new_user("customer", "Cliente V-M1")
        name = "Auto Verifica " + uuid.uuid4().hex[:6]
        status, workshop = call("POST", "/fleet/workshops", {"name": name, "currency": "AOA", "offers_home_service": True, "home_service_fee_cents": 300000, "address": {"lat": -8.9, "lng": 13.2, "line": "Rua 12, Talatona"}}, partner, PARTNER)
        chk(status == 201, f"oficina criada: {status}")
        wid = workshop["id"]
        tires = sql("SELECT id FROM catalog_items WHERE kind='workshop_category' AND code='tires'")
        for body in ({"name": "Revisao", "price_cents": 3500000, "duration_minutes": 120}, {"name": "Oleo", "price_cents": 1800000, "duration_minutes": 45, "category_id": tires}):
            assert call("POST", f"/fleet/workshops/{wid}/services", body, partner, PARTNER)[0] == 201
        assert call("PUT", f"/fleet/workshops/{wid}/schedule", {"capacity": 2, "about": "Desde 2010", "warranty": "3 meses", "hours": [{"weekday": d, "open_minute": 0, "close_minute": 1440} for d in range(7)]}, partner, PARTNER)[0] == 204
        q = name.replace(" ", "%20")
        ids = lambda r: [w["id"] for w in r.get("items", [])]
        chk(ids(call("GET", f"/workshops?category=tires&search={q}&lat=-8.9&lng=13.2", None, customer["tok"])[1]) == [wid], "categoria+pesquisa devolve a oficina")
        chk(ids(call("GET", f"/workshops?category=brakes&search={q}&lat=-8.9&lng=13.2", None, customer["tok"])[1]) == [], "outra categoria nao devolve")
        chk(ids(call("GET", f"/workshops?search={q}&lat=-8.0&lng=13.2&max_distance_km=5", None, customer["tok"])[1]) == [], "fora do raio nao devolve")
        card = call("GET", f"/workshops/{wid}?lat=-8.9&lng=13.2", None, customer["tok"])[1]
        chk(card.get("offers_home_service") is True and card.get("warranty") == "3 meses" and len(card.get("services", [])) == 2 and (card.get("is_open") is True or card.get("open_until")), f"cartao: domicilio={card.get('offers_home_service')} aberta={card.get('is_open')} ate={card.get('open_until')}")
        chk(call("POST", f"/workshops/{wid}/favorite", None, customer["tok"])[0] == 204, "favorito")
        fav = call("GET", "/workshops?favorites=true&lat=-8.9&lng=13.2", None, customer["tok"])[1]
        chk([w["id"] for w in fav.get("items", [])] == [wid] and fav["items"][0]["is_favorite"], "favoritos")


    with Scenario("V-M2", "2 servicos: duracao total, capacidade, dia fechado"):
        customer = new_user("customer", "Cliente V-M2")
        wid, first = workshop_fixture(partner, capacity=1)
        tires = sql("SELECT id FROM catalog_items WHERE kind='workshop_category' AND code='tires'")
        status, second = call("POST", f"/fleet/workshops/{wid}/services", {"name": "Alinhamento", "price_cents": 1000000, "duration_minutes": 45, "category_id": tires}, partner, PARTNER)
        chk(status == 201, f"segundo servico: {status}")
        day = next_weekday().isoformat()
        cal = call("GET", f"/workshops/{wid}/availability?service_ids={first},{second['id']}&from={day}", None, customer["tok"])[1]
        chk(cal.get("duration_minutes") == 165, f"duracao disponivel = 120+45 = 165: {cal.get('duration_minutes')}")
        slot = cal["days"][0]["slots"][0]["starts_at"]
        status, booked = book(customer, wid, [first, second["id"]], slot)
        chk(status == 201, f"marcar os 2 servicos: {status} {code(booked)}")
        got = sql(f"SELECT extract(epoch FROM ends_at-starts_at)/60 FROM bookings WHERE id='{booked['id']}'")
        chk(int(float(got)) == 165, f"a marcacao ocupa a duracao total (min): {got}")
        rival = new_user("customer", "Rival V-M2")
        status, res = book(rival, wid, [first], slot)
        chk(status == 409 and code(res) == "BOOKING_SLOT_FULL", f"capacidade 1: segundo no mesmo horario {status} {code(res)}")
        block = next_weekday(10)
        chk(call("POST", f"/fleet/workshops/{wid}/closures", {"date": block.isoformat(), "reason": "Feriado"}, partner, PARTNER)[0] == 204, "fechar dia")
        closed = call("GET", f"/workshops/{wid}/availability?service_ids={first}&from={block.isoformat()}", None, customer["tok"])[1]
        chk(closed["days"][0]["status"] == "closed", f"agenda mostra fechado: {closed['days'][0]['status']}")
        when = dt.datetime.combine(block, dt.time(10, 0), dt.timezone(dt.timedelta(hours=1))).isoformat()
        status, res = book(customer, wid, [first], when)
        chk(status == 422 and code(res) == "BOOKING_SLOT_INVALID", f"marcar em dia fechado recusado: {status} {code(res)}")
        chk(call("GET", f"/bookings/{booked['id']}", None, rival["tok"])[0] == 404, "outro cliente nao ve a marcacao")


    with Scenario("V-M3", "Ao domicilio: taxa e promocao 15% uma vez por cliente"):
        customer = new_user("customer", "Cliente V-M3")
        wid, sid = workshop_fixture(partner, capacity=3)
        now = dt.datetime.now(dt.timezone.utc)
        promo = {"description": "Primeira revisao -15%", "discount_bps": 1500, "valid_from": (now - dt.timedelta(days=1)).isoformat(), "valid_until": (now + dt.timedelta(days=30)).isoformat(), "per_customer_limit": 1, "is_public": True, "workshop_id": wid, "workshop_service_id": sid, "auto_apply": True}
        chk(call("POST", "/admin/vouchers", promo, admin, BACKOFFICE)[0] == 201, "promocao criada")
        card = call("GET", f"/workshops/{wid}?lat=-8.9&lng=13.2", None, customer["tok"])[1]
        chk([p["description"] for p in card.get("promotions", [])] == ["Primeira revisao -15%"], "promocao valida aparece")
        slot = slot_of(wid, sid, customer)
        status, booked = book(customer, wid, [sid], slot, at_home=True, home_lat=-8.84, home_lng=13.24, home_address="Casa")
        expected, discount = 3500000 + 300000, round((3500000 + 300000) * 0.15)
        chk(status == 201 and booked.get("home_fee_cents") == 300000 and booked.get("discount_cents") == discount and booked.get("total_cents") == expected - discount, f"taxa+desconto no total: {booked.get('home_fee_cents')} {booked.get('discount_cents')} {booked.get('total_cents')}")
        status, again = book(customer, wid, [sid], slot)
        chk(status == 201 and again.get("discount_cents") == 0, f"promocao so uma vez: {again.get('discount_cents')}")
        no_home, sid2 = workshop_fixture(partner, offers_home_service=False, home_service_fee_cents=0)
        status, res = book(new_user("customer", "Cliente V-M3b"), no_home, [sid2], slot_of(no_home, sid2, customer), at_home=True, home_lat=-8.84, home_lng=13.24)
        chk(code(res) == "HOME_SERVICE_UNAVAILABLE", f"oficina sem domicilio recusa: {status} {code(res)}")


    with Scenario("V-M4", "Fotos: 5 aceites, 6a recusada, tipo invalido"):
        customer = new_user("customer", "Cliente V-M4")
        wid, sid = workshop_fixture(partner, capacity=3)
        _, booked = book(customer, wid, [sid], slot_of(wid, sid, customer))
        rid = booked["request_id"]
        chk(code(call("POST", "/media/photos", {"content_type": "application/pdf", "size_bytes": 1000}, customer["tok"])[1]) == "PHOTO_TYPE_NOT_ALLOWED", "PDF recusado")
        ids = []
        for _ in range(6):
            status, photo = call("POST", "/media/photos", {"content_type": "image/jpeg", "size_bytes": 5}, customer["tok"], log=False)
            assert status == 201
            req = urllib.request.Request(photo["upload_url"], data=b"hello", method="PUT", headers={"Content-Type": "image/jpeg"})
            with urllib.request.urlopen(req, timeout=30) as sent:
                assert sent.status in (200, 204)
            ids.append(photo["id"])
        status, res = call("POST", f"/requests/{rid}/photos", {"photo_ids": ids[:5]}, customer["tok"])
        chk(status == 200 and len(res.get("items", [])) == 5, f"5 fotos anexadas: {status}")
        status, res = call("POST", f"/requests/{rid}/photos", {"photo_ids": ids[5:]}, customer["tok"])
        chk(code(res) == "PHOTO_LIMIT", f"6a foto recusada: {status} {code(res)}")


    with Scenario("V-M5", "Estados, orcamento extra aprovado/recusado"):
        customer = new_user("customer", "Cliente V-M5")
        wid, sid = workshop_fixture(partner, capacity=3)
        _, booked = book(customer, wid, [sid], slot_of(wid, sid, customer))
        bid, rid = booked["id"], booked["request_id"]
        move = lambda to: call("POST", f"/fleet/bookings/{bid}/status", {"status": to}, partner, PARTNER)
        chk(code(move("ready")[1]) == "BOOKING_STATE_INVALID", "salto de estado recusado")
        chk(code(call("POST", f"/fleet/bookings/{bid}/extras", {"description": "Cedo", "amount_cents": 100}, partner, PARTNER)[1]) == "BOOKING_STATE_INVALID", "extra antes de receber recusado")
        assert move("vehicle_received")[0] == 200
        chk(code(call("POST", f"/bookings/{bid}/cancel", {"reason_id": reason_id(customer["tok"], "customer", "maintenance")}, customer["tok"])[1]) == "BOOKING_NOT_CANCELLABLE", "com viatura na oficina nao cancela")
        assert move("in_service")[0] == 200
        _, a = call("POST", f"/fleet/bookings/{bid}/extras", {"description": "Pastilhas", "amount_cents": 1200000}, partner, PARTNER)
        _, b = call("POST", f"/fleet/bookings/{bid}/extras", {"description": "Filtro", "amount_cents": 500000}, partner, PARTNER)
        yes, no = a["extras"][0]["id"], b["extras"][1]["id"]
        chk(code(move("ready")[1]) == "BOOKING_EXTRAS_PENDING", "sem resposta nao fica pronta")
        chk(code(call("POST", f"/bookings/{bid}/approve", None, customer["tok"])[1]) == "BOOKING_EXTRAS_PENDING", "nem aprova")
        chk(call("POST", f"/bookings/{bid}/extras/{yes}/answer", {"approve": True}, new_user("customer", "Intruso V-M5")["tok"])[0] == 404, "outro cliente nao responde")
        view = call("POST", f"/bookings/{bid}/extras/{yes}/answer", {"approve": True}, customer["tok"])[1]
        exact("aprovado entra no total", 3500000 + 1200000, view.get("total_cents"))
        view = call("POST", f"/bookings/{bid}/extras/{no}/answer", {"approve": False}, customer["tok"])[1]
        exact("recusado nao entra no total", 3500000 + 1200000, view.get("total_cents"))
        chk(code(call("POST", f"/bookings/{bid}/extras/{no}/answer", {"approve": True}, customer["tok"])[1]) == "EXTRA_ALREADY_ANSWERED", "nao responde duas vezes")
        assert move("ready")[0] == 200
        seq = sql(f"SELECT string_agg(to_status, ',' ORDER BY id) FROM booking_events WHERE booking_id='{bid}'")
        chk(seq == "confirmed,vehicle_received,in_service,ready", f"historico de estados: {seq}")


    with Scenario("V-M6", "So cobra depois de aprovado"):
        customer = new_user("customer", "Cliente V-M6")
        wid, sid = workshop_fixture(partner, capacity=3)
        _, booked = book(customer, wid, [sid], slot_of(wid, sid, customer))
        bid, rid = booked["id"], booked["request_id"]
        status, res = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"])
        chk(status == 409 and code(res) == "REQUEST_NOT_PAYABLE", f"nao concluido nao paga: {status} {code(res)}")
        for to in ("vehicle_received", "in_service", "ready"):
            assert call("POST", f"/fleet/bookings/{bid}/status", {"status": to}, partner, PARTNER)[0] == 200
        status, res = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"])
        chk(status == 409 and code(res) == "REQUEST_NOT_PAYABLE", f"pronta sem aprovacao nao paga: {status} {code(res)}")
        chk(call("POST", f"/bookings/{bid}/approve", None, new_user("customer", "Intruso V-M6")["tok"])[0] == 404, "outro cliente nao aprova")
        status, view = call("POST", f"/bookings/{bid}/approve", None, customer["tok"])
        chk(status == 200 and view.get("status") == "completed" and view.get("amount_due_cents") == 3500000, f"aprovar conclui e deva 35 000 Kz: {status} {view.get('status')} {view.get('amount_due_cents')}")
        sql(f"UPDATE service_requests SET commission_cents=350000 WHERE id='{rid}'")
        status, payment = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK, "amount_cents": 1}, customer["tok"])
        chk(status == 201 and payment.get("amount_cents") == 3500000, f"cobra o valor do servidor, ignora amount_cents=1: {payment.get('amount_cents')}")
        wait_payment(customer, rid, "paid")
        balance = int(sql(f"SELECT COALESCE(sum(amount_cents),0) FROM ledger_entries WHERE account_type='workshop' AND account_id='{wid}'"))
        exact("ledger da oficina = total - comissao", 3500000 - 350000, balance)
        raw = fetch_raw(f"/requests/{rid}/receipt", customer["tok"])
        chk(raw[0] == 200 and raw[1].startswith(b"%PDF"), "recibo PDF")


# --------------------------------------------------------------------------
# Reboque
# --------------------------------------------------------------------------
def tow():
    with Scenario("V-R1", "Preco por tipo de viatura e km a mais (exacto da BD)"):
        customer = new_user("customer", "Cliente V-R1")
        titles = {"light": "Ligeiro", "suv_pickup": "SUV/Pick-up", "motorcycle": "Motociclo"}
        for code_, label in titles.items():
            item = tow_option(customer, code_)
            amount, included, extra = sql(f"SELECT amount_cents||'|'||(attrs->>'included_km')||'|'||(attrs->>'extra_km_cents') FROM catalog_items WHERE kind='towing_vehicle' AND code='{code_}'").split("|")
            over = max(0, item["distance_m"] - int(included) * 1000)
            want = int(amount) + round(over * int(extra) / 1000)
            exact(f"{label} ({item['distance_m']} m, incluidos {included} km)", want, item["total_cents"], 1)
            chk(item.get("vehicle_type") == label, f"tipo devolvido {item.get('vehicle_type')}")
        far = tow_option(customer, "light", {"lat": -8.9400, "lng": 13.1500})
        amount, included, extra = map(int, sql("SELECT amount_cents,(attrs->>'included_km')::int,(attrs->>'extra_km_cents')::int FROM catalog_items WHERE kind='towing_vehicle' AND code='light'").split("|"))
        over = max(0, far["distance_m"] - included * 1000)
        exact("km a mais cobrados ao centimo", amount + round(over * extra / 1000), far["total_cents"], 1)
        status, options = call("GET", f"/towing/options?lat={PICKUP['lat']}&lng={PICKUP['lng']}", None, customer["tok"])
        chk(status == 200 and options.get("incidents") and options.get("recommended_workshop", {}).get("id"), f"incidentes e oficina recomendada: {status} {options.get('recommended_workshop', {}).get('name')}")
        chk(code(create(customer, tow_option(customer, "light"))[1]) == "TOWING_DETAILS_REQUIRED", "o que aconteceu e obrigatorio")
        status, res = create(customer, tow_option(customer, "suv_pickup"), towing=towing_details(customer["tok"]))
        chk(status == 201 and res["details"]["towing"]["vehicle_type"] == "SUV/Pick-up" and res["details"]["towing"]["drivable"] is False, f"pedido guarda tipo/avaria/anda: {status}")
        cancel(customer["tok"], res["id"])


    with Scenario("V-R2", "Cancelar durante a procura e gratis"):
        customer = new_user("customer", "Cliente V-R2")
        _, created = create(customer, tow_option(customer), towing=towing_details(customer["tok"]))
        preview = call("GET", f"/requests/{created['id']}/cancellation", None, customer["tok"])[1]
        chk(preview.get("fee_cents") == 0, f"pre-aviso 0: {preview.get('fee_cents')}")
        status, res = call("POST", f"/requests/{created['id']}/cancel", {"reason_id": reason_id(customer["tok"], "customer", "towing", "solved")}, customer["tok"])
        chk(status == 200 and res.get("cancel_fee_cents") == 0, f"cancelado sem custo: {status} {res.get('cancel_fee_cents')}")


    with Scenario("V-R3", "Codigo de entrega de 4 digitos"):
        customer, driver = new_user("customer", "Cliente V-R3"), new_user("provider", "Reboque V-R3", "towing", kind="tow_truck")
        go_online(driver)
        _, created = create(customer, tow_option(customer), towing=towing_details(customer["tok"]))
        rid = created["id"]
        real = created.get("completion_code", "")
        chk(re.fullmatch(r"\d{4}", real) and sql(f"SELECT completion_code FROM service_requests WHERE id='{rid}'") == real, "codigo 4 digitos na BD")
        offer = wait_offer(driver, rid)
        chk("completion_code" not in json.dumps(offer), "motorista nao ve o codigo na oferta")
        accept(driver, offer)
        advance(driver, rid, ("en-route", "arrive", "start"))
        wrong = "0000" if real != "0000" else "1111"
        status, res = call("POST", f"/requests/{rid}/complete", {"code": wrong}, driver["tok"], PROVIDER)
        chk(code(res) == "COMPLETION_CODE_WRONG" and sql(f"SELECT status FROM service_requests WHERE id='{rid}'") == "in_progress", f"codigo errado recusa: {status} {code(res)}")
        status, res = call("POST", f"/requests/{rid}/complete", {"code": real}, driver["tok"], PROVIDER)
        chk(status == 200 and res.get("status") == "completed", f"codigo certo conclui: {status}")
        go_offline(driver)


# --------------------------------------------------------------------------
# Mudancas
# --------------------------------------------------------------------------
def mov():
    with Scenario("V-D1", "Volume vem dos artigos e viatura recomendada"):
        customer = new_user("customer", "Cliente V-D1")
        options = removal_options(customer)
        by = {i["code"]: i for i in options["items"]}
        chk(len(options["items"]) >= 10 and {i["group"] for i in options["items"]} >= {"Sala", "Quarto", "Cozinha"}, f"catalogo por divisao: {len(options['items'])}")
        items = [{"item_id": by["sofa"]["id"], "qty": 2}, {"item_id": by["bed"]["id"], "qty": 1}, {"item_id": by["wardrobe"]["id"], "qty": 2}, {"item_id": by["box"]["id"], "qty": 10}]
        volume = 2 * 1500 + 1600 + 2 * 1800 + 10 * 100
        window, when = window_at(options, "morning")
        status, quoted = call("POST", "/fare-estimates", {"category": "removals", "pickup": PICKUP, "destination": DEST, "scheduled_for": when, "removal": {"items": items, "helpers": 0, "extras": []}}, customer["tok"])
        chk(status == 201 and all(q["removal"]["volume_dm3"] == volume for q in quoted["items"]), f"volume no servidor {volume}: {[q['removal']['volume_dm3'] for q in quoted.get('items', [])]}")
        chk([q["vehicle_type"] for q in quoted["items"] if q["removal"]["recommended"]] == ["Camiao 3,5 t"] or [q["vehicle_type"] for q in quoted["items"] if q["removal"]["recommended"]] == ["Camião 3,5 t"], f"recomendada: {[q['vehicle_type'] for q in quoted['items'] if q['removal']['recommended']]}")
        chk("Carrinha" not in [q["vehicle_type"] for q in quoted["items"]], "carrinha nao leva 9,2 m3")
        est = next(q for q in quoted["items"] if q["removal"]["recommended"])
        status, created = removal_request(customer, est, window, when, details={"items": [{"name": "Sofa", "qty": 1}], "volume_m3": 999, "total_cents": 1})
        rm = created.get("details", {}).get("removal", {})
        chk(status == 201 and rm.get("volume_dm3") == volume and created["details"].get("volume_m3") != 999, f"app nao impoe volume/preco: volume {rm.get('volume_dm3')}, detalhe {created['details'].get('volume_m3')}")
        exact("preco fechado = estimativa", est["total_cents"], created.get("fare_cents"))
        chk(rm.get("duration_minutes") == 180 + 10 * volume // 1000 and rm.get("insurance_cents") == 50000000, f"duracao/seguro: {rm.get('duration_minutes')} {rm.get('insurance_cents')}")
        cancel(customer["tok"], created["id"])


    with Scenario("V-D2", "Total 85 000 + 2x5 000 + 15 000 = 110 000 Kz (exacto)"):
        customer = new_user("customer", "Cliente V-D2")
        status, res = call("POST", "/fare-estimates", {"category": "removals", "pickup": PICKUP, "destination": DEST, "removal": {"helpers": 2, "extras": ["packing"]}}, customer["tok"])
        chk(status == 201, f"estimativa: {status}")
        prices = {i["vehicle_type"]: i["removal"]["vehicle_cents"] for i in res["items"]}
        chk(prices == {"Carrinha": 4500000, "Camião 3,5 t": 8500000, "Camião 7 t": 14000000}, f"precos de catalogo: {prices}")
        extras = 2 * 500000 + 1500000
        got = {i["vehicle_type"]: i["total_cents"] for i in res["items"]}
        want = {name: price + extras for name, price in prices.items()}
        chk(got == want, f"total = viatura + 2x5 000 Kz + 15 000 Kz (extras {extras} cent): {got}")
        chk(got.get("Camião 3,5 t") == 11000000, f"o camiao 3,5 t soma 110 000 Kz: {got.get('Camião 3,5 t')}")
        chk(code(call("POST", "/fare-estimates", {"category": "removals", "pickup": PICKUP, "destination": DEST, "removal": {"helpers": 7}}, customer["tok"])[1]) == "REMOVAL_TOO_MANY_HELPERS", "maximo de ajudantes aplicado")
        chk(code(call("POST", "/fare-estimates", {"category": "removals", "pickup": PICKUP, "destination": DEST, "removal": {"extras": ["ouro"]}}, customer["tok"])[1]) == "REMOVAL_EXTRA_NOT_FOUND", "extra inexistente recusado")
        options = removal_options(customer)
        window, when = window_at(options, "midday", 11)
        est = removal_estimate(customer, when=when, helpers=2, extras=["packing"])
        status, created = removal_request(customer, est, window, when)
        chk(status == 201 and created.get("fare_cents") == 11000000, f"preco fechado na marcacao: {created.get('fare_cents')}")
        sql("UPDATE catalog_items SET amount_cents=amount_cents+100 WHERE kind='removal_vehicle' AND code='truck_3t'")
        view = call("GET", f"/requests/{created['id']}", None, customer["tok"])[1]
        sql("UPDATE catalog_items SET amount_cents=amount_cents-100 WHERE kind='removal_vehicle' AND code='truck_3t'")
        exact("mudar catalogo nao muda a marcacao", 11000000, view.get("fare_cents"))
        chk(call("POST", f"/requests/{created['id']}/route", {"destination": STOPS[0]}, customer["tok"])[0] >= 400, "preco fechado nao muda com rota")
        cancel(customer["tok"], created["id"])


    with Scenario("V-D3", "Janela/dia esgotado, 24 h e concorrencia"):
        customer = new_user("customer", "Cliente V-D3")
        options = removal_options(customer)
        luanda = dt.timezone(dt.timedelta(hours=1))
        now = dt.datetime.now(luanda)
        soon = soon_window = None
        for offset in (0, 1):
            for w in options["windows"]:
                minutes = {"morning": 480, "midday": 660, "afternoon": 840}[w["code"]]
                candidate = dt.datetime.combine(now.date() + dt.timedelta(days=offset), dt.time(minutes // 60, minutes % 60), luanda)
                if dt.timedelta(hours=1) < candidate - now < dt.timedelta(hours=23):
                    soon, soon_window = candidate.isoformat(), w
                    break
            if soon:
                break
        est = removal_estimate(customer, when=soon)
        status, created = removal_request(customer, est, soon_window, soon)
        chk(status == 201, f"marcar a menos de 24 h: {status}")
        preview = call("GET", f"/requests/{created['id']}/cancellation", None, customer["tok"])[1]
        chk(preview.get("fee_cents") == 1500000, f"a menos de 24 h taxa 15 000 Kz: {preview.get('fee_cents')}")
        status, res = call("POST", f"/requests/{created['id']}/cancel", {"reason_id": preview["reasons"][0]["id"]}, customer["tok"])
        chk(status == 409 and code(res) == "CANCEL_FEE_CHANGED", f"sem confirmar taxa nao cancela: {status} {code(res)}")
        status, res = cancel(customer["tok"], created["id"])
        chk(status == 200 and res.get("cancel_fee_cents") == 1500000, f"a menos de 24 h cobra: {res.get('cancel_fee_cents')}")
        far_customer = new_user("customer", "Cliente V-D3b")
        window, when = window_at(options, "morning", 20)
        _, far = removal_request(far_customer, removal_estimate(far_customer, when=when), window, when)
        preview = call("GET", f"/requests/{far['id']}/cancellation", None, far_customer["tok"])[1]
        chk(preview.get("fee_cents") == 0, f"a mais de 24 h gratis: {preview.get('fee_cents')}")
        _, wrong_when = window_at(options, "afternoon", 20)
        status, res = removal_request(far_customer, removal_estimate(far_customer, when=wrong_when), window, wrong_when)
        chk(code(res) == "REMOVAL_WINDOW_INVALID", f"hora fora da janela: {status} {code(res)}")
        cancel(far_customer["tok"], far["id"])

        truck = sql("SELECT id FROM catalog_items WHERE kind='removal_vehicle' AND code='truck_7t'")
        capacity = int(sql(f"SELECT attrs->>'daily_capacity' FROM catalog_items WHERE id='{truck}'"))
        window, when = window_at(options, "afternoon", 30)
        day = when[:10]
        rivals = [new_user("customer", f"Rival V-D3 {i}") for i in range(capacity + 3)]
        estimates = {r["id"]: removal_estimate(r, when=when, vehicle="Camião 7 t") for r in rivals}
        import concurrent.futures as cf
        with cf.ThreadPoolExecutor(len(rivals)) as pool:
            outcomes = list(pool.map(lambda r: removal_request(r, estimates[r["id"]], window, when), rivals))
        ok = [o for o in outcomes if o[0] == 201]
        full = [o for o in outcomes if code(o[1]) == "REMOVAL_DAY_FULL"]
        chk(len(ok) == capacity and len(full) == 3, f"capacidade diaria {capacity} sob concorrencia: {[o[0] for o in outcomes]}")
        cal = call("GET", f"/removals/availability?vehicle_id={truck}&from={day}", None, customer["tok"])[1]
        chk(cal["days"][0]["status"] == "full", f"disponibilidade mostra esgotado: {cal['days'][0]['status']}")
        for r, o in zip(rivals, outcomes):
            if o[0] == 201:
                cancel(r["tok"], o[1]["id"])
        cal = call("GET", f"/removals/availability?vehicle_id={truck}&from={day}", None, customer["tok"])[1]
        chk(cal["days"][0]["status"] == "free", f"cancelar liberta: {cal['days'][0]['status']}")


# --------------------------------------------------------------------------
# Pedidos / conta
# --------------------------------------------------------------------------
def orders():
    with Scenario("V-P1", "Separadores (Em curso/Historico/Marcados) e filtro por servico"):
        customer, driver, done = completed_ride("cash")
        call("POST", f"/requests/{done}/payments/cash", None, driver["tok"], PROVIDER)
        luanda = dt.timezone(dt.timedelta(hours=1))
        when = (dt.datetime.now(luanda) + dt.timedelta(days=2)).replace(minute=0, second=0, microsecond=0).isoformat()
        _, scheduled = create(customer, estimate(customer, scheduled_for=when), scheduled_for=when)
        _, towed = create(customer, tow_option(customer), towing=towing_details(customer["tok"]))
        w, mw = window_at(removal_options(customer), "morning", 3)
        _, moved = removal_request(customer, removal_estimate(customer, when=mw), w, mw)
        wid, sid = workshop_fixture(PARTNER_TOKEN[0], capacity=3)
        _, repaired = book(customer, wid, [sid], slot_of(wid, sid, customer))
        ids = lambda r: {i["id"] for i in r.get("items", [])}
        active = ids(call("GET", "/requests?status=created,searching,offered,awaiting_choice,accepted,provider_en_route,in_progress", None, customer["tok"])[1])
        history = ids(call("GET", "/requests?status=completed,cancelled,expired", None, customer["tok"])[1])
        marked = ids(call("GET", "/requests?status=scheduled", None, customer["tok"])[1])
        chk(towed["id"] in active and done not in active, f"Em curso inclui reboque: {len(active)}")
        view = call("GET", f"/requests/{moved['id']}", None, customer["tok"])[1]
        chk(moved["id"] in (marked if view.get("status") == "scheduled" else active), f"mudanca no separador do estado {view.get('status')}")
        chk(done in history and towed["id"] not in history, "Historico tem a concluida")
        chk(marked - {moved["id"]} == {scheduled["id"], repaired["request_id"]}, "Marcados junta corrida agendada e oficina")
        only = ids(call("GET", "/requests?category=towing", None, customer["tok"])[1])
        chk(only == {towed["id"]}, f"filtro por servico: {only}")
        for item in (scheduled, towed, moved):
            cancel(customer["tok"], item["id"])
        call("POST", f"/bookings/{repaired['id']}/cancel", {"reason_id": reason_id(customer["tok"], "customer", "maintenance")}, customer["tok"])


    with Scenario("V-P2", "Codigo, recibo e Repetir (requisito: cria pedido novo)"):
        customer, driver, rid = completed_ride("gpo")
        call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"])
        wait_payment(customer, rid, "paid")
        view = call("GET", f"/requests/{rid}", None, customer["tok"])[1]
        chk(re.fullmatch(r"TX-\d{5,}", view.get("code", "")), f"codigo legivel: {view.get('code')}")
        raw = fetch_raw(f"/requests/{rid}/receipt", customer["tok"])
        fare = view["breakdown"]["total_cents"]
        amount = f"{fare // 100:,}".replace(",", " ") + ",%02d Kz" % (fare % 100)
        chk(raw[0] == 200 and raw[1].startswith(b"%PDF") and amount.encode() in raw[1] and view["code"].encode() in raw[1], f"PDF com total {amount} e codigo")
        chk(call("GET", f"/requests/{rid}/receipt", None, new_user("customer", "Intruso V-P2")["tok"])[0] == 404, "outro cliente nao ve recibo")
        before = int(sql(f"SELECT count(*) FROM service_requests WHERE customer_id='{customer['id']}'"))
        status, preview = call("POST", f"/requests/{rid}/repeat", {"preview": True}, customer["tok"])
        chk(status == 201 and preview.get("estimates") and not preview.get("request") and int(sql(f"SELECT count(*) FROM service_requests WHERE customer_id='{customer['id']}'")) == before, f"Repetir em pre-visualizacao nao cria: {status}")
        status, repeated = call("POST", f"/requests/{rid}/repeat", {}, customer["tok"])
        after = int(sql(f"SELECT count(*) FROM service_requests WHERE customer_id='{customer['id']}'"))
        chk(status == 201 and repeated.get("estimates") and repeated.get("request", {}).get("id"), f"Repetir devolve o pedido novo e as estimativas: {status}")
        chk(after == before + 1, f"Repetir CRIA pedido novo com o mesmo trajeto (antes {before}, depois {after})")
        if after == before + 1:
            new_view = repeated["request"]
            same = (view["pickup"]["lat"] == new_view["pickup"]["lat"] and view["pickup"]["lng"] == new_view["pickup"]["lng"]
                    and view["destination"]["lat"] == new_view["destination"]["lat"] and view["destination"]["lng"] == new_view["destination"]["lng"]
                    and view["service_id"] == new_view["service_id"])
            chk(same, "pedido repetido mantem o trajeto e o servico")
            info("pedido repetido: " + new_view["id"])
        chk(call("POST", f"/requests/{rid}/tickets", {"subject": "Problema", "message": "Cobraram a mais"}, customer["tok"])[0] == 201, "problema ligado ao pedido")


    with Scenario("V-P3", "Notificacoes criadas, lidas e deep link"):
        customer, driver, rid = completed_ride("cash")
        deadline = time.time() + 20
        inbox = lambda tok=customer["tok"], q="": call("GET", "/notifications" + q, None, tok, log=False)[1]
        while time.time() < deadline and not any(i["title"] == "Servico concluido" or i["title"] == "Serviço concluído" for i in inbox().get("items", [])):
            time.sleep(0.5)
        items = inbox().get("items", [])
        titles = [i["title"] for i in items]
        done = next((i for i in items if i["title"] in ("Servico concluido", "Serviço concluído")), None)
        chk(done is not None and any("atribu" in t.lower() for t in titles), f"eventos geram notificacoes: {titles}")
        if done:
            chk(done.get("link_type") == "request" and done.get("link_id") == rid, f"deep link: {done.get('link_type')} {done.get('link_id')}")
            before = inbox()
            chk(call("POST", f"/notifications/{done['id']}/read", None, new_user("customer", "Intruso V-P3")["tok"])[0] == 404, "outro cliente nao marca")
            chk(call("POST", f"/notifications/{done['id']}/read", None, customer["tok"])[0] == 204, "marcar lida")
            chk(inbox()["unread"] == before["unread"] - 1, f"menos uma por ler: {inbox()['unread']}")
            call("POST", "/notifications/read-all", {}, customer["tok"])
            chk(inbox()["unread"] == 0 and inbox(q="?unread=true")["items"] == [], "todas lidas e filtro vazio")
        chk(any(i.get("link_type") == "offer" for i in call("GET", "/notifications", None, driver["tok"], PROVIDER, log=False)[1].get("items", [])), "motorista recebe oferta com deep link")


    with Scenario("V-P4", "Eliminar conta por SMS e preferencias persistidas"):
        customer = new_user("customer", "Cliente V-P4")
        chk(400 <= call("DELETE", "/users/me", {}, customer["tok"])[0] < 500, "sem confirmacao recusa")
        chk(call("POST", "/users/me/verification-code", {}, customer["tok"])[0] < 300, "pede codigo")
        time.sleep(1)
        real = otp_for(customer["phone"])
        chk(real is not None, "codigo sai por SMS (stub)")
        chk(400 <= call("DELETE", "/users/me", {"code": "000000" if real != "000000" else "111111"}, customer["tok"])[0] < 500, "codigo errado recusa")
        status, _ = call("DELETE", "/users/me", {"code": real}, customer["tok"])
        chk(status < 300 and sql(f"SELECT deleted_at IS NOT NULL FROM users WHERE id='{customer['id']}'") == "t", "conta eliminada (BD)")
        chk(call("GET", "/users/me", None, customer["tok"])[0] == 401, "sessao termina")
        other = new_user("customer", "Cliente V-P4b")
        prefs = {"notify_status": False, "notify_promotions": False, "notify_sms": False, "share_location": False, "language": "pt", "currency": "AOA"}
        status, _ = call("PUT", "/users/me/preferences", prefs, other["tok"])
        chk(status == 200 and sql(f"SELECT notify_sms FROM users WHERE id='{other['id']}'") == "f" and call("GET", "/users/me/preferences", None, other["tok"])[1].get("notify_status") is False, "toggles guardados e lidos")
        chk(call("PUT", "/users/me/preferences", dict(prefs, language="xx"), other["tok"])[0] >= 400, "idioma invalido recusado")


# --------------------------------------------------------------------------
# Seguranca
# --------------------------------------------------------------------------
def sec(admin, partner):
    with Scenario("V-S1", "IDOR entre clientes e motoristas"):
        owner, intruder = new_user("customer", "Dono V-S1"), new_user("customer", "Intruso V-S1")
        driver, stranger = new_user("provider", "Motorista V-S1", "taxi"), new_user("provider", "Estranho V-S1", "taxi")
        go_online(driver)
        _, created = create(owner, estimate(owner), payment_method="gpo")
        rid = created["id"]
        offer = wait_offer(driver, rid)
        chk(call("GET", f"/offers/{offer['id']}", None, stranger["tok"], PROVIDER)[0] == 404, "outro motorista nao ve oferta")
        chk(call("POST", f"/offers/{offer['id']}/accept", {"version": offer["request_version"]}, stranger["tok"], PROVIDER)[0] in (403, 404), "outro motorista nao aceita")
        accept(driver, offer)
        for label, method, path, body in (("ver", "GET", f"/requests/{rid}", None), ("cancelar", "POST", f"/requests/{rid}/cancel", {"reason_id": reason_id(owner["tok"], "customer", "taxi")}),
                                          ("rota", "POST", f"/requests/{rid}/route", {"destination": DEST}), ("boost", "POST", f"/requests/{rid}/boost", {"percent": 10}),
                                          ("metodo", "PUT", f"/requests/{rid}/payment-method", {"method": "cash"}), ("pagar", "POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK})):
            status, _ = call(method, path, body, intruder["tok"])
            chk(status == 404, f"cliente B {label}: {status}")
        for step in ("en-route", "arrive", "start", "complete", "payments/cash"):
            chk(call("POST", f"/requests/{rid}/{step}", None, stranger["tok"], PROVIDER)[0] == 404, f"motorista estranho {step}")
        chk(call("GET", "/admin/payments", None, owner["tok"])[0] == 403, "cliente nao acede a /admin/payments")
        chk(call("GET", "/admin/requests", None, partner, PARTNER)[0] == 403, "parceiro nao acede a /admin/requests")
        cancel(owner["tok"], rid)
        go_offline(driver)


    with Scenario("V-S2", "Motorista nao aprovado/suspenso/sem viatura nao opera"):
        draft = new_user("provider", "Rascunho V-S2")
        chk(go_online(draft)[0] == 403, "sem perfil aprovado nao fica online")
        customer, driver = new_user("customer", "Cliente V-S2"), new_user("provider", "Motorista V-S2", "taxi")
        go_online(driver)
        _, created = create(customer, estimate(customer))
        offer = wait_offer(driver, created["id"])
        chk(call("POST", f"/offers/{offer['id']}/accept", {"version": offer["request_version"]}, draft["tok"], PROVIDER)[0] in (403, 404), "nao aprovado nao aceita")
        sql(f"UPDATE providers SET status='suspended' WHERE user_id='{driver['id']}'")
        chk(accept(driver, offer)[0] in (403, 404, 409), "suspenso nao aceita")
        sql(f"UPDATE providers SET status='approved' WHERE user_id='{driver['id']}'")
        cancel(customer["tok"], created["id"])
        go_offline(driver)
        sql(f"UPDATE vehicles SET provider_id=NULL WHERE provider_id='{driver['id']}'")
        chk(go_online(driver)[0] == 403, "sem viatura nao fica online")


    with Scenario("V-S3", "Entradas invalidas nunca dao 5xx"):
        customer = new_user("customer", "Cliente V-S3")
        driver = new_user("provider", "Motorista V-S3", "taxi")
        missing = str(uuid.uuid4())
        huge = 99999999999999999999
        big32 = 99999999999
        service = sql("SELECT id FROM services WHERE slug='taxi'")
        category = sql("SELECT id FROM service_categories WHERE slug='taxi'")
        address = {"lat": -8.9, "lng": 13.2, "line": "Rua"}
        vehicle = {"kind": "van", "brand": "B", "model": "M", "year": 2020, "color": "C", "plate": "F-1", "seats": 3}
        cases = [
            ("POST", "/fare-estimates", {}, customer["tok"], CUSTOMER), ("POST", "/fare-estimates", {"category": "nada", "pickup": PICKUP, "destination": DEST}, customer["tok"], CUSTOMER),
            ("POST", "/requests", {}, customer["tok"], CUSTOMER), ("POST", "/requests", {"service_id": missing, "pickup": PICKUP}, customer["tok"], CUSTOMER),
            ("POST", "/requests", {"service_id": "x", "pickup": PICKUP}, customer["tok"], CUSTOMER), ("POST", "/requests", {"service_id": service, "pickup": PICKUP, "destination": DEST, "fare_estimate_id": missing}, customer["tok"], CUSTOMER),
            ("POST", "/requests", {"service_id": service, "pickup": PICKUP, "destination": DEST, "payment_method": "cartao"}, customer["tok"], CUSTOMER),
            ("GET", f"/requests/{missing}", None, customer["tok"], CUSTOMER), ("GET", "/requests/nao-e-uuid", None, customer["tok"], CUSTOMER), ("GET", "/requests?page=-1&size=99999999", None, customer["tok"], CUSTOMER),
            ("POST", f"/requests/{missing}/cancel", {"reason_id": missing}, customer["tok"], CUSTOMER), ("POST", f"/requests/{missing}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"], CUSTOMER),
            ("POST", f"/requests/{missing}/payments", {"method": "bitcoin"}, customer["tok"], CUSTOMER), ("POST", f"/requests/{missing}/payments/cash", None, driver["tok"], PROVIDER),
            ("POST", f"/offers/{missing}/accept", {"version": 1}, driver["tok"], PROVIDER), ("POST", f"/offers/{missing}/reject", None, driver["tok"], PROVIDER),
            ("POST", "/users/me/addresses", {}, customer["tok"], CUSTOMER), ("POST", "/users/me/addresses", {"kind": "home", "lat": 500, "lng": 500}, customer["tok"], CUSTOMER),
            ("POST", "/fleet/vehicles", {}, partner, PARTNER), ("POST", "/fleet/vehicles", dict(vehicle, seats=big32), partner, PARTNER),
            ("POST", "/fleet/vehicles", dict(vehicle, capacity_kg=-1, plate="F-4"), partner, PARTNER), ("POST", "/fleet/vehicles", dict(vehicle, kind="nave", plate="F-5"), partner, PARTNER),
            ("POST", "/fleet/drivers", {"name": "X", "phone": "abc", "service_id": service}, partner, PARTNER),
            ("POST", "/fleet/workshops", {}, partner, PARTNER), ("POST", "/fleet/workshops", {"name": "X", "currency": "AOA", "home_service_fee_cents": -5, "address": address}, partner, PARTNER),
            ("POST", "/fleet/workshops", {"name": "X", "currency": "AOA", "address": {"lat": 900, "lng": 13.2, "line": "R"}}, partner, PARTNER),
            ("POST", "/admin/rejection-reasons", {"scope": "nada", "code": "x", "label": "y"}, admin, BACKOFFICE),
            ("PUT", f"/admin/providers/{driver['id']}", {"status": "suspended", "reason_ids": [missing]}, admin, BACKOFFICE),
            ("GET", "/admin/requests?from=ontem", None, admin, BACKOFFICE), ("GET", "/admin/requests?status=%27%3B%20DROP%20TABLE%20users", None, admin, BACKOFFICE),
            ("PUT", "/admin/payment-methods", {"category_id": missing, "method": "gpo", "is_enabled": True}, admin, BACKOFFICE),
        ]
        fives, accepted = [], []
        for method, path, body, tok, client in cases:
            status, res = call(method, path, body, tok, client, idem=str(uuid.uuid4()) if path == "/requests" else None)
            if status >= 500:
                fives.append(f"{method} {path} -> {status} {code(res)}")
            elif status < 300:
                accepted.append(f"{method} {path} -> {status}")
        status, _ = call("POST", "/requests", None, customer["tok"], raw=b"{mal formado", idem=str(uuid.uuid4()))
        chk(status < 500, f"JSON malformado: {status}")
        chk(not fives, f"{len(cases)} casos: nenhum 5xx ({fives})")
        chk(not [a for a in accepted if "99999999999" in a or "-5" in a], f"sem aceitar valores absurdos/negativos: {accepted}")


    with Scenario("V-S4", "Preco e estado vem do servidor"):
        customer = new_user("customer", "Cliente V-S4")
        est = estimate(customer)
        status, created = create(customer, est, fare_cents=1, total_cents=1, commission_cents=0, status="completed", provider_id=str(uuid.uuid4()))
        chk(status == 201 and created.get("fare_cents") == est["total_cents"] and created.get("status") != "completed", f"payload nao impoe preco/estado: {created.get('fare_cents')} {created.get('status')}")
        cancel(customer["tok"], created["id"])


    with Scenario("V-S5", "Duplo clique/duplicados nao duplicam pedidos nem pagamentos"):
        customer = new_user("customer", "Cliente V-S5")
        est = estimate(customer)
        key = str(uuid.uuid4())
        import concurrent.futures as cf
        with cf.ThreadPoolExecutor(5) as pool:
            statuses = list(pool.map(lambda _: create(customer, est, idem=key)[0], range(5)))
        count = sql(f"SELECT count(*) FROM service_requests WHERE customer_id='{customer['id']}'")
        chk(count == "1", f"5 pedidos com a mesma Idempotency-Key -> {count} na BD ({statuses})")
        other = new_user("customer", "Cliente V-S5b")
        shared_estimate = estimate(other)
        with cf.ThreadPoolExecutor(3) as pool:
            statuses = list(pool.map(lambda _: create(other, shared_estimate, idem=str(uuid.uuid4()))[0], range(3)))
        count = sql(f"SELECT count(*) FROM service_requests WHERE customer_id='{other['id']}'")
        chk(statuses.count(201) == 1 and count == "1", f"1 estimativa com 3 chaves diferentes -> {count} pedido ({statuses})")
        payer, driver, rid = completed_ride("gpo")
        with cf.ThreadPoolExecutor(5) as pool:
            statuses = list(pool.map(lambda _: call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, payer["tok"], log=False)[0], range(5)))
        rows = sql(f"SELECT count(*) FROM payments WHERE request_id='{rid}'")
        chk(rows == "1", f"5 pagamentos simultaneos -> {rows} pagamento ({statuses})")


    with Scenario("V-S6", "Dois motoristas aceitam ao mesmo tempo"):
        customer, one, two = new_user("customer", "Cliente V-S6"), new_user("provider", "Motorista V-S6a", "towing", kind="tow_truck"), new_user("provider", "Motorista V-S6b", "towing", kind="tow_truck")
        go_online(one)
        go_online(two, {"lat": -8.8392, "lng": 13.2342, "accuracy_m": 10, "age_s": 0})
        _, created = create(customer, tow_option(customer), towing=towing_details(customer["tok"]))
        rid = created["id"]
        first, second = wait_offer(one, rid), wait_offer(two, rid)
        if not (first and second):
            Q.not_testable("o despacho nao ofereceu aos dois motoristas ao mesmo tempo")
        else:
            import concurrent.futures as cf
            with cf.ThreadPoolExecutor(2) as pool:
                statuses = sorted(pool.map(lambda pair: accept(*pair)[0], ((one, first), (two, second))))
            chk(statuses[0] == 200 and statuses[1] != 200, f"aceites simultaneos: {statuses}")
        cancel(customer["tok"], rid)
        go_offline(one, two)


# --------------------------------------------------------------------------
# Arranque / recolha
# --------------------------------------------------------------------------
GROUPS = {"docs": docs, "taxi": lambda: taxi(ADMIN[0]), "maint": lambda: maint(ADMIN[0], PARTNER_TOKEN[0]), "tow": tow,
          "mov": mov, "orders": orders, "sec": lambda: sec(ADMIN[0], PARTNER_TOKEN[0])}


def start():
    server = http.server.ThreadingHTTPServer(("127.0.0.1", STUB_PORT), Q.Stub)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    Q.compose("stop", "api")
    Q.neutralize_test_data()
    workdir = tempfile.mkdtemp()
    binary = os.path.join(workdir, "atlas-independent")
    subprocess.run(["go", "build", "-o", binary, "."], check=True, stdin=subprocess.DEVNULL)
    env = dict(os.environ, CASTBRICK_BASE_URL=f"http://127.0.0.1:{STUB_PORT}", RESEND_BASE_URL=f"http://127.0.0.1:{STUB_PORT}",
               RATE_LIMIT_REQUESTS="1000000", AUTH_RATE_LIMIT_REQUESTS="1000000", OTP_RESEND_COOLDOWN="0s", LOG_LEVEL="warn")
    log_dir = os.path.join("tests", "e2e", "evidence")
    os.makedirs(log_dir, exist_ok=True)
    api = subprocess.Popen([binary], env=env, stdout=open(os.path.join(log_dir, "independent-api.log"), "w"), stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL)
    for _ in range(60):
        try:
            urllib.request.urlopen(BASE + "/ready", timeout=2)
            break
        except OSError:
            time.sleep(1)
    global OPENAPI
    with urllib.request.urlopen(BASE + "/docs/openapi.json") as res:
        OPENAPI = list(json.load(res)["paths"])
    Q.OPENAPI = OPENAPI
    ADMIN[0], PARTNER_TOKEN[0] = staff("admin@agora.ao", BACKOFFICE), staff("parceiro@agora.ao", PARTNER)
    return api, server


def finish(api, server):
    Q.neutralize_test_data()
    drained = Q.wait_for_drain()
    api.terminate()
    api.wait(timeout=30)
    server.shutdown()
    stamp = time.strftime("%Y%m%d-%H%M%S")
    data = {sid: {"title": r["title"], "result": r["result"], "note": r["note"],
                  "checks": [{"ok": ok, "msg": msg} for ok, msg in r["checks"]], "steps": r["steps"]} for sid, r in Q.results.items()}
    path = os.path.join("tests", "e2e", "evidence", f"independent-{stamp}.json")
    with open(path, "w", encoding="utf-8") as handle:
        json.dump({"sms": Q.sms_log, "results": data}, handle, ensure_ascii=False, indent=1)
    if drained:
        Q.compose("up", "-d", "api")
    else:
        print("AVISO: eventos por tratar; a API do Docker fica parada para nao enviar SMS reais.", flush=True)
    tally = {}
    for sid, result in Q.results.items():
        tally[result["result"]] = tally.get(result["result"], 0) + 1
        print(f"{sid}\t{result['result']}\t{result['title']}")
        for ok, msg in result["checks"]:
            if not ok:
                print(f"    FALHOU: {msg}")
    print("\nRESUMO:", tally, f"evidencia {path}")


def main():
    api, server = start()
    try:
        for name in sys.argv[1:] or list(GROUPS):
            GROUPS[name]()
    finally:
        finish(api, server)


if __name__ == "__main__":
    main()
