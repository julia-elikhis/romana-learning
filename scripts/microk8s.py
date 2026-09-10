"""Deploy and exercise the Helm chart in a dedicated local MicroK8s VM.

Uses a dedicated namespace, persistent PostgreSQL and MinIO, and SOPS-encrypted settings.
Tests use synthetic lessons and remove their own records.
"""
import argparse
import base64
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
import uuid
import sops_secrets
import storage_settings
import workload_identity

ROOT = Path(__file__).resolve().parents[1]
VM = os.environ.get('MICROK8S_VM', 'romana-microk8s')
NS = os.environ.get('MICROK8S_NAMESPACE', 'romana-test')
RELEASE = 'practice'
APP = RELEASE + '-romanian'
MARKER = 'romana-chart-test'
PORT = int(os.environ.get('MICROK8S_PORT', '8080'))
if not re.fullmatch(r'[a-z0-9][a-z0-9-]{0,50}', VM) or not re.fullmatch(r'romana-test(?:-[a-z0-9-]+)?', NS):
    raise SystemExit('Use a valid VM name and a dedicated romana-test namespace.')
if not 1024 <= PORT <= 65535:
    raise SystemExit('MICROK8S_PORT must be between 1024 and 65535.')


def run(args, data=None, capture=True, check=True, timeout=300):
    sops_secrets.require_local()
    result = subprocess.run(args, cwd=ROOT, input=data, stdout=subprocess.PIPE if capture else None,
                            stderr=subprocess.PIPE if capture else None, timeout=timeout)
    if check and result.returncode:
        # Captured kubectl payloads may contain Secret data: don't echo them on failure.
        raise RuntimeError('Command failed: ' + ' '.join(args[:6]))
    return result


def guest(*args, **kwargs):
    return run(['limactl', 'shell', '--workdir=/tmp', VM, *args], **kwargs)


def kube(*args, **kwargs):
    return guest('sudo', 'microk8s', 'kubectl', '-n', NS, *args, **kwargs)


def setup():
    instances = run(['limactl', 'list', '--json']).stdout.decode().splitlines()
    if not any(json.loads(line).get('name') == VM for line in instances if line.strip()):
        run(['limactl', 'start', '--name=' + VM, '--cpus=2', '--memory=4', '--disk=40', '--vm-type=vz',
             '--containerd=none', '--mount-none', '--yes', 'template:ubuntu-24.04'], capture=False, timeout=900)
    else:
        run(['limactl', 'start', '--yes', VM], capture=False)
    if guest('test', '-x', '/snap/bin/microk8s', check=False).returncode:
        guest('sudo', 'snap', 'install', 'microk8s', '--classic', '--channel=1.35/stable', capture=False, timeout=900)
    guest('sudo', 'microk8s', 'status', '--wait-ready', capture=False)
    for addon in ['dns', 'hostpath-storage']:
        guest('sudo', 'microk8s', 'enable', addon, capture=False)
    print('MicroK8s is ready.', flush=True)


def namespace():
    existing = kube('get', 'namespace', NS, '-o', 'json', '--ignore-not-found').stdout
    if existing.strip():
        if json.loads(existing)['metadata'].get('labels', {}).get('app.kubernetes.io/part-of') != MARKER:
            raise RuntimeError('Refusing to modify a namespace not owned by this test harness.')
    else:
        apply({'apiVersion': 'v1', 'kind': 'Namespace', 'metadata': {'name': NS, 'labels': {'app.kubernetes.io/part-of': MARKER}}})


def apply(resource):
    kube('apply', '-f', '-', data=json.dumps(resource).encode())


def ensure_secret(name, values):
    existing = kube('get', 'secret', name, '-o', 'json', '--ignore-not-found').stdout
    if existing.strip():
        encoded = {key: base64.b64encode(value.encode()).decode() for key, value in values.items()}
        if json.loads(existing).get('data') != encoded:
            raise RuntimeError('Encrypted infrastructure credentials differ from the running Secret. Rotate credentials in the service before deploying.')
    else:
        # Use create with stdin so credentials aren't stored in an apply annotation or argv.
        kube('create', '-f', '-', data=json.dumps({'apiVersion': 'v1', 'kind': 'Secret',
             'metadata': {'name': name, 'namespace': NS}, 'type': 'Opaque', 'stringData': values}).encode())


