import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { SharedArray } from 'k6/data';
import exec from 'k6/execution';

const fixtures = JSON.parse(open(__ENV.FIXTURES));
const customers = new SharedArray('customers', () => fixtures.customers);
const providers = new SharedArray('providers', () => fixtures.providers);
const base = __ENV.BASE_URL;
const holdSeconds = Number(__ENV.HOLD_SECONDS || 130);
const driversDuration = `${holdSeconds + 70}s`;

const requestsCreated = new Counter('requests_created');
const offersAccepted = new Counter('offers_accepted');
const acceptConflicts = new Counter('accept_conflicts');
const timeToAssigned = new Trend('time_to_assigned_ms', true);

const positionRate = Number(__ENV.POSITION_RATE || 200);
const requestRate = Number(__ENV.REQUEST_RATE || 10);

export const options = {
  setupTimeout: '10m',
  scenarios: {
    positions: { executor: 'constant-arrival-rate', rate: positionRate, timeUnit: '1s', duration: driversDuration, preAllocatedVUs: 200, maxVUs: 1000, exec: 'sendPosition' },
    customers: { executor: 'ramping-arrival-rate', startRate: 1, timeUnit: '1s', preAllocatedVUs: 200, maxVUs: 1500, exec: 'customerFlow', stages: [
      { duration: '30s', target: requestRate }, { duration: `${holdSeconds}s`, target: requestRate }, { duration: '10s', target: 0 },
    ] },
    drivers: { executor: 'constant-arrival-rate', rate: providers.length, timeUnit: '1s', duration: driversDuration, preAllocatedVUs: 200, maxVUs: 1000, exec: 'driverPoll' },
  },
  thresholds: {
    'http_req_failed{kind:api}': ['rate<0.01'],
    'http_req_duration{name:locations}': ['p(95)<150'],
    'http_req_duration{name:fare_estimate}': ['p(95)<400'],
    'http_req_duration{name:create_request}': ['p(95)<500'],
    'http_req_duration{name:accept}': ['p(95)<500'],
    time_to_assigned_ms: ['p(95)<10000'],
    requests_created: ['count>100'],
    offers_accepted: ['count>500'],
  },
};

function headers(token, client, extra) {
  return { headers: Object.assign({ 'Content-Type': 'application/json', 'X-Client-Id': client, Authorization: `Bearer ${token}` }, extra || {}) };
}

function api(params, name) {
  params.tags = { kind: 'api', name };
  return params;
}

function drift(value) {
  return value + (Math.random() - 0.5) * 0.0004;
}

export function setup() {
  const batchSize = 50;
  for (let start = 0; start < providers.length; start += batchSize) {
    const batch = [];
    for (let i = start; i < Math.min(start + batchSize, providers.length); i++) {
      const provider = providers[i];
      batch.push(['POST', `${base}/api/v1/providers/me/online`, JSON.stringify({ position: { lat: provider.lat, lng: provider.lng, accuracy_m: 10, age_s: 600 } }), headers(provider.token, 'agora-provider-app')]);
    }
    http.batch(batch);
  }
}

export function sendPosition() {
  const provider = providers[exec.scenario.iterationInTest % providers.length];
  const body = { positions: [{ lat: drift(provider.lat), lng: drift(provider.lng), accuracy_m: 12, age_s: 0 }] };
  const res = http.post(`${base}/api/v1/providers/me/locations`, JSON.stringify(body), api(headers(provider.token, 'agora-provider-app'), 'locations'));
  check(res, { 'position accepted': (r) => r.status === 200 });
}

export function customerFlow() {
  const customer = customers[exec.scenario.iterationInTest % customers.length];
  const pickup = { lat: customer.lat, lng: customer.lng, reference: 'Teste de carga' };
  const destination = { lat: customer.lat - 0.05, lng: customer.lng - 0.03 };
  const auth = headers(customer.token, 'agora-customer-app');

  const estimate = http.post(`${base}/api/v1/fare-estimates`, JSON.stringify({ category: 'taxi', pickup, destination }), api(Object.assign({}, auth), 'fare_estimate'));
  if (!check(estimate, { 'estimate created': (r) => r.status === 201 })) {
    return;
  }
  const option = estimate.json('items.0');
  const key = `${customer.id}-${exec.scenario.iterationInTest}`;
  const startedAt = Date.now();
  const created = http.post(`${base}/api/v1/requests`, JSON.stringify({ service_id: option.service_id, fare_estimate_id: option.fare_estimate_id, pickup, destination }),
    api(headers(customer.token, 'agora-customer-app', { 'Idempotency-Key': key }), 'create_request'));
  if (!check(created, { 'request created': (r) => r.status === 201 })) {
    return;
  }
  requestsCreated.add(1);
  const id = created.json('id');
  for (let i = 0; i < 25; i++) {
    sleep(1);
    const view = http.get(`${base}/api/v1/requests/${id}`, api(Object.assign({}, auth), 'get_request'));
    const status = view.status === 200 ? view.json('status') : '';
    if (['accepted', 'provider_en_route', 'in_progress', 'completed'].includes(status)) {
      timeToAssigned.add(Date.now() - startedAt);
      return;
    }
    if (status === 'expired') {
      return;
    }
  }
}

export function driverPoll() {
  const provider = providers[exec.scenario.iterationInTest % providers.length];
  const auth = headers(provider.token, 'agora-provider-app');
  const active = http.get(`${base}/api/v1/requests/active`, api(Object.assign({}, auth), 'active'));
  if (active.status !== 200) {
    return;
  }
  const current = active.json('request');
  if (current && current.id) {
    const next = { accepted: 'en-route', provider_en_route: 'start', in_progress: 'complete' }[current.status];
    if (next) {
      http.post(`${base}/api/v1/requests/${current.id}/${next}`, null, api(Object.assign({}, auth), next));
    }
    return;
  }
  const offers = active.json('offers') || [];
  if (offers.length === 0) {
    return;
  }
  const offer = offers[0];
  http.post(`${base}/api/v1/offers/${offer.id}/delivered`, JSON.stringify({ channel: 'sse' }), api(Object.assign({}, auth), 'delivered'));
  const params = api(Object.assign({}, auth), 'accept');
  params.responseCallback = http.expectedStatuses(200, 409);
  const accepted = http.post(`${base}/api/v1/offers/${offer.id}/accept`, JSON.stringify({ version: offer.request_version }), params);
  if (accepted.status === 200) {
    offersAccepted.add(1);
  } else {
    acceptConflicts.add(1);
  }
}
