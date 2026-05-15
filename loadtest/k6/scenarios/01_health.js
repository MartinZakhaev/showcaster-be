/**
 * Load test: GET /health
 *
 * Baseline test — the health endpoint has no auth and no DB write.
 * Use this to establish the raw throughput ceiling of the server.
 *
 * Stages:
 *   0→50 VUs over 30 s  (ramp up)
 *   50 VUs for 1 min    (sustained load)
 *   50→0 VUs over 15 s  (ramp down)
 */
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';
import { BASE_URL, defaultThresholds } from '../config.js';

const healthDuration = new Trend('health_duration', true);
const healthErrors = new Rate('health_errors');

export const options = {
  stages: [
    { duration: '30s', target: 50 },
    { duration: '1m', target: 50 },
    { duration: '15s', target: 0 },
  ],
  thresholds: {
    ...defaultThresholds,
    health_duration: ['p(99)<100'], // health must be <100 ms at p99
    health_errors: ['rate<0.001'],  // near-zero errors
  },
};

export default function () {
  const res = http.get(`${BASE_URL}/health`);

  const ok = check(res, {
    'status 200': (r) => r.status === 200,
    'success true': (r) => r.json('success') === true,
    'status ok': (r) => r.json('data.status') === 'ok',
  });

  healthDuration.add(res.timings.duration);
  healthErrors.add(!ok);

  sleep(0.1);
}
