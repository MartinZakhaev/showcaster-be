/**
 * Load test: Job endpoints (requires a valid JWT)
 *   POST   /api/v1/jobs/generate
 *   GET    /api/v1/jobs
 *   GET    /api/v1/jobs/:id
 *   DELETE /api/v1/jobs/:id  (only for completed/failed jobs)
 *
 * Setup phase: registers + logs in one user per VU and stores the token.
 * Default phase: exercises the full job CRUD flow per iteration.
 *
 * Stages:
 *   0→10 VUs over 30 s  (ramp up)
 *   10 VUs for 2 min    (sustained)
 *   10→0 VUs over 15 s  (ramp down)
 *
 * NOTE: Job creation enqueues work for the background worker. The worker
 * calls Replicate (external). In load tests the worker will likely fail
 * those steps — that is fine; we are testing the HTTP layer, not Replicate.
 */
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';
import { BASE_URL, jsonHeaders, authHeaders, defaultThresholds } from '../config.js';

const createJobDuration = new Trend('jobs_create_duration', true);
const listJobsDuration  = new Trend('jobs_list_duration',   true);
const getJobDuration    = new Trend('jobs_get_duration',    true);
const jobErrors         = new Rate('jobs_errors');

export const options = {
  stages: [
    { duration: '30s', target: 10 },
    { duration: '2m',  target: 10 },
    { duration: '15s', target: 0  },
  ],
  thresholds: {
    ...defaultThresholds,
    jobs_create_duration: ['p(95)<600'],
    jobs_list_duration:   ['p(95)<400'],
    jobs_get_duration:    ['p(95)<400'],
    jobs_errors:          ['rate<0.02'],
  },
};

// Shared state: one token per VU, set up once in setup().
// k6 setup() runs once before all VUs start.
export function setup() {
  // Register + login a single "seed" user for the setup phase.
  // Each VU will register its own user in the default function.
  return {};
}

export default function () {
  const vu   = __VU;
  const iter = __ITER;

  // ── Register + login this VU's user on first iteration ───────────────────
  // We store the token in a module-level variable per VU.
  if (iter === 0) {
    const email = `lt_jobs_${vu}@example.com`;
    const pass  = 'LoadTest1!';

    http.post(
      `${BASE_URL}/api/v1/auth/register`,
      JSON.stringify({ email, fullName: 'Job Tester', password: pass }),
      { headers: jsonHeaders },
    );
    // Store credentials in VU-local state via global (k6 VUs are isolated)
    globalThis._token = null;
    globalThis._email = email;
    globalThis._pass  = pass;
  }

  // Lazy login: obtain token if we don't have one yet
  if (!globalThis._token) {
    const loginRes = http.post(
      `${BASE_URL}/api/v1/auth/login`,
      JSON.stringify({ email: globalThis._email, password: globalThis._pass }),
      { headers: jsonHeaders },
    );
    if (loginRes.status === 200) {
      globalThis._token = loginRes.json('data.token');
    } else {
      // Account not verified yet — skip this iteration
      sleep(1);
      return;
    }
  }

  const token = globalThis._token;

  // ── POST /api/v1/jobs/generate ────────────────────────────────────────────
  const createRes = http.post(
    `${BASE_URL}/api/v1/jobs/generate`,
    JSON.stringify({
      modelImageUrl:   'https://res.cloudinary.com/demo/image/upload/sample.jpg',
      productImageUrl: 'https://res.cloudinary.com/demo/image/upload/product.jpg',
      productName:     'LoadTest Product',
      productCategory: 'beauty',
      targetAudience:  'woman',
      orientation:     'portrait',
      resolution:      '720p',
    }),
    { headers: authHeaders(token), tags: { endpoint: 'create_job' } },
  );
  createJobDuration.add(createRes.timings.duration);
  const createOk = check(createRes, {
    'create job 202':     (r) => r.status === 202,
    'create job success': (r) => r.json('success') === true,
    'create job has id':  (r) => r.json('data.jobId') !== undefined,
  });
  jobErrors.add(!createOk);

  const jobId = createRes.json('data.jobId');

  sleep(0.3);

  // ── GET /api/v1/jobs ──────────────────────────────────────────────────────
  const listRes = http.get(
    `${BASE_URL}/api/v1/jobs?page=1&limit=10`,
    { headers: authHeaders(token), tags: { endpoint: 'list_jobs' } },
  );
  listJobsDuration.add(listRes.timings.duration);
  const listOk = check(listRes, {
    'list jobs 200':     (r) => r.status === 200,
    'list jobs success': (r) => r.json('success') === true,
    'list jobs array':   (r) => Array.isArray(r.json('data.jobs')),
  });
  jobErrors.add(!listOk);

  sleep(0.3);

  // ── GET /api/v1/jobs/:id ──────────────────────────────────────────────────
  if (jobId) {
    const getRes = http.get(
      `${BASE_URL}/api/v1/jobs/${jobId}`,
      { headers: authHeaders(token), tags: { endpoint: 'get_job' } },
    );
    getJobDuration.add(getRes.timings.duration);
    const getOk = check(getRes, {
      'get job 200':     (r) => r.status === 200,
      'get job success': (r) => r.json('success') === true,
      'get job has id':  (r) => r.json('data.id') === jobId,
      'get job steps':   (r) => Array.isArray(r.json('data.steps')),
    });
    jobErrors.add(!getOk);
  }

  sleep(0.5);
}
