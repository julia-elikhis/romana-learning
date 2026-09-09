import base64
import contextlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import microk8s
import sops_secrets


class SopsRoundTripTest(unittest.TestCase):
    def setUp(self):
        if not shutil.which('sops') or not shutil.which('age-keygen'):
            self.skipTest('Install sops and age for encryption integration tests')
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name) / 'project'
        self.root.mkdir()
        self.key = Path(self.directory.name) / 'private/keys.txt'
        self.patch = patch.object(sops_secrets, 'ROOT', self.root)
        self.patch.start()
        self.addCleanup(self.patch.stop)
        self.environment = patch.dict(os.environ, {'SOPS_AGE_KEY_FILE': str(self.key),
            'SOPS_AGE_RECIPIENTS': '', 'ROMANA_ENV': 'local', 'ROMANA_SECRETS_FILE': 'config/secrets.local.enc.yaml'})
        self.environment.start()
        self.addCleanup(self.environment.stop)
        # age is used only as an offline encryption fixture and legacy migration input.
        self.key.parent.mkdir()
        subprocess.run(['age-keygen', '-o', str(self.key)], check=True, capture_output=True)
        self.key.chmod(0o600)
        recipient = subprocess.check_output(['age-keygen', '-y', str(self.key)]).decode().strip()
        (self.root / '.sops.yaml').write_text("creation_rules:\n  - path_regex: 'config/secrets\\.[^/]+\\.enc\\.yaml$'\n    age: '" + recipient + "'\n")
        self.values = {key: '' for key in sops_secrets.settings_keys('local')}
        self.values.update(EXERCISE_API_URL='https://example.invalid/private?token=never-log-this',
            EXERCISE_API_KEY='quotes\'" dollars $HOME and\nnewlines',
            EXERCISE_API_MODEL='null', EXERCISE_API_EFFORT='on', AUTH_LEGACY_GITHUB_ID='00123',
            PGUSER='romanian', PGPASSWORD='database-only-secret',
            MINIO_ROOT_USER='test-user', MINIO_ROOT_PASSWORD='object-only-secret')

    def save(self):
        with contextlib.redirect_stdout(io.StringIO()):
            sops_secrets.save(self.values)

    def test_real_encryption_round_trip_and_no_plaintext_artifacts(self):
        self.save()
        self.assertEqual(sops_secrets.load(), self.values)
        self.assertNotIn('GCS_SERVICE_ACCOUNT', self.values)
        ciphertext = sops_secrets.secret_file().read_text()
        self.assertTrue(ciphertext.startswith('EXERCISE_API_URL: ENC['))
        for key, value in self.values.items():
            if value:
                self.assertRegex(ciphertext, r'(?m)^' + re.escape(key) + r': ENC\[AES256_GCM,.*type:str\]$')
            if len(value) > 10:
                self.assertNotIn(value, ciphertext)
        self.assertEqual(self.key.stat().st_mode & 0o777, 0o600)
        self.assertFalse(self.key.is_relative_to(self.root))
        self.assertEqual({str(p.relative_to(self.root)) for p in self.root.rglob('*') if p.is_file()},
                         {'.sops.yaml', 'config/secrets.local.enc.yaml'})
        with self.assertRaises(RuntimeError):
            sops_secrets.save(self.values)
        self.assertEqual(sops_secrets.load(), self.values)

    def test_tampered_ciphertext_and_wrong_key_fail_without_env_fallback(self):
        self.save()
        (self.root / '.env').write_text('EXERCISE_API_KEY=must-not-use-plaintext')
        with patch.dict(os.environ, {'SOPS_AGE_KEY_FILE': str(self.root / 'missing-key')}):
            with self.assertRaises(RuntimeError):
                sops_secrets.load()
        document, count = re.subn(r'^EXERCISE_API_KEY:.*$', 'EXERCISE_API_KEY: tampered-secret',
                                 sops_secrets.secret_file().read_text(), flags=re.MULTILINE)
        self.assertEqual(count, 1)
        sops_secrets.secret_file().write_text(document)
        with self.assertRaises(RuntimeError) as error:
            sops_secrets.load()
        self.assertNotIn('tampered-secret', str(error.exception))

    def test_exec_passes_exact_values_and_disables_plaintext_compose_input(self):
        self.save()
        with patch('os.execvpe') as execute:
            sops_secrets.execute(['--', 'docker', 'compose', 'build', 'app'])
        command, args, environment = execute.call_args.args
        self.assertEqual(command, 'docker')
        self.assertEqual(args, ['docker', 'compose', 'build', 'app'])
        self.assertEqual(environment['EXERCISE_API_KEY'], self.values['EXERCISE_API_KEY'])
        self.assertEqual(environment['COMPOSE_DISABLE_ENV_FILE'], 'true')

    def test_json_ciphertext_conversion_to_yaml_preserves_all_values(self):
        original = sops_secrets.call(['sops', 'encrypt', '--filename-override', str(sops_secrets.secret_file()),
            '--input-type', 'json', '--output-type', 'json'], json.dumps(self.values).encode())
        converted = sops_secrets.yaml_ciphertext(original)
        self.assertTrue(converted.startswith(b'EXERCISE_API_URL: ENC['))
        values = sops_secrets.call(['sops', 'decrypt', '--input-type', 'yaml', '--output-type', 'json'], converted)
        self.assertEqual(json.loads(values), self.values)
        for value in self.values.values():
            if len(value) > 10:
                self.assertNotIn(value.encode(), converted)

    def test_production_initialization_preserves_local_and_never_copies_credentials(self):
        self.save()
        local_path = sops_secrets.secret_file()
        original = local_path.read_bytes()
        with patch.dict(os.environ, {'ROMANA_ENV': 'prod', 'ROMANA_SECRETS_FILE': ''}), \
             contextlib.redirect_stdout(io.StringIO()):
            sops_secrets.initialize()
            prod_path = sops_secrets.secret_file()
            production = sops_secrets.load()
            self.assertEqual(prod_path.name, 'secrets.prod.enc.yaml')
            self.assertNotEqual(prod_path, local_path)
            self.assertEqual(production['PGPORT'], '5432')
            self.assertEqual(production['PGSSLMODE'], 'verify-full')
            for key in ('PGUSER', 'PGPASSWORD', 'PGHOST', 'PGDATABASE', 'GITHUB_CLIENT_SECRET', 'EXERCISE_API_KEY'):
                self.assertEqual(production[key], '')
            self.assertNotIn('MINIO_ROOT_PASSWORD', production)
            with self.assertRaises(RuntimeError):
                sops_secrets.require_database(production)
            with self.assertRaises(RuntimeError):
                sops_secrets.load(local_path)
        self.assertEqual(local_path.read_bytes(), original)
        self.assertEqual(sops_secrets.load(), self.values)
        with self.assertRaises(RuntimeError):
            sops_secrets.load(prod_path)

    def test_environment_execution_drops_inherited_database_and_storage_credentials(self):
        with patch.dict(os.environ, {'ROMANA_ENV': 'prod', 'ROMANA_SECRETS_FILE': '',
            'DATABASE_URL': 'postgres://stale-local-setting', 'MINIO_ROOT_USER': 'inherited-local-user',
            'MINIO_ROOT_PASSWORD': 'inherited-local-password', 'COURSE_S3_SECRET_KEY': 'inherited-s3-key',
            'ROMANA_LOCAL_COMPOSE_PROJECT': 'romanian', 'APP_MODE': 'local'}):
            production = {key: '' for key in sops_secrets.settings_keys()}
            production.update(PGHOST='cloud-db.example.invalid', PGPORT='5432', PGDATABASE='learning',
                              PGUSER='cloud-app', PGPASSWORD='synthetic-cloud-password', PGSSLMODE='verify-full')
            with contextlib.redirect_stdout(io.StringIO()):
                sops_secrets.save(production)
            with patch('os.execvpe') as execute:
                sops_secrets.execute(['application'])
            environment = execute.call_args.args[2]
        self.assertEqual(environment['PGHOST'], 'cloud-db.example.invalid')
        self.assertEqual(environment['PGPASSWORD'], 'synthetic-cloud-password')
        self.assertEqual(environment['APP_MODE'], 'public')
        self.assertEqual(environment['COURSE_STORAGE_PROVIDER'], 'gcs')
        self.assertFalse({'DATABASE_URL', 'MINIO_ROOT_USER', 'MINIO_ROOT_PASSWORD',
                          'COURSE_S3_SECRET_KEY', 'ROMANA_LOCAL_COMPOSE_PROJECT'} & environment.keys())

    def test_missing_production_file_never_falls_back_to_local(self):
        self.save()
        with patch.dict(os.environ, {'ROMANA_ENV': 'prod', 'ROMANA_SECRETS_FILE': ''}):
            with self.assertRaises(RuntimeError):
                sops_secrets.load()


