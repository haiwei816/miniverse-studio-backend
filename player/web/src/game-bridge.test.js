import test from 'node:test';
import assert from 'node:assert/strict';
import { encodeBridgeBody } from './game-bridge.js';

test('the game bridge accepts a bounded JSON DTO', () => {
  assert.equal(encodeBridgeBody({revision:0,data:'{"进度":2}'}),'{"revision":0,"data":"{\\"进度\\":2}"}');
});

test('structured-clone values outside JSON are rejected before API forwarding', () => {
  const cycle = {}; cycle.self=cycle;
  for (const body of [cycle,{value:1n},{value:NaN},{value:undefined},new Map(),new Uint8Array(4)]) assert.throws(()=>encodeBridgeBody(body));
});

test('the bridge limit measures UTF-8 bytes rather than UTF-16 characters', () => {
  assert.throws(()=>encodeBridgeBody({text:'游'.repeat(22000)}),/too_large/);
  assert.doesNotThrow(()=>encodeBridgeBody({text:'游'.repeat(20000)}));
});
