import importlib.util
import pathlib
import io
import tarfile
import tempfile
import unittest
import struct

ROOT = pathlib.Path(__file__).parent


class PackagePolicyTests(unittest.TestCase):
    def test_binary_architecture_must_match_target_chip(self):
        spec = importlib.util.spec_from_file_location('builder', ROOT / 'package.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as folder:
            binary = pathlib.Path(folder) / 'binary'
            binary.write_bytes(bytes.fromhex('cffaedfe') + struct.pack('<I', 0x1000007))
            module.verify_binary(binary, 'amd64')
            with self.assertRaises(ValueError):
                module.verify_binary(binary, 'arm64')
            binary.write_bytes(b'MZ')
            with self.assertRaises(ValueError):
                module.verify_binary(binary, 'arm64')

    def setUp(self):
        spec = importlib.util.spec_from_file_location('snapshot', ROOT / 'export_snapshot.py')
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)

    def test_only_identity_and_configuration_rows_are_exported(self):
        allowed = set(self.module.DATA_TABLES)
        self.assertTrue({'accounts', 'schema_migrations', 'novel_fetch_workshop_configs'} <= allowed)
        for name in ['auth_sessions', 'batch_automation_jobs', 'local_executor_jobs',
                     'yzcm_usage_requests', 'batch_factory_v12_books', 'agent_projects']:
            self.assertNotIn(name, allowed)

    def test_environment_never_exports_host_credentials_or_paths(self):
        safe = self.module.allowed_environment({
            'QIANTIE_MYSQL_DSN': 'production-private', 'QIANTIE_REDIS_URL': 'production-redis',
            'QIANTIE_BRIDGE_SECRET': 'production-bridge', 'QIANTIE_SCRIPT_HISTORY_DIR': '/production/users',
            'QINGYU_N8_ADMIN_TOKEN': 'connector-private', 'QIANTIE_REFERENCE_ASSET_TOS_BUCKET': 'bucket',
            'QIANTIE_CONFIG_SECRET_KEY': 'config-private', 'HOME': '/root',
        })
        self.assertEqual(set(safe), {'QINGYU_N8_ADMIN_TOKEN', 'QIANTIE_REFERENCE_ASSET_TOS_BUCKET', 'QIANTIE_CONFIG_SECRET_KEY'})

    def test_snapshot_contains_encryption_material_and_mfa_but_no_sessions(self):
        names = set(self.module.SYSTEM_FILES)
        self.assertTrue({'member-profiles.json', 'mfa.json', 'config-secret.key'} <= names)
        self.assertNotIn('sessions.json', names)
        self.assertNotIn('accounts.json', names)

    def test_archive_traversal_and_links_cannot_escape(self):
        spec = importlib.util.spec_from_file_location('builder', ROOT / 'package.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        for name, kind in [('../outside.txt', tarfile.REGTYPE), ('/outside.txt', tarfile.REGTYPE),
                           ('data/link', tarfile.SYMTYPE), ('private/link', tarfile.LNKTYPE)]:
            with self.subTest(name=name, kind=kind), tempfile.TemporaryDirectory() as folder:
                path = pathlib.Path(folder)
                with tarfile.open(path / 'bad.tgz', 'w:gz') as archive:
                    info = tarfile.TarInfo(name)
                    info.type = kind
                    info.linkname = '/outside.txt'
                    if kind == tarfile.REGTYPE:
                        info.size = 1
                        archive.addfile(info, io.BytesIO(b'x'))
                    else:
                        archive.addfile(info)
                with self.assertRaises(ValueError):
                    module.extract_snapshot(path / 'bad.tgz', path / 'output')

    def test_safe_snapshot_extracts_private_seed(self):
        spec = importlib.util.spec_from_file_location('builder', ROOT / 'package.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as folder:
            path = pathlib.Path(folder)
            with tarfile.open(path / 'safe.tgz', 'w:gz') as archive:
                info = tarfile.TarInfo('private/seed.sql')
                info.size = 8
                archive.addfile(info, io.BytesIO(b'SELECT 1'))
            module.extract_snapshot(path / 'safe.tgz', path / 'output')
            self.assertEqual((path / 'output/private/seed.sql').read_bytes(), b'SELECT 1')


if __name__ == '__main__':
    unittest.main()
