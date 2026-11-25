import { api } from './client.mjs';

export async function gm(name = 'admin') {
  const res = await api('/gm/login', 'POST', { username: name, password: 'sandbox-' + name });
  return res.access_token;
}
