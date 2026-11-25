let token = '';
let cookieSession = false;
let onLost = () => {};
export function setToken(value) { token = value || ''; }
export function setCookieSession(active) { cookieSession = Boolean(active); }
export function onSessionLost(fn) { onLost = fn; }
export const messages = {
  invalid_credentials: '账号或密码不正确', username_taken: '这个账号已被使用', invalid_username: '账号需为 3–32 位字母、数字或下划线',
  invalid_password: '请使用符合要求的密码', unauthorized: '请先登录', platform_unauthorized: '请重新登录',
  rate_limited: '操作过于频繁，请稍后重试', game_not_found: '游戏暂不可用', invalid_launch: '启动凭证无效，请重新启动',
  auth_rate_limited: '登录尝试过于频繁，请稍后重试', companion_model_not_configured: 'AI 模型尚未配置，暂时无法对话', companion_context_changed: '伙伴已切换世界，请返回平台重新进入',
  invalid_credentials_format: '账号需为 3–32 位字母、数字、下划线或短横线，首位为字母或数字；密码需为 8–128 位',
  ai_not_configured: 'AI 服务尚未配置，请稍后再试', internal_error: '服务暂时不可用，请稍后重试',
  invalid_companion: '请检查伙伴名称和风格', context_expired: '游戏上下文已更新，请重新进入游戏',
  csrf_rejected: '安全校验未通过，请刷新页面后重试', account_suspended: '账号当前处于封禁状态',
};
export async function request(path, method = 'GET', body, { anonymous = false, skipUnauthorized = false } = {}) {
  const upperMethod = String(method || 'GET').toUpperCase();
  const requestToken = anonymous ? '' : token;
  const useCookie = !anonymous && !requestToken;
  const headers = { 'Content-Type': 'application/json' };
  if (requestToken) headers.Authorization = `Bearer ${requestToken}`;
  if (!anonymous && upperMethod !== 'GET' && upperMethod !== 'HEAD') headers['X-MiniVerse-CSRF'] = '1';
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 45000);
  try {
    const fetchOpts = { method: upperMethod, headers, signal: controller.signal, body: body === undefined ? undefined : JSON.stringify(body) };
    if (!anonymous) fetchOpts.credentials = 'same-origin';
    const res = await fetch(path, fetchOpts);
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      if (res.status === 401 && !skipUnauthorized && ((requestToken && token === requestToken) || (useCookie && cookieSession))) onLost();
      const error = new Error(messages[data.error] || (res.status === 501 ? '此功能已预留接口，尚未接入' : `请求失败（${data.error || res.status}）`));
      error.code = data.error; error.status = res.status; throw error;
    }
    return data;
  } catch (error) {
    if (error.name === 'AbortError') throw new Error('请求超时，请稍后重试');
    if (error instanceof TypeError) throw new Error('无法连接服务，请检查网络后重试');
    throw error;
  } finally { clearTimeout(timeout); }
}
export async function createChallenge() {
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  const verifier = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier));
  const challenge = btoa(String.fromCharCode(...new Uint8Array(digest))).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
  return { verifier, challenge };
}
