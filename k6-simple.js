import http from 'k6/http';
import { sleep } from 'k6';
import { check } from 'k6';

export let options = {
    stages: [
        { duration: '30s', target: 100 }, // Ramp up to 1,000 users in 1 minute
        { duration: '1m', target: 100 }, // Stay at 10,000 users for 3 minutes
        { duration: '30s', target: 0 }, // Ramp down to 0 users in 1 minute
    ],
    thresholds: {
        http_req_duration: ['p(99)<500'], // 99% of requests must complete below 500ms
        'http_req_duration{staticAsset:yes}': ['p(99)<300'], // 99% of static asset requests should be below 300ms
        http_req_failed: ['rate<0.001'], // Error rate should be less than 01%
    },
};

export default function () {
    let res = http.get('http://localhost:8881/timestamp');
    check(res, {
        'is status 200': (r) => r.status === 200,
    });
    // sleep(1); // Adjust the sleep duration as needed
}
