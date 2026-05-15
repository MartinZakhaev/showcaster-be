// Shared configuration for all k6 load test scripts.
export const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

// Default thresholds applied to every test.
// Override per-script by merging with your own thresholds object.
export const defaultThresholds = {
  // 95th-percentile response time must stay under 500 ms
  http_req_duration: ['p(95)<500'],
  // Error rate must stay below 1%
  http_req_failed: ['rate<0.01'],
};

// Reusable JSON content-type header
export const jsonHeaders = { 'Content-Type': 'application/json' };

// Auth header builder
export function authHeaders(token) {
  return {
    'Content-Type': 'application/json',
    Authorization: `Bearer ${token}`,
  };
}
