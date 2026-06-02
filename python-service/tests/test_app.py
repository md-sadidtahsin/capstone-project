import importlib
import os
import tempfile
from datetime import datetime
from unittest.mock import MagicMock, patch

import pytest
import requests

# Use a temporary database file for tests.
db_fd, db_path = tempfile.mkstemp(suffix='.db')
os.close(db_fd)
os.environ['DATABASE'] = db_path

import app as app_module
from app import app, get_db, init_db


@pytest.fixture(autouse=True)
def client():
    if os.path.exists(db_path):
        os.remove(db_path)
    init_db()
    with app.test_client() as client:
        yield client


def teardown_module(module):
    try:
        os.remove(db_path)
    except OSError:
        pass


def test_dashboard_route_returns_html(client):
    response = client.get('/')
    assert response.status_code == 200
    assert b'<html' in response.data or b'<!doctype html>' in response.data


def test_health_route_returns_json(client):
    response = client.get('/health')
    assert response.status_code == 200
    data = response.get_json()
    assert data['status'] == 'healthy'
    assert data['service'] == 'python-metadata-service'
    assert 'timestamp' in data


def test_create_short_url_success_stores_metadata(client):
    go_response = MagicMock(status_code=200)
    go_response.json.return_value = {'short_code': 'abc123'}
    node_response = MagicMock(status_code=200)
    node_response.json.return_value = {
        'status': 'success',
        'title': 'Test Page',
        'description': 'Description',
        'favicon_url': 'http://example.com/favicon.ico',
    }

    with patch('app.requests.post', side_effect=[go_response, node_response]):
        response = client.post('/create', data={'long_url': 'http://example.com'})

    assert response.status_code == 200
    payload = response.get_json()
    assert payload['short_code'] == 'abc123'
    assert payload['metadata']['status'] == 'success'

    conn = get_db()
    cursor = conn.cursor()
    cursor.execute('SELECT short_code, metadata_status FROM url_metadata WHERE short_code = ?', ('abc123',))
    row = cursor.fetchone()
    conn.close()

    assert row is not None
    assert row['short_code'] == 'abc123'
    assert row['metadata_status'] == 'fetched'


def test_create_short_url_go_service_unavailable(client):
    with patch('app.requests.post', side_effect=requests.exceptions.RequestException('service down')):
        response = client.post('/create', data={'long_url': 'http://example.com'})

    assert response.status_code == 503
    assert response.get_json() == {'error': 'Go service unavailable'}


def test_receive_event_invalid_payload(client):
    response = client.post('/api/events', json={'foo': 'bar'})
    assert response.status_code == 400
    assert response.get_json() == {'error': 'Invalid event data'}


def test_receive_event_processes_click_event(client):
    conn = get_db()
    cursor = conn.cursor()
    cursor.execute(
        'INSERT INTO url_metadata (short_code, long_url, first_seen) VALUES (?, ?, ?)',
        ('abc123', 'http://example.com', datetime.now().isoformat()),
    )
    conn.commit()
    conn.close()

    response = client.post('/api/events', json={'short_code': 'abc123', 'clicked_at': '2026-06-01T00:00:00'})
    assert response.status_code == 200
    assert response.get_json() == {'status': 'success'}

    conn = get_db()
    cursor = conn.cursor()
    cursor.execute('SELECT COUNT(*) FROM click_events WHERE short_code = ?', ('abc123',))
    click_count = cursor.fetchone()[0]
    cursor.execute('SELECT total_clicks FROM url_metadata WHERE short_code = ?', ('abc123',))
    total_clicks = cursor.fetchone()[0]
    conn.close()

    assert click_count == 1
    assert total_clicks == 1


def test_get_stats_returns_counts(client):
    conn = get_db()
    cursor = conn.cursor()
    cursor.execute(
        'INSERT INTO url_metadata (short_code, long_url, first_seen, total_clicks) VALUES (?, ?, ?, ?)',
        ('abc123', 'http://example.com', datetime.now().isoformat(), 2),
    )
    cursor.execute(
        'INSERT INTO click_events (short_code, clicked_at) VALUES (?, ?)',
        ('abc123', datetime.now().isoformat()),
    )
    conn.commit()
    conn.close()

    response = client.get('/api/stats')
    assert response.status_code == 200
    data = response.get_json()
    assert data['total_urls'] == 1
    assert data['total_clicks'] == 1
    assert isinstance(data['top_urls'], list)
    assert isinstance(data['all_urls'], list)


def test_create_short_url_node_service_error_stores_failed_metadata(client):
    go_response = MagicMock(status_code=200)
    go_response.json.return_value = {'short_code': 'abc123'}
    node_response = MagicMock(status_code=500)

    with patch('app.requests.post', side_effect=[go_response, node_response]):
        response = client.post('/create', data={'long_url': 'http://example.com'})

    assert response.status_code == 200
    payload = response.get_json()
    assert payload['metadata']['status'] == 'unavailable'

    conn = get_db()
    cursor = conn.cursor()
    cursor.execute('SELECT metadata_status FROM url_metadata WHERE short_code = ?', ('abc123',))
    row = cursor.fetchone()
    conn.close()

    assert row['metadata_status'] == 'failed'


def test_init_redis_success(monkeypatch):
    mock_redis = MagicMock()
    mock_client = MagicMock()
    mock_redis.return_value = mock_client
    mock_thread = MagicMock()
    mock_thread.start = MagicMock()

    monkeypatch.setattr(app_module, 'redis', MagicMock(Redis=mock_redis))
    monkeypatch.setattr(app_module, 'threading', MagicMock(Thread=lambda *args, **kwargs: mock_thread))

    app_module.init_redis()

    mock_redis.assert_called_once()
    mock_client.ping.assert_called_once()
    mock_thread.start.assert_called_once()
