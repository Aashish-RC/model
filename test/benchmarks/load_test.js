import http from 'k6/http';
import { check } from 'k6';

export const options = {
  scenarios: {
    constant_rate: {
      executor: 'constant-arrival-rate',
      rate: __ENV.TARGET_RPS ? parseInt(__ENV.TARGET_RPS) : 500,
      timeUnit: '1s',
      duration: __ENV.TEST_DURATION ? __ENV.TEST_DURATION : '10s',
      preAllocatedVUs: __ENV.PRE_VUS ? parseInt(__ENV.PRE_VUS) : 100,
      maxVUs: __ENV.MAX_VUS ? parseInt(__ENV.MAX_VUS) : 1000,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  const url = (__ENV.GATEWAY_URL || 'http://127.0.0.1:8080') + '/v1/chat/completions';
  const payload = JSON.stringify({
    model: 'gpt-4o',
    messages: [
      { role: 'user', content: 'Benchmark payload test.' }
    ],
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Bearer ' + (__ENV.AUTH_TOKEN || 'sk-gw-default-test'),
    },
  };

  const res = http.post(url, payload, params);
  check(res, {
    'status is 200': (r) => r.status === 200,
  });
}
