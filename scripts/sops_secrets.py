"""Manage encrypted application settings without writing decrypted deployment files."""
import argparse
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
APP_KEYS = ('EXERCISE_API_URL', 'EXERCISE_API_KEY', 'EXERCISE_API_MODEL',
            'EXERCISE_API_EFFORT', 'EXERCISE_API_EFFORT_FORMAT', 'GITHUB_CLIENT_ID',
            'GITHUB_CLIENT_SECRET', 'APP_BASE_URL', 'AUTH_LEGACY_GITHUB_ID')
INFRA_KEYS = ('PGUSER', 'PGPASSWORD', 'MINIO_ROOT_USER', 'MINIO_ROOT_PASSWORD')
STORAGE_KEYS = ('GOOGLE_CLOUD_PROJECT', 'COURSE_STORAGE_BUCKET', 'COURSE_STORAGE_PREFIX', 'GCS_WIF_PROVIDER', 'GCS_SERVICE_ACCOUNT')
DATABASE_KEYS = ('PGHOST', 'PGPORT', 'PGDATABASE', 'PGUSER', 'PGPASSWORD', 'PGSSLMODE')


def selected_environment():
    selected = os.environ.get('ROMANA_ENV', 'local')
    if selected not in ('local', 'prod'):
        raise RuntimeError('Select APP_ENV=local or APP_ENV=prod (ROMANA_ENV for direct script calls).')
    return selected


def require_local():
    if selected_environment() != 'local':
        raise RuntimeError('This command manages local infrastructure. Use APP_ENV=local; production settings cannot be applied here.')


def settings_keys(selected=None):
    selected = selected or selected_environment()
    storage_keys = tuple(key for key in STORAGE_KEYS if selected != 'local' or key != 'GCS_SERVICE_ACCOUNT')
    return APP_KEYS + (INFRA_KEYS if selected == 'local' else DATABASE_KEYS) + storage_keys


def secret_file():
    selected = selected_environment()
    path = Path(os.environ.get('ROMANA_SECRETS_FILE') or ('config/secrets.' + selected + '.enc.yaml')).expanduser()
    return path if path.is_absolute() else ROOT / path


def call(args, data=None, environment=None):
    try:
        result = subprocess.run(args, input=data, cwd=ROOT, env=environment, capture_output=True, timeout=90)
    except (OSError, subprocess.SubprocessError):
        raise RuntimeError('Secret operation failed; check SOPS installation and key access.') from None
    if result.returncode:
        # Tool errors can contain decrypted values. Keep them out of logs.
        raise RuntimeError('Secret operation failed; check SOPS configuration and key access.')
    return result.stdout


def validate(values):
    selected = selected_environment()
    if not isinstance(values, dict) or set(values) != set(settings_keys(selected)):
        raise RuntimeError('Secret settings must match config/secrets.' + selected + '.example.yaml. Do not mix local and production files.')
    if any(not isinstance(v, str) or '\0' in v for v in values.values()):
        raise RuntimeError('Secret settings must contain strings without null bytes.')
    if selected == 'local' and any(not values[key] for key in INFRA_KEYS):
        raise RuntimeError('PostgreSQL and MinIO credentials cannot be empty.')
    if selected == 'prod':
        port = values['PGPORT']
        if port and (not port.isascii() or not port.isdigit() or not 1 <= int(port) <= 65535):
            raise RuntimeError('PGPORT must be a port number from 1 to 65535.')
        if values['PGSSLMODE'] and values['PGSSLMODE'] not in ('disable', 'allow', 'prefer', 'require', 'verify-ca', 'verify-full'):
            raise RuntimeError('PGSSLMODE is not a supported PostgreSQL TLS mode.')
        if values['PGDATABASE'] == 'postgres' or values['PGDATABASE'].startswith('template'):
            raise RuntimeError('Select a dedicated production application database, not a maintenance database.')
    if bool(values['GITHUB_CLIENT_ID']) != bool(values['GITHUB_CLIENT_SECRET']):
        raise RuntimeError('Set both GitHub OAuth credentials, or leave both empty.')
    return values


def require_database(values):
    if selected_environment() == 'prod':
        missing = [key for key in DATABASE_KEYS if not values[key]]
        if missing:
            raise RuntimeError('Complete production database settings with make APP_ENV=prod secrets-edit: ' + ', '.join(missing))