def private_environment():
    # SOPS verifies integrity and decrypts only into this process's memory.
    # Missing keys and damaged ciphertext fail before any deployment changes.
    sops_secrets.require_local()
    return sops_secrets.load()


def encrypt_secrets():
    """Migrate an existing local installation without changing credentials."""
    sops_secrets.require_local()
    if sops_secrets.secret_file().exists():
        raise RuntimeError('Encrypted settings already exist. Use make secrets-edit.')
    namespace()
    values = {key: '' for key in sops_secrets.settings_keys('local')}
    for name, mapping in [('test-database', {'username': 'PGUSER', 'password': 'PGPASSWORD'}),
                          ('test-minio', {'rootUser': 'MINIO_ROOT_USER', 'rootPassword': 'MINIO_ROOT_PASSWORD'}),
                          ('exercise-api', {key: key for key in sops_secrets.APP_KEYS if key.startswith('EXERCISE_')}),
                          ('github-auth', {key: key for key in sops_secrets.APP_KEYS if not key.startswith('EXERCISE_')})]:
        raw = kube('get', 'secret', name, '-o', 'json', '--ignore-not-found').stdout
        if raw.strip():
            data = json.loads(raw).get('data', {})
            for source, target in mapping.items():
                if source in data:
                    values[target] = base64.b64decode(data[source]).decode()
    plaintext = ROOT / '.env'
    if plaintext.is_file():
        # Compose parses the old env_file once. No plaintext copy or shell evaluation.
        config = {'services': {'settings': {'image': 'romanian:local', 'network_mode': 'none',
                  'env_file': [str(plaintext)], 'entrypoint': ['/usr/bin/env', '-0']}}}
        result = run(['docker', 'compose', '--project-name', 'romana-secret-import', '--project-directory', str(ROOT),
                      '--env-file', '/dev/null', '-f', '-', 'run', '--rm', '-T', '--no-deps', 'settings'],
                     data=json.dumps(config).encode())
        original = dict(line.split('=', 1) for line in result.stdout.decode().split('\0') if '=' in line)
        values.update({key: original.get(key, '') for key in sops_secrets.APP_KEYS})
    sops_secrets.save(values)
    if sops_secrets.load() != values:
        raise RuntimeError('Encrypted settings verification failed; plaintext was retained.')
    if plaintext.is_file():
        plaintext.unlink()
    print('Existing credentials encrypted; plaintext .env retired after verification.', flush=True)


def generation(environment):
    values = {key: environment.get(key, '') for key in
              ['EXERCISE_API_URL', 'EXERCISE_API_KEY', 'EXERCISE_API_MODEL', 'EXERCISE_API_EFFORT', 'EXERCISE_API_EFFORT_FORMAT']}
    if not values['EXERCISE_API_URL']:
        return '', False
    return sync_secret('exercise-api', values)


def authentication(environment):
    values = {key: environment.get(key, '') for key in ['GITHUB_CLIENT_ID', 'GITHUB_CLIENT_SECRET', 'AUTH_LEGACY_GITHUB_ID']}
    if not values['GITHUB_CLIENT_ID'] and not values['GITHUB_CLIENT_SECRET']:
        return '', False
    if not values['GITHUB_CLIENT_ID'] or not values['GITHUB_CLIENT_SECRET']:
        raise RuntimeError('Set both GitHub OAuth credentials in the SOPS-encrypted settings.')
    if environment.get('APP_BASE_URL'):
        values['APP_BASE_URL'] = environment['APP_BASE_URL']
    return sync_secret('github-auth', values)


