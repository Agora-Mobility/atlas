#!/usr/bin/env python3
"""Verificacao de QA dos fluxos das telas contra a API real, a BD e a AppyPay de teste.

Corre a partir da raiz do atlas com a stack levantada: python3 tests/e2e/qa_flows.py [grupo ...]
Grupos: taxi maintenance towing removals orders payments backoffice account overflow security.
Para a API do Docker durante a execucao e arranca uma local com os SMS e e-mails
apontados para um recetor local, para nunca enviar mensagens reais. Repoe a API no fim.
"""
import base64, concurrent.futures as cf, datetime as dt, hashlib, hmac, http.server, json, os, random, re, struct, subprocess, sys, tempfile, threading, time, traceback, urllib.error, urllib.request, uuid

BASE = "http://localhost:8700"
STUB_PORT = 8799
CUSTOMER, PROVIDER, BACKOFFICE, PARTNER = "agora-customer-app", "agora-provider-app", "agora-backoffice", "agora-partner-panel"
PICKUP, DEST = {"lat": -8.8390, "lng": 13.2340}, {"lat": -8.8150, "lng": 13.2300}
STOPS = [{"lat": -8.8250, "lng": 13.2450}, {"lat": -8.8300, "lng": 13.2200}, {"lat": -8.8200, "lng": 13.2500}, {"lat": -8.8350, "lng": 13.2500}]
DRIVER_AT = {"lat": -8.8391, "lng": 13.2341, "accuracy_m": 10, "age_s": 0}
PARTNER_ORG = "00000000-0000-0000-0000-0000000000b1"
PASSWORD = "Agora@2026"
SIMULATED_OK, SIMULATED_REJECT = "900000000", "900000003"

sms_log, sms_lock = [], threading.Lock()
results, current = {}, None


class Stub(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0))).decode()
        with sms_lock:
            sms_log.append(body)
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"messageId":"qa","status":"queued","recipientCount":1,"id":"qa"}')

    def log_message(self, *_):
        pass


def sms_to(phone):
    digits = phone.lstrip("+")
    with sms_lock:
        return [b for b in sms_log if digits in b]


def otp_for(phone):
    for body in reversed(sms_to(phone)):
        found = re.search(r"\b\d{6}\b", body)
        if found:
            return found.group()
    return None


def sql(query):
    out = subprocess.run(["docker", "compose", "exec", "-T", "postgres", "psql", "-U", "atlas", "-d", "atlas", "-At", "-c", "SET search_path=atlas;" + query],
                         capture_output=True, text=True, stdin=subprocess.DEVNULL)
    if out.returncode or "ERROR" in out.stderr:
        raise RuntimeError(out.stderr[:300])
    lines = [l for l in out.stdout.splitlines() if l != "SET"]
    return "\n".join(lines)


def call(method, path, body=None, tok=None, client=CUSTOMER, idem=None, raw=None, log=True):
    headers = {"X-Client-Id": client, "Content-Type": "application/json"}
    if tok:
        headers["Authorization"] = "Bearer " + tok
    if idem:
        headers["Idempotency-Key"] = idem
    data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
    req = urllib.request.Request(BASE + "/api/v1" + path, method=method, headers=headers, data=data)
    try:
        with urllib.request.urlopen(req, timeout=120) as res:
            status, text = res.status, res.read().decode()
    except urllib.error.HTTPError as err:
        status, text = err.code, err.read().decode()
    try:
        parsed = json.loads(text) if text else {}
    except ValueError:
        parsed = {"raw": text[:200]}
    if log and current:
        current["steps"].append({"req": f"{method} {path}", "body": json.loads(redact(json.dumps(body))), "status": status, "res": redact(text)[:600]})
    return status, parsed


SECRET_FIELDS = re.compile(r'"(access_token|refresh_token|mfa_token|password|new_password|secret)"\s*:\s*"[^"]*"')


def redact(text):
    return SECRET_FIELDS.sub(lambda m: f'"{m.group(1)}":"***"', text)


def code(res):
    return (res.get("error") or {}).get("code") if isinstance(res, dict) else None


class Scenario:
    def __init__(self, sid, title):
        self.sid, self.title = sid, title

    def __enter__(self):
        global current
        current = {"title": self.title, "checks": [], "steps": [], "result": None, "note": ""}
        results[self.sid] = current
        print(f"\n=== {self.sid} {self.title}", flush=True)
        return self

    def __exit__(self, kind, value, tb):
        global current
        if kind:
            current["checks"].append((False, f"EXCECAO {kind.__name__}: {value}"))
            print(traceback.format_exc()[-500:], flush=True)
        if current["result"] is None:
            current["result"] = "PASSOU" if current["checks"] and all(ok for ok, _ in current["checks"]) else "FALHOU"
        print("  ->", current["result"], flush=True)
        current = None
        return True


def chk(ok, message):
    current["checks"].append((bool(ok), message))
    print("  ", "OK " if ok else "XX ", message, flush=True)
    return ok


def info(message):
    current["checks"].append((True, "info: " + message))
    print("   .. ", message, flush=True)


def not_testable(reason):
    current["result"] = "NÃO TESTÁVEL"
    current["note"] = reason


def random_phone():
    return "+24492%07d" % random.randint(0, 9_999_999)


def new_user(role, name, approved_service=None, seats=5, kind="car", plate=None):
    uid, phone = str(uuid.uuid4()), random_phone()
    sql(f"INSERT INTO users (id,name,phone,password,is_verified,status,role) SELECT '{uid}','{name}','{phone}',password,true,'active','{role}' FROM users WHERE id='00000000-0000-0000-0000-0000000000a3'")
    client = PROVIDER if role == "provider" else CUSTOMER
    status, res = call("POST", "/auth/login", {"identifier": phone, "password": PASSWORD}, client=client, log=False)
    assert status == 200, res
    if approved_service:
        sql(f"INSERT INTO providers (user_id,status,service_id) SELECT '{uid}','approved',id FROM services WHERE slug='{approved_service}'")
        sql(f"INSERT INTO vehicles (provider_id,kind,brand,model,year,color,plate,seats) VALUES ('{uid}','{kind}','Toyota','Corolla',2020,'Branco','{plate or 'QA-' + phone[-6:]}',{seats})")
    return {"id": uid, "phone": phone, "tok": res["access_token"], "client": client, "name": name}


