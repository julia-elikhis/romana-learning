"""Configure narrowly scoped Google federation for the local MicroK8s service account."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import tempfile

import sops_secrets
import storage_settings


def google(arguments, allow_missing=False):
    result = subprocess.run(['gcloud', *arguments, '--quiet', '--format=json'],
                            capture_output=True, timeout=120)
    if result.returncode:
        if allow_missing and b'NOT_FOUND' in result.stderr:
            return None
        if b'PERMISSION_DENIED' in result.stderr or b'permission' in result.stderr.lower():
            raise RuntimeError('Google denied the Workload Identity operation. The setup account needs '
                               'roles/iam.workloadIdentityPoolAdmin on the pool project and permission '
                               'to update IAM on the selected service account and course bucket. '
                               'No application credentials were installed.')
        raise RuntimeError('Google federation setup failed; check the IAM/STS APIs, account access, and provider settings.')
    return json.loads(result.stdout) if result.stdout.strip() else {}


def identity(values, cluster):
    parts = values['Provider'].split('/')
    project, pool, provider = parts[1], parts[5], parts[7]
    subject = 'system:serviceaccount:' + cluster.NS + ':' + cluster.APP
    audience = 'https://iam.googleapis.com/' + values['Provider']
    return project, pool, provider, subject, audience


def credential_config(values, token_path='/var/run/secrets/google/token'):
    config = {'type': 'external_account', 'audience': '//iam.googleapis.com/' + values['Provider'],
            'subject_token_type': 'urn:ietf:params:oauth:token-type:jwt',
            'token_url': 'https://sts.googleapis.com/v1/token',
            'credential_source': {'file': token_path}}
    if values.get('ServiceAccount'):
        config['service_account_impersonation_url'] = ('https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/' +
                                                       values['ServiceAccount'] + ':generateAccessToken')
    return config


def helm_values(values):
    """Send only the selected identity to Helm; application secrets stay in Secret references."""
    if not values.get('ServiceAccount'):
        return {}
    return {'serviceAccount': {'gcpServiceAccount': values['ServiceAccount'],
                              'annotations': {'iam.gke.io/gcp-service-account': values['ServiceAccount']}}}


def setup(cluster, values):
    cluster.namespace()
    project, pool, provider, subject, audience = identity(values, cluster)
    common = ['--project=' + values['Project'], '--location=global']
    resource = google(['iam', 'workload-identity-pools', 'describe', pool, *common], allow_missing=True)
    if resource is None:
        google(['iam', 'workload-identity-pools', 'create', pool, *common,
                '--display-name=Romana Learning', '--description=Dedicated application Kubernetes federation'])
    elif resource.get('state') != 'ACTIVE' or resource.get('disabled'):
        raise RuntimeError('The configured identity pool is not active.')
    issuer = json.loads(cluster.kube('get', '--raw', '/.well-known/openid-configuration').stdout)['issuer']
    jwks = json.loads(cluster.kube('get', '--raw', '/openid/v1/jwks').stdout)
    if not jwks.get('keys') or any('d' in key for key in jwks['keys']):
        raise RuntimeError('The cluster must expose a public signing-key set.')
    common += ['--workload-identity-pool=' + pool]
    resource = google(['iam', 'workload-identity-pools', 'providers', 'describe', provider, *common], allow_missing=True)
    mapping = {'google.subject': 'assertion.sub'}
    condition = "google.subject == '" + subject + "'"
    if resource is not None:
        oidc = resource.get('oidc', {})
        if (resource.get('state') != 'ACTIVE' or resource.get('disabled') or
            resource.get('attributeMapping') != mapping or resource.get('attributeCondition') != condition or
            oidc.get('issuerUri') != issuer or oidc.get('allowedAudiences') != [audience]):
            raise RuntimeError('Existing provider trust differs from this cluster/account; refusing to broaden or replace it.')
    with tempfile.TemporaryDirectory(prefix='romana-public-jwks-') as directory:
        path = Path(directory) / 'jwks.json'
        path.write_text(json.dumps(jwks))
        action = 'create-oidc' if resource is None else 'update-oidc'
        google(['iam', 'workload-identity-pools', 'providers', action, provider, *common,
                '--issuer-uri=' + issuer, '--attribute-mapping=google.subject=assertion.sub',
                '--attribute-condition=' + condition, '--allowed-audiences=' + audience,
                '--jwk-json-path=' + str(path)])
    principal = ('principal://iam.googleapis.com/projects/' + project +
                 '/locations/global/workloadIdentityPools/' + pool + '/subject/' + subject)
    member = principal
    if values.get('ServiceAccount'):
        google(['iam', 'service-accounts', 'add-iam-policy-binding', values['ServiceAccount'],
                '--project=' + values['ServiceAccount'].split('@')[1].split('.')[0],
                '--member=' + principal, '--role=roles/iam.workloadIdentityUser', '--condition=None'])
        member = 'serviceAccount:' + values['ServiceAccount']
    for role in ('roles/storage.objectCreator', 'roles/storage.objectViewer'):
        google(['storage', 'buckets', 'add-iam-policy-binding', 'gs://' + values['Bucket'],
                '--member=' + member, '--role=' + role, '--condition=None'])
    print('Federation configured with bucket-only create/read access for the selected identity.')


def preflight(cluster, values):
    """Exchange a short-lived KSA token and verify actual bucket access before pausing the app."""
    _, _, _, _, audience = identity(values, cluster)
    token = cluster.kube('create', 'token', cluster.APP, '--audience=' + audience, '--duration=10m').stdout
    with tempfile.TemporaryDirectory(prefix='romana-wif-check-') as directory:
        token_path = Path(directory) / 'token'
        config_path = Path(directory) / 'credentials.json'
        token_path.write_bytes(token.strip())
        token_path.chmod(0o600)
        config_path.write_text(json.dumps(credential_config(values, str(token_path))))
        try:
            storage_settings.helper({'Action': 'check', 'Target': storage_settings.target('gcs', values)},
                                    credential_file=config_path)
        except RuntimeError:
            raise RuntimeError('Workload Identity could not access the course bucket. Run make workload-identity-setup '
                               'with an authorized Google account. The running storage was not changed.') from None
    print('Workload Identity token exchange and bucket access verified.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['setup', 'check'])
    arguments = parser.parse_args()
    try:
        sops_secrets.require_local()
        import microk8s
        values = storage_settings.cloud(sops_secrets.load())
        (setup if arguments.action == 'setup' else preflight)(microk8s, values)
    except (RuntimeError, OSError, ValueError, subprocess.SubprocessError) as error:
        print(str(error) if isinstance(error, RuntimeError) else 'Workload Identity operation failed.', file=sys.stderr)
        sys.exit(1)
