// Keep game credentials and their visible frame in the same lifecycle. The
// platform session owns each operation; delayed work cannot replace a new one.
export function createGameSessionController({ session, play, frameState, launchBusy, requireLogin, request, createChallenge, loadLibrary, notify, origin, fetch: gameFetch = (...args) => globalThis.fetch(...args) }) {
  let current = null;
  let generation = 0;

  function reset() {
    generation++;
    current = null;
    play.value = null;
    frameState.value = '';
    launchBusy.value = false;
  }

  async function launch(game) {
    if (!requireLogin() || launchBusy.value) return;
    const owner = session.value;
    const run = ++generation;
    const active = () => session.value === owner && generation === run;
    launchBusy.value = true;
    try {
      const { verifier, challenge } = await createChallenge();
      if (!active()) return;
      const data = await request(`/v1/platform/games/${encodeURIComponent(game.game_id)}/launch`, 'POST', { code_challenge: challenge });
      if (!active()) return;
      const entry = new URL(data.entry_url, origin);
      if (entry.origin !== origin || !entry.pathname.startsWith('/games/') || entry.search || entry.hash) throw new Error('此入口尚不支持安全的网页启动');
      const exchanged = await request('/v1/platform/launch/exchange', 'POST', { launch_code: data.launch_code, code_verifier: verifier, game_id: game.game_id }, { anonymous: true });
      if (!active()) return;
      current = { ...exchanged, epoch: data.epoch, channel: crypto.randomUUID(), pending: 0 };
      frameState.value = '正在连接游戏…';
      play.value = { ...game, src: entry.href };

      // The launch is already complete. An optional library refresh must not
      // discard the credentials while leaving a live frame on screen.
      try { await loadLibrary(); }
      catch (error) { if (active()) notify(error.message); }
    } catch (error) {
      if (active()) {
        current = null;
        play.value = null;
        frameState.value = '';
        notify(error.message);
      }
    } finally {
      if (active()) launchBusy.value = false;
    }
  }

  async function close() {
    const previous = current;
    const owner = session.value;
    reset();
    if (!previous) return;
    try {
      const result = await gameFetch(`/v1/games/${encodeURIComponent(previous.game_id)}/session/logout`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${previous.access_token}` },
        signal: AbortSignal.timeout(45000),
        body: '{}',
      });
      if (!result.ok && result.status !== 401) throw new Error('游戏会话结束确认失败，请重新登录后继续');
      if (session.value !== owner) return;
      try {
        // Another game may have started locally or on another device while
        // logout was in flight. Only clear the world belonging to this game.
        await request('/v1/platform/companion/context', 'POST', { game_id: '', expected_epoch: previous.epoch });
      } catch (error) {
        if (error.status !== 409 || error.code !== 'companion_context_changed') throw error;
      }
      if (session.value === owner) await loadLibrary();
    } catch (error) {
      if (session.value === owner) notify(error.message);
    }
  }

  return { get current() { return current; }, launch, close, reset };
}
