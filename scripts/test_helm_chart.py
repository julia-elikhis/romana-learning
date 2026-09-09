import json
import unittest

import helm_chart
import sops_secrets


class ProductionChartTest(unittest.TestCase):
    def test_only_identifiers_and_secret_references_reach_helm(self):
        settings = dict.fromkeys(sops_secrets.settings_keys('prod'), 'private-value-do-not-render')
        settings.update(APP_BASE_URL='https://learn.example.com', PGHOST='db.internal.example',
                        PGPORT='5432', PGDATABASE='learning', PGSSLMODE='verify-full',
                        GCS_SERVICE_ACCOUNT='learning@test-project.iam.gserviceaccount.com')
        values = helm_chart.production_values(settings)
        self.assertNotIn('private-value-do-not-render', json.dumps(values))
        self.assertEqual(values['auth']['baseURL'], settings['APP_BASE_URL'])
        self.assertEqual(values['database']['host'], settings['PGHOST'])
        self.assertEqual(values['database']['name'], settings['PGDATABASE'])
        self.assertEqual(values['database']['sslMode'], settings['PGSSLMODE'])
        self.assertEqual(values['serviceAccount']['annotations']['iam.gke.io/gcp-service-account'],
                         settings['GCS_SERVICE_ACCOUNT'])
        self.assertNotIn('storage', values)
        self.assertNotIn('generation', values)
        self.assertNotIn('existingSecret', values['auth'])


if __name__ == '__main__':
    unittest.main()