def sync_secret(name, values):
    existing = kube('get', 'secret', name, '-o', 'json', '--ignore-not-found').stdout
    resource = {'apiVersion': 'v1', 'kind': 'Secret', 'metadata': {'name': name, 'namespace': NS},
                'type': 'Opaque', 'stringData': values}
    operation = 'create'
    if existing.strip():
        current = json.loads(existing)
        encoded = {key: base64.b64encode(value.encode()).decode() for key, value in values.items()}
        if current.get('data') == encoded:
            return name, False
        resource['metadata']['resourceVersion'] = current['metadata']['resourceVersion']
        operation = 'replace'
    kube(operation, '-f', '-', data=json.dumps(resource).encode())
    print('Private settings synchronized to Secret ' + name + '.', flush=True)
    return name, True


def database(environment):
    ensure_secret('test-database', {'username': environment['PGUSER'], 'password': environment['PGPASSWORD']})
    ensure_secret('test-minio', {'rootUser': environment['MINIO_ROOT_USER'], 'rootPassword': environment['MINIO_ROOT_PASSWORD']})
    apply({'apiVersion': 'v1', 'kind': 'PersistentVolumeClaim', 'metadata': {'name': 'test-postgres', 'namespace': NS},
           'spec': {'accessModes': ['ReadWriteOnce'], 'storageClassName': 'microk8s-hostpath', 'resources': {'requests': {'storage': '1Gi'}}}})
    apply({'apiVersion': 'v1', 'kind': 'Service', 'metadata': {'name': 'postgres', 'namespace': NS},
           'spec': {'selector': {'app': 'test-postgres'}, 'ports': [{'port': 5432, 'targetPort': 5432}]}})
    container = {'name': 'postgres', 'image': 'postgres:17-alpine', 'env': [
        {'name': 'POSTGRES_DB', 'value': 'romanian_test'},
        {'name': 'PGDATA', 'value': '/var/lib/postgresql/data/pgdata'},
        {'name': 'POSTGRES_USER', 'valueFrom': {'secretKeyRef': {'name': 'test-database', 'key': 'username'}}},
        {'name': 'POSTGRES_PASSWORD', 'valueFrom': {'secretKeyRef': {'name': 'test-database', 'key': 'password'}}}],
        'ports': [{'containerPort': 5432}],
        'volumeMounts': [{'name': 'database', 'mountPath': '/var/lib/postgresql/data'}],
        'readinessProbe': {'exec': {'command': ['sh', '-c', 'pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"']}, 'periodSeconds': 3},
        'resources': {'requests': {'cpu': '100m', 'memory': '128Mi'}, 'limits': {'memory': '512Mi'}}}
    apply({'apiVersion': 'apps/v1', 'kind': 'Deployment', 'metadata': {'name': 'test-postgres', 'namespace': NS},
           'spec': {'replicas': 1, 'strategy': {'type': 'Recreate'}, 'selector': {'matchLabels': {'app': 'test-postgres'}},
                    'template': {'metadata': {'labels': {'app': 'test-postgres'}}, 'spec': {'containers': [container],
                       'volumes': [{'name': 'database', 'persistentVolumeClaim': {'claimName': 'test-postgres'}}]}}}})
    kube('rollout', 'status', 'deployment/test-postgres', '--timeout=180s', capture=False)


