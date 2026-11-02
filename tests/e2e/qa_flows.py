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


def fetch_raw(path, tok, client=CUSTOMER):
    req = urllib.request.Request(BASE + "/api/v1" + path, headers={"X-Client-Id": client, "Authorization": "Bearer " + tok})
    try:
        with urllib.request.urlopen(req, timeout=60) as res:
            return res.status, res.read()
    except urllib.error.HTTPError as err:
        return err.code, err.read()


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


def reason_id(tok, actor, category, code=None, client=CUSTOMER):
    status, res = call("GET", f"/cancellation-reasons?actor={actor}&category={category}", None, tok, client, log=False)
    assert status == 200, res
    items = res["items"]
    return next(i["id"] for i in items if code is None or i["code"] == code) if not code else next(i["id"] for i in items if i["code"] == code)


def cancel(tok, rid, client=CUSTOMER, log=False):
    status, preview = call("GET", f"/requests/{rid}/cancellation", None, tok, client, log=False)
    if status != 200 or not preview.get("reasons"):
        return status, preview
    return call("POST", f"/requests/{rid}/cancel", {"reason_id": preview["reasons"][0]["id"], "expected_fee_cents": preview["fee_cents"]}, tok, client, log=log)


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


ADMIN = [None]
PARTNER_TOKEN = [None]


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
        cancel(customer["tok"], created.get('id'))
        other = new_user("customer", "Cliente T1b")
        cheap = estimate(other)
        status, wrong = create(other, cheap, STOPS[:2])
        chk(status != 201, f"estimativa sem paragens não pode servir a um pedido com 2 paragens: status {status}, fare {wrong.get('fare_cents')} vs. correto {stopped['total_cents']}")
        if status == 201:
            cancel(other["tok"], wrong['id'])

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
        cancel(customer["tok"], rid)
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
            forged = estimate(customer)
            sql(f"UPDATE fare_estimates SET scheduled_for='{when.isoformat()}' WHERE id='{forged['fare_estimate_id']}'")
            status, res = create(customer, forged, scheduled_for=when.isoformat())
            chk(status == 422, f"pedido a {label}, com estimativa da mesma hora: {status} {code(res)}")
        other_hour = estimate(customer, scheduled_for=tomorrow.replace(hour=23).isoformat())
        status, res = create(customer, other_hour, scheduled_for=tomorrow.isoformat())
        chk(code(res) == "FARE_ESTIMATE_INVALID", f"estimativa de outra hora não serve: {status} {code(res)}")
        go_online(driver)
        sql(f"UPDATE service_requests SET next_action_at=now() WHERE id='{rid}'")
        offer = wait_offer(driver, rid, 30)
        chk(offer is not None, "ao simular a hora, o watcher despacha e o motorista recebe a oferta")
        _, view = call("GET", f"/requests/{rid}", None, customer["tok"])
        info(f"estado depois do despacho: {view.get('status')}")
        status, cancelled = cancel(customer["tok"], rid, log=True)
        chk(status == 200 and cancelled.get("cancel_fee_cents") == 0, f"cancelar marcada sem motorista é grátis: {status} {cancelled.get('cancel_fee_cents')}")
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
        chk(400 <= status < 500, f"sem motivo: {status} {code(res)}")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason": "texto livre"}, customer["tok"])
        chk(400 <= status < 500, f"texto livre não substitui o catálogo: {status} {code(res)}")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": str(uuid.uuid4())}, customer["tok"])
        chk(code(res) == "REASON_NOT_APPLICABLE", f"motivo inexistente: {status} {code(res)}")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": reason_id(customer["tok"], "provider", "taxi", "customer_no_show")}, customer["tok"])
        chk(code(res) == "REASON_NOT_APPLICABLE", f"motivo de outro ator: {status} {code(res)}")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": reason_id(customer["tok"], "customer", "towing", "solved")}, customer["tok"])
        chk(code(res) == "REASON_NOT_APPLICABLE", f"motivo de outro serviço: {status} {code(res)}")
        other = reason_id(customer["tok"], "customer", "taxi", "other")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": other}, customer["tok"])
        chk(status == 422, f"Outro motivo sem texto: {status} {code(res)}")
        status, preview = call("GET", f"/requests/{rid}/cancellation", None, customer["tok"])
        chk(status == 200 and preview.get("fee_cents") == 0, f"pré-aviso sem motorista: custo {preview.get('fee_cents')}")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": other, "note": "Mudei de ideias"}, customer["tok"])
        chk(status == 200 and res.get("cancel_fee_cents") == 0, f"sem motorista: {status}, custo {res.get('cancel_fee_cents')}")
        stored = sql(f"SELECT cancel_reason_id||'|'||cancel_reason FROM service_requests WHERE id='{rid}'")
        chk(stored.startswith(other) and "Mudei de ideias" in stored, f"motivo guardado por ID e com o texto (BD): {stored}")
        rider, driver = new_user("customer", "Cliente T5b"), new_user("provider", "Motorista T5", "taxi")
        go_online(driver)
        status, created = create(rider, estimate(rider))
        rid = created["id"]
        offer = wait_offer(driver, rid)
        accept(driver, offer)
        status, preview = call("GET", f"/requests/{rid}/cancellation", None, rider["tok"])
        chk(preview.get("fee_cents") == 0 and preview.get("free_until"), f"nos 2 primeiros minutos é grátis: {preview.get('fee_cents')} até {preview.get('free_until')}")
        sql(f"UPDATE service_requests SET accepted_at=now()-interval '121 seconds' WHERE id='{rid}'")
        fee = int(sql("SELECT r.fee_cents FROM cancellation_rules r JOIN services s ON s.id=r.service_id WHERE s.slug='taxi'"))
        status, preview = call("GET", f"/requests/{rid}/cancellation", None, rider["tok"])
        chk(preview.get("fee_cents") == fee and fee > 0, f"depois de 2 min: custo {preview.get('fee_cents')}, regra na BD {fee}")
        reason = reason_id(rider["tok"], "customer", "taxi", "changed_plans")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": reason}, rider["tok"])
        chk(code(res) == "CANCEL_FEE_CHANGED", f"sem confirmar o custo não cancela: {status} {code(res)}")
        status, res = call("POST", f"/requests/{rid}/cancel", {"reason_id": reason, "expected_fee_cents": fee}, rider["tok"])
        chk(status == 200 and res.get("cancel_fee_cents") == fee, f"com o custo confirmado: {status} {res.get('cancel_fee_cents')}")
        next_est = estimate(rider)
        status, res = create(rider, next_est)
        chk(code(res) == "PAYMENT_OUTSTANDING", f"a taxa por pagar é dívida: {status} {code(res)}")
        status, payment = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, rider["tok"])
        chk(status == 201 and payment.get("amount_cents") == fee, f"paga a taxa, não a tarifa: {status} {payment.get('amount_cents')}")
        chk(wait_payment(rider, rid, "paid"), "taxa paga na AppyPay de teste")
        status, res = create(rider, estimate(rider))
        chk(status == 201, f"sem dívida volta a pedir: {status}")
        cancel(rider["tok"], res.get("id"))
        customer2, driver2 = new_user("customer", "Cliente T5c"), driver
        status, created = create(customer2, estimate(customer2))
        offer = wait_offer(driver2, created["id"])
        accept(driver2, offer)
        sql(f"UPDATE service_requests SET accepted_at=now()-interval '600 seconds' WHERE id='{created['id']}'")
        before = int(sql(f"SELECT cancelled_requests FROM providers WHERE user_id='{driver2['id']}'"))
        status, res = call("POST", f"/requests/{created['id']}/cancel", {"reason_id": reason_id(driver2["tok"], "provider", "taxi", "vehicle_problem", PROVIDER)}, driver2["tok"], PROVIDER)
        after = int(sql(f"SELECT cancelled_requests FROM providers WHERE user_id='{driver2['id']}'"))
        chk(status == 200 and res.get("cancel_fee_cents") == 0 and after == before + 1, f"o motorista cancela: cliente paga {res.get('cancel_fee_cents')}, contagem {before} -> {after}")
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
        status, res = call("POST", f"/offers/{offer['id']}/reject", {"reason_id": str(uuid.uuid4())}, who["tok"], PROVIDER)
        chk(code(res) == "REASON_NOT_APPLICABLE", f"motivo inexistente: {status} {code(res)}")
        status, res = call("POST", f"/offers/{offer['id']}/reject", {}, who["tok"], PROVIDER)
        chk(status == 422, f"sem motivo: {status}")
        reason = reason_id(who["tok"], "offer", "taxi", "too_far", PROVIDER)
        status, res = call("POST", f"/offers/{offer['id']}/reject", {"reason_id": reason, "note": "Do outro lado"}, who["tok"], PROVIDER)
        chk(200 <= status < 300, f"recusar: {status}")
        stored = sql(f"SELECT reject_reason_id||'|'||reject_note FROM request_offers WHERE id='{offer['id']}'")
        chk(stored == f"{reason}|Do outro lado", f"motivo registado na oferta (BD): {stored}")
        status, report = call("GET", "/admin/reports/cancellations?scope=offer_reject&category=taxi", None, ADMIN[0], BACKOFFICE)
        chk(status == 200 and any(i["code"] == "too_far" and i["total"] >= 1 for i in report.get("items", [])), "o relatório agrupa pelo motivo")
        again = wait_offer(other, rid, 30)
        chk(again is not None, "a corrida é oferecida ao outro motorista")
        cancel(customer["tok"], rid)
        go_offline(first, second)

    with Scenario("T7", "Avaliação, gorjeta e média"):
        customer, driver, rid = completed_ride("cash")
        call("POST", f"/requests/{rid}/payments/cash", None, driver["tok"], PROVIDER)
        fare, commission = map(int, sql(f"SELECT fare_cents,commission_cents FROM service_requests WHERE id='{rid}'").split("|"))
        _, view = call("GET", f"/requests/{rid}", None, customer["tok"])
        breakdown = view.get("breakdown", {})
        chk(breakdown.get("total_cents") == fare and sum(i["amount_cents"] for i in breakdown.get("items", [])) == fare, f"resumo: linhas {breakdown.get('items')} somam o total {fare}")
        chk(view.get("can_rate") is True, "o histórico indica Avaliar")
        status, options = call("GET", "/rating-options?category=taxi", None, customer["tok"])
        codes = {c["code"]: c["id"] for c in options.get("compliments", [])}
        chk(status == 200 and "clean_car" in codes and len(options.get("tips", [])) == 3, f"elogios e gorjetas sugeridas do serviço: {list(codes)} {[t['amount_cents'] for t in options.get('tips', [])]}")
        intruder = new_user("customer", "Intruso T7")
        chk(call("POST", f"/requests/{rid}/rating", {"stars": 5}, intruder["tok"])[0] == 404, "outro cliente não avalia")
        for stars in (0, 6, 99999999999):
            status, _ = call("POST", f"/requests/{rid}/rating", {"stars": stars}, customer["tok"])
            chk(status == 422, f"{stars} estrelas: {status}")
        status, _ = call("POST", f"/requests/{rid}/rating", {}, customer["tok"])
        chk(status == 422, f"estrelas obrigatórias: {status}")
        body = {"stars": 4, "compliment_ids": [codes["clean_car"], codes["safe_driving"]], "comment": "Boa viagem"}
        status, res = call("POST", f"/requests/{rid}/rating", body, customer["tok"])
        chk(status == 201, f"avaliar: {status}")
        status, res = call("POST", f"/requests/{rid}/rating", body, customer["tok"])
        chk(code(res) == "ALREADY_RATED", f"avaliar duas vezes: {status} {code(res)}")
        stored = sql(f"SELECT rating_sum,rating_count,completed_requests FROM providers WHERE user_id='{driver['id']}'")
        chk(stored == "4|1|1", f"média e contagem na BD (soma|contagem|serviços): {stored}")
        _, view = call("GET", f"/requests/{rid}", None, customer["tok"])
        provider = view.get("provider", {})
        chk(provider.get("rating") == 4 and provider.get("rating_count") == 1 and provider.get("completed_requests") == 1, f"a app vê rating e nº de serviços: {provider.get('rating')} {provider.get('rating_count')} {provider.get('completed_requests')}")
        chk(view.get("can_rate") is False and view.get("rating", {}).get("stars") == 4, "a avaliação dada aparece no pedido")
        active = new_user("customer", "Cliente T7b")
        status, created = create(active, estimate(active))
        status, res = call("POST", f"/requests/{created['id']}/rating", {"stars": 5}, active["tok"])
        chk(code(res) == "RATING_NOT_ALLOWED", f"pedido não concluído: {status} {code(res)}")
        cancel(active["tok"], created["id"])
        status, res = call("POST", f"/requests/{rid}/tips", {"amount_cents": 100000000, "phone_number": SIMULATED_OK}, customer["tok"])
        chk(code(res) == "TIP_OUT_OF_RANGE", f"gorjeta acima do limite: {status} {code(res)}")
        status, tip = call("POST", f"/requests/{rid}/tips", {"amount_cents": 50000, "phone_number": SIMULATED_OK}, customer["tok"])
        chk(status == 201 and tip.get("method") == "gpo" and tip.get("amount_cents") == 50000, f"gorjeta digital em corrida a dinheiro, AppyPay real: {status} {tip.get('method')}")
        end = time.time() + 60
        while time.time() < end and sql(f"SELECT status FROM payments WHERE id='{tip.get('id')}'") != "paid":
            time.sleep(1)
        balance = int(sql(f"SELECT COALESCE(sum(amount_cents),0) FROM ledger_entries WHERE account_type='provider' AND account_id='{driver['id']}'"))
        chk(balance == 50000 - commission, f"gorjeta 100% para o motorista no ledger: {balance}, com comissão {commission} em dívida")
        chk(sql(f"SELECT count(*) FROM ledger_transactions t WHERE t.request_id='{rid}' AND t.kind='tip'") == "1", "gorjeta registada no ledger")

    with Scenario("T8", "Voucher"):
        admin = ADMIN[0]
        now = dt.datetime.now(dt.timezone.utc)
        def voucher(**extra):
            code_ = "QA%06d" % random.randint(0, 999999)
            body = {"code": code_, "discount_bps": 1000, "valid_from": (now - dt.timedelta(hours=1)).isoformat(), "valid_until": (now + dt.timedelta(hours=1)).isoformat(), "per_customer_limit": 1}
            body.update(extra)
            status, res = call("POST", "/admin/vouchers", body, admin, BACKOFFICE, log=False)
            assert status == 201, res
            return code_
        taxi_category = sql("SELECT id FROM service_categories WHERE slug='taxi'")
        towing_category = sql("SELECT id FROM service_categories WHERE slug='towing'")
        customer = new_user("customer", "Cliente T8")
        est = estimate(customer)
        fare = est["total_cents"]
        status, res = create(customer, est, voucher_code="NAOEXISTE")
        chk(code(res) == "VOUCHER_NOT_FOUND", f"voucher inexistente não é ignorado: {status} {code(res)}")
        expired = voucher(valid_from=(now - dt.timedelta(days=3)).isoformat(), valid_until=(now - dt.timedelta(days=1)).isoformat(), category_id=taxi_category)
        status, res = create(customer, est, voucher_code=expired)
        chk(code(res) == "VOUCHER_EXPIRED", f"expirado é recusado: {status} {code(res)}")
        other = voucher(category_id=towing_category)
        status, res = create(customer, est, voucher_code=other)
        chk(code(res) == "VOUCHER_NOT_APPLICABLE", f"de outro serviço é recusado: {status} {code(res)}")
        good = voucher(category_id=taxi_category)
        status, res = call("POST", "/vouchers/preview", {"code": good, "fare_estimate_id": est["fare_estimate_id"]}, customer["tok"])
        chk(status == 200 and res.get("discount_cents") == round(fare * 0.1), f"pré-visualização de 10%: {res.get('discount_cents')} de {fare}")
        status, created = create(customer, est, voucher_code=good, discount_cents=99999, payment_method="gpo")
        discount = round(fare * 0.1)
        chk(status == 201 and created.get("discount_cents") == discount and created.get("amount_due_cents") == fare - discount, f"desconto calculado no servidor, o do payload é ignorado: {created.get('discount_cents')}, a pagar {created.get('amount_due_cents')}")
        chk(sql(f"SELECT discount_cents||'|'||voucher_code FROM service_requests WHERE id='{created.get('id')}'") == f"{discount}|{good}", "guardado na BD")
        second = estimate(customer, STOPS[:1])
        status, res = create(customer, second, STOPS[:1], voucher_code=good)
        chk(code(res) == "VOUCHER_USED", f"segunda utilização pelo mesmo cliente: {status} {code(res)}")
        cancel(customer["tok"], created["id"])
        status, res = create(customer, estimate(customer, STOPS[1:2]), STOPS[1:2], voucher_code=good)
        chk(status == 201, f"cancelar devolve o voucher: {status}")
        cancel(customer["tok"], res.get("id"))
        rider, driver = new_user("customer", "Cliente T8b"), new_user("provider", "Motorista T8", "taxi")
        go_online(driver)
        est = estimate(rider)
        fare = est["total_cents"]
        good = voucher(category_id=taxi_category)
        status, created = create(rider, est, voucher_code=good, payment_method="gpo")
        rid = created["id"]
        accept(driver, wait_offer(driver, rid))
        advance(driver, rid)
        go_offline(driver)
        status, payment = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, rider["tok"])
        discount = round(fare * 0.1)
        chk(payment.get("amount_cents") == fare - discount, f"cobrança real com desconto: {payment.get('amount_cents')}")
        wait_payment(rider, rid, "paid")
        commission = int(sql(f"SELECT commission_cents FROM service_requests WHERE id='{rid}'"))
        balance = int(sql(f"SELECT COALESCE(sum(amount_cents),0) FROM ledger_entries WHERE account_type='provider' AND account_id='{driver['id']}'"))
        chk(balance == fare - commission, f"a AGORA paga o voucher, o motorista recebe {balance}")
        _, view = call("GET", f"/requests/{rid}", None, rider["tok"])
        breakdown = view.get("breakdown", {})
        chk(any(i["code"] == "voucher" and i["amount_cents"] == -discount for i in breakdown.get("items", [])) and breakdown.get("total_cents") == fare - discount, f"aparece no total: {breakdown.get('items')}")