def staff(email, client):
    sql(f"DELETE FROM user_mfa WHERE user_id=(SELECT id FROM users WHERE email='{email}')")
    status, res = call("POST", "/auth/login", {"identifier": email, "password": PASSWORD}, client=client, log=False)
    assert status == 202, res
    status, setup = call("POST", "/auth/mfa/setup", {"mfa_token": res["mfa_token"]}, client=client, log=False)
    assert status == 200, setup
    key = base64.b32decode(setup["secret"] + "=" * (-len(setup["secret"]) % 8))
    digest = hmac.new(key, struct.pack(">Q", int(time.time()) // 30), hashlib.sha1).digest()
    offset = digest[-1] & 15
    totp = "%06d" % ((struct.unpack(">I", digest[offset:offset + 4])[0] & 0x7fffffff) % 1000000)
    status, verified = call("POST", "/auth/mfa/verify", {"mfa_token": res["mfa_token"], "code": totp}, client=client, log=False)
    assert status == 200, verified
    return verified["access_token"]


def go_online(driver, position=DRIVER_AT):
    return call("POST", "/providers/me/online", {"position": position}, driver["tok"], PROVIDER)


def go_offline(*drivers):
    for driver in drivers:
        call("POST", "/providers/me/offline", None, driver["tok"], PROVIDER, log=False)


def estimate(customer, stops=None, category="taxi", slug=None, dest=DEST, scheduled_for=None):
    body = {"category": category, "pickup": PICKUP, "destination": dest}
    if stops is not None:
        body["stops"] = stops
    if scheduled_for:
        body["scheduled_for"] = scheduled_for
    status, res = call("POST", "/fare-estimates", body, customer["tok"])
    assert status == 201, res
    items = res["items"]
    return next(i for i in items if i["service_slug"] == (slug or items[0]["service_slug"]))


def create(customer, est, stops=None, idem=None, **extra):
    body = {"service_id": est["service_id"], "pickup": PICKUP, "destination": DEST, "fare_estimate_id": est["fare_estimate_id"]}
    if stops:
        body["stops"] = stops
    body.update(extra)
    return call("POST", "/requests", body, customer["tok"], idem=idem or str(uuid.uuid4()))


def wait_offer(driver, rid, seconds=40):
    end = time.time() + seconds
    while time.time() < end:
        _, state = call("GET", "/requests/active", None, driver["tok"], PROVIDER, log=False)
        for offer in state.get("offers", []):
            if offer["request_id"] == rid:
                return offer
        time.sleep(0.5)
    return None


def accept(driver, offer):
    return call("POST", f"/offers/{offer['id']}/accept", {"version": offer["request_version"]}, driver["tok"], PROVIDER)


def advance(driver, rid, steps=("en-route", "arrive", "start", "complete")):
    for step in steps:
        status, res = call("POST", f"/requests/{rid}/{step}", None, driver["tok"], PROVIDER)
        assert status == 200, (step, res)


def completed_ride(method="cash", service="taxi", category="taxi"):
    customer, driver = new_user("customer", "Cliente QA"), new_user("provider", "Motorista QA", service)
    go_online(driver)
    est = estimate(customer, category=category, slug=service)
    status, created = create(customer, est, payment_method=method)
    assert status == 201, created
    offer = wait_offer(driver, created["id"])
    assert offer, "sem oferta"
    status, res = accept(driver, offer)
    assert status == 200, res
    advance(driver, created["id"])
    go_offline(driver)
    return customer, driver, created["id"]


def wait_payment(customer, rid, want, seconds=60):
    end = time.time() + seconds
    while time.time() < end:
        _, view = call("GET", f"/requests/{rid}", None, customer["tok"], log=False)
        if view.get("payment_status") == want:
            return True
        time.sleep(1)
    return False


def route_missing(*paths):
    with open_api_lock:
        documented = OPENAPI
    return all(not any(p in known for known in documented) for p in paths)


open_api_lock, OPENAPI = threading.Lock(), []


def expected_fare(slug, distance, seconds, stops=0):
    row = sql(f"SELECT t.base_fee_cents,t.per_km_fee_cents,t.per_minute_fee_cents,t.minimum_fee_cents,t.zone_fee_cents,t.night_fee_cents,t.stop_fee_cents FROM service_tariffs t JOIN services s ON s.id=t.service_id WHERE s.slug='{slug}' AND t.valid_until IS NULL LIMIT 1").split("|")
    base, km, minute, minimum, zone, night, stop_fee = map(int, row)
    fare = base + round(distance * km / 1000) + round(seconds * minute / 60)
    return max(fare, minimum) + zone + stops * stop_fee, (base, km, minute, minimum, zone, night, stop_fee)


def taxi():
    with Scenario("T1", "Estimativa sem paragens e com 2 paragens"):
        customer = new_user("customer", "Cliente T1")
        plain, stopped = estimate(customer), estimate(customer, STOPS[:2])
        chk(stopped["total_cents"] != plain["total_cents"], f"preço muda: {plain['total_cents']} -> {stopped['total_cents']} cêntimos")
        chk(stopped["distance_m"] > plain["distance_m"], f"distância: {plain['distance_m']} -> {stopped['distance_m']} m")
        for label, item, count in (("sem", plain, 0), ("com", stopped, 2)):
            want, tariff = expected_fare(item["service_slug"], item["distance_m"], item["duration_s"], count)
            chk(abs(want - item["total_cents"]) <= 1, f"{label} paragens: tarifa {tariff} com {item['distance_m']} m e {item['duration_s']} s dá {want}; API {item['total_cents']}")
        status, created = create(customer, stopped, STOPS[:2])
        chk(status == 201 and created.get("fare_cents") == stopped["total_cents"], f"pedido com a estimativa certa fica a {created.get('fare_cents')}")
        call("POST", f"/requests/{created.get('id')}/cancel", {"reason": "QA"}, customer["tok"])
        other = new_user("customer", "Cliente T1b")
        cheap = estimate(other)
        status, wrong = create(other, cheap, STOPS[:2])
        chk(status != 201, f"estimativa sem paragens não pode servir a um pedido com 2 paragens: status {status}, fare {wrong.get('fare_cents')} vs. correto {stopped['total_cents']}")
        if status == 201:
            call("POST", f"/requests/{wrong['id']}/cancel", {"reason": "QA"}, other["tok"])

    with Scenario("T2", "Remover paragem recalcula; limite de paragens"):
        customer = new_user("customer", "Cliente T2")
        est = estimate(customer, STOPS[:2])
        status, created = create(customer, est, STOPS[:2])
        rid = created["id"]
        status, changed = call("POST", f"/requests/{rid}/route", {"destination": DEST, "stops": STOPS[:1]}, customer["tok"])
        one = estimate(new_user("customer", "Cliente T2b"), STOPS[:1])
        chk(status == 200 and abs(changed.get("fare_cents", 0) - one["total_cents"]) <= 2000, f"remover 1 paragem: {est['total_cents']} -> {changed.get('fare_cents')}; estimativa com 1 paragem {one['total_cents']}")
        chk(len(changed.get("stops", [])) == 1, "fica com 1 paragem")
        info("BD adjustments: " + sql(f"SELECT reason,amount_cents FROM fare_adjustments WHERE request_id='{rid}'").replace("\n", "; "))
        status, res = call("POST", f"/requests/{rid}/route", {"destination": DEST, "stops": STOPS[:4]}, customer["tok"])
        chk(400 <= status < 500, f"4 paragens na rota: {status} {code(res)}")
        status, res = call("POST", "/fare-estimates", {"category": "taxi", "pickup": PICKUP, "destination": DEST, "stops": STOPS[:4]}, customer["tok"])
        chk(400 <= status < 500, f"4 paragens na estimativa: {status} {code(res)}")
        status, res = create(customer, est, STOPS[:4])
        chk(400 <= status < 500, f"4 paragens no pedido: {status} {code(res)}")
        call("POST", f"/requests/{rid}/cancel", {"reason": "QA"}, customer["tok"])
        count = sql("SELECT count(*) FROM information_schema.columns WHERE table_schema='atlas' AND column_name ILIKE '%max_stops%'")
        chk(int(count) > 0, f"limite configurável na BD: colunas max_stops={count} (o limite é a constante maxStops)")

    with Scenario("T3", "Agendamento e despacho 30 min antes"):
        customer, driver = new_user("customer", "Cliente T3"), new_user("provider", "Motorista T3", "taxi")
        luanda = dt.timezone(dt.timedelta(hours=1))
        tomorrow = (dt.datetime.now(luanda) + dt.timedelta(days=1)).replace(hour=7, minute=30, second=0, microsecond=0)
        est = estimate(customer, scheduled_for=tomorrow.isoformat())
        status, created = create(customer, est, scheduled_for=tomorrow.isoformat())
        chk(status == 201 and created.get("status") == "scheduled", f"amanhã 07:30: {status} {created.get('status')}")
        rid = created["id"]
        next_at = sql(f"SELECT next_action_at AT TIME ZONE 'UTC', scheduled_for AT TIME ZONE 'UTC', extract(epoch from scheduled_for-next_action_at) FROM service_requests WHERE id='{rid}'")
        chk(next_at.endswith("|1800") or next_at.split("|")[-1].startswith("1800"), f"job marcado 30 min antes (BD): {next_at}")
        for label, when in (("8 dias", dt.datetime.now(luanda).replace(minute=0, second=0, microsecond=0) + dt.timedelta(days=8)), ("passado", dt.datetime.now(luanda).replace(minute=0, second=0, microsecond=0) - dt.timedelta(hours=1)),
                            ("fora dos intervalos de 15 min (07:37)", tomorrow.replace(minute=37))):
            status, res = call("POST", "/fare-estimates", {"category": "taxi", "pickup": PICKUP, "destination": DEST, "scheduled_for": when.isoformat()}, customer["tok"])
            chk(status == 422, f"estimativa a {label}: {status} {code(res)}")
            status, res = create(customer, est, scheduled_for=when.isoformat())
            chk(400 <= status < 500, f"pedido a {label}: {status} {code(res)}")
        other_hour = estimate(customer, scheduled_for=tomorrow.replace(hour=23).isoformat())
        status, res = create(customer, other_hour, scheduled_for=tomorrow.isoformat())
        chk(code(res) == "FARE_ESTIMATE_INVALID", f"estimativa de outra hora não serve: {status} {code(res)}")
        go_online(driver)
        sql(f"UPDATE service_requests SET next_action_at=now() WHERE id='{rid}'")
        offer = wait_offer(driver, rid, 30)
        chk(offer is not None, "ao simular a hora, o watcher despacha e o motorista recebe a oferta")
        _, view = call("GET", f"/requests/{rid}", None, customer["tok"])
        info(f"estado depois do despacho: {view.get('status')}")
        status, cancelled = call("POST", f"/requests/{rid}/cancel", {"reason": "Mudei de planos"}, customer["tok"])
        chk(status == 200, f"cancelar marcada: {status}")
        go_offline(driver)

    with Scenario("T4", "Pedir para outra pessoa"):
        customer, driver = new_user("customer", "Cliente T4"), new_user("provider", "Motorista T4", "taxi")
        passenger = random_phone()
        go_online(driver)
        est = estimate(customer)
        status, created = create(customer, est, passenger={"name": "Maria Neto", "phone": passenger}, payment_method="gpo")
        rid = created["id"]
        offer = wait_offer(driver, rid)
        accept(driver, offer)
        _, seen = call("GET", f"/requests/{rid}", None, driver["tok"], PROVIDER)
        chk(seen.get("passenger_name") == "Maria Neto", f"motorista vê o nome: {seen.get('passenger_name')}")
        chk(seen.get("passenger_phone") == passenger, f"motorista vê o telemóvel do passageiro: passenger_phone={seen.get('passenger_phone')!r}")
        time.sleep(3)
        messages = sms_to(passenger)
        chk(len(messages) >= 1, f"SMS ao passageiro: {len(messages)} mensagem(ns)")
        if messages:
            text = messages[-1]
            info("texto: " + text[:220])
            plate = sql(f"SELECT plate FROM vehicles WHERE provider_id='{driver['id']}'")
            chk(plate in text and "Motorista T4" in text, "o SMS traz o motorista e a matrícula")
            chk("táxi" in text, "o SMS usa o texto da categoria")
        advance(driver, rid)
        status, payment = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"])
        owner = sql(f"SELECT customer_id FROM payments WHERE request_id='{rid}'")
        chk(owner == customer["id"], "a cobrança fica em quem pediu (BD)")
        wait_payment(customer, rid, "paid")
        go_offline(driver)

    with Scenario("T5", "Cancelar: custo, motivo e pré-aviso"):
        customer = new_user("customer", "Cliente T5")
        est = estimate(customer)
        status, created = create(customer, est)
        rid = created["id"]
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason": ""}, customer["tok"])
        chk(400 <= status < 500, f"sem texto: {status} {code(res)}")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason": "O motorista está a demorar"}, customer["tok"])
        chk(status == 200, f"sem motorista: {status}")
        chk("cancel_fee_cents" in res or "fee_cents" in res, "a resposta indica o custo (0)")
        chk(sql(f"SELECT cancel_reason FROM service_requests WHERE id='{rid}'") == "O motorista está a demorar", "motivo guardado (BD)")
        status, _ = call("GET", "/cancellation-reasons", None, customer["tok"])
        chk(status == 200, f"catálogo de motivos: {status}")
        rider, driver = new_user("customer", "Cliente T5b"), new_user("provider", "Motorista T5", "taxi")
        go_online(driver)
        status, created = create(rider, estimate(rider))
        offer = wait_offer(driver, created["id"])
        accept(driver, offer)
        documented = [p for p in OPENAPI if "cancel" in p and "requests" in p and "admin" not in p]
        chk(any("preview" in p or "fee" in p for p in documented), f"pré-aviso de custo antes de confirmar; rotas documentadas: {documented}")
        status, res = call("POST", f"/requests/{created['id']}/cancel", {"reason": "qualquer texto fora do catálogo"}, rider["tok"])
        chk(status == 200 and ("cancel_fee_cents" in res or "fee_cents" in res), f"com motorista aplica a regra de custo: {status}, sem campo de custo")
        status, res2 = call("POST", f"/requests/{created['id']}/cancel", {"reason": "de novo"}, rider["tok"])
        info(f"cancelar duas vezes: {res2 and status} {code(res2)}")
        go_offline(driver)

    with Scenario("T6", "Motorista recusa oferta com motivo"):
        customer, first, second = new_user("customer", "Cliente T6"), new_user("provider", "Motorista T6a", "taxi"), new_user("provider", "Motorista T6b", "taxi")
        go_online(first)
        go_online(second, {"lat": -8.8420, "lng": 13.2380, "accuracy_m": 10, "age_s": 0})
        status, created = create(customer, estimate(customer))
        rid = created["id"]
        offer = wait_offer(first, rid, 15)
        who, other = (first, second) if offer else (second, first)
        offer = offer or wait_offer(second, rid, 15)
        status, res = call("POST", f"/offers/{offer['id']}/reject", {"reason": "Muito longe", "reason_id": str(uuid.uuid4())}, who["tok"], PROVIDER)
        chk(200 <= status < 300, f"recusar: {status}")
        columns = sql("SELECT string_agg(column_name,',') FROM information_schema.columns WHERE table_schema='atlas' AND table_name='request_offers'")
        stored = "Muito longe" in sql(f"SELECT row_to_json(o) FROM request_offers o WHERE id='{offer['id']}'")
        chk(stored, f"motivo registado na oferta (BD); colunas: {columns}")
        again = wait_offer(other, rid, 30)
        chk(again is not None, "a corrida é oferecida ao outro motorista")
        call("POST", f"/requests/{rid}/cancel", {"reason": "QA"}, customer["tok"])
        go_offline(first, second)

    with Scenario("T7", "Avaliação, gorjeta e média"):
        customer, driver, rid = completed_ride()
        body = {"stars": 4, "compliments": ["Condução segura", "Viatura limpa"], "tip_cents": 50000}
        status, res = call("POST", f"/requests/{rid}/rating", body, customer["tok"])
        chk(status in (200, 201), f"avaliar: {status}")
        status, res = call("POST", f"/requests/{rid}/ratings", body, customer["tok"])
        chk(status in (200, 201), f"rota alternativa /ratings: {status}")
        chk(any("rating" in p or "review" in p and "provider" not in p for p in OPENAPI), "existe rota de avaliação no OpenAPI")
        tables = sql("SELECT count(*) FROM information_schema.tables WHERE table_schema='atlas' AND (table_name ILIKE '%rating%' OR table_name ILIKE '%tip%')")
        chk(int(tables) > 0, f"tabelas de avaliação/gorjeta: {tables}")

    with Scenario("T8", "Voucher"):
        customer = new_user("customer", "Cliente T8")
        est = estimate(customer)
        status, created = create(customer, est, voucher_code="TAXI10", discount_cents=99999)
        chk(status == 201 and created.get("fare_cents", 0) < est["total_cents"], f"voucher de 10%: fare {created.get('fare_cents')} vs. {est['total_cents']}")
        chk(any("voucher" in p for p in OPENAPI), "existe rota de vouchers")
        if status == 201:
            call("POST", f"/requests/{created['id']}/cancel", {"reason": "QA"}, customer["tok"])


def maintenance(admin, partner):
    workshop_id = None
    with Scenario("M1", "Oficinas: lista, filtro e pesquisa"):
        customer = new_user("customer", "Cliente M1")
        body = {"name": "Auto Kianda QA", "currency": "AOA", "offers_home_service": True, "home_service_fee_cents": 300000, "opening_hours": "Seg-Sex 08:00-18:00", "address": {"lat": -8.9, "lng": 13.2, "line": "Rua 12, Talatona"}}
        status, workshop = call("POST", "/fleet/workshops", body, partner, PARTNER)
        chk(status == 201, f"parceiro cria oficina: {status}")
        workshop_id = workshop.get("id")
        for service in ({"name": "Revisão geral", "price_cents": 3500000, "duration_minutes": 120}, {"name": "Óleo e filtro", "price_cents": 1800000, "duration_minutes": 45}):
            status, _ = call("POST", f"/fleet/workshops/{workshop_id}/services", service, partner, PARTNER)
            chk(status == 201, f"serviço {service['name']}: {status}")
        status, _ = call("GET", "/workshops?category=Pneus&search=Kianda", None, customer["tok"])
        chk(status == 200, f"cliente lista oficinas: {status}")
        status, listed = call("GET", f"/admin/workshops?organization_id={PARTNER_ORG}", None, admin, BACKOFFICE)
        chk(status == 200 and listed.get("total", 0) >= 1, f"admin lista: {status} total {listed.get('total')}")
        status, got = call("GET", f"/fleet/workshops/{workshop_id}", None, partner, PARTNER)
        chk(got.get("offers_home_service") is True and len(got.get("services", [])) == 2, "parceiro lê ao domicílio e os 2 serviços")
        chk("open_until" in got or "is_open" in got, f"'aberta até' calculado; o horário é texto: {got.get('opening_hours')!r}")

    with Scenario("M2", "Marcação, capacidade e concorrência"):
        customer = new_user("customer", "Cliente M2")
        status, _ = call("GET", f"/workshops/{workshop_id}/availability?date=2026-11-09", None, customer["tok"])
        chk(status == 200, f"horários disponíveis: {status}")
        status, _ = call("POST", "/bookings", {"workshop_id": workshop_id}, customer["tok"])
        chk(status in (200, 201), f"marcar: {status}")
        chk(int(sql("SELECT count(*) FROM information_schema.tables WHERE table_schema='atlas' AND table_name ILIKE '%booking%'")) > 0, "tabela de marcações")

    with Scenario("M3", "Ao domicílio e promoção"):
        chk(False, "a taxa existe na oficina (M1) mas nada a soma: não há marcação nem preço; não há promoções")

    with Scenario("M4", "Fotos no R2"):
        customer = new_user("customer", "Cliente M4")
        status, created = call("POST", "/requests", {"service_id": sql("SELECT id FROM services WHERE slug='maintenance'"), "pickup": PICKUP, "details": {"problem": "QA"}}, customer["tok"], idem=str(uuid.uuid4()))
        rid = created.get("id")
        status, res = call("POST", f"/requests/{rid}/photos/upload-url", {"content_type": "image/jpeg", "size": 1000}, customer["tok"])
        chk(status == 200, f"URL pré-assinada para fotos do pedido: {status}")
        chk(any("photo" in p for p in OPENAPI), "rota de fotos no OpenAPI")
        if rid:
            call("POST", f"/requests/{rid}/cancel", {"reason": "QA"}, customer["tok"])

    with Scenario("M5", "Estados e orçamento extra"):
        statuses = sql("SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname LIKE 'service_requests_status%' LIMIT 1")
        chk("vehicle_received" in statuses or "ready" in statuses, f"estados de oficina existem: {statuses[:150]}")
        chk(any("extra" in p or "budget" in p for p in OPENAPI), "rota de orçamento extra")

    with Scenario("M6", "Só cobra depois de aprovar"):
        customer = new_user("customer", "Cliente M6")
        status, created = call("POST", "/requests", {"service_id": sql("SELECT id FROM services WHERE slug='maintenance'"), "pickup": PICKUP, "details": {"problem": "QA"}}, customer["tok"], idem=str(uuid.uuid4()))
        rid = created.get("id")
        status, res = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"])
        chk(status == 409 and code(res) == "REQUEST_NOT_PAYABLE", f"pedido não concluído não se paga: {status} {code(res)}")
        chk(any("approve" in p and "request" in p for p in OPENAPI), "passo de aprovação do serviço pelo cliente")
        call("POST", f"/requests/{rid}/cancel", {"reason": "QA"}, customer["tok"])


def towing():
    with Scenario("R1", "Preço por tipo de viatura e distância"):
        customer = new_user("customer", "Cliente R1")
        near, far = estimate(customer, category="towing"), estimate(customer, category="towing", dest={"lat": -8.9200, "lng": 13.1800})
        chk(far["total_cents"] > near["total_cents"], f"depende da distância: {near['distance_m']} m {near['total_cents']} -> {far['distance_m']} m {far['total_cents']}")
        status, res = call("POST", "/fare-estimates", {"category": "towing", "pickup": PICKUP, "destination": DEST}, customer["tok"])
        chk(len(res.get("items", [])) >= 3, f"Ligeiro, SUV e Motociclo com preços diferentes: {[i['service_name'] for i in res.get('items', [])]}")

    with Scenario("R2", "Cancelar durante a procura"):
        customer = new_user("customer", "Cliente R2")
        status, created = create(customer, estimate(customer, category="towing"))
        chk(status == 201, f"pedido: {status} {created.get('status')}")
        status, res = call("POST", f"/requests/{created['id']}/cancel", {"reason": "Já resolvi"}, customer["tok"])
        chk(status == 200 and res.get("status") == "cancelled", f"cancelar: {status} {res.get('status')}")
        info("sem custo porque não existe taxa nenhuma na API")

    with Scenario("R3", "Código de verificação da entrega"):
        customer, driver = new_user("customer", "Cliente R3"), new_user("provider", "Reboque R3", "towing", kind="tow_truck")
        go_online(driver)
        status, created = create(customer, estimate(customer, category="towing"))
        rid = created["id"]
        offer = wait_offer(driver, rid)
        accept(driver, offer)
        advance(driver, rid, ("en-route", "arrive", "start"))
        status, res = call("POST", f"/requests/{rid}/complete", {"code": "0000"}, driver["tok"], PROVIDER)
        chk(status >= 400, f"código errado deve recusar: {status} {res.get('status')}")
        chk("verification_code" in json.dumps(created) or "delivery_code" in json.dumps(created), "o cliente recebe um código")
        go_offline(driver)


def removals():
    with Scenario("D1", "Volume e viatura recomendada"):
        customer = new_user("customer", "Cliente D1")
        service = sql("SELECT id FROM services WHERE slug='removals'")
        body = {"service_id": service, "pickup": PICKUP, "destination": DEST, "details": {"items": [{"name": "Sofá", "qty": 1}], "volume_m3": 999}}
        status, created = call("POST", "/requests", body, customer["tok"], idem=str(uuid.uuid4()))
        chk(status == 201, f"pedido: {status} {created.get('status')}")
        chk(created.get("details", {}).get("volume_m3") != 999, "o volume não pode vir da app: guardou o valor enviado (999)")
        chk(any("item" in p or "volume" in p for p in OPENAPI), "catálogo de artigos e cálculo de volume")
        if created.get("id"):
            call("POST", f"/requests/{created['id']}/cancel", {"reason": "QA"}, customer["tok"])

    with Scenario("D2", "Total de viatura, ajudantes e extras"):
        customer = new_user("customer", "Cliente D2")
        status, res = call("POST", "/fare-estimates", {"category": "removals", "pickup": PICKUP, "destination": DEST, "helpers": 2, "extras": ["packing"]}, customer["tok"])
        chk(status == 201 and any(i["total_cents"] == 11000000 for i in res.get("items", [])), f"85 000 + 2×5 000 + 15 000 = 110 000 Kz; API: {[i['total_cents'] for i in res.get('items', [])]}")
        info("política de mudanças: " + sql("SELECT pricing_mode,offer_mode FROM dispatch_policies p JOIN service_categories c ON c.id=p.category_id WHERE c.slug='removals'"))

    with Scenario("D3", "Janelas e cancelamento a 24 h"):
        customer = new_user("customer", "Cliente D3")
        service = sql("SELECT id FROM services WHERE slug='removals'")
        when = (dt.datetime.now(dt.timezone.utc) + dt.timedelta(hours=12)).isoformat()
        status, created = call("POST", "/requests", {"service_id": service, "pickup": PICKUP, "destination": DEST, "scheduled_for": when}, customer["tok"], idem=str(uuid.uuid4()))
        status, res = call("POST", f"/requests/{created.get('id')}/cancel", {"reason": "QA"}, customer["tok"])
        chk(status == 200 and ("fee_cents" in res or "cancel_fee_cents" in res), f"a menos de 24 h aplica a regra: {status}, sem taxa")
        chk(any("window" in p or "slot" in p for p in OPENAPI), "janelas com capacidade")


def orders_and_account():
    with Scenario("P1", "Separadores e filtro por serviço"):
        customer, driver, done = completed_ride("cash")
        call("POST", f"/requests/{done}/payments/cash", None, driver["tok"], PROVIDER)
        luanda = dt.timezone(dt.timedelta(hours=1))
        status, scheduled = create(customer, estimate(customer), scheduled_for=(dt.datetime.now(luanda) + dt.timedelta(days=2)).isoformat())
        status, tow = create(customer, estimate(customer, category="towing"))
        status, removal = call("POST", "/requests", {"service_id": sql("SELECT id FROM services WHERE slug='removals'"), "pickup": PICKUP, "destination": DEST}, customer["tok"], idem=str(uuid.uuid4()))
        status, repair = call("POST", "/requests", {"service_id": sql("SELECT id FROM services WHERE slug='maintenance'"), "pickup": PICKUP}, customer["tok"], idem=str(uuid.uuid4()))
        ids = lambda res: {i["id"] for i in res.get("items", [])}
        _, active = call("GET", "/requests?status=created,searching,offered,awaiting_choice,accepted,provider_en_route,in_progress", None, customer["tok"])
        _, history = call("GET", "/requests?status=completed,cancelled,expired", None, customer["tok"])
        _, marked = call("GET", "/requests?status=scheduled", None, customer["tok"])
        chk(tow["id"] in ids(active) and done not in ids(active), f"Em curso: {len(ids(active))} pedidos, inclui reboque")
        chk(removal["id"] in ids(active) and repair["id"] in ids(active), "Em curso junta mudanças e manutenção")
        chk(done in ids(history) and tow["id"] not in ids(history), "Histórico tem a concluída")
        chk(ids(marked) == {scheduled["id"]}, "Marcados tem só a marcada")
        _, only = call("GET", "/requests?category=towing", None, customer["tok"])
        chk(ids(only) == {tow["id"]}, "filtro por serviço")
        for item in (scheduled, tow, removal, repair):
            call("POST", f"/requests/{item['id']}/cancel", {"reason": "QA"}, customer["tok"], log=False)

    with Scenario("P2", "Recibo PDF e Repetir"):
        customer, _, rid = completed_ride("cash")
        status, _ = call("GET", f"/requests/{rid}/receipt", None, customer["tok"])
        chk(status == 200, f"recibo: {status}")
        status, _ = call("POST", f"/requests/{rid}/repeat", {}, customer["tok"])
        chk(status in (200, 201), f"repetir: {status}")

    with Scenario("P3", "Caixa de notificações"):
        customer, _, rid = completed_ride("cash")
        status, _ = call("GET", "/notifications", None, customer["tok"])
        chk(status == 200, f"caixa de entrada: {status}")
        status, _ = call("POST", "/notifications/read-all", {}, customer["tok"])
        chk(status in (200, 204), f"marcar lidas: {status}")
        columns = sql("SELECT string_agg(column_name,',') FROM information_schema.columns WHERE table_schema='atlas' AND table_name='notifications'")
        info("a tabela notifications é um registo de envios: " + columns)
        log = sql(f"SELECT template,channel,status FROM notifications WHERE user_id='{customer['id']}' LIMIT 5").replace("\n", "; ")
        info("envios do cliente: " + (log or "nenhum"))

    with Scenario("P4", "Eliminar conta por SMS e toggles"):
        customer = new_user("customer", "Cliente P4")
        status, res = call("DELETE", "/users/me", {}, customer["tok"])
        chk(400 <= status < 500, f"sem confirmação: {status} {code(res)}")
        status, res = call("POST", "/users/me/verification-code", {}, customer["tok"])
        chk(status < 300, f"pedir código: {status}")
        time.sleep(1)
        real = otp_for(customer["phone"])
        chk(real is not None, "o código sai por SMS")
        status, res = call("DELETE", "/users/me", {"code": "000000" if real != "000000" else "111111"}, customer["tok"])
        chk(400 <= status < 500, f"código errado: {status} {code(res)}")
        status, res = call("DELETE", "/users/me", {"code": real}, customer["tok"])
        chk(status < 300, f"código certo: {status}")
        chk(sql(f"SELECT deleted_at IS NOT NULL FROM users WHERE id='{customer['id']}'") == "t", "conta eliminada (BD)")
        status, _ = call("GET", "/users/me", None, customer["tok"])
        chk(status == 401, f"a sessão termina: {status}")
        other = new_user("customer", "Cliente P4b")
        status, _ = call("PATCH", "/users/me", {"notify_promotions": False, "notify_sms": False}, other["tok"])
        persisted = "notify" in sql("SELECT string_agg(column_name,',') FROM information_schema.columns WHERE table_schema='atlas' AND table_name='users'")
        chk(persisted, f"toggles persistidos: PATCH {status}, mas não há colunas de preferências")


def payments_real(admin):
    with Scenario("X1", "Express real, webhook forjado e reembolso"):
        customer, driver, rid = completed_ride("gpo")
        status, pending = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK, "amount_cents": 1}, customer["tok"])
        fare = sql(f"SELECT fare_cents FROM service_requests WHERE id='{rid}'")
        chk(status == 201 and str(pending.get("amount_cents")) == fare, f"amount_cents enviado é ignorado: cobra {pending.get('amount_cents')}, fare {fare}")
        chk(wait_payment(customer, rid, "paid"), "fica pago depois da confirmação da AppyPay")
        status, res = call("POST", f"/admin/payments/{pending['id']}/refund", {"amount_cents": int(fare) + 1, "reason": "QA"}, admin, BACKOFFICE)
        chk(code(res) == "REFUND_TOO_LARGE", f"reembolso acima do pago: {status} {code(res)}")
        status, res = call("POST", f"/admin/payments/{pending['id']}/refund", {"amount_cents": 1000, "reason": "QA"}, admin, BACKOFFICE)
        chk(status == 200 and res.get("refunded_cents") == 1000, f"reembolso parcial real: {status} {res.get('refunded_cents')}")
        with cf.ThreadPoolExecutor(2) as pool:
            both = list(pool.map(lambda _: call("POST", f"/admin/payments/{pending['id']}/refund", {"reason": "QA"}, admin, BACKOFFICE, log=False)[0], range(2)))
        refunded = sql(f"SELECT refunded_cents FROM payments WHERE id='{pending['id']}'")
        chk(sorted(both)[0] == 200 and sorted(both)[1] != 200 and refunded == fare, f"2 reembolsos totais em simultâneo: {both}, reembolsado {refunded} de {fare}")

    with Scenario("X2", "Referência real e webhook forjado"):
        customer, driver, rid = completed_ride("cash")
        sql(f"UPDATE category_payment_methods SET is_enabled=true WHERE method='reference' AND category_id=(SELECT id FROM service_categories WHERE slug='taxi')")
        try:
            status, pending = call("POST", f"/requests/{rid}/payments", {"method": "reference"}, customer["tok"])
            reference = pending.get("reference") or {}
            chk(status == 201 and reference.get("number") and reference.get("entity"), f"referência real: {status} {reference}")
            status, _ = call("POST", "/webhooks/appypay", {"id": sql(f"SELECT provider_charge_id FROM payments WHERE id='{pending['id']}'"), "merchantTransactionId": pending["code"], "amount": 1, "responseStatus": {"successful": True, "status": "Success"}})
            time.sleep(1)
            chk(sql(f"SELECT status FROM payments WHERE id='{pending['id']}'") == "pending", f"webhook forjado ({status}) não paga")
        finally:
            sql(f"UPDATE category_payment_methods SET is_enabled=false WHERE method='reference' AND category_id=(SELECT id FROM service_categories WHERE slug='taxi')")