def deploy():
    environment = private_environment()
    selected = storage_settings.mode()
    cloud_values = storage_settings.cloud(environment) if selected == 'gcs' else None
    namespace()
    current = storage_settings.active(sys.modules[__name__])
    if cloud_values is not None:
        account = kube('get', 'serviceaccount', APP, '--ignore-not-found', '-o', 'name').stdout
        if not account.strip():
            annotations = {'meta.helm.sh/release-name': RELEASE, 'meta.helm.sh/release-namespace': NS}
            if cloud_values.get('ServiceAccount'):
                annotations['iam.gke.io/gcp-service-account'] = cloud_values['ServiceAccount']
            apply({'apiVersion': 'v1', 'kind': 'ServiceAccount', 'metadata': {'name': APP, 'namespace': NS,
                   'labels': {'app.kubernetes.io/managed-by': 'Helm'}, 'annotations': annotations},
                   'automountServiceAccountToken': False})
        workload_identity.preflight(sys.modules[__name__], cloud_values)
    destination = storage_settings.target(selected, cloud_values)
    switching = current is not None and current != destination
    old_replicas = 1
    old_revision = None
    if switching:
        old_replicas = json.loads(kube('get', 'deployment', APP, '-o', 'json').stdout)['spec']['replicas']
        old_revision = json.loads(guest('sudo', 'microk8s', 'helm3', 'status', RELEASE, '-n', NS, '-o', 'json').stdout)['version']
    print('Preparing the separate test database and credentials.', flush=True)
    database(environment)
    generation_secret, generation_changed = generation(environment)
    auth_secret, auth_changed = authentication(environment)
    google_changed = False
    if cloud_values is not None:
        # Versioned references preserve the previous configuration during Helm rollback.
        settings = {key: environment[key] for key in sops_secrets.STORAGE_KEYS[:3]}
        configuration = {'credentials.json': json.dumps(workload_identity.credential_config(cloud_values))}
        def versioned_secret(prefix, data):
            digest = hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest()[:12]
            return sync_secret(prefix + '-' + digest, data)
        settings_secret, settings_changed = versioned_secret('google-storage', settings)
        identity_secret, identity_changed = versioned_secret('google-workload-identity', configuration)
        google_changed = settings_changed or identity_changed
    image = json.loads(run(['docker', 'image', 'inspect', 'romanian:local']).stdout)[0]
    tag = 'microk8s-' + image['Id'].split(':')[1][:12]
    run(['docker', 'tag', 'romanian:local', 'romanian:' + tag])
    remote = '/tmp/romana-chart-test-' + uuid.uuid4().hex
    guest('mkdir', '-p', remote)
    paused = False
    upgrade_started = False
    try:
        with tempfile.TemporaryDirectory(prefix='romana-chart-test-') as tmp:
            archive = Path(tmp) / 'app.tar'
            print('Importing the local application image into MicroK8s.', flush=True)
            run(['docker', 'save', '-o', str(archive), 'romanian:' + tag], timeout=300)
            run(['limactl', 'copy', str(archive), VM + ':' + remote + '/app.tar'], timeout=300)
        guest('sudo', 'microk8s', 'ctr', 'image', 'import', remote + '/app.tar', capture=False)
        buffer = io.BytesIO()
        chart = ROOT / 'charts' / 'romanian'
        # Copy only chart inputs needed by this profile, never private values or credentials.
        inputs = [chart / name for name in ['Chart.yaml', 'Chart.lock', 'values.yaml', 'values.schema.json', 'values-microk8s.yaml', 'values-minio.yaml', '.helmignore']]
        inputs.extend((chart / 'templates').rglob('*'))
        inputs.extend((chart / 'charts').glob('*.tgz'))
        with tarfile.open(fileobj=buffer, mode='w:gz') as archive:
            for path in inputs:
                if path.is_file() and not path.is_symlink() and '.private.' not in path.name:
                    archive.add(path, arcname='romanian/' + str(path.relative_to(chart)))
        guest('tar', '-xzf', '-', '-C', remote, data=buffer.getvalue())
        def upgrade(replicas=1):
            nonlocal upgrade_started
            args = ['sudo', 'microk8s', 'helm3', 'upgrade', '--install', RELEASE, remote + '/romanian',
                    '--namespace', NS, '-f', remote + '/romanian/values-microk8s.yaml']
            identity_values = {}
            if selected == 'minio':
                args += ['-f', remote + '/romanian/values-minio.yaml']
            else:
                identity_values = workload_identity.helm_values(cloud_values)
                args += ['--set-string', 'storage.gcs.settingsSecret=' + settings_secret,
                         '--set-string', 'storage.gcs.workloadIdentity.credentialsSecret=' + identity_secret,
                         '--set-string', 'storage.gcs.workloadIdentity.provider=' + cloud_values['Provider']]
            args += ['-f', '-', '--set-string', 'image.tag=' + tag, '--set-string', 'generation.existingSecret=' + generation_secret,
                     '--set-string', 'auth.existingSecret=' + auth_secret, '--set', 'replicaCount=' + str(replicas),
                     '--wait', '--timeout', '5m']
            upgrade_started = True
            guest(*args, data=json.dumps(identity_values).encode(), capture=False, timeout=360)

        if switching:
            print('Pausing the app while original documents are copied and verified.', flush=True)
            kube('scale', 'deployment/' + APP, '--replicas=0')
            paused = True
            kube('wait', '--for=delete', 'pod', '-l', 'app.kubernetes.io/name=romanian,app.kubernetes.io/instance=' + RELEASE, '--timeout=90s')
            originals = json.loads(sql("SELECT coalesce(json_agg(json_build_object('key',object_key,'hash',content_hash)),'[]'::json) FROM materials"))
            if selected == 'minio':
                # Start MinIO with the app stopped, so no request observes a partially copied library.
                upgrade(0)
            storage_settings.operation(sys.modules[__name__], 'sync', destination, originals, source=current)
            print('Verified ' + str(len(originals)) + ' originals in the selected storage. Source copies retained.', flush=True)
        print('Installing the app with ' + ('Google Cloud Storage.' if selected == 'gcs' else 'local MinIO.'), flush=True)
        upgrade()
        paused = False
        if generation_changed or auth_changed or google_changed:
            kube('rollout', 'restart', 'deployment/' + APP)
            kube('rollout', 'status', 'deployment/' + APP, '--timeout=180s', capture=False)
    except Exception:
        if paused:
            if upgrade_started and old_revision is not None:
                guest('sudo', 'microk8s', 'helm3', 'rollback', RELEASE, str(old_revision), '-n', NS, '--wait', '--timeout=5m', capture=False, timeout=360)
            kube('scale', 'deployment/' + APP, '--replicas=' + str(old_replicas))
            kube('rollout', 'status', 'deployment/' + APP, '--timeout=180s', capture=False)
        raise
    finally:
        # Remove only the temporary directory generated by this invocation.
        guest('rm', '-rf', remote)
    print('Deployment ready. Run make microk8s-test or make microk8s-forward.', flush=True)