def load(path=None):
    path = Path(path) if path is not None else secret_file()
    if not path.is_file():
        raise RuntimeError('Encrypted secrets are missing. Run make secrets-init or make secrets-import.')
    # SOPS parses the YAML on disk; JSON is only the in-memory interface to Python.
    raw = call(['sops', 'decrypt', '--input-type', 'yaml', '--output-type', 'json', str(path)])
    try:
        return validate(json.loads(raw))
    except (ValueError, UnicodeError):
        raise RuntimeError('Decrypted secrets are not valid settings.') from None


def gcp_rule(key):
    if not re.fullmatch(r'projects/[a-z0-9-]+/locations/[a-z0-9-]+/keyRings/[a-zA-Z0-9_-]+/cryptoKeys/[a-zA-Z0-9_-]+', key):
        raise RuntimeError('Provide a full Cloud KMS CryptoKey resource name in KMS_KEY.')
    return "creation_rules:\n  - path_regex: 'config/secrets\\.[^/]+\\.enc\\.yaml$'\n    gcp_kms: '" + key + "'\n"


def ensure_config():
    config = ROOT / '.sops.yaml'
    if config.exists():
        return
    rule = gcp_rule(os.environ.get('SOPS_GCP_KMS_IDS', ''))
    with config.open('x') as output:
        output.write(rule)


def atomic_write(path, data):
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=path.parent, prefix='.sops-', suffix='.enc.yaml', delete=False) as output:
            temporary = Path(output.name)
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def yaml_ciphertext(encrypted_json, environment=None):
    # Rotate preserves the verified recipient list and emits native encrypted YAML.
    # Only ciphertext reaches stdin; no intermediate plaintext file is created.
    return call(['sops', 'rotate', '--input-type', 'json', '--output-type', 'yaml', '/dev/stdin'],
                encrypted_json, environment=environment)


def use_gcp(key):
    """Replace the local recipient only after Cloud KMS encryption and decryption pass."""
    rule = gcp_rule(key)
    path, config = secret_file(), ROOT / '.sops.yaml'
    original = path.read_bytes()
    # This helper owns one project-wide creation rule. Preserve custom configurations.
    if config.exists():
        fields = re.findall(r'^\s*(?:-\s*)?([a-z_]+):', config.read_text(), re.MULTILINE)
        if fields.count('path_regex') != 1 or set(fields) - {'creation_rules', 'path_regex', 'age', 'gcp_kms'}:
            raise RuntimeError('Custom SOPS rules found. Configure one project rule before running secrets-use-gcp.')
    values = load(path)
    environment = dict(os.environ)
    # Explicitly use only the selected KMS key, even if other recipient variables exist.
    for name in ('SOPS_AGE_RECIPIENTS', 'SOPS_KMS_ARN', 'SOPS_PGP_FP', 'SOPS_GCP_KMS_IDS',
                 'SOPS_AZURE_KEYVAULT_URLS', 'SOPS_HUAWEICLOUD_KMS_IDS', 'SOPS_VAULT_URIS'):
        environment.pop(name, None)
    with tempfile.NamedTemporaryFile(dir=ROOT, suffix='.yaml', mode='w') as candidate_config:
        candidate_config.write(rule)
        candidate_config.flush()
        encrypted = call(['sops', '--config', candidate_config.name, 'encrypt', '--gcp-kms', key,
                          '--filename-override', str(path), '--input-type', 'json', '--output-type', 'json'],
                         json.dumps(values).encode(), environment=environment)
    metadata = json.loads(encrypted).get('sops', {})
    if [entry.get('resource_id') for entry in metadata.get('gcp_kms', [])] != [key] or any(
            metadata.get(name) for name in ('age', 'pgp', 'kms', 'azure_kv', 'hckms', 'hc_vault', 'key_groups')):
        raise RuntimeError('The new encrypted file must use only the selected Google Cloud KMS key.')
    encrypted = yaml_ciphertext(encrypted, environment=environment)
    # With only a GCP recipient, this cannot succeed by falling back to the age key.
    checked = call(['sops', 'decrypt', '--input-type', 'yaml', '--output-type', 'json'], encrypted, environment=environment)
    if json.loads(checked) != values:
        raise RuntimeError('Cloud KMS decryption verification failed; the original file is unchanged.')
    if path.read_bytes() != original:
        raise RuntimeError('Settings changed during migration; retry without concurrent edits.')
    atomic_write(path, encrypted)
    try:
        atomic_write(config, rule.encode())
    except OSError:
        atomic_write(path, original)
        raise
    print('Secrets now use Google Cloud KMS. Google-authenticated decryption verified.', flush=True)