def backoffice(admin, partner):
    with Scenario("A1", "Frota e pedidos do parceiro com corridas reais"):
        customer = new_user("customer", "Cliente A1")
        phone = random_phone()
        service = sql("SELECT id FROM services WHERE slug='towing'")
        plate = "QA-%06d" % random.randint(0, 999999)
        status, vehicle = call("POST", "/fleet/vehicles", {"kind": "tow_truck", "brand": "Isuzu", "model": "NPR", "year": 2020, "color": "Branco", "plate": plate, "seats": 3, "capacity_kg": 3500}, partner, PARTNER)
        chk(status == 201, f"criar reboque: {status}")
        status, registered = call("POST", "/fleet/drivers", {"name": "Reboque Frota", "phone": phone, "service_id": service, "vehicle_id": vehicle.get("id")}, partner, PARTNER)
        chk(status == 201, f"registar motorista: {status}")
        uid = registered.get("user_id")
        time.sleep(1)
        otp = otp_for(phone)
        status, _ = call("POST", "/auth/recovery/confirm", {"phone": phone, "code": otp, "new_password": "Frase-Segura-QA-1"}, client=PROVIDER)
        chk(status == 200, f"motorista define a palavra-passe pelo SMS: {status}")
        status, login = call("POST", "/auth/login", {"identifier": phone, "password": "Frase-Segura-QA-1"}, client=PROVIDER)
        chk(status == 200, f"login: {status}")
        driver = {"id": uid, "phone": phone, "tok": login.get("access_token"), "client": PROVIDER}
        status, res = go_online(driver, DRIVER_AT)
        chk(status == 403, f"não aprovado não fica online: {status}")
        sql(f"UPDATE providers SET status='approved', service_id='{service}' WHERE user_id='{uid}'")
        status, res = go_online(driver)
        chk(status == 200, f"aprovado fica online: {status}")
        status, created = create(customer, estimate(customer, category="towing"))
        rid = created["id"]
        offer = wait_offer(driver, rid)
        accept(driver, offer)
        advance(driver, rid)
        go_offline(driver)
        chk(sql(f"SELECT organization_id FROM service_requests WHERE id='{rid}'") == PARTNER_ORG, "o pedido fica ligado ao parceiro (BD)")
        status, mine = call("GET", "/fleet/requests?category=towing", None, partner, PARTNER)
        chk(status == 200 and any(i["id"] == rid for i in mine.get("items", [])), f"parceiro vê a corrida: {status} total {mine.get('total')}")
        status, summary = call("GET", "/fleet/requests/summary?group_by=status", None, partner, PARTNER)
        chk(status == 200 and summary.get("total", 0) >= 1, f"resumo do parceiro: {status} {summary.get('groups', [])[:1]}")
        status, detail = call("GET", f"/fleet/requests/{rid}", None, partner, PARTNER)
        chk(status == 200 and detail["request"].get("contact_phone") in (None, "") and len(detail.get("history", [])) > 0, f"detalhe com histórico, sem telefone do cliente: {status}")
        status, listed = call("GET", f"/admin/requests?category=towing&organization_id={PARTNER_ORG}", None, admin, BACKOFFICE)
        chk(status == 200 and any(i["id"] == rid and i["provider_name"] == "Reboque Frota" for i in listed.get("items", [])), f"admin vê: {status}")
        status, grouped = call("GET", "/admin/requests/summary?group_by=category", None, admin, BACKOFFICE)
        fare = sql(f"SELECT fare_cents FROM service_requests WHERE id='{rid}'")
        tow = next((g for g in grouped.get("groups", []) if g["key"] == "towing"), {})
        chk(status == 200 and tow.get("completed_fare_cents", 0) >= int(fare), f"resumo por categoria: {tow}")
        status, detail = call("GET", f"/admin/requests/{rid}", None, admin, BACKOFFICE)
        chk(status == 200 and len(detail.get("history", [])) >= 5, f"detalhe admin: {status} {len(detail.get('history', []))} eventos")


