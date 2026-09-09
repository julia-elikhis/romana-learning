"""Verify the local MicroK8s app uses explicit parameters for its dedicated database.

Read-only: does not start another application or modify saved answers.
Account isolation and write/persistence checks are covered by microk8s.py test.
"""
import json
from microk8s import APP, kube, namespace, sql

namespace()
config = json.loads(kube('get', 'configmap', APP, '-o', 'json').stdout)['data']
assert config['PGHOST'] == 'postgres'
assert config['PGDATABASE'] == 'romanian_test'
assert config['PGPORT'] == '5432'
assert 'DATABASE_URL' not in config and 'PGPASSWORD' not in config
assert sql('SELECT current_database()') == config['PGDATABASE']
deployment = json.loads(kube('get', 'deployment', APP, '-o', 'json').stdout)
container = deployment['spec']['template']['spec']['containers'][0]
assert any(e.get('configMapRef', {}).get('name') == APP for e in container['envFrom'])
env = {entry['name']: entry for entry in container['env']}
assert 'DATABASE_URL' not in env
for key in ['PGUSER', 'PGPASSWORD']:
    assert env[key]['valueFrom']['secretKeyRef']['name'] == 'test-database'
print('PASS: dedicated database parameters and credential Secret are wired into the app')
