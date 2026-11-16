import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, expect, test } from 'vitest';
import { prepareReleaseIdentity, writeReleaseManifest } from './release-identity.mjs';

const temporaryDirectories: string[] = [];

function temporaryDirectory() {
  const directory = mkdtempSync(join(tmpdir(), 'yzcm-release-'));
  temporaryDirectories.push(directory);
  return directory;
}

function git(directory: string, ...arguments_: string[]) {
  return execFileSync('git', arguments_, { cwd: directory, encoding: 'utf8' }).trim();
}

afterEach(() => {
  while (temporaryDirectories.length) rmSync(temporaryDirectories.pop()!, { recursive: true, force: true });
});

test('derives an auditable release identity from the current Git commit', () => {
  const directory = temporaryDirectory();
  git(directory, 'init');
  git(directory, 'config', 'user.name', 'Release Test');
  git(directory, 'config', 'user.email', 'release-test@example.invalid');
  writeFileSync(join(directory, 'tracked.txt'), 'baseline\n');
  git(directory, 'add', 'tracked.txt');
  git(directory, 'commit', '-m', 'baseline');

  const commit = git(directory, 'rev-parse', 'HEAD');
  expect(prepareReleaseIdentity({ cwd: directory, version: '1.1.0', material: 'liquid-smooth-a' })).toEqual({
    version: '1.1.0',
    material: 'liquid-smooth-a',
    commit,
    shortCommit: commit.slice(0, 7),
    dirty: false,
  });

  writeFileSync(join(directory, 'tracked.txt'), 'changed\n');
  expect(prepareReleaseIdentity({ cwd: directory, version: '1.1.0', material: 'liquid-smooth-a' }).dirty).toBe(true);
});

test('refuses to manufacture a release identity outside Git', () => {
  const directory = temporaryDirectory();
  expect(() => prepareReleaseIdentity({ cwd: directory, version: '1.1.0', material: 'liquid-smooth-a' }))
    .toThrow(/Git repository/);
});

test('writes a release manifest with exact wallpaper hashes', () => {
  const directory = temporaryDirectory();
  mkdirSync(join(directory, 'src/assets/glass'), { recursive: true });
  mkdirSync(join(directory, 'dist'), { recursive: true });
  writeFileSync(join(directory, 'src/assets/glass/wallpaper-light.png'), 'light-wallpaper');
  writeFileSync(join(directory, 'src/assets/glass/wallpaper-dark.png'), 'dark-wallpaper');
  const identity = {
    version: '1.1.0',
    material: 'liquid-smooth-a',
    commit: '0123456789abcdef0123456789abcdef01234567',
    shortCommit: '0123456',
    dirty: false,
  };

  const manifest = writeReleaseManifest({
    cwd: directory,
    identity,
    builtAt: '2026-10-24T08:00:00.000Z',
  });

  expect(manifest).toEqual({
    ...identity,
    builtAt: '2026-10-24T08:00:00.000Z',
    wallpapers: {
      light: 'sha256:6a92719141f20312f82ba56ec33780838eb73ebcb73fda4f7c4df8aeb0abb6a7',
      dark: 'sha256:2647418ffc296ef9b1cc1f2fbb2010df795d256d6b1a73d66246764c95f06c60',
    },
  });
  expect(JSON.parse(readFileSync(join(directory, 'dist/release-manifest.json'), 'utf8'))).toEqual(manifest);
});