def security(admin, partner):
    with Scenario("S1", "IDOR entre clientes e motoristas"):
        owner, intruder = new_user("customer", "Dono S1"), new_user("customer", "Intruso S1")
        driver, stranger = new_user("provider", "Motorista S1", "taxi"), new_user("provider", "Estranho S1", "taxi")
        go_online(driver)
        status, created = create(owner, estimate(owner), payment_method="gpo")
        rid = created["id"]
        offer = wait_offer(driver, rid)
        chk(call("GET", f"/offers/{offer['id']}", None, stranger["tok"], PROVIDER)[0] == 404, "outro motorista não vê a oferta")
        chk(call("POST", f"/offers/{offer['id']}/accept", {"version": offer["request_version"]}, stranger["tok"], PROVIDER)[0] in (403, 404), "outro motorista não aceita")
        accept(driver, offer)
        for label, method, path, body in (("ver", "GET", f"/requests/{rid}", None), ("cancelar", "POST", f"/requests/{rid}/cancel", {"reason": "x"}), ("rota", "POST", f"/requests/{rid}/route", {"destination": DEST}),
                                          ("boost", "POST", f"/requests/{rid}/boost", {"percent": 10}), ("método", "PUT", f"/requests/{rid}/payment-method", {"method": "cash"}),
                                          ("pagamentos", "GET", f"/requests/{rid}/payments", None), ("pagar", "POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK})):
            status, _ = call(method, path, body, intruder["tok"])
            chk(status == 404, f"cliente B {label}: {status}")
        for step in ("en-route", "arrive", "start", "complete", "payments/cash"):
            status, _ = call("POST", f"/requests/{rid}/{step}", None, stranger["tok"], PROVIDER)
            chk(status == 404, f"motorista estranho {step}: {status}")
        chk(call("GET", "/admin/payments", None, owner["tok"])[0] == 403, "cliente não acede a /admin/payments")
        chk(call("GET", "/admin/requests", None, partner, PARTNER)[0] == 403, "parceiro não acede a /admin/requests")
        chk(call("GET", f"/partners/{PARTNER_ORG}/fleet/vehicles", None, partner, PARTNER)[0] == 403, "parceiro não usa as rotas da plataforma")
        call("POST", f"/requests/{rid}/cancel", {"reason": "QA"}, owner["tok"])
        go_offline(driver)

    with Scenario("S2", "Motorista não aprovado"):
        uid, phone = str(uuid.uuid4()), random_phone()
        draft = new_user("provider", "Rascunho S2")
        status, _ = go_online(draft)
        chk(status == 403, f"sem perfil aprovado não fica online: {status}")
        customer, driver = new_user("customer", "Cliente S2"), new_user("provider", "Motorista S2", "taxi")
        go_online(driver)
        status, created = create(customer, estimate(customer))
        offer = wait_offer(driver, created["id"])
        status, _ = call("POST", f"/offers/{offer['id']}/accept", {"version": offer["request_version"]}, draft["tok"], PROVIDER)
        chk(status in (403, 404), f"não aprovado não aceita: {status}")
        sql(f"UPDATE providers SET status='suspended' WHERE user_id='{driver['id']}'")
        status, _ = accept(driver, offer)
        chk(status in (403, 404, 409), f"suspenso com oferta aberta não aceita: {status}")
        sql(f"UPDATE providers SET status='approved' WHERE user_id='{driver['id']}'")
        call("POST", f"/requests/{created['id']}/cancel", {"reason": "QA"}, customer["tok"])
        go_offline(driver)
        sql(f"UPDATE vehicles SET provider_id=NULL WHERE provider_id='{driver['id']}'")
        status, _ = go_online(driver)
        chk(status == 403, f"sem viatura não fica online: {status}")

    with Scenario("S3", "Entradas inválidas nunca dão 500"):
        customer = new_user("customer", "Cliente S3")
        driver = new_user("provider", "Motorista S3", "taxi")
        missing = str(uuid.uuid4())
        huge = 99999999999999999999
        big32 = 99999999999
        service = sql("SELECT id FROM services WHERE slug='taxi'")
        category = sql("SELECT id FROM service_categories WHERE slug='taxi'")
        address = {"lat": -8.9, "lng": 13.2, "line": "Rua"}
        vehicle = {"kind": "van", "brand": "B", "model": "M", "year": 2020, "color": "C", "plate": "F-1", "seats": 3}
        cases = [
            ("POST", "/fare-estimates", {}, customer["tok"], CUSTOMER), ("POST", "/fare-estimates", {"category": "taxi", "pickup": {"lat": 999, "lng": 999}, "destination": DEST}, customer["tok"], CUSTOMER),
            ("POST", "/fare-estimates", {"category": "nada", "pickup": PICKUP, "destination": DEST}, customer["tok"], CUSTOMER), ("POST", "/requests", {}, customer["tok"], CUSTOMER),
            ("POST", "/requests", {"service_id": missing, "pickup": PICKUP}, customer["tok"], CUSTOMER), ("POST", "/requests", {"service_id": "x", "pickup": PICKUP}, customer["tok"], CUSTOMER),
            ("POST", "/requests", {"service_id": service, "pickup": PICKUP, "destination": DEST, "fare_estimate_id": missing}, customer["tok"], CUSTOMER),
            ("POST", "/requests", {"service_id": service, "pickup": PICKUP, "destination": DEST, "payment_method": "cartao"}, customer["tok"], CUSTOMER),
            ("GET", f"/requests/{missing}", None, customer["tok"], CUSTOMER), ("GET", "/requests/nao-e-uuid", None, customer["tok"], CUSTOMER), ("GET", "/requests?page=-1&size=99999999", None, customer["tok"], CUSTOMER),
            ("POST", f"/requests/{missing}/cancel", {"reason": "x"}, customer["tok"], CUSTOMER), ("POST", f"/requests/{missing}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"], CUSTOMER),
            ("POST", f"/requests/{missing}/payments", {"method": "bitcoin"}, customer["tok"], CUSTOMER), ("POST", f"/requests/{missing}/payments", {}, customer["tok"], CUSTOMER), ("POST", f"/requests/{missing}/payments/cash", None, driver["tok"], PROVIDER),
            ("PUT", f"/requests/{missing}/payment-method", {"method": "gpo"}, customer["tok"], CUSTOMER), ("POST", f"/offers/{missing}/accept", {"version": 1}, driver["tok"], PROVIDER), ("POST", f"/offers/{missing}/reject", None, driver["tok"], PROVIDER),
            ("POST", "/users/me/addresses", {}, customer["tok"], CUSTOMER), ("POST", "/users/me/addresses", {"kind": "home", "lat": 500, "lng": 500}, customer["tok"], CUSTOMER), ("POST", "/webhooks/appypay", {}, None, CUSTOMER),
            ("POST", "/fleet/vehicles", {}, partner, PARTNER), ("POST", "/fleet/vehicles", dict(vehicle, seats=big32), partner, PARTNER), ("POST", "/fleet/vehicles", dict(vehicle, year=big32, plate="F-2"), partner, PARTNER),
            ("POST", "/fleet/vehicles", dict(vehicle, capacity_kg=-1, plate="F-4"), partner, PARTNER), ("POST", "/fleet/vehicles", dict(vehicle, kind="nave", plate="F-5"), partner, PARTNER),
            ("POST", "/fleet/drivers", {"name": "X", "phone": "abc", "service_id": service}, partner, PARTNER), ("POST", "/fleet/drivers", {"name": "X", "phone": random_phone(), "service_id": missing}, partner, PARTNER), ("PUT", f"/fleet/drivers/{missing}/vehicle", {"vehicle_id": missing}, partner, PARTNER),
            ("POST", "/fleet/workshops", {}, partner, PARTNER), ("POST", "/fleet/workshops", {"name": "X", "currency": "AOA", "home_service_fee_cents": -5, "address": address}, partner, PARTNER), ("POST", "/fleet/workshops", {"name": "X", "currency": "AOA", "home_service_fee_cents": huge, "address": address}, partner, PARTNER),
            ("POST", "/fleet/workshops", {"name": "X", "currency": "AOA", "address": {"lat": 900, "lng": 13.2, "line": "R"}}, partner, PARTNER), ("POST", f"/fleet/workshops/{missing}/services", {"name": "X", "price_cents": 1}, partner, PARTNER),
            ("POST", "/admin/rejection-reasons", {"scope": "nada", "code": "x", "label": "y"}, admin, BACKOFFICE),
            ("PUT", f"/admin/rejection-reasons/{missing}", {"label": "x", "is_active": True}, admin, BACKOFFICE), ("PUT", f"/admin/providers/{missing}", {"status": "rejected", "reason_ids": [missing]}, admin, BACKOFFICE), ("PUT", f"/admin/providers/{driver['id']}", {"status": "suspended", "reason_ids": [missing]}, admin, BACKOFFICE),
            ("GET", "/admin/requests?from=ontem", None, admin, BACKOFFICE), ("GET", "/admin/requests/summary?group_by=x", None, admin, BACKOFFICE), ("GET", "/admin/requests?status=%27%3B%20DROP%20TABLE%20users", None, admin, BACKOFFICE), ("GET", f"/admin/requests/{missing}", None, admin, BACKOFFICE),
            ("GET", "/admin/providers?search=%25%27", None, admin, BACKOFFICE), ("GET", "/admin/vehicles?organization_id=x", None, admin, BACKOFFICE), ("POST", f"/admin/payments/{missing}/refund", {"reason": "x"}, admin, BACKOFFICE), ("POST", f"/admin/payments/{missing}/refund", {"amount_cents": -5, "reason": "x"}, admin, BACKOFFICE),
            ("PUT", "/admin/payment-methods", {"category_id": missing, "method": "gpo", "is_enabled": True}, admin, BACKOFFICE), ("PUT", "/admin/payment-methods", {"category_id": category, "method": "x"}, admin, BACKOFFICE),
            ("GET", f"/partners/{missing}/fleet/vehicles", None, admin, BACKOFFICE), ("POST", f"/partners/{missing}/fleet/vehicles", vehicle, admin, BACKOFFICE),
        ]
        fives, accepted = [], []
        for method, path, body, tok, client in cases:
            status, res = call(method, path, body, tok, client, idem=str(uuid.uuid4()) if path == "/requests" else None)
            if status >= 500:
                fives.append(f"{method} {path} {json.dumps(body)[:90]} -> {status} {code(res)}")
            elif status < 300:
                accepted.append(f"{method} {path} {json.dumps(body)[:100]} -> {status}")
        status, res = call("POST", "/requests", None, customer["tok"], raw=b"{mal formado", idem=str(uuid.uuid4()))
        chk(status < 500, f"JSON malformado: {status}")
        chk(not fives, f"{len(cases)} entradas inválidas, com 5xx: {fives}")
        info("aceites com 2xx: " + " | ".join(accepted))
        chk(not [a for a in accepted if "99999999999" in a or "-5" in a], "valores negativos e absurdos não são aceites")

    with Scenario("S4", "Preço vem do servidor"):
        customer = new_user("customer", "Cliente S4")
        est = estimate(customer)
        status, created = create(customer, est, fare_cents=1, total_cents=1, commission_cents=0, status="completed", provider_id=str(uuid.uuid4()))
        chk(status == 201 and created.get("fare_cents") == est["total_cents"] and created.get("status") != "completed", f"fare_cents=1 é ignorado: {created.get('fare_cents')} {created.get('status')}")
        call("POST", f"/requests/{created['id']}/cancel", {"reason": "QA"}, customer["tok"])

    with Scenario("S5", "Duplo clique não duplica corridas nem cobranças"):
        customer = new_user("customer", "Cliente S5")
        est = estimate(customer)
        key = str(uuid.uuid4())
        with cf.ThreadPoolExecutor(5) as pool:
            statuses = list(pool.map(lambda _: create(customer, est, idem=key)[0], range(5)))
        count = sql(f"SELECT count(*) FROM service_requests WHERE customer_id='{customer['id']}'")
        chk(count == "1", f"5 envios com a mesma chave: {statuses}, {count} pedido na BD")
        other = new_user("customer", "Cliente S5b")
        est = estimate(other)
        with cf.ThreadPoolExecutor(3) as pool:
            statuses = list(pool.map(lambda _: create(other, est)[0], range(3)))
        count = sql(f"SELECT count(*) FROM service_requests WHERE customer_id='{other['id']}' AND status NOT IN ('cancelled','expired')")
        chk(count == "1", f"mesma estimativa com chaves diferentes: {statuses}, {count} pedidos ativos")
        for uid, tok in ((customer["id"], customer["tok"]), (other["id"], other["tok"])):
            for rid in sql(f"SELECT id FROM service_requests WHERE customer_id='{uid}'").splitlines():
                call("POST", f"/requests/{rid}/cancel", {"reason": "QA"}, tok, log=False)
        payer, driver, rid = completed_ride("gpo")
        with cf.ThreadPoolExecutor(5) as pool:
            statuses = list(pool.map(lambda _: call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, payer["tok"], log=False)[0], range(5)))
        rows = sql(f"SELECT count(*) FROM payments WHERE request_id='{rid}'")
        chk(rows == "1", f"5 pagamentos em simultâneo: {statuses}, {rows} pagamento na BD")
        cash, driver, rid = completed_ride("cash")
        with cf.ThreadPoolExecutor(5) as pool:
            statuses = list(pool.map(lambda _: call("POST", f"/requests/{rid}/payments/cash", None, driver["tok"], PROVIDER, log=False)[0], range(5)))
        rows = sql(f"SELECT count(*) FROM payments WHERE request_id='{rid}'")
        chk(rows == "1", f"5 confirmações de dinheiro: {statuses}, {rows} pagamento")

    with Scenario("S6", "Concorrência: dois motoristas aceitam"):
        customer, one, two = new_user("customer", "Cliente S6"), new_user("provider", "Motorista S6a", "towing", kind="tow_truck"), new_user("provider", "Motorista S6b", "towing", kind="tow_truck")
        go_online(one)
        go_online(two, {"lat": -8.8392, "lng": 13.2342, "accuracy_m": 10, "age_s": 0})
        status, created = create(customer, estimate(customer, category="towing"))
        rid = created["id"]
        first, second = wait_offer(one, rid), wait_offer(two, rid)
        if not (first and second):
            not_testable("o reboque não ofereceu a ambos os motoristas")
        else:
            with cf.ThreadPoolExecutor(2) as pool:
                statuses = sorted(pool.map(lambda pair: accept(*pair)[0], ((one, first), (two, second))))
            chk(statuses[0] == 200 and statuses[1] != 200, f"aceites em simultâneo: {statuses}")
        call("POST", f"/requests/{rid}/cancel", {"reason": "QA"}, customer["tok"])
        go_offline(one, two)


