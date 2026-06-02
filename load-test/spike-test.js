import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';

const errorRate = new Rate('errors');

export const options = {
  stages: [
    { duration: '2m', target: 30 },   // Ramp up (morning traffic)
    { duration: '3m', target: 100 },  // 12:00 PM SPIKE
    { duration: '2m', target: 100 },  // Hold peak
    { duration: '2m', target: 20 },   // Cool down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],
    errors: ['rate<0.1'],
  },
};

export default function () {
  // Create URL
  const createRes = http.post('http://a7f1995bb6fbc41c88fc5f196970ecb8-431e7041e1697807.elb.ap-south-1.amazonaws.com/create', { long_url: 'https://example.com' });
  check(createRes, { 'status 200': (r) => r.status === 200 }) || errorRate.add(1);
  sleep(0.5);

  // Click short URL
  if (createRes.json()?.short_code) {
    const clickRes = http.get(`http://a7f1995bb6fbc41c88fc5f196970ecb8-431e7041e1697807.elb.ap-south-1.amazonaws.com/api/go/${createRes.json().short_code}`, { redirects: 0 });
    check(clickRes, { 'redirect 3xx': (r) => r.status >= 300 && r.status < 400 }) || errorRate.add(1);
  }
  sleep(0.5);
}