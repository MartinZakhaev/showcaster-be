/**
 * Spike test: sudden burst of traffic against the health + login endpoints.
 *
 * Simulates a traffic spike (e.g. marketing campaign launch) to verify the
 * server doesn't crash and recovers gracefully.
 *
 * Stages:
 *   0→5 VUs   over 10 s  (baseline)
 *   5→200 VUs over 10 s  (spike)
 *   200 VUs   for 30 s   (hold spike)
 *   200→5 VUs over 10 s  (recovery)
 *   5 VUs     for 30 s   (verify recovery)
 *   5→0 VUs   over 5 s   (ramp down)
 */
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';
import { BASE_URL, jsonHeaders } from '../config.js';

const spikeErrors = new Rate('spike_errors');

export const options = {
  stages: [
    { duration: '10s', target: 5   },
    { duration: '10s', target: 200 },
    { duration: '30s', target: 200 },
    { duration: '10s', target: 5   },
    { duration: '30s', target: 5   },
    { duration: '5s',  target: 0   },
  ],
  thresholds: {
    // During a spike we relax the p95 threshold but still require no crashes
    http_req_duration: ['p(95)<2000'],
    http_req_failed:   ['rate<0.05'],
    spike_errors:      ['rate<0.05'],
  },
};

export default function () {
  // Mix of health checks and login attempts
  const roll = Math.random();

  if (roll < 0.5) {
    // Health check
    const res = http.get(`${BASE_URL}/health`);
    spikeErrors.add(!check(res, { 'health 200': (r) => r.status === 200 }));
  } else {
    // Login attempt (will return 401 for unknown users — that's fine)
    const res = http.post(
      `${BASE_URL}/api/v1/auth/login`,
      JSON.stringify({ email: `spike_${__VU}@example.com`, password: 'WrongPass1!' }),
      { headers: jsonHeaders },
    );
    spikeErrors.add(!check(res, { 'login not 5xx': (r) => r.status < 500 }));
  }

  sleep(0.1);
}
