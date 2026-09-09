"""Verify the local API, idempotency, and container-restart persistence.

Uses one uniquely identified test attempt and removes only that attempt afterward.
Run from the repository root with the Compose stack already running.
"""
import json
import subprocess
import time
import urllib.error
import urllib.request
import uuid

base = 'http://127.0.0.1:8080'
attempt_id = 'smoke-' + str(uuid.uuid4())


def request(path, body=None):
    req = urllib.request.Request(base + path,
        data=json.dumps(body).encode() if body is not None else None,
        headers={'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=5) as response:
        return json.load(response)


try:
    assert request('/readyz')['status'] == 'ready'
    exercises = request('/api/exercises')
    assert len(exercises) == 5 and 'answer' not in exercises[0]
    payload = {'id': attempt_id, 'exerciseId': 'home-1', 'answer': 'case'}
    assert request('/api/attempts', payload)['correct'] is True
    saved = request('/api/progress')
    assert request('/api/attempts', payload)['correct'] is True
    assert request('/api/progress') == saved, 'Retry double-counted progress'
    try:
        request('/api/attempts', {**payload, 'answer': 'casă'})
        raise AssertionError('Conflicting attempt should fail')
    except urllib.error.HTTPError as error:
        assert error.code == 409
    subprocess.run(['docker', 'compose', 'restart', 'postgres', 'app'], check=True)
    for _ in range(45):
        try:
            if request('/readyz')['status'] == 'ready':
                break
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(1)
    else:
        raise AssertionError('App did not recover after restart')
    assert request('/api/progress') == saved, 'Progress changed after restart'
    print('PASS: health, exercise payload, save, retry deduplication, conflict, restart persistence')
finally:
    subprocess.run(['docker', 'compose', 'exec', '-T', 'postgres', 'psql',
                    '-U', 'romanian', '-d', 'romanian', '-v', 'ON_ERROR_STOP=1',
                    '-c', "DELETE FROM attempts WHERE id = '" + attempt_id + "'"], check=True)
