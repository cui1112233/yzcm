"""Operator build helper; not required on the target Mac."""
import argparse
import hashlib
import json
import pathlib
import shutil
import tarfile
import zipfile
import struct


def verify_binary(binary, arch):
    with pathlib.Path(binary).open('rb') as stream:
        header = stream.read(8)
    cpu = {'arm64': 0x100000c, 'amd64': 0x1000007}[arch]
    if len(header) != 8 or header[:4] != bytes.fromhex('cffaedfe') or struct.unpack('<I', header[4:8])[0] != cpu:
        raise ValueError('Wrong Mach-O architecture: ' + arch)


def extract_snapshot(archive, destination):
    destination = pathlib.Path(destination).resolve()
    with tarfile.open(archive, 'r:gz') as source:
        for member in source.getmembers():
            name = pathlib.PurePosixPath(member.name)
            if name.is_absolute() or '..' in name.parts or member.issym() or member.islnk():
                raise ValueError('Unsafe snapshot path')
            if not name.parts or name.parts[0] not in ('data', 'private', 'snapshot.json'):
                raise ValueError('Unexpected snapshot member')
            target = destination.joinpath(*name.parts).resolve()
            if destination not in target.parents or member.size > 512 * 1024 * 1024:
                raise ValueError('Snapshot member outside package or too large')
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            elif member.isfile():
                target.parent.mkdir(parents=True, exist_ok=True)
                with source.extractfile(member) as incoming, target.open('wb') as outgoing:
                    shutil.copyfileobj(incoming, outgoing)
            else:
                raise ValueError('Unexpected snapshot member type')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--snapshot', required=True)
    parser.add_argument('--binaries', required=True)
    parser.add_argument('--out', required=True)
    parser.add_argument('--source-sha', required=True)
    parser.add_argument('--frontend-entry', required=True)
    parser.add_argument('--frontend-index-sha', required=True)
    parser.add_argument('--packaging-commit', required=True)
    parser.add_argument('--acceptance', required=True)
    parser.add_argument('--private-verification', required=True)
    args = parser.parse_args()
    target = pathlib.Path(args.out).resolve()
    if target.exists():
        raise SystemExit('Output already exists; refusing to overwrite a potentially used installation')
    target.mkdir(parents=True)
    extract_snapshot(args.snapshot, target)
    files = pathlib.Path(__file__).parent
    for original, result in [('common.sh', 'common.sh'), ('setup.command', '首次安装.command'),
                             ('start.command', '启动.command'), ('stop.command', '停止.command'),
                             ('backup.command', '备份.command'), ('README.md', '使用说明.txt')]:
        (target / result).write_bytes((files / original).read_bytes().replace(b'\r\n', b'\n'))
    (target / 'bin').mkdir()
    for arch in ('arm64', 'amd64'):
        filename = 'qiantie-darwin-' + arch
        binary = pathlib.Path(args.binaries) / filename
        verify_binary(binary, arch)
        shutil.copy2(binary, target / 'bin' / filename)
    (target / 'source-sha.txt').write_text(args.source_sha + '\n')
    (target / 'frontend-entry.txt').write_text(args.frontend_entry + '\n')
    evidence = {'runtime': json.loads(pathlib.Path(args.acceptance).read_text()),
                'configuration': json.loads(pathlib.Path(args.private_verification).read_text())}
    (target / 'acceptance.json').write_text(json.dumps(evidence, indent=2))
    manifest = {'sourceSha': args.source_sha, 'packagingCommit': args.packaging_commit,
                'sourcePatch': 'productionmedia: enable existing disk-space guard on Darwin; rename linux file to unix',
                'frontendEntry': args.frontend_entry,
                'frontendIndexSha256': args.frontend_index_sha, 'target': 'macOS arm64 / amd64', 'files': {}}
    for item in sorted(target.rglob('*')):
        if item.is_file():
            manifest['files'][item.relative_to(target).as_posix()] = hashlib.sha256(item.read_bytes()).hexdigest()
    (target / 'manifest.json').write_text(json.dumps(manifest, indent=2))
    archive = target.with_suffix('.zip')
    with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED, compresslevel=6) as output:
        for item in sorted(target.rglob('*')):
            if not item.is_file():
                continue
            relative = item.relative_to(target).as_posix()
            info = zipfile.ZipInfo(target.name + '/' + relative)
            info.create_system = 3
            executable = relative.startswith('bin/') or relative.endswith(('.command', '.sh'))
            info.external_attr = ((0o100700 if executable else 0o100600) << 16)
            info.compress_type = zipfile.ZIP_DEFLATED
            output.writestr(info, item.read_bytes())
    print(json.dumps({'archive': str(archive), 'bytes': archive.stat().st_size,
                      'sha256': hashlib.sha256(archive.read_bytes()).hexdigest()}))


if __name__ == '__main__':
    main()
