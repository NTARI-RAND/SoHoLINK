import http from 'k6/http';
import { check } from 'k6';
import { Rate } from 'k6/metrics';
import { BASE_URL } from './config.js';

const serverErrorRate = new Rate('server_errors');

export const options = {
  vus: 10,
  duration: '60s',
  thresholds: {
    server_errors: ['rate==0'],
  },
};

export default function () {
  // Rotate through seed consumer emails with bad passwords to exercise the
  // rate limiter. Both 401 (bad credentials) and 429 (rate limited) are
  // expected and acceptable outcomes.
  const idx = (__ITER % 10) + 1;
  const email = `consumer-${String(idx).padStart(2, '0')}@seed.internal`;

  const res = http.post(
    `${BASE_URL}/login`,
    { email: email, password: 'wrongpassword', role: 'consumer' },
    { redirects: 0 }
  );

  check(res, {
    'no server error (not 5xx)': (r) => r.status < 500,
    'expected outcome (401 or 429)': (r) => r.status === 401 || r.status === 429,
  });

  serverErrorRate.add(res.status >= 500);
}
