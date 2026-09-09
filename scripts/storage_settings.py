"""Storage profiles and local administrative operations; credentials never go in argv."""
import contextlib
import json
import os
from pathlib import Path
import re
import subprocess
import base64

ROOT = Path(__file__).resolve().parents[1]


def mode():
    selected = os.environ.get('ROMANA_STORAGE', 'gcs')
    if selected not in ('gcs', 'minio'):
        raise RuntimeError('ROMANA_STORAGE must be gcs or minio.')
    return selected


def credentials_path():
    default = Path(os.environ.get('CLOUDSDK_CONFIG', Path.home() / '.config/gcloud')) / 'application_default_credentials.json'
    path = Path(os.environ.get('ROMANA_GCS_CREDENTIALS_FILE', os.environ.get('GOOGLE_APPLICATION_CREDENTIALS', default))).expanduser().resolve()
    if not path.is_file():
        raise RuntimeError('Google storage credentials are missing. Run gcloud auth application-default login or select a credentials file.')
    return path


def helper(payload, credential_file=None):
    environment = dict(os.environ)
    if any(payload.get(side, {}).get('Provider') == 'gcs' for side in ('Source', 'Target')):
        # Host credentials are only for administrative copying/cleanup, never deployed.
        environment['GOOGLE_APPLICATION_CREDENTIALS'] = str(credential_file or credentials_path())
    result = subprocess.run(['go', 'run', './cmd/storage'], cwd=ROOT, env=environment,
                            input=json.dumps(payload).encode(), capture_output=True, timeout=360)
    if result.returncode:
        raise RuntimeError('Storage operation failed; check Google access, MinIO connectivity, and original-file integrity.')
    return json.loads(result.stdout)


def cloud(environment):
    values = {key: environment[variable] for key, variable in [
        ('Project', 'GOOGLE_CLOUD_PROJECT'), ('Bucket', 'COURSE_STORAGE_BUCKET'),
        ('Prefix', 'COURSE_STORAGE_PREFIX'), ('Provider', 'GCS_WIF_PROVIDER')]}
    # Local Google storage uses the Kubernetes principal directly; production can impersonate its Google account.
    values['ServiceAccount'] = environment.get('GCS_SERVICE_ACCOUNT', '')
    if not re.fullmatch(r'[a-z0-9][a-z0-9._-]{1,220}[a-z0-9]', values['Bucket']):
        raise RuntimeError('Set COURSE_STORAGE_BUCKET with make secrets-edit.')
    if not re.fullmatch(r'projects/[0-9]+/locations/global/workloadIdentityPools/[a-z0-9-]+/providers/[a-z0-9-]+', values['Provider']):
        raise RuntimeError('Set GCS_WIF_PROVIDER to the numeric project/pool/provider resource with make secrets-edit.')
    if values['ServiceAccount'] and not re.fullmatch(r'[a-z][a-z0-9-]+@[a-z0-9-]+\.iam\.gserviceaccount\.com', values['ServiceAccount']):
        raise RuntimeError('Set GCS_SERVICE_ACCOUNT with make secrets-edit.')
    return values


def active(cluster):
    raw = cluster.kube('get', 'configmap', cluster.APP, '-o', 'json', '--ignore-not-found').stdout
    if not raw.strip():
        return None
    config = json.loads(raw)['data']
    if config.get('COURSE_STORAGE_PROVIDER') == 'gcs' and 'COURSE_STORAGE_BUCKET' not in config:
        deployment = json.loads(cluster.kube('get', 'deployment', cluster.APP, '-o', 'json').stdout)
        variables = deployment['spec']['template']['spec']['containers'][0]['env']
        for variable in variables:
            if variable['name'] in ('COURSE_STORAGE_BUCKET', 'COURSE_STORAGE_PREFIX'):
                ref = variable['valueFrom']['secretKeyRef']
                secret = json.loads(cluster.kube('get', 'secret', ref['name'], '-o', 'json').stdout)['data']
                config[variable['name']] = base64.b64decode(secret[ref['key']]).decode()
    return descriptor(config)


def descriptor(config):
    provider = config['COURSE_STORAGE_PROVIDER']
    if provider not in ('gcs', 's3'):
        raise RuntimeError('Automatic switching supports Google Cloud Storage and local MinIO.')
    return {'Provider': provider, 'Bucket': config['COURSE_STORAGE_BUCKET'],
            'Prefix': config.get('COURSE_STORAGE_PREFIX', '')}


def target(selected, cloud_values=None):
    if selected == 'minio':
        return {'Provider': 's3', 'Bucket': 'courses', 'Prefix': 'courses/'}
    return {'Provider': 'gcs', 'Bucket': cloud_values['Bucket'], 'Prefix': cloud_values['Prefix']}


def operation(cluster, action, destination, objects, source=None):
    payload = {'Action': action, 'Target': dict(destination), 'Objects': objects}
    if source is not None:
        payload['Source'] = dict(source)
    with contextlib.ExitStack() as stack:
        if any(payload.get(side, {}).get('Provider') == 's3' for side in ('Source', 'Target')):
            values = json.loads(cluster.kube('get', 'secret', 'test-minio', '-o', 'json').stdout)['data']
            for side in ('Source', 'Target'):
                if payload.get(side, {}).get('Provider') == 's3':
                    payload[side].update(Endpoint='http://127.0.0.1:19000', Region='us-east-1',
                        AccessKey=base64.b64decode(values['rootUser']).decode(),
                        SecretKey=base64.b64decode(values['rootPassword']).decode())
            stack.enter_context(cluster.forwarding(service=cluster.RELEASE + '-minio', local_port=19000,
                                target_port=9000, health_path='/minio/health/live'))
        return helper(payload)
