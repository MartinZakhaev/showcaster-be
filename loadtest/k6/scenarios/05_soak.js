/**
 * Soak test: sustained moderate load over a long period.
 *
 * Detects memory leaks, goroutine leaks, and DB connection exhaustion
 * that only appear after extended operation.
 *
 * Stages:
 *   0→20 VUs over 1 min   (ramp up)
 *   20 VUs  for 30 min    (sustained — reduce to 5 min for quick runs)
 *   20→0 VUs over 1 min   (ramp down)
 *
 * Run with: k6 run --env DURATION=5m scenarios/05_soak.js
 */
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';
import { BASE_URL, jsonHeaders, authHeaders } from '../config.js';

const soakDuration = new Trend('soak_req_duration', true);
const soakErrors   = new Rate('soak_errors');

const DURATION = __ENV.DURATION || '30m';

export const options = {
  stages: [
    { duration: '1m',    target: 20 },
    { duration: DURATION, target: 20 },
    { duration: '1m',    target: 0  },
  ],
  thresholds: {
    http_req_duration: ['p(95)<800'],
    http_req_failed:   ['rate<0.01'],
    soak_errors:       ['rate<0.01'],
  },
};

export default function () {
  const vu   = __VU;
  const iter = __ITER;

  // Register once per VU
  if (iter === 0) {
    const email = `soak_${vu}@example.com`;
    http.post(
      `${BASE_URL}/api/v1/auth/register`,
      JSON.stringify({ email, fullName: 'Soak Tester', password: 'SoakTest1!' }),
      { headers: jsonHeaders },
    );
    globalThis._soakEmail = email;
    globalThis._soakToken = null;
  }

  // Lazy login
  if (!globalThis._soakToken) {
    const r = http.post(
      `${BASE_URL}/api/v1/auth/login`,
      JSON.stringify({ email: globalThis._soakEmail, password: 'SoakTest1!' }),
      { headers: jsonHeaders },
    );
    if (r.status === 200) globalThis._soakToken = r.json('data.token');
    else { sleep(2); return; }
  }

  const token = globalThis._soakToken;

  // Rotate through endpoints to exercise all code paths
  const roll = Math.random();

  let res;
  if (roll < 0.3) {
    res = http.get(`${BASE_URL}/health`);
    check(res, { 'health 200': (r) => r.status === 200 });
  } else if (roll < 0.6) {
    res = http.get(`${BASE_URL}/api/v1/jobs?limit=10`, { headers: authHeaders(token) });
    check(res, { 'list 200': (r) => r.status === 200 });
  } else {
    res = http.post(
      `${BASE_URL}/api/v1/jobs/generate`,
      JSON.stringify({
        modelImageUrl:   'https://res.cloudinary.com/demo/image/upload/sample.jpg',
        productImageUrl: 'https://res.cloudinary.com/demo/image/upload/product.jpg',
        productName:     'Soak Product',
        productCategory: 'health',
        targetAudience:  'unisex',
        orientation:     'landscape',
        resolution:      '720p',
      }),
      { headers: authHeaders(token) },
    );
    check(res, { 'create 202': (r) => r.status === 202 });
  }

  soakDuration.add(res.timings.duration);
  soakErrors.add(res.status >= 500);

  sleep(1);
}