def account_and_realtime():
    with Scenario("E1", "Moradas guardadas"):
        customer, intruder = new_user("customer", "Cliente E1"), new_user("customer", "Intruso E1")
        status, home = call("POST", "/users/me/addresses", {"kind": "home", "label": "Casa", "lat": -8.83, "lng": 13.23, "line": "Rua Rainha Ginga"}, customer["tok"])
        chk(status == 201, f"guardar Casa: {status}")
        status, res = call("POST", "/users/me/addresses", {"kind": "home", "lat": -8.84, "lng": 13.24, "line": "Outra"}, customer["tok"])
        chk(400 <= status < 500, f"segunda Casa: {status} {code(res)}")
        status, other = call("POST", "/users/me/addresses", {"kind": "other", "label": "Ginásio", "lat": -8.85, "lng": 13.25, "line": "Rua B"}, customer["tok"])
        chk(status == 201, f"outra morada: {status}")
        status, listed = call("GET", "/users/me/addresses", None, customer["tok"])
        chk(len(listed.get("items", [])) == 2, f"lista: {len(listed.get('items', []))}")
        status, _ = call("DELETE", f"/users/me/addresses/{home['id']}", None, intruder["tok"])
        chk(status == 404, f"intruso apaga: {status}")
        status, _ = call("DELETE", f"/users/me/addresses/{home['id']}", None, customer["tok"])
        chk(status in (200, 204), f"apagar: {status}")

    with Scenario("E2", "Perfil, palavra-passe e telemóvel"):
        customer = new_user("customer", "Cliente E2")
        status, res = call("PATCH", "/users/me", {"name": "Nome Novo QA"}, customer["tok"])
        chk(status == 200 and sql(f"SELECT name FROM users WHERE id='{customer['id']}'") == "Nome Novo QA", f"editar nome: {status}")
        status, res = call("PUT", "/users/password", {"current_password": "errada-QA-1", "new_password": "Frase-Nova-QA-2"}, customer["tok"])
        chk(400 <= status < 500, f"palavra-passe atual errada: {status} {code(res)}")
        status, res = call("PUT", "/users/password", {"current_password": PASSWORD, "new_password": "Frase-Nova-QA-2"}, customer["tok"])
        chk(status < 300, f"alterar palavra-passe: {status} {code(res)}")
        status, res = call("POST", "/auth/login", {"identifier": customer["phone"], "password": "Frase-Nova-QA-2"})
        chk(status == 200, f"login com a nova: {status}")
        token = res.get("access_token")
        new_phone = random_phone()
        status, res = call("POST", "/users/me/phone/verification-code", {"phone": new_phone, "password": "Frase-Nova-QA-2"}, token)
        chk(status < 300, f"pedir código do novo número: {status} {code(res)}")
        time.sleep(1)
        otp = otp_for(new_phone)
        status, res = call("PUT", "/users/me/phone", {"phone": new_phone, "code": "000000" if otp != "000000" else "111111"}, token)
        chk(400 <= status < 500, f"código errado: {status} {code(res)}")
        status, res = call("PUT", "/users/me/phone", {"phone": new_phone, "code": otp}, token)
        chk(status < 300 and sql(f"SELECT phone FROM users WHERE id='{customer['id']}'") == new_phone, f"trocar número: {status} {code(res)}")

    with Scenario("E3", "Tempo real por SSE"):
        customer, driver = new_user("customer", "Cliente E3"), new_user("provider", "Motorista E3", "taxi")
        events = []

        def listen():
            req = urllib.request.Request(BASE + "/events", headers={"X-Client-Id": CUSTOMER, "Authorization": "Bearer " + customer["tok"], "Accept": "text/event-stream"})
            try:
                with urllib.request.urlopen(req, timeout=120) as stream:
                    for raw in stream:
                        line = raw.decode().strip()
                        if line.startswith(("event:", "data:")):
                            events.append(line)
            except (OSError, ValueError):
                pass

        threading.Thread(target=listen, daemon=True).start()
        time.sleep(1)
        go_online(driver)
        status, created = create(customer, estimate(customer), payment_method="gpo")
        rid = created["id"]
        offer = wait_offer(driver, rid)
        accept(driver, offer)
        advance(driver, rid, ("en-route",))
        for step in range(4):
            time.sleep(1.2)
            status, moved = call("POST", "/providers/me/locations", {"positions": [{"lat": -8.8391 - 0.00001 * (step + 1), "lng": 13.2341, "accuracy_m": 10, "age_s": 0}]}, driver["tok"], PROVIDER)
            info(f"posição {step + 1}: {status} accepted={moved.get('accepted')} rejected={moved.get('rejected')}")
        advance(driver, rid, ("arrive", "start", "complete"))
        call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"])
        wait_payment(customer, rid, "paid")
        time.sleep(2)
        names = [e.split(":", 1)[1].strip() for e in events if e.startswith("event:")]
        statuses = re.findall(r'"to":"(\w+)"|"status":"(\w+)"', " ".join(events))
        seen = {a or b for a, b in statuses}
        chk("request.status" in names, f"estados chegam por SSE: {sorted(seen)}")
        chk("request.position" in names, f"posição do motorista chega por SSE; eventos: {sorted(set(names))}")
        chk("request.payment" in names, "pagamento chega por SSE")
        chk({"accepted", "completed"} <= seen, "inclui aceite e concluída")
        go_offline(driver)

    with Scenario("E4", "Paginação limitada"):
        _, res = call("GET", "/requests?page=-1&size=99999999", None, new_user("customer", "Cliente E4")["tok"])
        chk(res.get("size", 0) <= 100, f"paginação limitada: size={res.get('size')} page={res.get('page')}")


