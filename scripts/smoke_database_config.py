"""Verify parameter-based configuration against a separate DB on local Postgres.

Creates and removes only a uniquely named test database and test container.
Does not use any remote credentials or modify the learner's progress.
"""
import json
import subprocess
import time
import uuid

token = uuid.uuid4().hex
database = 'romanian_chart_test_' + token
container = 'romanian-chart-test-' + token
attempt = 'chart-' + token


def run(*args):
    return subprocess.run(args, check=True, text=True, capture_output=True).stdout.strip()


def sql(db, statement):
    return run('docker', 'compose', 'exec', '-T', 'postgres', 'psql', '-U',
               'romanian', '-d', db, '-v', 'ON_ERROR_STOP=1', '-Atc', statement)


created = False
try:
    sql('postgres', 'CREATE DATABASE ' + database)
    created = True
    run('docker', 'compose', 'run', '--no-deps', '-d', '--name', container,
        '-e', 'DATABASE_URL=', '-e', 'PGHOST=postgres', '-e', 'PGPORT=5432',
        '-e', 'PGDATABASE=' + database, '-e', 'PGUSER=romanian',
        '-e', 'PGPASSWORD=local-development-only', '-e', 'PGSSLMODE=disable', 'app')
    for _ in range(30):
        try:
            ready = run('docker', 'exec', container, 'wget', '-qO-', 'http://127.0.0.1:8080/readyz')
            if json.loads(ready)['status'] == 'ready':
                break
        except (subprocess.CalledProcessError, json.JSONDecodeError):
            pass
        time.sleep(1)
    else:
        raise AssertionError('Parameter-configured app did not become ready')
    payload = json.dumps({'id': attempt, 'exerciseId': 'home-1', 'answer': 'case'})
    result = run('docker', 'exec', container, 'wget', '-qO-',
                 '--header=Content-Type:application/json', '--post-data=' + payload,
                 'http://127.0.0.1:8080/api/attempts')
    assert json.loads(result)['correct'] is True
    assert sql(database, "SELECT count(*) FROM attempts WHERE id='" + attempt + "'") == '1'
    assert sql('romanian', "SELECT count(*) FROM attempts WHERE id='" + attempt + "'") == '0'
    print('PASS: explicit PostgreSQL settings save only into the separate selected database')
finally:
    subprocess.run(['docker', 'rm', '-f', container], capture_output=True)
    if created:
        sql('postgres', 'DROP DATABASE ' + database)
