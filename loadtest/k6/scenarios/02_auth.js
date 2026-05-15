/**
 * Load test: Auth endpoints
 *   POST /api/v1/auth/register
 *   POST /api/v1/auth/login
 *   POST /api/v1/auth/resend-otp
 *
 * Each VU registers a unique user, then hammers login with correct and
 * incorrect credentials to exercise both the happy path and the 401 path.
 *
 * Stages:
 *   0→20 VUs over 30 s  (ramp up)
 *   20 VUs for 2 min    (sustained)
 *   20→0 VUs over 15 s  (ramp down)
 */
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate, Counter } from 'k6/metrics';
import { BASE_URL, jsonHeaders, defaultThresholds } from '../config.js';

const registerDuration = new Trend('auth_register_duration', true);
const loginDuration    = new Trend('auth_login_duration', true);
const resendDuration   = new Trend('auth_resend_duration', true);
const authErrors       = new Rate('auth_errors');
const registrations    = new Counter('auth_registrations_total');

export const options = {
  stages: [
    { duration: '30s', target: 20 },
    { duration: '2m',  target: 20 },
    { duration: '15s', target: 0  },
  ],
  thresholds: {
    ...defaultThresholds,
    auth_register_duration: ['p(95)<800'],
    auth_login_duration:    ['p(95)<300'],
    auth_errors:            ['rate<0.02'],
  },
};

export default function () {
  const vu     = __VU;
  const iter   = __ITER;
  const email  = `lt_${vu}_${iter}@example.com`;
  const pass   = 'LoadTest1!';

  // ── Register ──────────────────────────────────────────────────────────────
  const regRes = http.post(
    `${BASE_URL}/api/v1/auth/register`,
    JSON.stringify({ email, fullName: 'Load Tester', password: pass }),
    { headers: jsonHeaders, tags: { endpoint: 'register' } },
  );
  registerDuration.add(regRes.timings.duration);
  registrations.add(1);
  const regOk = check(regRes, {
    'register 201':      (r) => r.status === 201,
    'register success':  (r) => r.json('success') === true,
  });
  authErrors.add(!regOk);

  sleep(0.2);

  // ── Login — correct credentials ───────────────────────────────────────────
  const loginRes = http.post(
    `${BASE_URL}/api/v1/auth/login`,
    JSON.stringify({ email, password: pass }),
    { headers: jsonHeaders, tags: { endpoint: 'login' } },
  );
  loginDuration.add(loginRes.timings.duration);
  // Login returns 401 because the account is not yet verified — that is the
  // expected behaviour; we still check the response shape is correct.
  const loginOk = check(loginRes, {
    'login has success field': (r) => r.json('success') !== undefined,
    'login not 5xx':           (r) => r.status < 500,
  });
  authErrors.add(!loginOk);

  sleep(0.2);

  // ── Login — wrong password (expect 401) ───────────────────────────────────
  const badRes = http.post(
    `${BASE_URL}/api/v1/auth/login`,
    JSON.stringify({ email, password: 'WrongPass99!' }),
    { headers: jsonHeaders, tags: { endpoint: 'login_bad' } },
  );
  check(badRes, {
    'bad login 401':     (r) => r.status === 401,
    'bad login success false': (r) => r.json('success') === false,
  });

  sleep(0.2);

  // ── Resend OTP ────────────────────────────────────────────────────────────
  const resendRes = http.post(
    `${BASE_URL}/api/v1/auth/resend-otp`,
    JSON.stringify({ email }),
    { headers: jsonHeaders, tags: { endpoint: 'resend_otp' } },
  );
  resendDuration.add(resendRes.timings.duration);
  check(resendRes, {
    'resend 200':     (r) => r.status === 200,
    'resend success': (r) => r.json('success') === true,
  });

  sleep(0.5);
}
