process.env.DATABASE = ':memory:';

const request = require('supertest');
const axios = require('axios');
const { app, db, fetchUrlMetadata } = require('../server');

jest.mock('axios');

afterAll((done) => {
  db.close(done);
});

test('GET /health returns healthy status', async () => {
  const res = await request(app).get('/health');
  expect(res.status).toBe(200);
  expect(res.body).toEqual(expect.objectContaining({ status: 'healthy', service: 'metadata-service' }));
});

test('POST /api/metadata returns validation error when payload is missing', async () => {
  const res = await request(app)
    .post('/api/metadata')
    .send({ short_code: 'abc123' })
    .set('Accept', 'application/json');

  expect(res.status).toBe(400);
  expect(res.body).toEqual({ error: 'short_code and long_url are required' });
});

test('POST /api/metadata stores and returns metadata', async () => {
  const html = '<html><head><title>Test Page</title><meta name="description" content="Desc"></head><body></body></html>';
  axios.get.mockResolvedValue({ data: html });

  const payload = { short_code: 'abc123', long_url: 'http://example.com' };

  const res = await request(app)
    .post('/api/metadata')
    .send(payload)
    .set('Accept', 'application/json');

  expect(res.status).toBe(200);
  expect(res.body).toEqual(expect.objectContaining({
    short_code: payload.short_code,
    url: payload.long_url,
    title: 'Test Page',
    description: 'Desc',
    favicon_url: 'http://example.com/favicon.ico',
    status: 'success'
  }));
});

test('GET /api/metadata/:short_code returns stored metadata', async () => {
  const html = '<html><head><title>Test Page</title><meta name="description" content="Desc"></head><body></body></html>';
  axios.get.mockResolvedValue({ data: html });

  await request(app)
    .post('/api/metadata')
    .send({ short_code: 'abc123', long_url: 'http://example.com' })
    .set('Accept', 'application/json');

  const res = await request(app).get('/api/metadata/abc123');

  expect(res.status).toBe(200);
  expect(res.body).toEqual(expect.objectContaining({ short_code: 'abc123', url: 'http://example.com' }));
});

test('GET /api/metadata returns metadata list', async () => {
  const res = await request(app).get('/api/metadata');
  expect(res.status).toBe(200);
  expect(res.body).toEqual(expect.objectContaining({ metadata: expect.any(Array), count: expect.any(Number) }));
});

test('GET /api/metadata/:short_code returns 404 when metadata is missing', async () => {
  const res = await request(app).get('/api/metadata/notfound');
  expect(res.status).toBe(404);
  expect(res.body).toEqual({ error: 'Metadata not found' });
});

test('fetchUrlMetadata falls back to og:title and og:description when title is missing', async () => {
  axios.get.mockResolvedValue({ data: '<html><head><meta property="og:title" content="OG Title"><meta property="og:description" content="OG Description"></head></html>' });
  const metadata = await fetchUrlMetadata('http://example.com');
  expect(metadata.title).toBe('OG Title');
  expect(metadata.description).toBe('OG Description');
});

test('fetchUrlMetadata converts relative favicon URLs to absolute URLs', async () => {
  axios.get.mockResolvedValue({ data: '<html><head><title>Title</title><meta name="description" content="Desc"><link rel="icon" href="/images/favicon.ico"></head></html>' });
  const metadata = await fetchUrlMetadata('http://example.com');
  expect(metadata.favicon_url).toBe('http://example.com/images/favicon.ico');
});

test('fetchUrlMetadata converts protocol-relative favicon URLs to absolute URLs', async () => {
  axios.get.mockResolvedValue({ data: '<html><head><title>Title</title><meta name="description" content="Desc"><link rel="icon" href="//example.com/favicon.ico"></head></html>' });
  const metadata = await fetchUrlMetadata('http://example.com');
  expect(metadata.favicon_url).toBe('http://example.com/favicon.ico');
});

test('fetchUrlMetadata converts path-relative favicon URLs to absolute URLs', async () => {
  axios.get.mockResolvedValue({ data: '<html><head><title>Title</title><meta name="description" content="Desc"><link rel="icon" href="images/favicon.ico"></head></html>' });
  const metadata = await fetchUrlMetadata('http://example.com');
  expect(metadata.favicon_url).toBe('http://example.com/images/favicon.ico');
});

test('GET /api/metadata/:short_code returns 500 when database get errors', async () => {
  const originalGet = db.get;
  db.get = jest.fn((sql, params, callback) => callback(new Error('DB failure'), null));

  const res = await request(app).get('/api/metadata/abc123');
  expect(res.status).toBe(500);
  expect(res.body).toEqual({ error: 'Database error' });

  db.get = originalGet;
});

test('GET /api/metadata returns 500 when database all errors', async () => {
  const originalAll = db.all;
  db.all = jest.fn((sql, callback) => callback(new Error('DB failure'), null));

  const res = await request(app).get('/api/metadata');
  expect(res.status).toBe(500);
  expect(res.body).toEqual({ error: 'Database error' });

  db.all = originalAll;
});

test('POST /api/metadata returns 500 when database insert fails', async () => {
  const html = '<html><head><title>Test Page</title><meta name="description" content="Desc"></head><body></body></html>';
  axios.get.mockResolvedValue({ data: html });

  const originalRun = db.run;
  db.run = jest.fn((sql, params, callback) => callback(new Error('DB insert failed')));

  const res = await request(app)
    .post('/api/metadata')
    .send({ short_code: 'abc123', long_url: 'http://example.com' })
    .set('Accept', 'application/json');

  expect(res.status).toBe(500);
  expect(res.body).toEqual({ error: 'Failed to store metadata' });

  db.run = originalRun;
});

test('fetchUrlMetadata falls back to default title when no title is present', async () => {
  axios.get.mockResolvedValue({ data: '<html><head></head><body></body></html>' });
  const metadata = await fetchUrlMetadata('http://example.com');

  expect(metadata.title).toBe('No title found');
  expect(metadata.description).toBe('No description available');
  expect(metadata.favicon_url).toBe('http://example.com/favicon.ico');
});

test('fetchUrlMetadata falls back to default description when no description is present', async () => {
  axios.get.mockResolvedValue({ data: '<html><head><title>Title</title></head><body></body></html>' });
  const metadata = await fetchUrlMetadata('http://example.com');

  expect(metadata.title).toBe('Title');
  expect(metadata.description).toBe('No description available');
  expect(metadata.favicon_url).toBe('http://example.com/favicon.ico');
});

test('fetchUrlMetadata returns fallback on axios failure', async () => {
  axios.get.mockRejectedValue(new Error('Network failure'));
  const metadata = await fetchUrlMetadata('http://example.com');

  expect(metadata).toEqual({
    title: 'Unable to fetch title',
    description: 'Could not retrieve page information',
    favicon_url: null
  });
});