def save(values, path=None):
    values = validate(values)
    path = Path(path) if path is not None else secret_file()
    if path.exists():
        raise RuntimeError('Encrypted secrets already exist; use make secrets-edit to change them.')
    ensure_config()
    encrypted = call(['sops', 'encrypt', '--filename-override', str(path), '--input-type', 'json',
                      '--output-type', 'yaml'], json.dumps(values).encode())
    # Verify decryption before persisting anything or retiring a plaintext source.
    checked = call(['sops', 'decrypt', '--input-type', 'yaml', '--output-type', 'json'], encrypted)
    if json.loads(checked) != values:
        raise RuntimeError('SOPS round-trip verification failed.')
    path.parent.mkdir(parents=True, exist_ok=True)
    # Only ciphertext goes to disk. Link atomically without overwriting an existing file.
    with tempfile.NamedTemporaryFile(dir=path.parent, prefix='.sops-', suffix='.enc.yaml') as output:
        output.write(encrypted)
        output.flush()
        os.fsync(output.fileno())
        os.link(output.name, path)
    print('Encrypted settings saved and decryption verified.', flush=True)


def initialize():
    selected = selected_environment()
    if selected == 'local' and (ROOT / '.env').exists():
        raise RuntimeError('Existing .env found; run make secrets-import to preserve its settings.')
    values = {key: '' for key in settings_keys(selected)}
    if selected == 'local':
        values.update(APP_BASE_URL='http://localhost:8080', PGUSER='romanian',
                      PGPASSWORD=secrets.token_hex(24), MINIO_ROOT_USER='romanian-test',
                      MINIO_ROOT_PASSWORD=secrets.token_hex(24))
    else:
        # The cloud database and its role already exist; never invent or copy credentials.
        values.update(PGPORT='5432', PGSSLMODE='verify-full', COURSE_STORAGE_PREFIX='courses/')
    save(values)
    if selected == 'prod':
        print('Production settings created independently. Fill the cloud connection and application settings with make APP_ENV=prod secrets-edit.')


def execute(command):
    if command[:1] == ['--']:
        command = command[1:]
    if not command:
        raise RuntimeError('Provide a command after exec --.')
    values = load()
    require_database(values)
    environment = dict(os.environ)
    # Prevent inherited settings from another environment from selecting a different DB.
    for key in set(APP_KEYS + INFRA_KEYS + STORAGE_KEYS + DATABASE_KEYS + (
            'DATABASE_URL', 'APP_MODE', 'COURSE_STORAGE_PROVIDER', 'COURSE_S3_ENDPOINT',
            'COURSE_S3_ACCESS_KEY', 'COURSE_S3_SECRET_KEY', 'COURSE_S3_REGION', 'ROMANA_LOCAL_COMPOSE_PROJECT')):
        environment.pop(key, None)
    environment.update(values)
    environment['ROMANA_ENV'] = selected_environment()
    environment['APP_MODE'] = 'public' if selected_environment() == 'prod' else 'local'
    if selected_environment() == 'local':
        environment['ROMANA_LOCAL_COMPOSE_PROJECT'] = 'romanian'
    else:
        environment['COURSE_STORAGE_PROVIDER'] = 'gcs'
    # Compose must not silently read a leftover plaintext .env.
    environment['COMPOSE_DISABLE_ENV_FILE'] = 'true'
    os.execvpe(command[0], command, environment)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['init', 'check', 'edit', 'exec', 'use-gcp', 'require-local'])
    parser.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.action == 'require-local':
        require_local()
    elif args.action == 'init':
        initialize()
    elif args.action == 'use-gcp':
        use_gcp(args.command[0] if len(args.command) == 1 else '')
    elif args.action == 'exec':
        execute(args.command)
    elif args.action == 'edit':
        # SOPS owns the editor lifecycle and reencrypts the result.
        os.execvp('sops', ['sops', 'edit', str(secret_file())])
    else:
        values = load()
        require_database(values)
        print('Encrypted ' + selected_environment() + ' settings decrypt and validate successfully.')


if __name__ == '__main__':
    try:
        main()
    except RuntimeError as error:
        # RuntimeError messages above are fixed descriptions, never raw tool output.
        print(str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError):
        # Never include raw exceptions or subprocess output with secret contents.
        print('Secret operation failed. Check the encrypted file, SOPS key access, and setup instructions.', file=sys.stderr)
        sys.exit(1)
