import { createApp, ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue';
import { request, setToken, setCookieSession, onSessionLost, createChallenge } from './api.js';
import { GameArt, Companion } from './art.js';
import { encodeBridgeBody } from './game-bridge.js';
import { createGameSessionController } from './game-session.js';
import { DiscoverView } from './views/discover.js';
import { LibraryView } from './views/library.js';
import { CompanionView } from './views/companion.js';
import { DevicesView } from './views/devices.js';
import './style.css';

const NAV = [
  { id: 'discover', icon: '◈', name: '发现', shortName: '发现', subtitle: '你的下一站，从这里开始' },
  { id: 'library', icon: '▦', name: '我的游戏', shortName: '游戏', subtitle: '熟悉的世界，随时回来' },
  { id: 'companion', icon: '✦', name: '我的伙伴', shortName: '伙伴', subtitle: '不同世界，同一个老朋友' },
  { id: 'devices', icon: '⌘', name: '实体伙伴', shortName: '实体', subtitle: '给你的伙伴，一个现实中的身体' },
];

const COMPANION_LEVELS = {
  basic: '资料问答（了解世界设定）',
  aware: '场景感知（跟随所在场景）',
  context: '状态感知（结合当前目标给提示）',
  interactive: '深度互动（游戏内联动反馈）',
};

const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

function trapDialogTab(container, event) {
  if (event.key !== 'Tab' || !container) return;
  const nodes = Array.from(container.querySelectorAll(FOCUSABLE)).filter(el => el.offsetParent !== null);
  if (!nodes.length) {
    event.preventDefault();
    container.focus();
    return;
  }
  const first = nodes[0];
  const last = nodes[nodes.length - 1];
  if (event.shiftKey && (document.activeElement === first || document.activeElement === container)) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

const app = createApp({
  components: { GameArt, Companion, DiscoverView, LibraryView, CompanionView, DevicesView },
  setup() {
    const session = ref(null), view = ref('discover'), games = ref([]), library = ref([]);
    const libraryLoading = ref(false), libraryError = ref('');
    const search = ref(''), category = ref('全部'), loading = ref(false), error = ref(''), detail = ref(null);
    const nextCursor = ref(''), loadingMore = ref(false), categoryOptions = ref([]);
    const toast = ref(''), authOpen = ref(false), authMode = ref('login'), username = ref(''), password = ref(''), authBusy = ref(false), authError = ref('');
    const pendingAction = ref(null);
    const companion = ref({ name: '星伴', style: 'gentle' }), memories = ref([]), memoryText = ref(''), chatText = ref(''), chatBusy = ref(false), companionBusy = ref(false);
    const companionLoaded = ref(false), companionError = ref(''), worldContext = ref(null);
    const chat = ref([]), companionMode = ref(''), capabilities = ref(null), devicesError = ref('');
    const chatLog = ref(null), chatAtBottom = ref(true), hasNewChat = ref(false);
    const play = ref(null), frame = ref(null), frameState = ref(''), launchBusy = ref(false), accountOpen = ref(false);
    const authDialog = ref(null), accountDialog = ref(null), playDialog = ref(null);
    const theme = ref(localStorage.getItem('miniverse.player.theme') || 'dark');
    let toastTimer, searchTimer, requestNumber = 0;
    const bridgeRules = new Set(['GET /profile', 'GET /saves/web-main', 'PUT /saves/web-main', 'POST /events', 'GET /activities', 'PUT /companion/context', 'POST /companion/chat']);
    const current = computed(() => NAV.find(n => n.id === view.value) || NAV[0]);
    const categories = computed(() => ['全部', ...categoryOptions.value]);
    const displayed = computed(() => games.value);
    const featured = computed(() => games.value[0]);
    const libraryGames = computed(() => library.value.filter(g => g.favorite || g.last_played_at));
    const favorites = computed(() => new Set(library.value.filter(g => g.favorite).map(g => g.game_id)));
    const recent = computed(() => [...library.value].filter(g => g.last_played_at).sort((a, b) => b.last_played_at - a.last_played_at).slice(0, 2));
    const modeLabel = computed(() => {
      if (companionMode.value === 'model') return '真实 AI 对话';
      if (companionMode.value === 'demo') return '规则演示模式';
      if (companionMode.value === 'unconfigured') return 'AI 服务未配置';
      return '待连线确认';
    });
    const modeHint = computed(() => {
      if (companionMode.value === 'model') return '当前已连接 AI 模型。对话不自动成为长期记忆，重要内容请点击下方明确保存。';
      if (companionMode.value === 'demo') return '当前为规则演示模式，将按预设规则回复。连接 AI 模型后可提供自然对话。';
      if (companionMode.value === 'unconfigured') return '当前环境尚未配置 AI 模型服务，暂时无法发起模型对话。';
      return '对话不自动成为长期记忆。想让伙伴记住的事，由你明确保存。';
    });
    const worldContextText = computed(() => {
      const ctx = worldContext.value;
      if (!ctx || !ctx.game_id) return '当前世界：平台大厅（未进入游戏）';
      const match = games.value.find(g => g.game_id === ctx.game_id) || library.value.find(g => g.game_id === ctx.game_id);
      const name = match ? match.name : ctx.game_id;
      const parts = [name];
      if (ctx.scene_id) parts.push(ctx.scene_id);
      if (ctx.quest_stage) parts.push(ctx.quest_stage);
      return `当前世界：${parts.join(' · ')}`;
    });

    function companionLevelLabel(level) {
      return COMPANION_LEVELS[level] || '基础陪伴（了解世界设定）';
    }
    function notify(message) { toast.value = message; clearTimeout(toastTimer); toastTimer = setTimeout(() => toast.value = '', 4500); }
    function applyTheme() { document.documentElement.dataset.theme = theme.value; localStorage.setItem('miniverse.player.theme', theme.value); }
    watch(theme, applyTheme); applyTheme();
    function requireLogin(action = null) {
      if (session.value) return true;
      if (action) pendingAction.value = action;
      authOpen.value = true;
      authError.value = '';
      return false;
    }
    function closeAuth() {
      authOpen.value = false;
      pendingAction.value = null;
    }
    function go(id) { detail.value = null; location.hash = id; }
    async function route() {
      window.scrollTo(0, 0);
      const path = location.hash.slice(1) || 'discover';
      if (path.startsWith('game/')) {
        view.value = 'discover';
        let id = '';
        try { id = decodeURIComponent(path.slice(5)); } catch {}
        detail.value = games.value.find(g => g.game_id === id) || null;
        if (!detail.value && id) {
          try {
            const data = await request(`/v1/platform/games/${encodeURIComponent(id)}`, 'GET', undefined, { anonymous: true });
            if (location.hash.slice(1) === path) detail.value = data;
          } catch (e) { notify(e.message); }
        }
      } else {
        detail.value = null;
        view.value = NAV.some(n => n.id === path) ? path : 'discover';
      }
    }
    function openDetail(game) { location.hash = `game/${encodeURIComponent(game.game_id)}`; detail.value = game; }
    async function loadGames(append = false) {
      if (append && !nextCursor.value) return;
      const id = ++requestNumber;
      if (append) loadingMore.value = true; else loading.value = true;
      error.value = '';
      const query = new URLSearchParams();
      if (search.value.trim()) query.set('q', search.value.trim());
      if (category.value !== '全部') query.set('category', category.value);
      if (append) query.set('cursor', nextCursor.value);
      try {
        const data = await request(`/v1/platform/games?${query}`, 'GET', undefined, { anonymous: true });
        if (id === requestNumber) {
          games.value = append ? [...games.value, ...(data.items || [])] : data.items || [];
          nextCursor.value = data.next_cursor || '';
          categoryOptions.value = data.categories || [...new Set([...categoryOptions.value, ...games.value.map(g => g.category).filter(Boolean)])];
        }
      } catch (e) { if (id === requestNumber) error.value = e.message; }
      finally { if (id === requestNumber) { loading.value = false; loadingMore.value = false; } }
    }
    watch([search, category], () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => loadGames(), 250); });
    async function loadLibrary() {
      if (!session.value) { library.value = []; libraryError.value = ''; libraryLoading.value = false; return; }
      const owner = session.value;
      libraryLoading.value = true;
      libraryError.value = '';
      try {
        const data = await request('/v1/platform/library');
        if (session.value === owner) library.value = data.items || [];
      } catch (e) {
        if (session.value === owner) libraryError.value = e.message;
        throw e;
      } finally {
        if (session.value === owner) libraryLoading.value = false;
      }
    }
    async function loadCompanion() {
      if (!session.value) return;
      const owner = session.value;
      companionBusy.value = true;
      companionError.value = '';
      try {
        const [profile, memory, context] = await Promise.all([
          request('/v1/platform/companion'),
          request('/v1/platform/companion/memories'),
          request('/v1/platform/companion/context').catch(() => null),
        ]);
        if (session.value === owner) {
          companion.value = profile;
          memories.value = memory.items || [];
          worldContext.value = context;
          companionLoaded.value = true;
          if (profile.chat_mode) companionMode.value = profile.chat_mode;
        }
      } catch (e) {
        if (session.value === owner) {
          companionError.value = e.message;
          if (!companionLoaded.value) notify(e.message);
        }
      } finally {
        if (session.value === owner) companionBusy.value = false;
      }
    }
    async function loadDevices() {
      if (!session.value) return;
      const owner = session.value;
      devicesError.value = '';
      try {
        const data = await request('/v1/platform/devices/capabilities');
        if (session.value === owner) capabilities.value = data;
      } catch (e) { if (session.value === owner) devicesError.value = e.message; }
    }
    watch(view, async id => {
      if (id === 'library' && session.value) await loadLibrary().catch(() => {});
      if (id === 'devices' && session.value) await loadDevices();
      if (id === 'companion' && session.value) await loadCompanion();
    });
    async function authenticate() {
      if (authBusy.value) return;
      authBusy.value = true;
      authError.value = '';
      try {
        const data = await request(`/v1/platform/auth/${authMode.value}`, 'POST', { username: username.value.trim(), password: password.value }, { anonymous: true });
        session.value = { player_id: data.player_id, username: data.username };
        setToken('');
        setCookieSession(true);
        password.value = '';
        authOpen.value = false;
        chat.value = []; companionMode.value = ''; companionLoaded.value = false; companionError.value = '';
        notify(`欢迎回来，${data.username}`);
        await Promise.all([loadLibrary().catch(() => {}), loadCompanion()]);
        if (view.value === 'devices') await loadDevices();
        const queued = pendingAction.value;
        pendingAction.value = null;
        if (queued?.type === 'launch' && queued.game) await launch(queued.game);
        else if (queued?.type === 'favorite' && queued.game) await favorite(queued.game);
      } catch (e) { authError.value = e.message; } finally { authBusy.value = false; }
    }
    async function restoreSession() {
      try {
        const me = await request('/v1/platform/me', 'GET', undefined, { skipUnauthorized: true });
        if (me && me.player_id) {
          session.value = me;
          setToken('');
          setCookieSession(true);
          await Promise.all([loadLibrary().catch(() => {}), loadCompanion().catch(() => {})]);
          if (view.value === 'devices') await loadDevices();
        }
      } catch {}
    }
    const gameSessions = createGameSessionController({
      session,
      play,
      frameState,
      launchBusy,
      requireLogin: () => requireLogin(),
      request,
      createChallenge,
      loadLibrary,
      notify,
      origin: location.origin,
    });
    async function launch(game) {
      if (!requireLogin({ type: 'launch', game })) return;
      await gameSessions.launch(game);
    }
    const closeGame = gameSessions.close;
    function clearSession() {
      setToken('');
      setCookieSession(false);
      session.value = null; library.value = []; libraryLoading.value = false; libraryError.value = '';
      memories.value = []; companion.value = { name: '星伴', style: 'gentle' }; companionLoaded.value = false; companionError.value = ''; worldContext.value = null;
      chat.value = []; chatText.value = ''; memoryText.value = ''; companionMode.value = ''; capabilities.value = null; devicesError.value = '';
      companionBusy.value = false; chatBusy.value = false; accountOpen.value = false; pendingAction.value = null;
      hasNewChat.value = false; chatAtBottom.value = true;
      gameSessions.reset();
    }
    onSessionLost(() => { clearSession(); notify('会话已过期，请重新登录'); });
    async function logout() {
      const owner = session.value;
      if (play.value) await closeGame();
      if (session.value !== owner) return;
      try { await request('/v1/platform/auth/logout', 'POST', {}); }
      catch (e) { if (session.value === owner) notify(e.message); }
      if (session.value === owner) { clearSession(); notify('已退出登录'); }
    }
    async function favorite(game) {
      if (!requireLogin({ type: 'favorite', game })) return;
      const owner = session.value;
      try {
        await request(`/v1/platform/library/${encodeURIComponent(game.game_id)}`, 'PUT', { favorite: !favorites.value.has(game.game_id) });
        if (session.value === owner) await loadLibrary().catch(() => {});
      } catch (e) { if (session.value === owner) notify(e.message); }
    }
    async function onFrameMessage(event) {
      // The game has an opaque sandbox origin: it cannot read the platform DOM
      // or its credentials. Only this exact frame can use its bounded API bridge.
      if (!play.value || event.origin !== 'null' || event.source !== frame.value?.contentWindow || !gameSessions.current) return;
      const message = event.data;
      if (message?.type === 'miniverse:ready') {
        event.source.postMessage({ type: 'miniverse:launch', game_id: gameSessions.current.game_id, player_id: gameSessions.current.player_id, epoch: gameSessions.current.epoch, channel: gameSessions.current.channel }, '*');
        return;
      }
      if (message?.channel !== gameSessions.current.channel) return;
      if (message.type === 'miniverse:started') frameState.value = '游戏已连接 · 隔离游戏会话';
      else if (message.type === 'miniverse:error') frameState.value = '连接异常，请查看游戏内提示';
      else if (message.type === 'miniverse:exit') closeGame();
      else if (message.type === 'miniverse:request') {
        const currentSession = gameSessions.current;
        const key = `${message.method} ${message.path}`;
        let encodedBody; try { encodedBody = encodeBridgeBody(message.body); } catch {}
        if (!bridgeRules.has(key) || typeof message.id !== 'string' || message.id.length > 64 || encodedBody === undefined || currentSession.pending >= 8) {
          event.source.postMessage({ type: 'miniverse:response', channel: currentSession.channel, id: message.id, status: 403, data: { error: 'bridge_request_denied' } }, '*');
          return;
        }
        currentSession.pending++;
        try {
          const res = await fetch(`/v1/games/${encodeURIComponent(currentSession.game_id)}${message.path}`, {
            method: message.method,
            headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${currentSession.access_token}` },
            signal: AbortSignal.timeout(45000),
            body: message.method === 'GET' ? undefined : encodedBody,
          });
          const data = await res.json().catch(() => ({ error: 'invalid_response' }));
          if (gameSessions.current === currentSession && event.source === frame.value?.contentWindow) {
            event.source.postMessage({ type: 'miniverse:response', channel: currentSession.channel, id: message.id, status: res.status, data }, '*');
          }
        } catch {
          if (gameSessions.current === currentSession) {
            event.source.postMessage({ type: 'miniverse:response', channel: currentSession.channel, id: message.id, status: 503, data: { error: 'network_unavailable' } }, '*');
          }
        } finally { currentSession.pending--; }
      }
    }
    async function saveCompanion() {
      if (!session.value || !companionLoaded.value) return;
      const owner = session.value; companionBusy.value = true;
      try {
        const data = await request('/v1/platform/companion', 'PUT', { name: companion.value.name.trim(), style: companion.value.style });
        if (session.value === owner) { companion.value = data; if (data.chat_mode) companionMode.value = data.chat_mode; notify('伙伴设置已保存'); }
      } catch (e) { if (session.value === owner) notify(e.message); }
      finally { if (session.value === owner) companionBusy.value = false; }
    }
    function registerChatLog(el) { chatLog.value = el; }
    function onChatScroll() {
      const el = chatLog.value;
      if (!el) return;
      const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= 24;
      chatAtBottom.value = nearBottom;
      if (nearBottom) hasNewChat.value = false;
    }
    async function scrollChatBottom(force = false) {
      await nextTick();
      const el = chatLog.value;
      if (!el) return;
      if (force || chatAtBottom.value) {
        el.scrollTop = el.scrollHeight;
        chatAtBottom.value = true;
        hasNewChat.value = false;
      } else {
        hasNewChat.value = true;
      }
    }
    async function dispatchChat(text, history) {
      const owner = session.value;
      chatBusy.value = true;
      try {
        const result = await request('/v1/platform/companion/chat', 'POST', { message: text, history });
        if (session.value === owner) {
          companionMode.value = result.mode;
          chat.value.push({ role: 'assistant', text: result.reply });
          await scrollChatBottom(false);
        }
      } catch (e) {
        if (session.value === owner) {
          chat.value.push({ role: 'error', text: e.message, retryText: text });
          await scrollChatBottom(false);
        }
      } finally { if (session.value === owner) chatBusy.value = false; }
    }
    async function sendChat() {
      if (!requireLogin() || chatBusy.value || !chatText.value.trim()) return;
      const text = chatText.value.trim();
      const history = chat.value.filter(m => m.role === 'user' || m.role === 'assistant').slice(-6).map(m => ({ role: m.role, content: m.text.slice(0, 1600) }));
      chatText.value = '';
      chat.value.push({ role: 'user', text });
      await scrollChatBottom(true);
      await dispatchChat(text, history);
    }
    async function retryChat(index) {
      if (chatBusy.value) return;
      const target = chat.value[index];
      if (!target || target.role !== 'error' || !target.retryText) return;
      const prior = chat.value.slice(0, index);
      const lastUserIdx = prior.length - 1;
      const trimmed = prior[lastUserIdx]?.role === 'user' && prior[lastUserIdx]?.text === target.retryText ? prior.slice(0, lastUserIdx) : prior;
      const history = trimmed.filter(m => m.role === 'user' || m.role === 'assistant').slice(-6).map(m => ({ role: m.role, content: m.text.slice(0, 1600) }));
      chat.value.splice(index, 1);
      await scrollChatBottom(true);
      await dispatchChat(target.retryText, history);
    }
    async function addMemory() {
      if (!session.value || !memoryText.value.trim()) return;
      const owner = session.value, text = memoryText.value.trim();
      try {
        await request('/v1/platform/companion/memories', 'POST', { text });
        if (session.value !== owner) return;
        if (memoryText.value.trim() === text) memoryText.value = '';
        const data = await request('/v1/platform/companion/memories');
        if (session.value === owner) { memories.value = data.items || []; notify('这条记忆已保存'); }
      } catch (e) { if (session.value === owner) notify(e.message); }
    }
    async function deleteMemory(memory) {
      if (!session.value) return;
      const owner = session.value;
      try {
        await request(`/v1/platform/companion/memories/${encodeURIComponent(memory.id)}`, 'DELETE');
        if (session.value === owner) memories.value = memories.value.filter(m => m.id !== memory.id);
      } catch (e) { if (session.value === owner) notify(e.message); }
    }
    function formatDate(value) { return value ? new Date(value * 1000).toLocaleDateString('zh-CN', { month: 'long', day: 'numeric' }) : ''; }

    function manageDialogFocus(isOpen, containerRef) {
      let restoreEl = null;
      watch(isOpen, async open => {
        if (open) {
          restoreEl = document.activeElement;
          await nextTick();
          const el = containerRef.value;
          if (el) {
            const first = el.querySelector(FOCUSABLE);
            (first || el).focus();
          }
        } else if (restoreEl && typeof restoreEl.focus === 'function') {
          restoreEl.focus();
          restoreEl = null;
        }
      });
    }
    manageDialogFocus(authOpen, authDialog);
    manageDialogFocus(accountOpen, accountDialog);
    manageDialogFocus(computed(() => !!play.value), playDialog);

    function onKey(event) {
      if (event.key === 'Tab') {
        if (play.value && playDialog.value) trapDialogTab(playDialog.value, event);
        else if (authOpen.value && authDialog.value) trapDialogTab(authDialog.value, event);
        else if (accountOpen.value && accountDialog.value) trapDialogTab(accountDialog.value, event);
        return;
      }
      if (event.key === 'Escape' && !play.value) {
        if (authOpen.value) closeAuth();
        else if (accountOpen.value) accountOpen.value = false;
        else if (detail.value) go('discover');
      }
    }
    onMounted(() => {
      route();
      loadGames();
      restoreSession();
      window.addEventListener('hashchange', route);
      window.addEventListener('message', onFrameMessage);
      window.addEventListener('keydown', onKey);
    });
    onBeforeUnmount(() => {
      clearTimeout(toastTimer);
      clearTimeout(searchTimer);
      window.removeEventListener('hashchange', route);
      window.removeEventListener('message', onFrameMessage);
      window.removeEventListener('keydown', onKey);
    });
    return {
      NAV, current, session, view, games, displayed, featured, libraryGames, libraryLoading, libraryError, recent, favorites,
      search, category, categories, nextCursor, loadingMore, loading, error, detail, toast, authOpen, authMode, username, password,
      authBusy, authError, accountOpen, companion, companionLoaded, companionError, worldContextText, memories, memoryText, chatText,
      chat, chatBusy, companionBusy, companionMode, modeLabel, modeHint, hasNewChat, capabilities, devicesError, play, frame,
      frameState, launchBusy, theme, authDialog, accountDialog, playDialog,
      go, openDetail, loadGames, loadLibrary, loadCompanion, authenticate, closeAuth, logout, favorite, launch, closeGame, saveCompanion,
      sendChat, retryChat, registerChatLog, onChatScroll, scrollChatBottom, addMemory, deleteMemory, formatDate, requireLogin, loadDevices, companionLevelLabel,
    };
  },
  template: `
    <div class="app-shell">
      <aside class="sidebar">
        <a class="brand" href="#discover" aria-label="MiniVerse 首页"><span class="brand-mark">m<span>✦</span></span><span>MiniVerse<small>PLAY. CONNECT. EXPLORE.</small></span></a>
        <div class="nav-label">你的宇宙</div>
        <nav aria-label="主要导航">
          <button v-for="item in NAV" :key="item.id" :class="['nav-item', { active: view === item.id }]" :aria-current="view === item.id ? 'page' : null" @click="go(item.id)">
            <span class="nav-icon" aria-hidden="true">{{ item.icon }}</span>
            <span class="nav-text">{{ item.name }}</span>
            <span class="nav-text-short">{{ item.shortName }}</span>
            <span v-if="view === item.id" class="active-dot"></span>
          </button>
        </nav>
        <div class="sidebar-bottom">
          <div class="sidebar-note"><span>✦</span><p>好奇心没有边界。<br><small>带上伙伴，再出发一次。</small></p></div>
          <button class="theme-button" @click="theme = theme === 'dark' ? 'light' : 'dark'">
            {{ theme === 'dark' ? '☀ 浅色模式' : '◐ 深色模式' }}<span>切换外观</span>
          </button>
        </div>
      </aside>
      <div class="main-shell">
        <header class="topbar">
          <div class="breadcrumb">MINIVERSE <span>/</span> {{ detail ? '游戏详情' : current.name }}</div>
          <div class="top-actions">
            <span class="online-dot"></span><span class="top-label">一起探索更多</span>
            <button type="button" class="button secondary compact mobile-theme-btn" @click="theme = theme === 'dark' ? 'light' : 'dark'" :aria-label="theme === 'dark' ? '切换到浅色模式' : '切换到深色模式'">{{ theme === 'dark' ? '☀' : '◐' }}</button>
            <button v-if="!session" class="button primary compact" @click="requireLogin()">登录 / 注册 <span>↗</span></button>
            <button v-else class="account-button" @click="accountOpen = true"><span class="avatar">{{ session.username?.slice(0, 1).toUpperCase() }}</span><span>{{ session.username }}</span><span>⌄</span></button>
          </div>
        </header>
        <main id="main">
          <template v-if="detail">
            <button class="text-button back-link" @click="go('discover')">← 返回发现</button>
            <section class="detail-hero">
              <GameArt :game="detail" large />
              <div class="detail-heading">
                <div class="eyebrow">{{ detail.category }} · 网页畅玩</div>
                <h1>{{ detail.name }}</h1>
                <p>{{ detail.summary }}</p>
                <div class="detail-actions">
                  <button class="button primary" :disabled="launchBusy" @click="launch(detail)">{{ launchBusy ? '正在准备…' : '进入游戏' }} <span>↗</span></button>
                  <button class="button secondary" @click="favorite(detail)">{{ favorites.has(detail.game_id) ? '♥ 已收藏' : '♡ 收藏游戏' }}</button>
                </div>
              </div>
            </section>
            <div class="detail-columns">
              <section class="panel">
                <div class="eyebrow">ABOUT THIS WORLD</div>
                <h2>关于这个世界</h2>
                <p class="description">{{ detail.description || detail.summary }}</p>
                <div class="tag-row"><span class="tag">{{ detail.category }}</span><span class="tag">版本 {{ detail.version }}</span><span class="tag">云存档</span></div>
              </section>
              <section class="panel companion-support">
                <span class="tiny-orb">✦</span>
                <h3>让伙伴一起冒险</h3>
                <p>支持伙伴上下文与分级提示。游戏进度独立保存，伙伴延续共同经历。</p>
                <span class="tag">{{ companionLevelLabel(detail.companion_level) }}</span>
              </section>
            </div>
          </template>
          <DiscoverView
            v-else-if="view === 'discover'"
            :session="session"
            :recent="recent"
            :error="error"
            :loading="loading"
            :featured="featured"
            :games="games"
            :displayed="displayed"
            :categories="categories"
            :category="category"
            :search="search"
            :favorites="favorites"
            :launch-busy="launchBusy"
            :next-cursor="nextCursor"
            :loading-more="loadingMore"
            :companion-level-label="companionLevelLabel"
            :format-date="formatDate"
            @open-detail="openDetail"
            @go="go"
            @launch="launch"
            @load-games="loadGames"
            @update:search="search = $event"
            @update:category="category = $event"
            @favorite="favorite"
          />
          <LibraryView
            v-else-if="view === 'library'"
            :session="session"
            :library-loading="libraryLoading"
            :library-games="libraryGames"
            :library-error="libraryError"
            :launch-busy="launchBusy"
            :format-date="formatDate"
            @require-login="requireLogin()"
            @load-library="loadLibrary().catch(() => {})"
            @go="go"
            @open-detail="openDetail"
            @favorite="favorite"
            @launch="launch"
          />
          <CompanionView
            v-else-if="view === 'companion'"
            :session="session"
            :companion="companion"
            :companion-busy="companionBusy"
            :companion-loaded="companionLoaded"
            :companion-error="companionError"
            :companion-mode="companionMode"
            :mode-label="modeLabel"
            :mode-hint="modeHint"
            :world-context-text="worldContextText"
            :chat="chat"
            :chat-text="chatText"
            :chat-busy="chatBusy"
            :has-new-chat="hasNewChat"
            :memories="memories"
            :memory-text="memoryText"
            @require-login="requireLogin()"
            @load-companion="loadCompanion"
            @save-companion="saveCompanion"
            @send-chat="sendChat"
            @retry-chat="retryChat"
            @register-chat-log="registerChatLog"
            @chat-scroll="onChatScroll"
            @scroll-bottom="scrollChatBottom"
            @add-memory="addMemory"
            @delete-memory="deleteMemory"
            @update:chat-text="chatText = $event"
            @update:memory-text="memoryText = $event"
          />
          <DevicesView
            v-else-if="view === 'devices'"
            :session="session"
            :devices-error="devicesError"
            :capabilities="capabilities"
            @require-login="requireLogin()"
            @load-devices="loadDevices"
          />
          <footer class="footer"><span class="footer-brand">m<span>✦</span> MiniVerse</span><span>每一个世界，都值得好奇。</span><span>玩家平台 · 软件预览版</span></footer>
        </main>
      </div>
      <div v-if="toast" class="toast" role="status">{{ toast }}</div>
      <div v-if="authOpen" class="overlay" @click.self="closeAuth">
        <section ref="authDialog" tabindex="-1" class="dialog auth-dialog" role="dialog" aria-modal="true" aria-labelledby="auth-title">
          <button class="close-button" @click="closeAuth" aria-label="关闭">×</button>
          <span class="dialog-brand">m✦</span>
          <div class="eyebrow">WELCOME TO YOUR NEXT WORLD</div>
          <h2 id="auth-title">{{ authMode === 'login' ? '欢迎回来，探索家。' : '新的冒险，从这里开始。' }}</h2>
          <p>一个账号，带上伙伴，探索不同世界。</p>
          <form @submit.prevent="authenticate">
            <label>账号<input v-model="username" autocomplete="username" required minlength="3" maxlength="32" placeholder="3–32 位字母、数字或下划线" /></label>
            <label>密码<input v-model="password" type="password" :autocomplete="authMode === 'register' ? 'new-password' : 'current-password'" required :minlength="authMode === 'register' ? 8 : 1" maxlength="128" placeholder="至少 8 位密码" /></label>
            <p v-if="authError" class="error-text" role="alert">{{ authError }}</p>
            <button class="button primary wide" :disabled="authBusy">{{ authBusy ? '正在连接…' : authMode === 'login' ? '登录，继续冒险' : '注册账号' }} ↗</button>
          </form>
          <div class="auth-switch">{{ authMode === 'login' ? '第一次来到这里？' : '已经有账号了？' }}<button class="text-button" @click="authMode = authMode === 'login' ? 'register' : 'login'; authError = ''">{{ authMode === 'login' ? '创建账号' : '立即登录' }}</button></div>
          <small class="auth-note">会话通过同源 HttpOnly Cookie 保护（24 小时有效），隔离游戏内页无法读取你的平台凭证。</small>
        </section>
      </div>
      <div v-if="accountOpen && session" class="overlay" @click.self="accountOpen = false">
        <section ref="accountDialog" tabindex="-1" class="dialog account-dialog" role="dialog" aria-modal="true" aria-label="账号信息">
          <button class="close-button" @click="accountOpen = false" aria-label="关闭">×</button>
          <span class="avatar large-avatar">{{ session.username?.slice(0, 1).toUpperCase() }}</span>
          <h2>{{ session.username }}</h2>
          <p>你的游戏和伙伴，与账号相伴。</p>
          <label>玩家 ID<code class="player-id">{{ session.player_id }}</code></label>
          <button class="button secondary wide" @click="logout">退出登录</button>
        </section>
      </div>
      <section v-if="play" ref="playDialog" tabindex="-1" class="play-shell" role="dialog" aria-modal="true" :aria-label="play.name">
        <header class="play-header"><div><strong>{{ play.name }}</strong><small>{{ frameState }}</small></div><button class="button secondary compact" @click="closeGame">返回平台 ↗</button></header>
        <iframe ref="frame" :src="play.src" :title="play.name" sandbox="allow-scripts" referrerpolicy="no-referrer"></iframe>
      </section>
    </div>
  `,
});
app.mount('#app');