class SecretDeploymentTest(unittest.TestCase):
    def test_production_settings_are_rejected_before_any_local_deployment_or_secret_read(self):
        with patch.dict(os.environ, {'ROMANA_ENV': 'prod'}), patch.object(sops_secrets, 'load') as load, \
             patch.object(microk8s, 'namespace') as namespace, patch.object(microk8s, 'database') as database:
            with self.assertRaises(RuntimeError):
                microk8s.deploy()
            load.assert_not_called()
            namespace.assert_not_called()
            database.assert_not_called()

    def test_make_local_targets_reject_production_before_running_docker_or_microk8s(self):
        for target in ('up', 'db', 'storage', 'backend', 'microk8s-deploy', 'secrets-import'):
            result = subprocess.run(['make', '--no-print-directory', 'APP_ENV=prod', target],
                                    cwd=sops_secrets.ROOT, capture_output=True, timeout=10)
            self.assertNotEqual(result.returncode, 0, target)
            self.assertIn(b'production settings cannot be applied here', result.stderr)
            self.assertNotIn(b'docker compose', result.stdout)

    def test_provider_tool_errors_are_redacted(self):
        response = subprocess.CompletedProcess([], 1, b'private-output', b'private-error')
        with patch('subprocess.run', return_value=response):
            with self.assertRaises(RuntimeError) as error:
                sops_secrets.call(['sops', 'decrypt', 'file'])
        self.assertNotIn('private-', str(error.exception))

    def test_decryption_failure_precedes_any_cluster_mutation(self):
        with patch.object(microk8s, 'private_environment', side_effect=RuntimeError('Missing key')), \
             patch.object(microk8s, 'namespace') as namespace, patch.object(microk8s, 'database') as database:
            with self.assertRaises(RuntimeError):
                microk8s.deploy()
            namespace.assert_not_called()
            database.assert_not_called()

    def test_deployment_does_not_silently_rotate_infrastructure_credentials(self):
        current = {'data': {'password': base64.b64encode(b'current-secret').decode()}}
        response = subprocess.CompletedProcess([], 0, json.dumps(current).encode())
        with patch.object(microk8s, 'kube', return_value=response) as kube:
            microk8s.ensure_secret('test-database', {'password': 'current-secret'})
            with self.assertRaises(RuntimeError):
                microk8s.ensure_secret('test-database', {'password': 'different-secret'})
        self.assertTrue(all(call.args[0] == 'get' for call in kube.call_args_list))