def next_weekday(days_ahead=7):
    return (dt.date.today() + dt.timedelta(days=days_ahead))


def new_vehicle(customer):
    if customer.get("vehicle_id"):
        return customer["vehicle_id"]
    status, vehicle = call("POST", "/users/me/vehicles", {"brand": "Toyota", "model": "Hilux", "plate": "LD-" + customer["phone"][-6:], "year": 2019, "fuel": "diesel"}, customer["tok"], log=False)
    assert status == 201, vehicle
    customer["vehicle_id"] = vehicle["id"]
    return vehicle["id"]


def book(customer, workshop_id, service_ids, slot, **extra):
    body = {"workshop_id": workshop_id, "service_ids": service_ids, "vehicle_id": new_vehicle(customer), "starts_at": slot, "description": "Faz barulho", "mileage_km": 85000, "payment_method": "gpo"}
    body.update(extra)
    return call("POST", "/bookings", body, customer["tok"], idem=str(uuid.uuid4()))


def workshop_fixture(partner, capacity=1, **fields):
    body = {"name": "Oficina QA " + uuid.uuid4().hex[:6], "currency": "AOA", "offers_home_service": True, "home_service_fee_cents": 300000, "address": {"lat": -8.9, "lng": 13.2, "line": "Rua 12, Talatona"}}
    body.update(fields)
    status, workshop = call("POST", "/fleet/workshops", body, partner, PARTNER, log=False)
    assert status == 201, workshop
    tires = sql("SELECT id FROM catalog_items WHERE kind='workshop_category' AND code='tires'")
    status, service = call("POST", f"/fleet/workshops/{workshop['id']}/services", {"name": "Revisão geral", "price_cents": 3500000, "duration_minutes": 120, "category_id": tires}, partner, PARTNER, log=False)
    assert status == 201, service
    status, _ = call("PUT", f"/fleet/workshops/{workshop['id']}/schedule", {"capacity": capacity, "about": "Desde 2010", "warranty": "3 meses", "hours": [{"weekday": d, "open_minute": 0, "close_minute": 1440} for d in range(7)]}, partner, PARTNER, log=False)
    assert status == 204
    return workshop["id"], service["id"]


def slot_of(workshop_id, service_id, customer, days_ahead=7):
    day = next_weekday(days_ahead).isoformat()
    status, view = call("GET", f"/workshops/{workshop_id}/availability?service_ids={service_id}&from={day}", None, customer["tok"], log=False)
    assert status == 200, view
    return view["days"][0]["slots"][0]["starts_at"]


