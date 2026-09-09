import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import unittest
from unittest.mock import Mock, patch

import microk8s
import storage_settings
import workload_identity as wi


class WorkloadIdentityTest(unittest.TestCase):
    def setUp(self):
        self.values = {'Project': 'test-project', 'Bucket': 'private-courses', 'Prefix': 'courses/',
                       'Provider': 'projects/123456/locations/global/workloadIdentityPools/learning/providers/microk8s',
                       'ServiceAccount': 'learning@test-project.iam.gserviceaccount.com'}
        self.cluster = Mock(NS='romana-test', APP='practice-romanian')

    def test_impersonation_has_no_private_key_or_user_refresh_token(self):
        config = wi.credential_config(self.values)
        self.assertEqual(config['type'], 'external_account')
        self.assertEqual(config['credential_source'], {'file': '/var/run/secrets/google/token'})
        self.assertEqual(config['audience'], '//iam.googleapis.com/' + self.values['Provider'])
        self.assertEqual(config['service_account_impersonation_url'],
            'https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/' + self.values['ServiceAccount'] + ':generateAccessToken')
        self.assertFalse({'private_key', 'refresh_token', 'client_secret'} & config.keys())

    def test_local_google_storage_uses_its_kubernetes_identity_without_a_google_account(self):
        local = {'GOOGLE_CLOUD_PROJECT': self.values['Project'], 'COURSE_STORAGE_BUCKET': self.values['Bucket'],
                 'COURSE_STORAGE_PREFIX': self.values['Prefix'], 'GCS_WIF_PROVIDER': self.values['Provider']}
        values = storage_settings.cloud(local)
        self.assertEqual(values['ServiceAccount'], '')
        self.assertEqual(wi.helm_values(values), {})
        self.assertNotIn('service_account_impersonation_url', wi.credential_config(values))
        self.cluster.kube.side_effect = [Mock(stdout=b'{"issuer":"https://kubernetes.default.svc"}'),
                                        Mock(stdout=b'{"keys":[{"kty":"RSA","kid":"public"}]}')]
        def google(args, **kwargs):
            return None if 'describe' in args else {}
        with patch.object(wi, 'google', side_effect=google) as call, contextlib.redirect_stdout(io.StringIO()):
            wi.setup(self.cluster, values)
        commands = [c.args[0] for c in call.call_args_list]
        bindings = [c for c in commands if 'add-iam-policy-binding' in c]
        self.assertEqual(len(bindings), 2)
        expected = '--member=principal://iam.googleapis.com/projects/123456/locations/global/workloadIdentityPools/learning/subject/system:serviceaccount:romana-test:practice-romanian'
        self.assertTrue(all(expected in c for c in bindings))
        self.assertFalse(any('service-accounts' in c for c in commands))

    def test_preflight_uses_short_lived_token_and_removes_it_on_failure(self):
        self.cluster.kube.return_value.stdout = b'synthetic-token\n'
        paths = []
        def check(payload, credential_file):
            config = json.loads(credential_file.read_text())
            token = Path(config['credential_source']['file'])
            paths.extend([token, credential_file])
            self.assertEqual(token.read_text(), 'synthetic-token')
            self.assertEqual(token.stat().st_mode & 0o777, 0o600)
            self.assertEqual(payload['Action'], 'check')
            raise RuntimeError('provider detail that must not leak')
        with patch.object(storage_settings, 'helper', side_effect=check), self.assertRaises(RuntimeError) as error:
            wi.preflight(self.cluster, self.values)
        self.assertNotIn('provider detail', str(error.exception))
        self.assertTrue(all(not path.exists() for path in paths))
        self.assertIn('--duration=10m', self.cluster.kube.call_args.args)

    def test_setup_scopes_impersonation_and_bucket_roles_to_selected_identity(self):
        self.cluster.kube.side_effect = [Mock(stdout=b'{"issuer":"https://kubernetes.default.svc"}'),
                                        Mock(stdout=b'{"keys":[{"kty":"RSA","kid":"public"}]}')]
        def google(args, **kwargs):
            return None if 'describe' in args else {}
        with patch.object(wi, 'google', side_effect=google) as call, contextlib.redirect_stdout(io.StringIO()):
            wi.setup(self.cluster, self.values)
        commands = [c.args[0] for c in call.call_args_list]
        provider = next(c for c in commands if 'create-oidc' in c)
        self.assertIn("--attribute-condition=google.subject == 'system:serviceaccount:romana-test:practice-romanian'", provider)
        bindings = [c for c in commands if 'add-iam-policy-binding' in c]
        self.assertEqual(len(bindings), 3)
        self.assertIn('--role=roles/iam.workloadIdentityUser', bindings[0])
        for command in bindings[1:]:
            self.assertIn('gs://private-courses', command)
            self.assertIn('--member=serviceAccount:' + self.values['ServiceAccount'], command)
        self.assertFalse(any('projects' in c[:2] for c in bindings))

    def test_failed_federation_leaves_running_app_and_storage_untouched(self):
        with patch.object(microk8s, 'private_environment', return_value={}), \
             patch.object(storage_settings, 'mode', return_value='gcs'), \
             patch.object(storage_settings, 'cloud', return_value=self.values), \
             patch.object(storage_settings, 'active', return_value={'Provider': 's3'}), \
             patch.object(microk8s, 'namespace'), patch.object(microk8s, 'kube', return_value=Mock(stdout=b'existing')), \
             patch.object(wi, 'preflight', side_effect=RuntimeError('access denied')), \
             patch.object(microk8s, 'database') as database, patch.object(microk8s, 'sync_secret') as secret:
            with self.assertRaises(RuntimeError):
                microk8s.deploy()
            database.assert_not_called()
            secret.assert_not_called()

    def test_default_storage_and_explicit_local_mode(self):
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(storage_settings.mode(), 'gcs')
        with patch.dict(os.environ, {'ROMANA_STORAGE': 'minio'}):
            self.assertEqual(storage_settings.mode(), 'minio')


if __name__ == '__main__':
    unittest.main()