def forward_command(pidfile, service=APP, local_port=PORT, target_port=8080):
    return ['limactl', 'shell', '--workdir=/tmp', VM, 'sudo', 'setsid', 'sh', '-c',
            'echo "$$" > "$1"; shift; exec "$@"', 'sh', pidfile,
            '/snap/bin/microk8s', 'kubectl', '-n', NS, 'port-forward', 'service/' + service,
            str(local_port) + ':' + str(target_port), '--address=127.0.0.1']


@contextlib.contextmanager
def forwarding(service=APP, local_port=PORT, target_port=8080, health_path='/readyz'):
    health_url = 'http://127.0.0.1:' + str(local_port) + health_path
    try:
        with urllib.request.urlopen(health_url, timeout=1):
            pass
    except (OSError, urllib.error.URLError):
        pass
    else:
        raise RuntimeError('Port ' + str(local_port) + ' is already in use; stop that listener before forwarding.')
    pidfile = '/tmp/romana-forward-' + uuid.uuid4().hex + '.pid'
    with tempfile.TemporaryFile() as log:
        process = subprocess.Popen(forward_command(pidfile, service, local_port, target_port), cwd=ROOT, stdout=log, stderr=log)
        try:
            for _ in range(60):
                if process.poll() is not None:
                    raise RuntimeError('MicroK8s port forwarding failed.')
                try:
                    with urllib.request.urlopen(health_url, timeout=2) as response:
                        if response.status == 200:
                            break
                except (OSError, urllib.error.URLError):
                    time.sleep(1)
            else:
                raise RuntimeError('MicroK8s app did not become reachable.')
            yield process
        finally:
            # Closing limactl alone leaves remote kubectl children alive. Stop the exact
            # process group created for this forward before closing the host process.
            try:
                guest('sudo', 'sh', '-c',
                      'if [ -f "$1" ]; then read -r pid < "$1"; '
                      '/bin/kill -TERM -- "-$pid" 2>/dev/null || true; rm -f -- "$1"; fi',
                      'sh', pidfile, timeout=30)
            finally:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


