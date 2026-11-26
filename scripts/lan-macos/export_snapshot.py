"""Operator-only read-only export. Run over SSH; never print secret contents."""
import datetime
import json
import os
import pathlib
import re
import shlex
import subprocess
import sys
import tarfile

DATA_TABLES = (
    'schema_migrations', 'accounts', 'users', 'batch_factory_v12_bridge_users',
    'novel_fetch_workshop_configs', 'novel_agent_defaults',
    'batch_factory_v12_shuihuo_configs', 'batch_factory_v12_capability_settings',
    'batch_factory_v12_automation_presets', 'batch_factory_v12_automation_preset_revisions',
    'batch_factory_v12_prompt_definitions', 'batch_factory_v12_prompt_versions',
    'image_capacity_accounts', 'image_capacity_policies',
)
SYSTEM_FILES = (
    'member-profiles.json', 'member-profile-details.json', 'mfa.json', 'config-secret.key',
    'presets.json', 'script-constraint-prompts.json',
)
ENV_KEYS = {
    'QINGYU_N8_ADMIN_TOKEN', 'QINGYU_MATERIAL_SELECT_URL',
    'QIANTIE_N8_COMIC_OPEN_API_KEY', 'QIANTIE_N8_COMIC_BASE_URL',
    'QIANTIE_N8_ADMIN_BASE_URL', 'QIANTIE_N8_COMIC_PRODUCER_ACCOUNTS',
    'QIANTIE_CONFIG_SECRET_KEY', 'QIANTIE_GIANT_MATERIAL_PLUGIN_URL', 'QIANTIE_GIANT_MATERIAL_PLUGIN_TOKEN',
}
for _prefix in ('REFERENCE_ASSET', 'NOVEL_FETCH', 'AGENT_ASSET', 'PRODUCTION_MEDIA', 'BATCH_FACTORY_V11_MERGE'):
    ENV_KEYS.update('QIANTIE_' + _prefix + '_TOS_' + suffix for suffix in
                    ('ENDPOINT', 'REGION', 'BUCKET', 'ACCESS_KEY', 'SECRET_KEY', 'PUBLIC_BASE_URL'))


def allowed_environment(environment):
    return {key: value for key, value in environment.items() if key in ENV_KEYS and value}