def maintenance(admin, partner):
    with Scenario("M1", "Oficinas: lista, filtro e pesquisa"):
        customer = new_user("customer", "Cliente M1")
        name = "Auto Kianda " + uuid.uuid4().hex[:6]
        body = {"name": name, "currency": "AOA", "offers_home_service": True, "home_service_fee_cents": 300000, "opening_hours": "Seg-Sex 08:00-18:00", "address": {"lat": -8.9, "lng": 13.2, "line": "Rua 12, Talatona"}}
        status, workshop = call("POST", "/fleet/workshops", body, partner, PARTNER)
        chk(status == 201, f"parceiro cria oficina: {status}")
        workshop_id = workshop.get("id")
        tires = sql("SELECT id FROM catalog_items WHERE kind='workshop_category' AND code='tires'")
        for service in ({"name": "Revisão geral", "price_cents": 3500000, "duration_minutes": 120}, {"name": "Óleo e filtro", "price_cents": 1800000, "duration_minutes": 45, "category_id": tires}):
            status, _ = call("POST", f"/fleet/workshops/{workshop_id}/services", service, partner, PARTNER)
            chk(status == 201, f"serviço {service['name']}: {status}")
        status, _ = call("PUT", f"/fleet/workshops/{workshop_id}/schedule", {"capacity": 2, "about": "Desde 2010", "warranty": "3 meses", "hours": [{"weekday": d, "open_minute": 0, "close_minute": 1440} for d in range(7)]}, partner, PARTNER)
        chk(status == 204, f"horário estruturado: {status}")
        status, found = call("GET", f"/workshops?category=tires&search={name.replace(' ', '%20')}&lat=-8.9&lng=13.2", None, customer["tok"])
        ids = [w["id"] for w in found.get("items", [])]
        chk(status == 200 and ids == [workshop_id], f"cliente lista oficinas com categoria e pesquisa: {status} {ids}")
        status, none = call("GET", f"/workshops?category=brakes&search={name.replace(' ', '%20')}&lat=-8.9&lng=13.2", None, customer["tok"])
        chk(status == 200 and none.get("items") == [], f"outra categoria não a devolve: {none.get('items')}")
        status, far = call("GET", f"/workshops?search={name.replace(' ', '%20')}&lat=-8.0&lng=13.2&max_distance_km=5", None, customer["tok"])
        chk(far.get("items") == [], f"fora da distância: {far.get('items')}")
        status, listed = call("GET", f"/admin/workshops?organization_id={PARTNER_ORG}", None, admin, BACKOFFICE)
        chk(status == 200 and listed.get("total", 0) >= 1, f"admin lista: {status} total {listed.get('total')}")
        status, got = call("GET", f"/fleet/workshops/{workshop_id}", None, partner, PARTNER)
        chk(got.get("offers_home_service") is True and len(got.get("services", [])) == 2, "parceiro lê ao domicílio e os 2 serviços")
        status, card = call("GET", f"/workshops/{workshop_id}?lat=-8.9&lng=13.2", None, customer["tok"])
        chk("open_until" in card or "is_open" in card, f"'aberta até' calculado: {card.get('is_open')} {card.get('open_until')}")
        chk(card.get("is_open") is True and card.get("offers_home_service") is True and card.get("warranty") == "3 meses" and len(card.get("services", [])) == 2, "detalhe: aberta, ao domicílio, garantia e serviços")
        chk(call("POST", f"/workshops/{workshop_id}/favorite", None, customer["tok"])[0] == 204, "favorito")
        _, favorites = call("GET", "/workshops?favorites=true&lat=-8.9&lng=13.2", None, customer["tok"])
        chk([w["id"] for w in favorites.get("items", [])] == [workshop_id] and favorites["items"][0]["is_favorite"] is True, "lista de favoritos")

    with Scenario("M2", "Marcação, capacidade e concorrência"):
        customer = new_user("customer", "Cliente M2")
        workshop_id, service_id = workshop_fixture(partner, capacity=1)
        day = next_weekday().isoformat()
        status, cal = call("GET", f"/workshops/{workshop_id}/availability?service_ids={service_id}&from={day}&days=2", None, customer["tok"])
        chk(status == 200 and cal["days"][0]["status"] == "free" and cal["duration_minutes"] == 120, f"horários disponíveis: {status} {cal.get('duration_minutes')}")
        slot = cal["days"][0]["slots"][0]["starts_at"]
        chk(book(customer, workshop_id, [service_id], "2020-01-01T10:00:00Z")[0] == 422, "marcar no passado é recusado")
        rivals = [new_user("customer", f"Rival {i}") for i in range(5)]
        vehicles = {r["id"]: new_vehicle(r) for r in rivals}
        def race(rival):
            return call("POST", "/bookings", {"workshop_id": workshop_id, "service_ids": [service_id], "vehicle_id": vehicles[rival["id"]], "starts_at": slot, "payment_method": "gpo"}, rival["tok"], idem=str(uuid.uuid4()), log=False)
        with cf.ThreadPoolExecutor(5) as pool:
            outcomes = list(pool.map(race, rivals))
        won = [r for r in outcomes if r[0] == 201]
        full = [r for r in outcomes if code(r[1]) == "BOOKING_SLOT_FULL"]
        chk(len(won) == 1 and len(full) == 4, f"corrida pelo último lugar: {[o[0] for o in outcomes]}")
        count = sql(f"SELECT count(*) FROM bookings WHERE workshop_id='{workshop_id}' AND starts_at='{slot}' AND status<>'cancelled'")
        chk(count == "1", f"uma só marcação na BD: {count}")
        booking = won[0][1]
        chk(re.fullmatch(r"MK-\d{4,}", booking.get("code", "")), f"código MK: {booking.get('code')}")
        status, after = call("GET", f"/workshops/{workshop_id}/availability?service_ids={service_id}&from={day}", None, customer["tok"])
        chk(slot not in [s["starts_at"] for s in after["days"][0]["slots"]], "o horário cheio desaparece")
        idem = str(uuid.uuid4())
        body = {"workshop_id": workshop_id, "service_ids": [service_id], "vehicle_id": new_vehicle(customer), "starts_at": cal["days"][1]["slots"][0]["starts_at"], "payment_method": "gpo"}
        first, second = call("POST", "/bookings", body, customer["tok"], idem=idem), call("POST", "/bookings", body, customer["tok"], idem=idem)
        chk(first[1].get("id") == second[1].get("id") and first[0] == 201, f"mesma chave não duplica: {first[0]} {second[0]}")
        block = next_weekday(10).isoformat()
        chk(call("POST", f"/fleet/workshops/{workshop_id}/closures", {"date": block, "reason": "Feriado"}, partner, PARTNER)[0] == 204, "bloquear um dia")
        status, closed = call("GET", f"/workshops/{workshop_id}/availability?service_ids={service_id}&from={block}", None, customer["tok"])
        chk(closed["days"][0]["status"] == "closed", f"dia fechado: {closed['days'][0]['status']}")
        chk(call("GET", f"/fleet/workshops/{workshop_id}/bookings", None, new_user("customer", "Intruso M2")["tok"])[0] in (401, 403), "cliente não usa rotas de parceiro")
        chk(call("GET", f"/bookings/{booking['id']}", None, customer["tok"])[0] == 404, "outro cliente não vê a marcação")
        chk(call("POST", "/requests", {"service_id": sql("SELECT id FROM services WHERE slug='maintenance'"), "pickup": PICKUP}, customer["tok"], idem=str(uuid.uuid4()))[1].get("error", {}).get("code") == "USE_BOOKINGS", "técnico ao local aposentado")
        status, ics = fetch_raw(f"/bookings/{first[1]['id']}/calendar", customer["tok"])
        chk(status == 200 and b"BEGIN:VCALENDAR" in ics and first[1]["code"].encode() in ics, f"dados de calendário: {status}")
        time.sleep(2)
        chk(any(first[1]["code"] in b for b in sms_to(customer["phone"])), "SMS da marcação com o código")

    with Scenario("M3", "Ao domicílio e promoção"):
        customer = new_user("customer", "Cliente M3")
        workshop_id, service_id = workshop_fixture(partner, capacity=3)
        now = dt.datetime.now(dt.timezone.utc)
        promo = {"description": "Primeira revisão -15%", "discount_bps": 1500, "valid_from": (now - dt.timedelta(days=1)).isoformat(), "valid_until": (now + dt.timedelta(days=30)).isoformat(), "per_customer_limit": 1, "is_public": True, "workshop_id": workshop_id, "workshop_service_id": service_id, "auto_apply": True}
        status, voucher = call("POST", "/admin/vouchers", promo, admin, BACKOFFICE)
        chk(status == 201, f"admin cria a promoção: {status}")
        expired = dict(promo, description="Expirada", discount_bps=9000, valid_from=(now - dt.timedelta(days=5)).isoformat(), valid_until=(now - dt.timedelta(days=1)).isoformat())
        chk(call("POST", "/admin/vouchers", expired, admin, BACKOFFICE)[0] == 201, "promoção expirada existe")
        _, card = call("GET", f"/workshops/{workshop_id}?lat=-8.9&lng=13.2", None, customer["tok"])
        chk([p["description"] for p in card.get("promotions", [])] == ["Primeira revisão -15%"], f"só a válida aparece: {card.get('promotions')}")
        slot = slot_of(workshop_id, service_id, customer)
        status, booked = book(customer, workshop_id, [service_id], slot, at_home=True, home_lat=-8.84, home_lng=13.24, home_address="Casa")
        expected = 3500000 + 300000
        discount = round(expected * 0.15)
        chk(status == 201 and booked.get("home_fee_cents") == 300000 and booked.get("discount_cents") == discount and booked.get("total_cents") == expected - discount, f"taxa e desconto: {booked.get('home_fee_cents')} {booked.get('discount_cents')} {booked.get('total_cents')}")
        status, again = book(customer, workshop_id, [service_id], slot)
        chk(status == 201 and again.get("discount_cents") == 0, f"uma vez por cliente: desconto {again.get('discount_cents')}")
        _, card = call("GET", f"/workshops/{workshop_id}?lat=-8.9&lng=13.2", None, customer["tok"])
        chk(card.get("promotions") == [], f"já não aparece a quem a usou: {card.get('promotions')}")
        reason = reason_id(customer["tok"], "customer", "maintenance")
        status, _ = call("POST", f"/bookings/{booked['id']}/cancel", {"reason_id": reason}, customer["tok"])
        chk(status == 200, f"cancelar devolve a promoção: {status}")
        chk(sql(f"SELECT released_at IS NOT NULL FROM voucher_redemptions WHERE request_id='{booked['request_id']}'") == "t", "resgate libertado (BD)")
        other = new_user("customer", "Cliente M3b")
        no_home, service_b = workshop_fixture(partner, offers_home_service=False, home_service_fee_cents=0)
        status, res = book(other, no_home, [service_b], slot_of(no_home, service_b, other), at_home=True, home_lat=-8.84, home_lng=13.24)
        chk(code(res) == "HOME_SERVICE_UNAVAILABLE", f"sem domicílio recusa: {status} {code(res)}")

    with Scenario("M4", "Fotos no R2"):
        customer = new_user("customer", "Cliente M4")
        workshop_id, service_id = workshop_fixture(partner, capacity=3)
        status, booked = book(customer, workshop_id, [service_id], slot_of(workshop_id, service_id, customer))
        rid = booked.get("request_id")
        status, res = call("POST", "/media/photos", {"content_type": "image/jpeg", "size_bytes": 1000}, customer["tok"])
        chk(status == 201 and res.get("upload_url"), f"URL pré-assinada para fotos do pedido: {status}")
        chk(any("photo" in p for p in OPENAPI), "rota de fotos no OpenAPI")
        chk(code(call("POST", "/media/photos", {"content_type": "application/pdf", "size_bytes": 1000}, customer["tok"])[1]) == "PHOTO_TYPE_NOT_ALLOWED", "tipo validado")
        status, res = call("POST", f"/requests/{rid}/photos", {"photo_ids": [str(uuid.uuid4())]}, customer["tok"])
        chk(status == 404, f"foto inexistente: {status}")
        ids = []
        for _ in range(6):
            status, photo = call("POST", "/media/photos", {"content_type": "image/jpeg", "size_bytes": 5}, customer["tok"], log=False)
            req = urllib.request.Request(photo["upload_url"], data=b"hello", method="PUT", headers={"Content-Type": "image/jpeg"})
            with urllib.request.urlopen(req, timeout=30) as sent:
                assert sent.status in (200, 204)
            ids.append(photo["id"])
        status, res = call("POST", f"/requests/{rid}/photos", {"photo_ids": ids[:5]}, customer["tok"])
        chk(status == 200 and len(res.get("items", [])) == 5, f"5 fotos no R2: {status}")
        status, res = call("POST", f"/requests/{rid}/photos", {"photo_ids": ids[5:]}, customer["tok"])
        chk(code(res) == "PHOTO_LIMIT", f"a 6.ª foto é recusada: {status} {code(res)}")

    with Scenario("M5", "Estados e orçamento extra"):
        customer = new_user("customer", "Cliente M5")
        workshop_id, service_id = workshop_fixture(partner, capacity=3)
        status, booked = book(customer, workshop_id, [service_id], slot_of(workshop_id, service_id, customer))
        bid, rid = booked["id"], booked["request_id"]
        statuses = sql("SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname LIKE 'bookings_status%' LIMIT 1")
        chk("vehicle_received" in statuses or "ready" in statuses, f"estados de oficina existem: {statuses[:150]}")
        chk(any("extra" in p or "budget" in p for p in OPENAPI), "rota de orçamento extra")
        move = lambda to: call("POST", f"/fleet/bookings/{bid}/status", {"status": to}, partner, PARTNER)
        chk(code(move("ready")[1]) == "BOOKING_STATE_INVALID", "salto de estado é recusado")
        chk(code(call("POST", f"/fleet/bookings/{bid}/extras", {"description": "Cedo", "amount_cents": 100}, partner, PARTNER)[1]) == "BOOKING_STATE_INVALID", "extra antes de a viatura chegar")
        status, mechanic = call("POST", f"/fleet/workshops/{workshop_id}/mechanics", {"name": "Domingos Paulo"}, partner, PARTNER)
        chk(status == 201, f"mecânico sem conta: {status}")
        ready_at = (dt.datetime.now(dt.timezone.utc) + dt.timedelta(days=8)).isoformat()
        status, assigned = call("PUT", f"/fleet/bookings/{bid}/assignment", {"mechanic_id": mechanic["id"], "expected_ready_at": ready_at}, partner, PARTNER)
        chk(status == 200 and assigned.get("mechanic") == "Domingos Paulo" and assigned.get("expected_ready_at"), f"mecânico e hora prevista: {status}")
        chk(move("vehicle_received")[0] == 200, "Viatura recebida")
        _, active = call("GET", "/requests?status=in_progress", None, customer["tok"])
        chk(rid in [i["id"] for i in active.get("items", [])], "aparece em curso")
        reason = reason_id(customer["tok"], "customer", "maintenance")
        chk(code(call("POST", f"/bookings/{bid}/cancel", {"reason_id": reason}, customer["tok"])[1]) == "BOOKING_NOT_CANCELLABLE", "com a viatura na oficina já não cancela")
        chk(code(call("POST", f"/requests/{rid}/cancel", {"reason_id": reason}, customer["tok"])[1]) == "BOOKING_MANAGED", "o pedido associado não cancela à parte")
        chk(move("in_service")[0] == 200, "Em serviço")
        _, extra = call("POST", f"/fleet/bookings/{bid}/extras", {"description": "Pastilhas", "amount_cents": 1200000}, partner, PARTNER)
        _, other = call("POST", f"/fleet/bookings/{bid}/extras", {"description": "Filtro de ar", "amount_cents": 500000}, partner, PARTNER)
        approved_id, declined_id = extra["extras"][0]["id"], other["extras"][1]["id"]
        chk(code(move("ready")[1]) == "BOOKING_EXTRAS_PENDING", "sem resposta ao extra não fica pronta")
        chk(code(call("POST", f"/bookings/{bid}/approve", None, customer["tok"])[1]) == "BOOKING_EXTRAS_PENDING", "nem se aprova")
        intruder = new_user("customer", "Intruso M5")
        chk(call("POST", f"/bookings/{bid}/extras/{approved_id}/answer", {"approve": True}, intruder["tok"])[0] == 404, "outro cliente não responde")
        status, view = call("POST", f"/bookings/{bid}/extras/{approved_id}/answer", {"approve": True}, customer["tok"])
        chk(view.get("total_cents") == 3500000 + 1200000, f"aprovado entra no total: {view.get('total_cents')}")
        status, view = call("POST", f"/bookings/{bid}/extras/{declined_id}/answer", {"approve": False}, customer["tok"])
        chk(view.get("total_cents") == 3500000 + 1200000 and view.get("extras_cents") == 1200000, f"recusado não entra no total: {view.get('total_cents')}")
        chk(code(call("POST", f"/bookings/{bid}/extras/{declined_id}/answer", {"approve": True}, customer["tok"])[1]) == "EXTRA_ALREADY_ANSWERED", "não se responde duas vezes")
        chk(call("POST", f"/bookings/{bid}/messages", {"body": "Quando fica pronta?"}, customer["tok"])[0] == 201, "chat do cliente")
        chk(call("POST", f"/fleet/bookings/{bid}/messages", {"body": "Amanhã às 10h"}, partner, PARTNER)[0] == 201, "chat da oficina")
        _, chat = call("GET", f"/bookings/{bid}/messages", None, customer["tok"])
        chk(len(chat.get("items", [])) == 2, f"mensagens: {len(chat.get('items', []))}")
        status, res = call("POST", f"/bookings/{bid}/call", None, customer["tok"])
        chk(status == 503 and code(res) == "CALLS_UNAVAILABLE", f"chamada mascarada sem fornecedor: {status} {code(res)}")
        chk(move("ready")[0] == 200, "Pronta para levantar")
        chk(sql(f"SELECT string_agg(to_status, ',' ORDER BY id) FROM booking_events WHERE booking_id='{bid}'") == "confirmed,vehicle_received,in_service,ready", "histórico de estados (BD)")

    with Scenario("M6", "Só cobra depois de aprovar"):
        customer = new_user("customer", "Cliente M6")
        workshop_id, service_id = workshop_fixture(partner, capacity=3)
        status, booked = book(customer, workshop_id, [service_id], slot_of(workshop_id, service_id, customer))
        bid, rid = booked["id"], booked["request_id"]
        status, res = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"])
        chk(status == 409 and code(res) == "REQUEST_NOT_PAYABLE", f"pedido não concluído não se paga: {status} {code(res)}")
        chk(any("approve" in p and "booking" in p for p in OPENAPI), "passo de aprovação do serviço pelo cliente")
        for to in ("vehicle_received", "in_service", "ready"):
            assert call("POST", f"/fleet/bookings/{bid}/status", {"status": to}, partner, PARTNER, log=False)[0] == 200
        status, res = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"])
        chk(status == 409 and code(res) == "REQUEST_NOT_PAYABLE", f"pronta, mas sem aprovação, não se paga: {status} {code(res)}")
        chk(call("POST", f"/bookings/{bid}/approve", None, new_user("customer", "Intruso M6")["tok"])[0] == 404, "outro cliente não aprova")
        status, view = call("POST", f"/bookings/{bid}/approve", None, customer["tok"])
        chk(status == 200 and view.get("status") == "completed" and view.get("amount_due_cents") == 3500000, f"aprovar: {status} {view.get('status')} {view.get('amount_due_cents')}")
        sql(f"UPDATE service_requests SET commission_cents=350000 WHERE id='{rid}'")
        status, payment = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK, "amount_cents": 1}, customer["tok"])
        chk(status == 201 and payment.get("amount_cents") == 3500000, f"Express com o valor do servidor: {status} {payment.get('amount_cents')}")
        chk(wait_payment(customer, rid, "paid"), "pago depois da confirmação da AppyPay")
        commission = int(sql(f"SELECT commission_cents FROM service_requests WHERE id='{rid}'"))
        balance = int(sql(f"SELECT COALESCE(sum(amount_cents),0) FROM ledger_entries WHERE account_type='workshop' AND account_id='{workshop_id}'"))
        chk(commission == 350000 and balance == 3500000 - commission, f"ledger da oficina: {balance} com comissão {commission}")
        status, rating = call("POST", f"/requests/{rid}/rating", {"stars": 5, "comment": "Bom"}, customer["tok"])
        chk(status == 201, f"avaliar: {status}")
        _, reviews = call("GET", f"/workshops/{workshop_id}/reviews", None, customer["tok"])
        chk(reviews.get("average") == 5 and reviews.get("total") == 1, f"avaliações da oficina: {reviews.get('average')} {reviews.get('total')}")
        status, pdf = fetch_raw(f"/requests/{rid}/receipt", customer["tok"])
        chk(status == 200 and pdf.startswith(b"%PDF"), f"recibo: {status}")
        ref_customer = new_user("customer", "Cliente M6b")
        status, res = book(ref_customer, workshop_id, [service_id], slot_of(workshop_id, service_id, ref_customer), payment_method="reference")
        chk(status == 201 and res.get("status") == "awaiting_payment", f"com referência espera pagamento: {status} {res.get('status')}")
        status, gpo = call("POST", f"/requests/{res['request_id']}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, ref_customer["tok"])
        chk(status == 409, f"sem Express antes de aprovar: {status} {code(gpo)}")


def towing_details(tok):
    status, options = call("GET", "/towing/options", None, tok, log=False)
    assert status == 200, options
    incident = next(i["id"] for i in options["incidents"] if i["code"] == "breakdown")
    return {"incident_id": incident, "drivable": False}


def tow_option(customer, code_="light", dest=DEST):
    label = sql(f"SELECT label FROM catalog_items WHERE kind='towing_vehicle' AND code='{code_}'")
    status, res = call("POST", "/fare-estimates", {"category": "towing", "pickup": PICKUP, "destination": dest}, customer["tok"])
    assert status == 201, res
    return next(i for i in res["items"] if i.get("vehicle_type") == label)


def towing():
    with Scenario("R1", "Preço por tipo de viatura e distância"):
        customer = new_user("customer", "Cliente R1")
        near = {c: tow_option(customer, c) for c in ("light", "suv_pickup", "motorcycle")}
        prices = {c: near[c]["total_cents"] for c in near}
        chk(near["light"]["distance_m"] <= 10000 and prices == {"light": 2500000, "suv_pickup": 3200000, "motorcycle": 1500000}, f"dentro dos km incluídos: Ligeiro 25 000, SUV 32 000, Motociclo 15 000 Kz: {prices}")
        far_dest = {"lat": -8.9400, "lng": 13.1500}
        far = tow_option(customer, "light", far_dest)
        extra = round((far["distance_m"] - 10000) * 50000 / 1000)
        chk(far["distance_m"] > 10000 and abs(far["total_cents"] - (2500000 + extra)) <= 1, f"km a mais: {far['distance_m']} m, {far['total_cents']} = 2 500 000 + {extra}")
        status, options = call("GET", f"/towing/options?lat={PICKUP['lat']}&lng={PICKUP['lng']}", None, customer["tok"])
        chk(status == 200 and options.get("incidents") and options.get("recommended_workshop", {}).get("id"), f"incidentes e oficina recomendada: {options.get('recommended_workshop', {}).get('name')}")
        status, res = create(customer, near["light"])
        chk(code(res) == "TOWING_DETAILS_REQUIRED", f"o que aconteceu é obrigatório: {status} {code(res)}")
        details = towing_details(customer["tok"])
        status, res = create(customer, near["light"], towing={"incident_id": str(uuid.uuid4()), "drivable": True})
        chk(code(res) == "INCIDENT_NOT_FOUND", f"incidente inexistente: {status} {code(res)}")
        status, created = create(customer, near["suv_pickup"], towing=details)
        chk(status == 201 and created.get("fare_cents") == 3200000 and created["details"]["towing"]["vehicle_type"] == "SUV/Pick-up" and created["details"]["towing"]["drivable"] is False, f"pedido com tipo, avaria e se anda: {status} {created.get('fare_cents')}")
        cancel(customer["tok"], created.get("id"))

    with Scenario("R2", "Cancelar durante a procura"):
        customer = new_user("customer", "Cliente R2")
        status, created = create(customer, tow_option(customer), towing=towing_details(customer["tok"]))
        chk(status == 201, f"pedido: {status} {created.get('status')}")
        status, preview = call("GET", f"/requests/{created['id']}/cancellation", None, customer["tok"])
        chk(preview.get("fee_cents") == 0, f"pré-aviso durante a procura: custo {preview.get('fee_cents')}")
        status, res = call("POST", f"/requests/{created['id']}/cancel", {"reason_id": reason_id(customer["tok"], "customer", "towing", "solved")}, customer["tok"])
        chk(status == 200 and res.get("status") == "cancelled" and res.get("cancel_fee_cents") == 0, f"cancelar: {status} {res.get('status')}, custo {res.get('cancel_fee_cents')}")

    with Scenario("R3", "Código de verificação da entrega"):
        customer, driver = new_user("customer", "Cliente R3"), new_user("provider", "Reboque R3", "towing", kind="tow_truck")
        go_online(driver)
        status, created = create(customer, tow_option(customer), towing=towing_details(customer["tok"]))
        rid = created["id"]
        real = created.get("completion_code", "")
        chk(re.fullmatch(r"\d{4}", real), "o cliente recebe um código de 4 dígitos")
        chk(sql(f"SELECT completion_code FROM service_requests WHERE id='{rid}'") == real, "código na BD")
        offer = wait_offer(driver, rid)
        chk(real not in json.dumps(offer), "o motorista não vê o código na oferta")
        accept(driver, offer)
        advance(driver, rid, ("en-route", "arrive", "start"))
        wrong = "0000" if real != "0000" else "1111"
        status, res = call("POST", f"/requests/{rid}/complete", {"code": wrong}, driver["tok"], PROVIDER)
        chk(code(res) == "COMPLETION_CODE_WRONG", f"código errado recusa: {status} {code(res)}")
        chk(sql(f"SELECT status FROM service_requests WHERE id='{rid}'") == "in_progress", "o pedido continua em curso (BD)")
        status, res = call("POST", f"/requests/{rid}/complete", {"code": real}, driver["tok"], PROVIDER)
        chk(status == 200 and res.get("status") == "completed", f"código certo conclui: {status} {res.get('status')}")
        status, options = call("GET", "/rating-options?category=towing", None, customer["tok"])
        chk([c["label"] for c in options.get("compliments", [])] == ["Chegou rápido", "Cuidadoso", "Profissional", "Boa comunicação"], f"elogios do reboque: {[c['label'] for c in options.get('compliments', [])]}")
        chk([t["amount_cents"] for t in options.get("tips", [])] == [50000, 100000, 200000], f"gorjetas 500, 1 000 e 2 000 Kz: {[t['amount_cents'] for t in options.get('tips', [])]}")
        go_offline(driver)


def removal_options(customer):
    status, options = call("GET", "/removals/options", None, customer["tok"], log=False)
    assert status == 200, options
    return options


def removal_estimate(customer, when=None, helpers=0, extras=None, items=None, vehicle="Camião 3,5 t"):
    body = {"category": "removals", "pickup": PICKUP, "destination": DEST, "removal": {"helpers": helpers, "extras": extras or [], "items": items or []}}
    if when:
        body["scheduled_for"] = when
    status, res = call("POST", "/fare-estimates", body, customer["tok"])
    assert status == 201, res
    return next(i for i in res["items"] if i["vehicle_type"] == vehicle) if vehicle else res["items"]


def window_at(options, code_, days_ahead=10):
    luanda = dt.timezone(dt.timedelta(hours=1))
    window = next(w for w in options["windows"] if w["code"] == code_)
    minutes = {"morning": 480, "midday": 660, "afternoon": 840}[code_]
    day = dt.datetime.now(luanda).date() + dt.timedelta(days=days_ahead)
    return window, dt.datetime.combine(day, dt.time(minutes // 60, minutes % 60), luanda).isoformat()


def removal_request(customer, est, window, when, method="gpo", **fields):
    body = {"service_id": est["service_id"], "pickup": PICKUP, "destination": DEST, "fare_estimate_id": est["fare_estimate_id"], "scheduled_for": when, "payment_method": method,
            "removal": {"window_id": window["id"], "property_code": "home", "pickup_floor": 3, "pickup_elevator": False, "destination_floor": 1, "destination_elevator": True, "contact_name": "Ana", "notes": "Portão azul"}}
    body.update(fields)
    return call("POST", "/requests", body, customer["tok"], idem=str(uuid.uuid4()))


def removals():
    with Scenario("D1", "Volume e viatura recomendada"):
        customer = new_user("customer", "Cliente D1")
        options = removal_options(customer)
        chk(len(options["items"]) >= 10 and {i["group"] for i in options["items"]} >= {"Sala", "Quarto", "Cozinha"}, f"catálogo por divisão: {len(options['items'])}")
        by_code = {i["code"]: i for i in options["items"]}
        items = [{"item_id": by_code["sofa"]["id"], "qty": 2}, {"item_id": by_code["bed"]["id"], "qty": 1}, {"item_id": by_code["wardrobe"]["id"], "qty": 2}, {"item_id": by_code["box"]["id"], "qty": 10}]
        volume = 2 * 1500 + 1600 + 2 * 1800 + 10 * 100
        window, when = window_at(options, "morning")
        status, quoted = call("POST", "/fare-estimates", {"category": "removals", "pickup": PICKUP, "destination": DEST, "scheduled_for": when, "removal": {"items": items, "helpers": 0, "extras": []}}, customer["tok"])
        chk(status == 201 and all(q["removal"]["volume_dm3"] == volume for q in quoted["items"]), f"volume no servidor: {[q['removal']['volume_dm3'] for q in quoted.get('items', [])]} = {volume}")
        chk([q["vehicle_type"] for q in quoted["items"] if q["removal"]["recommended"]] == ["Camião 3,5 t"], f"recomendada: {[q['vehicle_type'] for q in quoted['items'] if q['removal']['recommended']]}")
        chk("Carrinha" not in [q["vehicle_type"] for q in quoted["items"]], "a carrinha não leva 9,2 m³")
        chk(any("item" in p or "volume" in p or "removals/options" in p for p in OPENAPI), "catálogo de artigos e cálculo de volume")
        est = next(q for q in quoted["items"] if q["removal"]["recommended"])
        status, created = removal_request(customer, est, window, when, details={"items": [{"name": "Sofá", "qty": 1}], "volume_m3": 999, "total_cents": 1})
        chk(status == 201, f"pedido: {status} {created.get('status')}")
        chk(created.get("details", {}).get("volume_m3") != 999, "o volume não pode vir da app: ignorou o valor enviado (999)")
        chk(created.get("details", {}).get("removal", {}).get("volume_dm3") == volume, f"guardou o volume do servidor: {created.get('details', {}).get('removal', {}).get('volume_dm3')}")
        chk(created.get("fare_cents") == est["total_cents"], f"preço fechado: {created.get('fare_cents')}")
        places = created.get("details", {}).get("removal", {}).get("places", {})
        chk(places.get("pickup_floor") == 3 and places.get("destination_elevator") is True and created["details"]["removal"].get("property") == "Casa", f"andar, elevador e tipo: {places}")
        chk(created["details"]["removal"].get("duration_minutes") == 180 + 10 * volume // 1000 and created["details"]["removal"].get("insurance_cents") == 50000000, "duração e seguro informativo")
        status, res = call("POST", "/fare-estimates", {"category": "removals", "pickup": PICKUP, "destination": DEST, "removal": {"items": [{"item_id": str(uuid.uuid4()), "qty": 1}]}}, customer["tok"])
        chk(code(res) == "REMOVAL_ITEM_NOT_FOUND", f"artigo inexistente: {status} {code(res)}")
        status, res = call("POST", "/fare-estimates", {"category": "removals", "pickup": PICKUP, "destination": DEST, "removal": {"items": [{"item_id": by_code["wardrobe"]["id"], "qty": 400}]}}, customer["tok"])
        chk(code(res) == "REMOVAL_TOO_LARGE", f"maior do que qualquer viatura: {status} {code(res)}")
        cancel(customer["tok"], created["id"])

    with Scenario("D2", "Total de viatura, ajudantes e extras"):
        customer = new_user("customer", "Cliente D2")
        status, res = call("POST", "/fare-estimates", {"category": "removals", "pickup": PICKUP, "destination": DEST, "removal": {"helpers": 2, "extras": ["packing"]}}, customer["tok"])
        chk(status == 201 and any(i["total_cents"] == 11000000 for i in res.get("items", [])), f"85 000 + 2×5 000 + 15 000 = 110 000 Kz; API: {[i['total_cents'] for i in res.get('items', [])]}")
        info("política de mudanças: " + sql("SELECT pricing_mode,offer_mode FROM dispatch_policies p JOIN service_categories c ON c.id=p.category_id WHERE c.slug='removals'"))
        chk(sql("SELECT pricing_mode||'/'||offer_mode FROM dispatch_policies p JOIN service_categories c ON c.id=p.category_id WHERE c.slug='removals'") == "fixed/parallel", "propostas aposentadas: preço fixo")
        prices = {i["vehicle_type"]: i["removal"]["vehicle_cents"] for i in res["items"]}
        chk(prices == {"Carrinha": 4500000, "Camião 3,5 t": 8500000, "Camião 7 t": 14000000}, f"preços das viaturas: {prices}")
        status, too_many = call("POST", "/fare-estimates", {"category": "removals", "pickup": PICKUP, "destination": DEST, "removal": {"helpers": 7}}, customer["tok"])
        chk(code(too_many) == "REMOVAL_TOO_MANY_HELPERS", f"limite de ajudantes: {status} {code(too_many)}")
        status, wrong = call("POST", "/fare-estimates", {"category": "removals", "pickup": PICKUP, "destination": DEST, "removal": {"extras": ["ouro"]}}, customer["tok"])
        chk(code(wrong) == "REMOVAL_EXTRA_NOT_FOUND", f"extra inexistente: {status} {code(wrong)}")
        van = sql("SELECT id FROM catalog_items WHERE kind='removal_vehicle' AND code='van'")
        until = (dt.datetime.now(dt.timezone.utc) + dt.timedelta(days=2)).isoformat()
        status, _ = call("PUT", f"/admin/catalog/removal_vehicle/{van}", {"code": "van", "label": "Carrinha", "description": "Até 6 m³", "amount_cents": 4500000, "promo_amount_cents": 4000000, "promo_until": until, "display_order": 1, "attrs": {"max_volume_dm3": 6000, "base_minutes": 120, "minutes_per_m3": 10, "daily_capacity": 6}}, ADMIN[0], BACKOFFICE)
        chk(status == 200, f"promoção da viatura no backoffice: {status}")
        promo = removal_estimate(customer, vehicle="Carrinha")
        chk(promo["total_cents"] == 4000000, f"preço promocional: {promo['total_cents']}")
        call("PUT", f"/admin/catalog/removal_vehicle/{van}", {"code": "van", "label": "Carrinha", "description": "Até 6 m³", "amount_cents": 4500000, "display_order": 1, "attrs": {"max_volume_dm3": 6000, "base_minutes": 120, "minutes_per_m3": 10, "daily_capacity": 6}}, ADMIN[0], BACKOFFICE, log=False)
        final = removal_estimate(customer, helpers=2, extras=["packing"], when=None)
        options = removal_options(customer)
        window, when = window_at(options, "midday", 11)
        sched = removal_estimate(customer, when=when, helpers=2, extras=["packing"])
        status, created = removal_request(customer, sched, window, when)
        chk(status == 201 and created.get("fare_cents") == 11000000, f"preço fechado na marcação: {created.get('fare_cents')}")
        sql(f"UPDATE catalog_items SET amount_cents=amount_cents+100 WHERE kind='removal_vehicle' AND code='truck_3t'")
        _, view = call("GET", f"/requests/{created['id']}", None, customer["tok"])
        sql(f"UPDATE catalog_items SET amount_cents=amount_cents-100 WHERE kind='removal_vehicle' AND code='truck_3t'")
        chk(view.get("fare_cents") == 11000000, f"mudar o catálogo não muda a marcação: {view.get('fare_cents')}")
        status, res = call("POST", f"/requests/{created['id']}/route", {"destination": STOPS[0]}, customer["tok"])
        chk(status >= 400, f"o preço fechado não muda com a rota: {status} {code(res)}")
        cancel(customer["tok"], created["id"])

    with Scenario("D3", "Janelas e cancelamento a 24 h"):
        customer = new_user("customer", "Cliente D3")
        service = sql("SELECT id FROM services WHERE slug='removals'")
        options = removal_options(customer)
        luanda = dt.timezone(dt.timedelta(hours=1))
        now_local = dt.datetime.now(luanda)
        soon = None
        for offset in (0, 1):
            for w in options["windows"]:
                minutes = {"morning": 480, "midday": 660, "afternoon": 840}[w["code"]]
                candidate = dt.datetime.combine(now_local.date() + dt.timedelta(days=offset), dt.time(minutes // 60, minutes % 60), luanda)
                if dt.timedelta(hours=1) < candidate - now_local < dt.timedelta(hours=23):
                    soon, soon_window = candidate.isoformat(), w
                    break
            if soon:
                break
        est = removal_estimate(customer, when=soon)
        status, created = removal_request(customer, est, soon_window, soon)
        chk(status == 201, f"marcar a menos de 24 h: {status} {code(created)}")
        status, preview = call("GET", f"/requests/{created['id']}/cancellation", None, customer["tok"])
        chk(preview.get("fee_cents") == 1500000, f"a menos de 24 h paga a taxa: {preview.get('fee_cents')}")
        reason = preview["reasons"][0]["id"]
        status, res = call("POST", f"/requests/{created['id']}/cancel", {"reason_id": reason}, customer["tok"])
        chk(status == 409 and code(res) == "CANCEL_FEE_CHANGED", f"sem confirmar a taxa não cancela: {status} {code(res)}")
        status, res = cancel(customer["tok"], created.get('id'))
        chk(status == 200 and ("fee_cents" in res or "cancel_fee_cents" in res) and res.get("cancel_fee_cents") == 1500000, f"a menos de 24 h aplica a regra: {status}, taxa {res.get('cancel_fee_cents')}")
        chk(any("window" in p or "slot" in p or "availability" in p for p in OPENAPI), "janelas com capacidade")
        far_customer = new_user("customer", "Cliente D3b")
        window, when = window_at(options, "morning", 20)
        est = removal_estimate(far_customer, when=when)
        status, far = removal_request(far_customer, est, window, when)
        status, preview = call("GET", f"/requests/{far['id']}/cancellation", None, far_customer["tok"])
        chk(preview.get("fee_cents") == 0, f"a mais de 24 h é grátis: {preview.get('fee_cents')}")
        _, wrong_when = window_at(options, "afternoon", 20)
        wrong_est = removal_estimate(far_customer, when=wrong_when)
        status, res = removal_request(far_customer, wrong_est, window, wrong_when)
        chk(code(res) == "REMOVAL_WINDOW_INVALID", f"hora fora da janela: {status} {code(res)}")
        status, res = call("POST", "/requests", {"service_id": service, "pickup": PICKUP, "destination": DEST, "fare_estimate_id": wrong_est["fare_estimate_id"], "scheduled_for": wrong_when}, far_customer["tok"], idem=str(uuid.uuid4()))
        chk(status >= 400, f"sem janela nem tipo recusa: {status} {code(res)}")
        cancel(far_customer["tok"], far["id"])
        truck = sql("SELECT id FROM catalog_items WHERE kind='removal_vehicle' AND code='truck_7t'")
        capacity = int(sql(f"SELECT attrs->>'daily_capacity' FROM catalog_items WHERE id='{truck}'"))
        window, when = window_at(options, "afternoon", 30)
        day = when[:10]
        rivals = [new_user("customer", f"Rival D3 {i}") for i in range(capacity + 3)]
        estimates = {r["id"]: removal_estimate(r, when=when, vehicle="Camião 7 t") for r in rivals}
        with cf.ThreadPoolExecutor(len(rivals)) as pool:
            outcomes = list(pool.map(lambda r: removal_request(r, estimates[r["id"]], window, when), rivals))
        statuses = [o[0] for o in outcomes]
        chk(statuses.count(201) == capacity and sum(1 for o in outcomes if code(o[1]) == "REMOVAL_DAY_FULL") == 3, f"capacidade diária sob concorrência: {statuses}")
        status, cal = call("GET", f"/removals/availability?vehicle_id={truck}&from={day}", None, customer["tok"])
        chk(cal["days"][0]["status"] == "full", f"dia Esgotado: {cal['days'][0]['status']}")
        for r, o in zip(rivals, outcomes):
            if o[0] == 201:
                cancel(r["tok"], o[1]["id"])
        status, cal = call("GET", f"/removals/availability?vehicle_id={truck}&from={day}", None, customer["tok"])
        chk(cal["days"][0]["status"] == "free", f"cancelar liberta o dia: {cal['days'][0]['status']}")
        ref = new_user("customer", "Cliente D3c")
        window, when = window_at(options, "morning", 25)
        est = removal_estimate(ref, when=when)
        status, res = removal_request(ref, est, window, when, method="reference")
        rid = res.get("id")
        chk(status == 201 and sql(f"SELECT next_action_at IS NULL FROM service_requests WHERE id='{rid}'") == "t", f"com referência não procura motorista antes de pagar: {status}")
        status, gpo = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, ref["tok"])
        chk(status == 409, f"antes do fim, só a referência: {status} {code(gpo)}")
        cancel(ref["tok"], rid)
        team_customer, team_driver = new_user("customer", "Cliente D3d"), new_user("provider", "Equipa D3", "removals", kind="truck")
        window, when = window_at(options, "morning", 26)
        est = removal_estimate(team_customer, when=when)
        status, created = removal_request(team_customer, est, window, when)
        chk(status == 201, f"mudança para a equipa: {status} {code(created)}")
        sql(f"UPDATE service_requests SET provider_id='{team_driver['id']}', status='accepted', next_action_at=NULL WHERE id='{created['id']}'")
        members = {"members": [{"name": "Paulo", "role": "lead", "verified": True}, {"name": "João", "role": "helper"}]}
        status, res = call("PUT", f"/requests/{created['id']}/team", members, team_driver["tok"], PROVIDER)
        chk(status == 200 and all(m["verified"] is False for m in res.get("items", [])), f"o motorista não verifica ninguém: {status} {res.get('items')}")
        chk(call("PUT", f"/requests/{created['id']}/team", members, new_user("provider", "Outro D3", "removals", kind="truck")["tok"], PROVIDER)[0] == 404, "outro motorista não altera")
        _, view = call("GET", f"/requests/{created['id']}", None, team_customer["tok"])
        chk([m["name"] for m in view.get("team", [])] == ["Paulo", "João"], f"o cliente vê a equipa: {view.get('team')}")
        status, opts = call("GET", "/rating-options?category=removals", None, team_customer["tok"])
        chk([c["label"] for c in opts.get("compliments", [])] == ["Pontuais", "Cuidadosos", "Rápidos", "Simpáticos"] and [t["amount_cents"] for t in opts.get("tips", [])] == [200000, 500000, 1000000], "elogios e gorjetas das mudanças")


def orders_and_account():
    with Scenario("P1", "Separadores e filtro por serviço"):
        customer, driver, done = completed_ride("cash")
        status, _ = call("POST", f"/requests/{done}/payments/cash", None, driver["tok"], PROVIDER)
        chk(status in (200, 201), f"o motorista confirma o dinheiro: {status}")
        luanda = dt.timezone(dt.timedelta(hours=1))
        when = (dt.datetime.now(luanda) + dt.timedelta(days=2)).replace(minute=0, second=0, microsecond=0).isoformat()
        status, scheduled = create(customer, estimate(customer, scheduled_for=when), scheduled_for=when)
        status, tow = create(customer, tow_option(customer), towing=towing_details(customer["tok"]))
        moving_window, moving_when = window_at(removal_options(customer), "morning", 3)
        status, removal = removal_request(customer, removal_estimate(customer, when=moving_when), moving_window, moving_when)
        shop, shop_service = workshop_fixture(PARTNER_TOKEN[0], capacity=3)
        status, repair_booking = book(customer, shop, [shop_service], slot_of(shop, shop_service, customer))
        repair = {"id": repair_booking["request_id"]}
        ids = lambda res: {i["id"] for i in res.get("items", [])}
        _, active = call("GET", "/requests?status=created,searching,offered,awaiting_choice,accepted,provider_en_route,in_progress", None, customer["tok"])
        _, history = call("GET", "/requests?status=completed,cancelled,expired", None, customer["tok"])
        _, marked = call("GET", "/requests?status=scheduled", None, customer["tok"])
        chk(tow["id"] in ids(active) and done not in ids(active), f"Em curso: {len(ids(active))} pedidos, inclui reboque")
        _, removal_view = call("GET", f"/requests/{removal['id']}", None, customer["tok"])
        removal_tab = ids(marked) if removal_view.get("status") == "scheduled" else ids(active)
        chk(removal["id"] in removal_tab, f"as mudanças aparecem no separador do seu estado: {removal_view.get('status')}")
        chk(done in ids(history) and tow["id"] not in ids(history), "Histórico tem a concluída")
        chk(ids(marked) - {removal["id"]} == {scheduled["id"], repair["id"]}, "Marcados junta a corrida marcada e a marcação de oficina")
        _, only = call("GET", "/requests?category=towing", None, customer["tok"])
        chk(ids(only) == {tow["id"]}, "filtro por serviço")
        for item in (scheduled, tow, removal):
            cancel(customer["tok"], item['id'])
        call("POST", f"/bookings/{repair_booking['id']}/cancel", {"reason_id": reason_id(customer["tok"], "customer", "maintenance")}, customer["tok"], log=False)

    with Scenario("P2", "Código, recibo PDF, Repetir e ajuda"):
        customer, driver, rid = completed_ride("gpo")
        status, payment = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"])
        wait_payment(customer, rid, "paid")
        _, view = call("GET", f"/requests/{rid}", None, customer["tok"])
        chk(re.fullmatch(r"TX-\d{5,}", view.get("code", "")), f"código legível: {view.get('code')}")
        chk(view.get("service_name"), f"nome do serviço: {view.get('service_name')}")
        raw = fetch_raw(f"/requests/{rid}/receipt", customer["tok"])
        chk(raw[0] == 200 and raw[1].startswith(b"%PDF"), f"recibo em PDF: {raw[0]}, {len(raw[1])} bytes")
        fare = view["breakdown"]["total_cents"]
        kz = fare // 100
        amount = f"{kz:,}".replace(",", " ") + ",%02d Kz" % (fare % 100)
        chk(amount.encode() in raw[1] and view["code"].encode() in raw[1], f"o PDF tem os valores do pedido: {amount}, {view['code']}")
        chk(call("GET", f"/requests/{rid}/receipt", None, new_user("customer", "Intruso P2")["tok"])[0] == 404, "outro cliente não vê o recibo")
        before = sql(f"SELECT count(*) FROM service_requests WHERE customer_id='{customer['id']}'")
        status, repeated = call("POST", f"/requests/{rid}/repeat", {}, customer["tok"])
        chk(status == 201 and repeated.get("estimates") and sql(f"SELECT count(*) FROM service_requests WHERE customer_id='{customer['id']}'") == before, f"Repetir prepara novo preço, sem pedir: {status}")
        status, ticket = call("POST", f"/requests/{rid}/tickets", {"subject": "Problema", "message": "Cobraram a mais"}, customer["tok"])
        chk(status == 201 and ticket.get("request_id") == rid, f"problema ligado ao pedido: {status} {ticket.get('code')}")
        chk(call("POST", f"/requests/{rid}/tickets", {"subject": "x", "message": "y"}, new_user("customer", "Intruso P2b")["tok"])[0] == 404, "ticket no pedido de outro")
        chk(call("POST", f"/admin/support/tickets/{ticket.get('id')}/messages", {"message": "Vamos ver"}, ADMIN[0], BACKOFFICE)[0] == 200, "o apoio responde")
        _, mine = call("GET", f"/support/tickets/{ticket.get('id')}", None, customer["tok"])
        chk(len(mine.get("messages", [])) == 2, "o cliente vê a resposta")
        trip, driver2 = new_user("customer", "Cliente P2c"), new_user("provider", "Motorista P2c", "taxi")
        go_online(driver2)
        est = estimate(trip, STOPS[:2])
        _, created = create(trip, est, STOPS[:2])
        accept(driver2, wait_offer(driver2, created["id"]))
        advance(driver2, created["id"], ("en-route", "arrive", "start"))
        status, _ = call("POST", f"/requests/{created['id']}/stops/0/arrive", None, driver2["tok"], PROVIDER)
        _, view = call("GET", f"/requests/{created['id']}", None, trip["tok"])
        chk(status == 200 and view["stops"][0].get("arrived_at") and not view["stops"][1].get("arrived_at"), f"horas por paragem: {[s.get('arrived_at') for s in view['stops']]}")
        advance(driver2, created["id"], ("complete",))
        go_offline(driver2)
        _, recent = call("GET", "/users/me/recent-destinations", None, trip["tok"])
        chk(len(recent.get("items", [])) == 1, f"destinos recentes: {len(recent.get('items', []))}")

    with Scenario("P3", "Caixa de notificações"):
        customer, driver, rid = completed_ride("cash")
        def inbox(tok=customer["tok"], query=""):
            return call("GET", "/notifications" + query, None, tok, log=False)[1]
        status, _ = call("GET", "/notifications", None, customer["tok"])
        chk(status == 200, f"caixa de entrada: {status}")
        deadline = time.time() + 20
        while time.time() < deadline and not any(i["title"] == "Serviço concluído" for i in inbox().get("items", [])):
            time.sleep(0.5)
        items = inbox().get("items", [])
        titles = [i["title"] for i in items]
        chk("Prestador atribuído" in titles and "Serviço concluído" in titles, f"criada nos eventos: {titles}")
        done = next(i for i in items if i["title"] == "Serviço concluído")
        chk(done["link_type"] == "request" and done["link_id"] == rid, f"deep link para o pedido: {done['link_type']} {done['link_id']}")
        before = inbox()
        chk(before["unread"] == before["total"] and before["unread"] >= 2, f"não lidas: {before['unread']} de {before['total']}")
        chk(call("POST", f"/notifications/{done['id']}/read", None, new_user("customer", "Intruso P3")["tok"])[0] == 404, "outro cliente não marca")
        chk(call("POST", f"/notifications/{done['id']}/read", None, customer["tok"])[0] == 204, "marcar uma como lida")
        chk(inbox()["unread"] == before["unread"] - 1, f"menos uma por ler: {inbox()['unread']}")
        status, marked = call("POST", "/notifications/read-all", {}, customer["tok"])
        chk(status in (200, 204) and marked.get("marked") == before["unread"] - 1, f"marcar lidas: {status} {marked}")
        chk(inbox()["unread"] == 0 and inbox(query="?unread=true")["items"] == [], "todas lidas (BD e filtro)")
        chk(sql(f"SELECT count(*) FROM inbox_items WHERE user_id='{customer['id']}' AND read_at IS NULL") == "0", "BD confirma")
        chk(any(i["link_type"] == "offer" for i in call("GET", "/notifications", None, driver["tok"], PROVIDER, log=False)[1].get("items", [])), "o motorista recebe a oferta com deep link")
        columns = sql("SELECT string_agg(column_name,',') FROM information_schema.columns WHERE table_schema='atlas' AND table_name='notifications'")
        info("a tabela notifications continua a ser o registo de envios: " + columns)
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
        prefs = {"notify_status": False, "notify_promotions": False, "notify_sms": False, "share_location": False, "language": "pt", "currency": "AOA"}
        status, saved = call("PUT", "/users/me/preferences", prefs, other["tok"])
        persisted = "notify" in sql("SELECT string_agg(column_name,',') FROM information_schema.columns WHERE table_schema='atlas' AND table_name='users'")
        chk(persisted and status == 200 and sql(f"SELECT notify_sms FROM users WHERE id='{other['id']}'") == "f", f"toggles persistidos: PUT {status} (BD)")
        chk(call("PUT", "/users/me/preferences", dict(prefs, language="xx"), other["tok"])[0] >= 400, "idioma inválido recusado")
        chk(call("GET", "/users/me/preferences", None, other["tok"])[1].get("notify_status") is False, "lê as preferências")
        shop, shop_service = workshop_fixture(PARTNER_TOKEN[0], capacity=3)
        sent_before = len(sms_to(other["phone"]))
        status, muted = book(other, shop, [shop_service], slot_of(shop, shop_service, other))
        deadline = time.time() + 15
        while time.time() < deadline and sql(f"SELECT count(*) FROM inbox_items WHERE user_id='{other['id']}'") == "0":
            time.sleep(0.5)
        time.sleep(1)
        chk(len(sms_to(other["phone"])) == sent_before, f"SMS desligado não é enviado: {len(sms_to(other['phone']))}")
        chk(sql(f"SELECT count(*) FROM notifications WHERE user_id='{other['id']}' AND channel='sms' AND status='skipped'") != "0", "fica registado como não enviado")
        status, _ = call("POST", "/users/me/verification-code", {}, other["tok"])
        time.sleep(1)
        chk(status < 300 and len(sms_to(other["phone"])) == sent_before + 1, f"o código de identidade não depende das preferências: {status} {len(sms_to(other['phone']))}")

    with Scenario("P5", "Convites, ajuda e foto de perfil"):
        friend, invited = new_user("customer", "Amigo P5"), new_user("customer", "Convidado P5")
        status, referral = call("GET", "/users/me/referral", None, friend["tok"])
        chk(status == 200 and referral.get("reward_cents") == 100000 and len(referral.get("code", "")) == 8, f"código e prémio: {referral}")
        chk(code(call("POST", "/users/me/referral/redeem", {"code": referral["code"]}, friend["tok"])[1]) == "REFERRAL_OWN_CODE", "o próprio código é recusado")
        chk(call("POST", "/users/me/referral/redeem", {"code": "NAOEXISTE"}, invited["tok"])[0] == 404, "código inexistente")
        chk(call("POST", "/users/me/referral/redeem", {"code": referral["code"]}, invited["tok"])[0] == 204, "usar o código do amigo")
        chk(code(call("POST", "/users/me/referral/redeem", {"code": referral["code"]}, invited["tok"])[1]) == "REFERRAL_ALREADY_USED", "só uma vez")
        wallet = lambda tok: call("GET", "/wallet", None, tok, log=False)[1].get("balance_cents")
        chk(wallet(friend["tok"]) == 0, "sem prémio antes de pagar")
        driver = new_user("provider", "Motorista P5", "taxi")
        go_online(driver)
        est = estimate(invited)
        _, created = create(invited, est, payment_method="gpo")
        rid = created["id"]
        accept(driver, wait_offer(driver, rid))
        chk(wallet(friend["tok"]) == 0, "aceite ainda não dá prémio")
        advance(driver, rid)
        chk(wallet(friend["tok"]) == 0, "concluído sem pagar ainda não dá prémio")
        call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, invited["tok"])
        chk(wait_payment(invited, rid, "paid"), "o amigo pagou")
        go_offline(driver)
        chk(wallet(friend["tok"]) == 100000, f"o prémio vai para a Carteira: {wallet(friend['tok'])}")
        chk(sql(f"SELECT count(*) FROM ledger_transactions WHERE kind='referral_reward' AND request_id='{rid}'") == "1", "registado uma vez no ledger (BD)")
        chk(code(call("POST", "/users/me/referral/redeem", {"code": referral["code"]}, invited["tok"])[1]) in ("REFERRAL_TOO_LATE", "REFERRAL_ALREADY_USED"), "depois de pagar já não conta")
        status, stats = call("GET", "/users/me/referral", None, friend["tok"])
        chk(stats.get("rewarded") == 1 and stats.get("earned_cents") == 100000, f"o amigo vê o que ganhou: {stats}")
        status, help_view = call("GET", "/help?search=reembolso", None, invited["tok"])
        chk(status == 200 and [t["code"] for t in help_view.get("topics", [])] == ["payments"], f"pesquisa nos temas: {[t['code'] for t in help_view.get('topics', [])]}")
        chk(help_view.get("support_phone") and help_view.get("recent_request", {}).get("id") == rid, "linha de apoio e pedido recente")
        chk(len(call("GET", "/help", None, invited["tok"])[1].get("topics", [])) >= 6, "todos os temas")
        status, photo = call("POST", "/media/photos", {"content_type": "image/jpeg", "size_bytes": 5}, invited["tok"], log=False)
        req = urllib.request.Request(photo["upload_url"], data=b"hello", method="PUT", headers={"Content-Type": "image/jpeg"})
        with urllib.request.urlopen(req, timeout=30) as sent:
            assert sent.status in (200, 204)
        chk(call("PUT", "/users/me/avatar", {"photo_id": photo["id"]}, friend["tok"])[0] == 404, "a foto de outro é recusada")
        status, avatar = call("PUT", "/users/me/avatar", {"photo_id": photo["id"]}, invited["tok"])
        chk(status == 200 and avatar.get("url"), f"foto de perfil: {status}")
        chk(call("GET", "/users/me/avatar", None, invited["tok"])[1].get("url"), "ler a foto de perfil")

    with Scenario("L1", "Chat, respostas rápidas e partilha"):
        customer, driver = new_user("customer", "Cliente L1"), new_user("provider", "Motorista L1", "taxi")
        go_online(driver)
        _, created = create(customer, estimate(customer))
        rid = created["id"]
        chk(code(call("POST", f"/requests/{rid}/messages", {"body": "Olá"}, customer["tok"])[1]) == "CHAT_CLOSED", "sem motorista não há chat")
        accept(driver, wait_offer(driver, rid))
        chk(call("POST", f"/requests/{rid}/messages", {"body": "Estou à porta"}, customer["tok"])[0] == 201, "o cliente envia")
        _, replies = call("GET", "/quick-replies?category=taxi", None, driver["tok"], PROVIDER)
        reply = replies.get("items", [{}])[0]
        status, sent = call("POST", f"/requests/{rid}/messages", {"quick_reply_id": reply.get("id")}, driver["tok"], PROVIDER)
        chk(status == 201 and sent.get("body") == reply.get("label"), f"resposta rápida: {status} {sent.get('body')}")
        intruder = new_user("customer", "Intruso L1")
        chk(call("GET", f"/requests/{rid}/messages", None, intruder["tok"])[0] == 404, "outro cliente não lê")
        chk(call("POST", f"/requests/{rid}/messages", {"body": "x"}, intruder["tok"])[0] == 404, "outro cliente não escreve")
        _, chat = call("GET", f"/requests/{rid}/messages", None, customer["tok"])
        chk(len(chat.get("items", [])) == 2, f"histórico: {len(chat.get('items', []))}")
        status, res = call("POST", f"/requests/{rid}/call", None, customer["tok"])
        chk(status == 503 and code(res) == "CALLS_UNAVAILABLE", f"sem fornecedor de chamadas: {status} {code(res)}")
        status, share = call("POST", f"/requests/{rid}/share", None, customer["tok"])
        chk(status in (200, 201) and share.get("token"), f"ligação temporária: {status}")
        token = share.get("token", "")
        chk(call("POST", f"/requests/{rid}/share", None, intruder["tok"])[0] == 404, "outro cliente não partilha")
        with urllib.request.urlopen(BASE + f"/api/v1/public/trips/{token}", timeout=20) as res_public:
            trip_text = res_public.read().decode()
        chk(customer["phone"] not in trip_text and driver["phone"] not in trip_text and "plate" in trip_text.lower(), "a ligação pública mostra a matrícula e nunca telefones")
        chk(token not in sql(f"SELECT token_hash FROM request_shares WHERE request_id='{rid}'"), "na BD só fica o hash")
        chk(call("DELETE", f"/requests/{rid}/share", None, customer["tok"])[0] in (200, 204), "revogar")
        try:
            urllib.request.urlopen(BASE + f"/api/v1/public/trips/{token}", timeout=20)
            revoked = 200
        except urllib.error.HTTPError as err:
            revoked = err.code
        chk(revoked in (404, 410), f"revogada deixa de abrir: {revoked}")
        status, eta = call("GET", f"/requests/{rid}/eta", None, customer["tok"])
        chk(status == 200, f"ETA: {status} {eta.get('source')}")

    with Scenario("L2", "SOS"):
        status, res = call("POST", f"/requests/{rid}/sos", {"note": "Ajuda", "lat": -8.84, "lng": 13.24}, intruder["tok"])
        chk(status == 404, f"outro cliente não aciona: {status}")
        status, alert = call("POST", f"/requests/{rid}/sos", {"note": "Ajuda", "lat": -8.84, "lng": 13.24}, customer["tok"])
        chk(status == 201 and alert.get("status") == "open", f"SOS registado: {status}")
        status, again = call("POST", f"/requests/{rid}/sos", {"note": "Outra vez"}, customer["tok"])
        chk(again.get("id") == alert.get("id"), f"o segundo toque não duplica: {status}")
        chk(sql(f"SELECT count(*) FROM sos_alerts WHERE request_id='{rid}'") == "1", "uma linha na BD")
        chk(call("GET", "/admin/sos?status=open", None, customer["tok"])[0] in (401, 403), "o cliente não vê a lista")
        _, opened = call("GET", "/admin/sos?status=open", None, ADMIN[0], BACKOFFICE)
        chk(alert.get("id") in [a["id"] for a in opened.get("items", [])], "o operador vê o alerta")
        chk(call("POST", f"/admin/sos/{alert['id']}/resolve", None, ADMIN[0], BACKOFFICE)[0] in (200, 204), "o operador resolve")
        advance(driver, rid, ("en-route", "arrive", "start", "complete"))
        go_offline(driver)
        chk(code(call("POST", f"/requests/{rid}/messages", {"body": "Tarde"}, customer["tok"])[1]) == "CHAT_CLOSED", "pedido terminado fecha o chat")

    with Scenario("L3", "Ledger do motorista: comissão, gorjeta e liquidação"):
        customer, driver, rid = completed_ride("gpo")
        driver_id = driver["id"]
        fare = int(sql(f"SELECT fare_cents FROM service_requests WHERE id='{rid}'"))
        commission = fare // 10
        sql(f"UPDATE service_requests SET commission_cents={commission} WHERE id='{rid}'")
        chk(commission > 0, f"a prova usa uma comissão real: {commission}")
        call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"])
        chk(wait_payment(customer, rid, "paid"), "pago")
        balance = lambda: call("GET", f"/admin/providers/{driver_id}/ledger", None, ADMIN[0], BACKOFFICE, log=False)[1].get("balance_cents")
        chk(balance() == fare - commission, f"o motorista recebe tarifa menos comissão: {balance()} = {fare} - {commission}")
        chk(call("GET", f"/admin/providers/{driver_id}/ledger", None, customer["tok"])[0] in (401, 403), "o cliente não lê o ledger de outro")
        _, summary = call("GET", "/admin/ledger/summary", None, ADMIN[0], BACKOFFICE)
        chk(summary.get("unbalanced_transactions") == 0, f"as transações somam zero: {summary.get('unbalanced_transactions')}")
        status, tip = call("POST", f"/requests/{rid}/tips", {"amount_cents": 50000, "method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"], idem=str(uuid.uuid4()))
        chk(status == 201, f"gorjeta: {status} {code(tip)}")
        deadline = time.time() + 40
        while time.time() < deadline and balance() != fare - commission + 50000:
            time.sleep(1)
        chk(balance() == fare - commission + 50000, f"gorjeta 100% para o motorista: {balance()}")
        reference = "QA-" + uuid.uuid4().hex[:8]
        amount = balance()
        chk(code(call("POST", f"/admin/providers/{driver_id}/settlements", {"direction": "payout", "amount_cents": amount + 1, "reference": reference}, ADMIN[0], BACKOFFICE)[1]) is not None, "não paga acima do saldo")
        status, settled = call("POST", f"/admin/providers/{driver_id}/settlements", {"direction": "payout", "amount_cents": amount, "reference": reference}, ADMIN[0], BACKOFFICE)
        chk(status == 200 and settled.get("applied") is True and balance() == 0, f"liquidação: {status} {balance()}")
        sql(f"INSERT INTO ledger_transactions (id,idempotency_key,kind,currency) VALUES ('{uuid.uuid4()}','qa-topup-{reference}','wallet_credit','AOA')")
        top_up = sql(f"SELECT id FROM ledger_transactions WHERE idempotency_key='qa-topup-{reference}'")
        sql(f"INSERT INTO ledger_entries (transaction_id,account_type,account_id,amount_cents) VALUES ('{top_up}','provider','{driver_id}',100)")
        sql(f"INSERT INTO ledger_entries (transaction_id,account_type,amount_cents) VALUES ('{top_up}','external',-100)")
        status, again = call("POST", f"/admin/providers/{driver_id}/settlements", {"direction": "payout", "amount_cents": 1, "reference": reference}, ADMIN[0], BACKOFFICE)
        chk(status == 200 and again.get("applied") is False and balance() == 100, f"a mesma referência só conta uma vez: {status} {again.get('applied')} saldo {balance()}")



def payments_real(admin):
    with Scenario("X1", "Express real, webhook forjado e reembolso"):
        customer, driver, rid = completed_ride("gpo")
        status, pending = call("POST", f"/requests/{rid}/payments", {"method": "gpo", "phone_number": SIMULATED_OK, "amount_cents": 1}, customer["tok"])
        fare = sql(f"SELECT fare_cents FROM service_requests WHERE id='{rid}'")
        chk(status == 201 and str(pending.get("amount_cents")) == fare, f"amount_cents enviado é ignorado: cobra {pending.get('amount_cents')}, fare {fare}")
        chk(wait_payment(customer, rid, "paid"), "fica pago depois da confirmação da AppyPay")
        status, res = call("POST", f"/admin/payments/{pending['id']}/refund", {"amount_cents": int(fare) + 1, "reason": "QA"}, admin, BACKOFFICE, idem=str(uuid.uuid4()))
        chk(code(res) == "REFUND_TOO_LARGE", f"reembolso acima do pago: {status} {code(res)}")
        status, res = call("POST", f"/admin/payments/{pending['id']}/refund", {"amount_cents": 1000, "reason": "QA"}, admin, BACKOFFICE, idem=str(uuid.uuid4()))
        chk(status == 200 and res.get("refunded_cents") == 1000, f"reembolso parcial real: {status} {res.get('refunded_cents')}")
        with cf.ThreadPoolExecutor(2) as pool:
            both = list(pool.map(lambda _: call("POST", f"/admin/payments/{pending['id']}/refund", {"reason": "QA"}, admin, BACKOFFICE, idem=str(uuid.uuid4()), log=False)[0], range(2)))
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
        status, created = create(customer, tow_option(customer), towing=towing_details(customer["tok"]))
        rid = created["id"]
        offer = wait_offer(driver, rid)
        accept(driver, offer)
        advance(driver, rid, ("en-route", "arrive", "start"))
        call("POST", f"/requests/{rid}/complete", {"code": created["completion_code"]}, driver["tok"], PROVIDER)
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
        for label, method, path, body in (("ver", "GET", f"/requests/{rid}", None), ("cancelar", "POST", f"/requests/{rid}/cancel", {"reason_id": reason_id(owner["tok"], "customer", "taxi")}), ("rota", "POST", f"/requests/{rid}/route", {"destination": DEST}),
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
        cancel(owner["tok"], rid)
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
        cancel(customer["tok"], created['id'])
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
            ("POST", f"/requests/{missing}/cancel", {"reason_id": missing}, customer["tok"], CUSTOMER), ("POST", f"/requests/{missing}/payments", {"method": "gpo", "phone_number": SIMULATED_OK}, customer["tok"], CUSTOMER),
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
        cancel(customer["tok"], created['id'])

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
                cancel(tok, rid)
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
        status, created = create(customer, tow_option(customer), towing=towing_details(customer["tok"]))
        rid = created["id"]
        first, second = wait_offer(one, rid), wait_offer(two, rid)
        if not (first and second):
            not_testable("o reboque não ofereceu a ambos os motoristas")
        else:
            with cf.ThreadPoolExecutor(2) as pool:
                statuses = sorted(pool.map(lambda pair: accept(*pair)[0], ((one, first), (two, second))))
            chk(statuses[0] == 200 and statuses[1] != 200, f"aceites em simultâneo: {statuses}")
        cancel(customer["tok"], rid)
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


QA_CUSTOMERS = "SELECT id FROM users WHERE name ~ '^(Cliente|Rival|Intruso|Amigo|Convidado|Motorista|Reboque|Equipa|Outro|Dono|Estranho|Rascunho) '"


def neutralize_test_data():
    sql(f"UPDATE bookings SET status='cancelled' WHERE status NOT IN ('completed','cancelled') AND customer_id IN ({QA_CUSTOMERS})")
    sql(f"UPDATE service_requests SET status='cancelled', cancelled_by='system', cancel_reason='QA terminado', next_action_at=NULL, reminder_at=NULL, needs_operator=false, updated_at=now() WHERE status NOT IN ('completed','cancelled','expired') AND customer_id IN ({QA_CUSTOMERS})")
    sql(f"UPDATE payments SET status='expired', next_check_at=NULL, updated_at=now() WHERE status='pending' AND customer_id IN ({QA_CUSTOMERS})")


def events_drained():
    if sql("SELECT count(*) FROM outbox WHERE published_at IS NULL") != "0":
        return False
    out = subprocess.run(["docker", "compose", "exec", "-T", "rabbitmq", "rabbitmqctl", "-q", "list_queues", "name", "messages"], capture_output=True, text=True, stdin=subprocess.DEVNULL)
    if out.returncode:
        return False
    for line in out.stdout.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1].isdigit() and int(parts[1]) > 0 and not parts[0].endswith((".dlq", ".unrouted")):
            return False
    return True


def wait_for_drain(seconds=120):
    deadline = time.time() + seconds
    calm = 0
    while time.time() < deadline:
        calm = calm + 1 if events_drained() else 0
        if calm >= 3:
            return True
        time.sleep(1)
    return False


def main():
    global OPENAPI
    server = http.server.ThreadingHTTPServer(("127.0.0.1", STUB_PORT), Stub)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    compose("stop", "api")
    neutralize_test_data()
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
        ADMIN[0], PARTNER_TOKEN[0] = admin, partner
        groups = {"taxi": taxi, "maintenance": lambda: maintenance(admin, partner), "towing": towing, "removals": removals, "orders": orders_and_account, "payments": lambda: payments_real(admin),
                  "backoffice": lambda: backoffice(admin, partner), "account": account_and_realtime, "overflow": lambda: overflow(admin, partner), "security": lambda: security(admin, partner)}
        for name in sys.argv[1:] or groups:
            groups[name]()
    finally:
        neutralize_test_data()
        drained = wait_for_drain()
        api.terminate()
        api.wait(timeout=30)
        stamp = time.strftime("%Y%m%d-%H%M%S")
        serialize(os.path.join("tests", "e2e", "evidence", f"run-{stamp}.json"))
        if drained:
            compose("up", "-d", "api")
        else:
            print("AVISO: ainda há eventos por tratar. A API do Docker fica parada, para não enviar SMS reais a números de teste.", flush=True)
    tally = {}
    for sid, result in results.items():
        tally[result["result"]] = tally.get(result["result"], 0) + 1
        print(f"{sid}\t{result['result']}\t{result['title']}")
    print(tally)


if __name__ == "__main__":
    main()