def request(path, body=None, method=None, raw=False, timeout=10, cookie=''):
    req = urllib.request.Request('http://127.0.0.1:' + str(PORT) + path,
          data=None if body is None else json.dumps(body).encode(), method=method,
          headers={'Content-Type': 'application/json', 'Cookie': cookie})
    with urllib.request.urlopen(req, timeout=timeout) as response:
        return response.read() if raw else json.load(response)


def test():
    namespace()
    config = json.loads(kube('get', 'configmap', APP, '-o', 'json').stdout)['data']
    local = config['COURSE_STORAGE_PROVIDER'] == 's3'
    assert config['COURSE_STORAGE_PROVIDER'] in ('s3', 'gcs')
    if local:
        assert config['COURSE_S3_ENDPOINT'] == 'http://' + RELEASE + '-minio:9000'
    claims_before = json.loads(kube('get', 'pvc', '-o', 'json').stdout)['items']
    assert len(claims_before) >= (2 if local else 1) and all(c['status']['phase'] == 'Bound' for c in claims_before)
    claim_uids = {c['metadata']['name']: c['metadata']['uid'] for c in claims_before}
    lesson = b'Eu sunt acasa in fiecare zi.\nNoi avem o casa foarte frumoasa.'
    material_id = ''
    attempt_id = 'chart-' + str(uuid.uuid4())
    session = test_session()
    def call(path, body=None, method=None, **kwargs):
        return request(path, body, method, cookie=session['cookie'], **kwargs)
    try:
        with forwarding():
            boundary = 'test-' + uuid.uuid4().hex
            body = (f'--{boundary}\r\nContent-Disposition: form-data; name="title"\r\n\r\nChart test {uuid.uuid4()}\r\n'
                    f'--{boundary}\r\nContent-Disposition: form-data; name="kind"\r\n\r\nnotes\r\n'
                    f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="lesson.txt"\r\n\r\n').encode()
            body += lesson + f'\r\n--{boundary}--\r\n'.encode()
            req = urllib.request.Request('http://127.0.0.1:' + str(PORT) + '/api/materials', data=body,
                  headers={'Content-Type': 'multipart/form-data; boundary=' + boundary, 'Cookie': session['cookie']})
            with urllib.request.urlopen(req, timeout=15) as response:
                material_id = json.load(response)['id']
            call('/api/materials/' + material_id, {'text': lesson.decode(), 'reviewed': True}, 'PATCH')
            call('/api/materials/' + material_id + '/generate', {'count': 5, 'mode': 'local'})
            draft = call('/api/materials/' + material_id)['exercises'][0]
            call('/api/drafts/' + draft['id'] + '/status', {'status': 'published', 'reviewed': True})
            exercise = request('/api/exercises?materialId=' + material_id)[0]
            assert 'answers' not in exercise and 'sourceQuote' not in exercise
            payload = {'id': attempt_id, 'exerciseId': draft['id'], 'answer': draft['answers'][0]}
            assert request('/api/attempts', payload)['saved'] is False
            assert call('/api/progress')['attempts'] == 0
            assert call('/api/attempts', payload)['correct']
            progress = call('/api/progress')
            assert call('/api/attempts', payload)['saved'] and call('/api/progress') == progress
            assert call('/api/materials/' + material_id + '/original', raw=True) == lesson
        print('Upload, generation, publication, original download, and answer retry passed.', flush=True)
        for deployment in [APP, 'test-postgres'] + ([RELEASE + '-minio'] if local else []):
            kube('rollout', 'restart', 'deployment/' + deployment)
            kube('rollout', 'status', 'deployment/' + deployment, '--timeout=180s', capture=False)
        with forwarding():
            assert call('/api/progress') == progress
            assert call('/api/materials/' + material_id + '/original', raw=True) == lesson
        after = json.loads(kube('get', 'pvc', '-o', 'json').stdout)['items']
        assert {c['metadata']['name']: c['metadata']['uid'] for c in after} == claim_uids
        print('PASS: chart deployment and database/storage persistence across pod restarts.', flush=True)
    finally:
        cleanup_test(session['userID'])


def sql(statement):
    return kube('exec', '-i', 'deployment/test-postgres', '--', 'sh', '-c',
                'exec psql -XAt -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1',
                data=statement.encode()).stdout.decode().strip()


def test_session():
    """Create an isolated test identity through database administration, never an HTTP bypass.

    Callers capture stdout privately; the cookie must not appear in test logs.
    This helper only runs in the namespace bearing the test harness marker.
    """
    namespace()
    user_id = 'browser-test-' + uuid.uuid4().hex
    token = secrets.token_urlsafe(32)
    hashed = hashlib.sha256(token.encode()).hexdigest()
    sql("BEGIN; INSERT INTO app_users(id,login,is_admin) VALUES('" + user_id + "','" + user_id + "',true);"
        "INSERT INTO auth_sessions(token_hash,user_id,expires_at) VALUES('" + hashed + "','" + user_id + "',now()+interval '1 hour'); COMMIT;")
    return {'userID': user_id, 'cookie': 'romana_session=' + token}


def cleanup_test(user_id):
    namespace()
    if not re.fullmatch(r'browser-test-[a-f0-9]{32}', user_id):
        raise ValueError('A temporary test account ID is required')
    if sql("SELECT count(*) FROM app_users WHERE id='" + user_id + "' AND github_id IS NULL") != '1':
        raise ValueError('Refusing to remove a non-test account')
    material_ids = sql("SELECT id FROM materials WHERE owner_id='" + user_id + "'").splitlines()
    if any(not re.fullmatch(r'[a-f0-9]{32}', mid) for mid in material_ids):
        raise ValueError('Invalid test material ID')
    if material_ids:
        objects = json.loads(sql("SELECT json_agg(json_build_object('key',object_key,'hash',content_hash)) FROM materials WHERE owner_id='" + user_id + "'"))
        storage_settings.operation(sys.modules[__name__], 'delete', storage_settings.active(sys.modules[__name__]), objects)
    sql("BEGIN; DELETE FROM attempts WHERE user_id='" + user_id + "';"
        "DELETE FROM exercises WHERE material_id IN (SELECT id FROM materials WHERE owner_id='" + user_id + "');"
        "DELETE FROM materials WHERE owner_id='" + user_id + "';"
        "DELETE FROM app_users WHERE id='" + user_id + "' AND github_id IS NULL; COMMIT;")


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['setup', 'deploy', 'test', 'forward', 'stop', 'cleanup-test', 'test-session', 'encrypt-secrets'])
    action = parser.parse_args().action
    if action == 'forward':
        try:
            with forwarding() as process:
                with contextlib.ExitStack() as stack:
                    print('App: http://127.0.0.1:' + str(PORT), flush=True)
                    if storage_settings.active(sys.modules[__name__])['Provider'] == 's3':
                        stack.enter_context(forwarding(service=RELEASE + '-minio-console', local_port=9001, target_port=9001, health_path='/'))
                        print('MinIO console: http://127.0.0.1:9001', flush=True)
                    process.wait()
        except KeyboardInterrupt:
            pass
    elif action == 'stop':
        run(['limactl', 'stop', VM], capture=False)
    elif action == 'cleanup-test':
        fixture = json.load(sys.stdin)
        cleanup_test(fixture.get('userID', ''))
    elif action == 'test-session':
        print(json.dumps(test_session()))
    elif action == 'encrypt-secrets':
        encrypt_secrets()
    else:
        globals()[action]()
