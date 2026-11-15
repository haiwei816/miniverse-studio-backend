// Only JSON data crosses the allowlisted game API bridge. Structured cloning
// also supports cycles, BigInt, maps and binary buffers, which are not API DTOs.
export function encodeBridgeBody(body) {
  const ancestors = new Set();
  function validate(value, depth) {
    if (depth > 20) throw new Error('bridge_body_too_deep');
    if (value === null || typeof value === 'string' || typeof value === 'boolean') return;
    if (typeof value === 'number' && Number.isFinite(value)) return;
    if (typeof value !== 'object') throw new Error('bridge_body_not_json');
    if (ancestors.has(value)) throw new Error('bridge_body_cycle');
    if (!Array.isArray(value) && Object.getPrototypeOf(value) !== Object.prototype && Object.getPrototypeOf(value) !== null) throw new Error('bridge_body_not_json');
    ancestors.add(value);
    for (const entry of Array.isArray(value) ? value : Object.values(value)) validate(entry, depth + 1);
    ancestors.delete(value);
  }
  const value = body ?? {};
  validate(value, 0);
  const encoded = JSON.stringify(value);
  if (new TextEncoder().encode(encoded).byteLength > 65536) throw new Error('bridge_body_too_large');
  return encoded;
}