class GoogleMigrationTest(unittest.TestCase):
    key = 'projects/test-project/locations/global/keyRings/learning/cryptoKeys/secrets'

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.path = self.root / 'config/secrets.local.enc.yaml'
        self.path.parent.mkdir()
        self.original = b'previous encrypted settings'
        self.path.write_bytes(self.original)
        self.rule = "creation_rules:\n  - path_regex: 'config/secrets\\.[^/]+\\.enc\\.yaml$'\n    age: 'previous-recipient'\n"
        (self.root / '.sops.yaml').write_text(self.rule)
        self.patches = [patch.object(sops_secrets, 'ROOT', self.root),
                        patch.dict(os.environ, {'ROMANA_ENV': 'local', 'ROMANA_SECRETS_FILE': str(self.path)})]
        for mocked in self.patches:
            mocked.start()
            self.addCleanup(mocked.stop)
        self.values = {'EXERCISE_API_KEY': 'never-log-this-secret'}
        self.ciphertext = json.dumps({'sops': {'gcp_kms': [{'resource_id': self.key}], 'age': []}}).encode()
        self.yaml_ciphertext = b'EXERCISE_API_KEY: ENC[synthetic]\nsops: {}\n'

    def test_new_configuration_uses_google_instead_of_generating_an_age_key(self):
        (self.root / '.sops.yaml').unlink()
        with patch.dict(os.environ, {'SOPS_GCP_KMS_IDS': self.key}), patch.object(sops_secrets, 'call') as call:
            sops_secrets.ensure_config()
        self.assertIn('gcp_kms:', (self.root / '.sops.yaml').read_text())
        call.assert_not_called()

    def test_migration_checks_google_decryption_before_replacing_age(self):
        def provider(args, data=None, environment=None):
            self.assertEqual(self.path.read_bytes(), self.original)
            self.assertEqual((self.root / '.sops.yaml').read_text(), self.rule)
            self.assertNotIn('SOPS_AGE_RECIPIENTS', environment)
            if 'encrypt' in args:
                return self.ciphertext
            if 'rotate' in args:
                self.assertEqual(data, self.ciphertext)
                self.assertIn('yaml', args)
                return self.yaml_ciphertext
            self.assertEqual(data, self.yaml_ciphertext)
            self.assertIn('yaml', args)
            return json.dumps(self.values).encode()
        with patch.object(sops_secrets, 'load', return_value=self.values), \
             patch.object(sops_secrets, 'call', side_effect=provider), contextlib.redirect_stdout(io.StringIO()):
            sops_secrets.use_gcp(self.key)
        self.assertEqual(self.path.read_bytes(), self.yaml_ciphertext)
        self.assertEqual((self.root / '.sops.yaml').read_text(), sops_secrets.gcp_rule(self.key))

    def test_permission_failure_or_changed_values_preserve_the_working_file(self):
        for response in (RuntimeError('KMS access denied'), b'{"EXERCISE_API_KEY":"different"}'):
            with self.subTest(response=type(response).__name__), \
                 patch.object(sops_secrets, 'load', return_value=self.values), \
                 patch.object(sops_secrets, 'call', side_effect=[self.ciphertext, self.yaml_ciphertext, response]):
                with self.assertRaises(RuntimeError):
                    sops_secrets.use_gcp(self.key)
                self.assertEqual(self.path.read_bytes(), self.original)
                self.assertEqual((self.root / '.sops.yaml').read_text(), self.rule)

    def test_migration_rejects_age_fallback(self):
        ciphertext = json.dumps({'sops': {'gcp_kms': [{'resource_id': self.key}], 'age': [{'recipient': 'old'}]}}).encode()
        with patch.object(sops_secrets, 'load', return_value=self.values), patch.object(sops_secrets, 'call', return_value=ciphertext):
            with self.assertRaises(RuntimeError):
                sops_secrets.use_gcp(self.key)
        self.assertEqual(self.path.read_bytes(), self.original)

    def test_config_write_failure_restores_previous_ciphertext(self):
        write = sops_secrets.atomic_write
        def write_or_fail(path, data):
            if path.name == '.sops.yaml':
                raise OSError('simulated failure')
            return write(path, data)
        with patch.object(sops_secrets, 'load', return_value=self.values), \
             patch.object(sops_secrets, 'call', side_effect=[self.ciphertext, self.yaml_ciphertext, json.dumps(self.values).encode()]), \
             patch.object(sops_secrets, 'atomic_write', side_effect=write_or_fail):
            with self.assertRaises(OSError):
                sops_secrets.use_gcp(self.key)
        self.assertEqual(self.path.read_bytes(), self.original)
        self.assertEqual((self.root / '.sops.yaml').read_text(), self.rule)


if __name__ == '__main__':
    unittest.main()
