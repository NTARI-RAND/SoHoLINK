import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';
import { BASE_URL, CONSUMER_EMAIL, CONSUMER_PASSWORD } from './config.js';

const errorRate = new Rate('errors');

export const options = {
  vus: 50,
  duration: '30s',
  thresholds: {
    'http_req_duration{name:marketplace}': ['p(95)<500'],
    errors: ['rate<0.01'],
  },
};

export function setup() {
  const res = http.post(
    `${BASE_URL}/login`,
    { email: CONSUMER_EMAIL, password: CONSUMER_PASSWORD, role: 'consumer' },
    { redirects: 0 }
  );
  const cookie = res.cookies['session_token'];
  if (!cookie || cookie.length === 0) {
    throw new Error(`setup: login failed — status ${res.status}, no session_token cookie`);
  }
  return { sessionToken: cookie[0].value };
}

export default function (data) {
  const res = http.get(`${BASE_URL}/consumer/marketplace`, {
    headers: { Cookie: `session_token=${data.sessionToken}` },
    tags: { name: 'marketplace' },
  });

  const ok = check(res, {
    'status is 200': (r) => r.status === 200,
    'body contains marketplace': (r) => r.body.includes('marketplace'),
  });
  errorRate.add(!ok);

  sleep(0.5);
}
