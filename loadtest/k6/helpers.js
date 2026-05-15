import http from 'k6/http';
import { check } from 'k6';
import { BASE_URL, jsonHeaders, authHeaders } from './config.js';

// ── Auth helpers ─────────────────────────────────────────────────────────────

/**
 * Registers a new user and returns { email, password }.
 * Uses a unique suffix so parallel VUs don't collide.
 */
export function registerUser(suffix) {
  const email = `loadtest_${suffix}_${Date.now()}@example.com`;
  const password = 'LoadTest1!';
  const res = http.post(
    `${BASE_URL}/api/v1/auth/register`,
    JSON.stringify({ email, fullName: 'Load Tester', password }),
    { headers: jsonHeaders },
  );
  check(res, { 'register 201': (r) => r.status === 201 });
  return { email, password };
}

/**
 * Logs in with the given credentials and returns the JWT token string.
 * Fails the check and returns null if login is unsuccessful.
 */
export function loginUser(email, password) {
  const res = http.post(
    `${BASE_URL}/api/v1/auth/login`,
    JSON.stringify({ email, password }),
    { headers: jsonHeaders },
  );
  const ok = check(res, { 'login 200': (r) => r.status === 200 });
  if (!ok) return null;
  return res.json('data.token');
}

// ── Job helpers ───────────────────────────────────────────────────────────────

export const sampleJobPayload = {
  modelImageUrl: 'https://res.cloudinary.com/demo/image/upload/sample.jpg',
  productImageUrl: 'https://res.cloudinary.com/demo/image/upload/product.jpg',
  productName: 'LoadTest Product',
  productCategory: 'beauty',
  targetAudience: 'woman',
  orientation: 'portrait',
  resolution: '720p',
};

/**
 * Submits a job and returns the jobId string, or null on failure.
 */
export function createJob(token) {
  const res = http.post(
    `${BASE_URL}/api/v1/jobs/generate`,
    JSON.stringify(sampleJobPayload),
    { headers: authHeaders(token) },
  );
  const ok = check(res, { 'create job 202': (r) => r.status === 202 });
  if (!ok) return null;
  return res.json('data.jobId');
}