def overflow(admin, partner):
    with Scenario("S7", "Números acima do limite da coluna"):
        plate = "QA-OVF-%04d" % random.randint(0, 9999)
        status, res = call("POST", "/fleet/vehicles", {"kind": "van", "brand": "B", "model": "M", "year": 2020, "color": "C", "plate": plate, "seats": 3, "capacity_kg": 99999999999}, partner, PARTNER)
        stored = sql(f"SELECT capacity_kg FROM vehicles WHERE plate='{plate}'")
        chk(status >= 400 or stored == "99999999999", f"capacity_kg=99999999999: {status}, API devolve {res.get('capacity_kg')}, BD guarda {stored}")
        reason = "qa_ovf_" + uuid.uuid4().hex[:6]
        status, res = call("POST", "/admin/rejection-reasons", {"scope": "document", "code": reason, "label": "QA overflow", "display_order": 99999999999}, admin, BACKOFFICE)
        stored = sql(f"SELECT display_order FROM rejection_reasons WHERE code='{reason}'")
        chk(status >= 400 or stored == "99999999999", f"display_order=99999999999: {status}, API devolve {res.get('display_order')}, BD guarda {stored}")
        sql(f"UPDATE rejection_reasons SET is_active=false WHERE code='{reason}'")
        status, workshop = call("POST", "/fleet/workshops", {"name": "QA Overflow", "currency": "AOA", "address": {"lat": -8.9, "lng": 13.2, "line": "R"}}, partner, PARTNER)
        status, res = call("POST", f"/fleet/workshops/{workshop['id']}/services", {"name": "QA", "price_cents": 1, "duration_minutes": 99999999999}, partner, PARTNER)
        stored = sql(f"SELECT duration_minutes FROM workshop_services WHERE workshop_id='{workshop['id']}'")
        chk(status >= 400 or stored == "99999999999", f"duration_minutes=99999999999: {status}, API devolve {res.get('duration_minutes')}, BD guarda {stored}")
        call("DELETE", f"/fleet/workshops/{workshop['id']}", None, partner, PARTNER, log=False)


