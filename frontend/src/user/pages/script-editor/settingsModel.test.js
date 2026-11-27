import test from 'node:test';import assert from 'node:assert/strict';import {layoutStorageKey} from './settingsModel.js';
test('layout storage is scoped by account and work',()=>assert.equal(layoutStorageKey('alice','work-1'),'script-editor:layout:alice:work-1'));