def main():
    output = pathlib.Path(sys.argv[1]).resolve()
    if output.exists():
        raise SystemExit('Snapshot destination already exists; refusing to overwrite')
    output.mkdir(mode=0o700, parents=True)
    environment = dict(line.split('=', 1) for line in pathlib.Path('/opt/www/qiantie/qiantie.env').read_text().splitlines()
                       if '=' in line and not line.lstrip().startswith('#'))
    environment = {key: value.strip().strip('"').strip("'") for key, value in environment.items()}
    match = re.fullmatch(r'([^:]+):(.*?)@tcp\(([^:]+):(\d+)\)/([^?]+)(?:\?.*)?', environment['QIANTIE_MYSQL_DSN'])
    if not match:
        raise SystemExit('Unsupported production DSN; nothing exported')
    username, password, host, port, database = match.groups()
    child_env = os.environ.copy()
    child_env['MYSQL_PWD'] = password
    connection = ['-h', host, '-P', port, '-u', username]

    def query(sql):
        result = subprocess.run(['mysql'] + connection + [database, '-N', '-e', sql], env=child_env,
                                capture_output=True, text=True)
        if result.returncode:
            raise RuntimeError('Read-only metadata query failed')
        return [line.split('\t') for line in result.stdout.splitlines() if line]

    tables = {row[0] for row in query('SHOW TABLES')}
    if not {'accounts', 'schema_migrations'} <= tables:
        raise SystemExit('Required account/schema tables absent')
    private = output / 'private'
    private.mkdir(mode=0o700)
    dump_options = ['--set-gtid-purged=OFF', '--no-tablespaces', '--skip-triggers', '--skip-comments', '--hex-blob']
    with (private / 'seed.sql').open('wb') as stream:
        for args in [dump_options + ['--no-data', '--skip-lock-tables', database],
                     dump_options + ['--no-create-info', '--single-transaction', '--complete-insert', database] +
                     [table for table in DATA_TABLES if table in tables]]:
            result = subprocess.run(['mysqldump'] + connection + args, env=child_env, stdout=stream, stderr=subprocess.PIPE)
            if result.returncode:
                raise RuntimeError('Read-only snapshot dump failed; partial snapshot must not be distributed')
    (private / 'seed.sql').chmod(0o600)
    values = allowed_environment(environment)
    (private / 'settings.env').write_text(''.join('export ' + key + '=' + shlex.quote(value) + '\n'
                                               for key, value in sorted(values.items())))
    (private / 'settings.env').chmod(0o600)

    copied = []

    def copy_file(source, relative):
        source = pathlib.Path(source)
        if not source.exists():
            return
        if source.is_symlink() or not source.is_file():
            raise RuntimeError('Snapshot source is not a regular file: ' + source.name)
        target = output / relative
        target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        target.write_bytes(source.read_bytes())
        target.chmod(0o600)
        copied.append(relative)

    system = pathlib.Path(environment.get('QIANTIE_MEMBER_SYSTEM_DIR', '/opt/www/qiantie-data/system'))
    for name in SYSTEM_FILES:
        copy_file(system / name, 'data/system/' + name)
    explicit_key = environment.get('QIANTIE_CONFIG_SECRET_KEY_FILE')
    if explicit_key:
        copy_file(explicit_key, 'data/system/config-secret.key')
    if not (output / 'data/system/member-profiles.json').exists():
        raise RuntimeError('Team profile snapshot is required')
    users = pathlib.Path(environment.get('QIANTIE_SCRIPT_HISTORY_DIR', '/opt/www/qiantie-data/users'))
    identities = [row[0] for row in query('SELECT username FROM accounts ORDER BY username')]
    for owner in identities:
        if not re.fullmatch(r'[a-zA-Z0-9_.@-]+', owner):
            raise RuntimeError('Unsupported account folder name')
        copy_file(users / owner / 'api-config.json', 'data/users/' + owner + '/api-config.json')
    preset = environment.get('QIANTIE_BATCH_FACTORY_AUTOMATION_PRESETS_PATH')
    if preset:
        copy_file(preset, 'data/batch-factory-v11-automation-presets.json')
    avatars = pathlib.Path(environment.get('QIANTIE_MEMBER_AVATAR_DIR', '/opt/www/qiantie-data/avatars'))
    if avatars.exists():
        for item in avatars.rglob('*'):
            if item.is_file() and item.suffix.lower() in ('.png', '.jpg', '.jpeg', '.webp', '.gif'):
                copy_file(item, 'data/avatars/' + item.relative_to(avatars).as_posix())
    counts = query('SELECT COUNT(*),SUM(active) FROM accounts')[0]
    manifest = {'createdAtUtc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                'accounts': int(counts[0]), 'activeAccounts': int(counts[1]),
                'dataTables': [table for table in DATA_TABLES if table in tables], 'files': copied,
                'excluded': ['auth_sessions', 'execution jobs', 'works', 'production DB/Redis credentials', 'SSH keys']}
    (output / 'snapshot.json').write_text(json.dumps(manifest, indent=2))
    archive = output.with_suffix('.tgz')
    with tarfile.open(archive, 'w:gz') as tar:
        for file in output.rglob('*'):
            if file.is_file():
                tar.add(file, arcname=file.relative_to(output).as_posix(), recursive=False)
    archive.chmod(0o600)
    print(json.dumps({'archive': str(archive), 'accounts': manifest['accounts'], 'activeAccounts': manifest['activeAccounts'],
                      'files': len(copied), 'bytes': archive.stat().st_size}))


if __name__ == '__main__':
    main()