def serialize(path):
    data = {sid: {"title": r["title"], "result": r["result"], "note": r["note"], "checks": [{"ok": ok, "msg": msg} for ok, msg in r["checks"]], "steps": r["steps"]} for sid, r in results.items()}
    with open(path, "w", encoding="utf-8") as handle:
        json.dump({"sms": sms_log, "results": data}, handle, ensure_ascii=False, indent=1)


def compose(*args):
    subprocess.run(["docker", "compose", *args], stdin=subprocess.DEVNULL, capture_output=True)


def main():
    global OPENAPI
    server = http.server.ThreadingHTTPServer(("127.0.0.1", STUB_PORT), Stub)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    compose("stop", "api")
    workdir = tempfile.mkdtemp()
    binary = os.path.join(workdir, "atlas-qa")
    subprocess.run(["go", "build", "-o", binary, "."], check=True, stdin=subprocess.DEVNULL)
    env = dict(os.environ, CASTBRICK_BASE_URL=f"http://127.0.0.1:{STUB_PORT}", RESEND_BASE_URL=f"http://127.0.0.1:{STUB_PORT}", RATE_LIMIT_REQUESTS="1000000", AUTH_RATE_LIMIT_REQUESTS="1000000", OTP_RESEND_COOLDOWN="0s", LOG_LEVEL="warn")
    log_path = os.path.join("tests", "e2e", "evidence", "api.log")
    os.makedirs(os.path.dirname(log_path), exist_ok=True)
    api = subprocess.Popen([binary], env=env, stdout=open(log_path, "w"), stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL)
    try:
        for _ in range(60):
            try:
                urllib.request.urlopen(BASE + "/ready", timeout=2)
                break
            except OSError:
                time.sleep(1)
        with urllib.request.urlopen(BASE + "/docs/openapi.json") as res:
            OPENAPI = list(json.load(res)["paths"])
        admin, partner = staff("admin@agora.ao", BACKOFFICE), staff("parceiro@agora.ao", PARTNER)
        groups = {"taxi": taxi, "maintenance": lambda: maintenance(admin, partner), "towing": towing, "removals": removals, "orders": orders_and_account, "payments": lambda: payments_real(admin),
                  "backoffice": lambda: backoffice(admin, partner), "account": account_and_realtime, "overflow": lambda: overflow(admin, partner), "security": lambda: security(admin, partner)}
        for name in sys.argv[1:] or groups:
            groups[name]()
    finally:
        api.terminate()
        api.wait(timeout=30)
        stamp = time.strftime("%Y%m%d-%H%M%S")
        serialize(os.path.join("tests", "e2e", "evidence", f"run-{stamp}.json"))
        compose("up", "-d", "api")
    tally = {}
    for sid, result in results.items():
        tally[result["result"]] = tally.get(result["result"], 0) + 1
        print(f"{sid}\t{result['result']}\t{result['title']}")
    print(tally)


if __name__ == "__main__":
    main()
