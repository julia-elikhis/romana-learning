"""Render/lint the chart with environment-specific SOPS configuration."""
import argparse
import json
from pathlib import Path
import subprocess
import sys

import sops_secrets
import workload_identity

ROOT = Path(__file__).resolve().parents[1]
CHART = ROOT / 'charts' / 'romanian'


def production_values(settings):
    # Only routing/identity/database identifiers and Secret references reach Helm.
    # Passwords, OAuth credentials, and the exercise API endpoint stay out of values.
    values = {
        'auth': {'baseURL': settings['APP_BASE_URL']},
        'database': {
            'host': settings['PGHOST'],
            'port': int(settings['PGPORT']), 'name': settings['PGDATABASE'],
            'sslMode': settings['PGSSLMODE'],
        },
    }
    values.update(workload_identity.helm_values({'ServiceAccount': settings['GCS_SERVICE_ACCOUNT']}))
    return values


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('template', 'lint'))
    parser.add_argument('--release')
    parser.add_argument('--namespace')
    parser.add_argument('-f', '--values', action='append', default=[])
    parser.add_argument('--ingress-only', action='store_true')
    args = parser.parse_args()
    if args.ingress_only and args.action != 'template':
        parser.error('--ingress-only requires template')
    production = sops_secrets.selected_environment() == 'prod'
    default_name = 'romana-learning' if production else 'romanian'
    release = args.release or default_name
    namespace = args.namespace or default_name
    command = ['helm', args.action]
    if args.action == 'template':
        command.append(release)
    command += [str(CHART), '--namespace', namespace]
    values = {}
    if production:
        settings = sops_secrets.load()
        sops_secrets.require_database(settings)
        values = production_values(settings)
        command += ['-f', str(CHART / 'values-prod.yaml'), '-f', str(CHART / 'values-ingress-nginx.yaml')]
    for path in args.values:
        command += ['-f', path]
    command += ['-f', '-']
    if args.ingress_only:
        command += ['--show-only', 'templates/ingress.yaml']
    return subprocess.run(command, input=json.dumps(values).encode(), cwd=ROOT).returncode


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (RuntimeError, OSError, ValueError):
        # Never include decrypted settings or underlying tool diagnostics here.
        print('Chart configuration failed; check the selected SOPS environment and required settings.', file=sys.stderr)
        sys.exit(1)
