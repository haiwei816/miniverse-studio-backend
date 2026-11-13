import "./styles/fonts.css";
import "./styles/theme.css";
import { createApp, ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from "vue";
import { request, setToken, onSessionLost } from "./api.js";
import { notify, ToastHost, Drawer, Segmented, Empty, SkeletonRows } from "./components/ui.js";
import { Icon } from "./components/icons.js";
import { Sparkline, TrendChart } from "./components/charts.js";
import { errorText } from "./format.js";
import { OverviewView } from "./views/overview.js";
import { PlayersView } from "./views/players.js";
import { OperationsView } from "./views/operations.js";
import { AuditView } from "./views/audit.js";
import { SystemView } from "./views/system.js";
import { CommandPalette } from "./views/palette.js";
import { isDesktop, onTrayAction, notifyNative } from "./desktop.js";

const NAV = [
  { id: "overview", label: "概览", icon: "overview", title: "概览", sub: "玩家、审批与运行状态的一屏摘要" },
  { id: "players", label: "玩家", icon: "players", title: "玩家与资产", sub: "搜索玩家，查看存档、库存、邮件与流水" },
  { id: "operations", label: "运营", icon: "operations", title: "运营与审批", sub: "补发、活动配置与公告，双人审批后执行" },
  { id: "audit", label: "审计", icon: "audit", title: "操作审计", sub: "所有管理写操作与异常记录" },
  { id: "system", label: "系统", icon: "system", title: "系统状态", sub: "服务健康、失败操作与接入信息" },
];

const THEME_KEY = "miniverse.theme";

function resolveTheme(mode) {
  if (mode === "dark") return "dark";
  if (mode === "light") return "light";
  return matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

const app = createApp({
  components: { ToastHost, CommandPalette },
  setup() {
    const token = ref("");
    const role = ref("");
    const username = ref("");
    const password = ref("");
    const games = ref([]);
    const game = ref("");
    const view = ref("overview");
    const tick = ref(0);
    const busy = ref(false);
    const menuOpen = ref(false);
    const paletteOpen = ref(false);
    const playerQuery = ref("");
    const themeMode = ref(localStorage.getItem(THEME_KEY) || "auto");

    const desktop = isDesktop();
    const current = computed(() => NAV.find((n) => n.id === view.value) || NAV[0]);
    const gameName = computed(() => {
      const found = games.value.find((g) => g.game_id === game.value);
      return found ? found.name || found.game_id : game.value;
    });

    function applyTheme() {
      document.documentElement.dataset.theme = resolveTheme(themeMode.value);
    }
    watch(themeMode, (mode) => {
      localStorage.setItem(THEME_KEY, mode);
      applyTheme();
    });
    const media = matchMedia("(prefers-color-scheme: dark)");
    const onMedia = () => themeMode.value === "auto" && applyTheme();
    media.addEventListener("change", onMedia);
    applyTheme();

    function cycleTheme() {
      themeMode.value = themeMode.value === "auto" ? "light" : themeMode.value === "light" ? "dark" : "auto";
    }
    const themeIcon = computed(() => (themeMode.value === "dark" ? "moon" : "sun"));

    function resetSession() {
      token.value = "";
      setToken("");
      role.value = "";
      games.value = [];
      game.value = "";
      view.value = "overview";
      password.value = "";
    }
    onSessionLost(() => {
      resetSession();
      notify("登录已过期，请重新登录", "error");
    });

    async function login() {
      if (!username.value || !password.value) {
        notify("请输入账号和密码", "error");
        return;
      }
      busy.value = true;
      try {
        const session = await request("/gm/login", { method: "POST", body: { username: username.value, password: password.value } });
        setToken(session.access_token);
        token.value = session.access_token;
        role.value = session.role;
        password.value = "";
        const list = await request("/gm/games");
        games.value = Array.isArray(list) ? list : [];
        if (!games.value.some((g) => g.game_id === game.value)) game.value = games.value.length ? games.value[0].game_id : "";
        busy.value = false;
        tick.value++;
      } catch (e) {
        busy.value = false;
        notify(errorText(e.code), "error");
      }
    }

    function logout() {
      resetSession();
      notify("已退出登录", "success");
    }

    function go(id) {
      view.value = id;
      menuOpen.value = false;
      paletteOpen.value = false;
    }

    function refresh() {
      tick.value++;
    }

    function openPlayer(uid) {
      playerQuery.value = uid || "";
      view.value = "players";
      menuOpen.value = false;
      paletteOpen.value = false;
      tick.value++;
    }

    function onKeydown(event) {
      const meta = event.metaKey || event.ctrlKey;
      if (meta && event.key.toLowerCase() === "k") {
        event.preventDefault();
        paletteOpen.value = true;
        return;
      }
      if (meta && event.key.toLowerCase() === "r" && token.value) {
        event.preventDefault();
        refresh();
        return;
      }
      if (event.key === "Escape") {
        menuOpen.value = false;
      }
    }
    document.addEventListener("keydown", onKeydown);
    onBeforeUnmount(() => document.removeEventListener("keydown", onKeydown));

    watch(game, () => {
      if (token.value) tick.value++;
    });

    // Desktop shell: tray menu items arrive as events, and a slow poll turns
    // new pending work into a native notification instead of a silent badge.
    let poll = null;
    if (desktop) {
      document.documentElement.dataset.shell = "desktop";
      onTrayAction((action) => {
        if (action === "award") {
          view.value = "operations";
          paletteOpen.value = true;
        }
        if (action === "refresh") refresh();
        if (action === "overview") go("overview");
      });
      onMounted(() => {
        let last = null;
        poll = setInterval(async () => {
          if (!token.value || !game.value || document.hidden) return;
          try {
            const data = await request(`/gm/games/${game.value}/overview`);
            const pending = data.totals.requests_pending;
            if (last !== null && pending > last) {
              notifyNative("有新的待审申请", `${gameName.value} · 当前 ${pending} 条待处理`);
            }
            last = pending;
          } catch {
            /* polling is best effort */
          }
        }, 60000);
      });
      onBeforeUnmount(() => poll && clearInterval(poll));
    }

    onMounted(async () => {
      // Desktop shell has no server-side session cookie: the token lives in
      // memory only, so a restart means signing in again (30-minute sessions).
      if (desktop) document.documentElement.dataset.shell = "desktop";
    });

    return {
      token, role, username, password, busy, games, game, view, tick, menuOpen, paletteOpen,
      playerQuery, themeMode, themeIcon, desktop, current, NAV, gameName,
      login, logout, go, refresh, openPlayer, cycleTheme,
    };
  },
  template: `
  <div v-if="!token" class="login-shell">
    <div class="login">
      <div class="dots" aria-hidden="true"><i></i><i></i><i></i></div>
      <div class="eyebrow">MINIVERSE STUDIO</div>
      <h1>运营控制台</h1>
      <p class="page-sub">使用已分配角色与游戏权限的管理账号登录。</p>
      <form @submit.prevent="login">
        <label class="field">
          <span class="field-label">管理员账号</span>
          <input class="input" v-model="username" autocomplete="username" placeholder="admin" />
        </label>
        <label class="field">
          <span class="field-label">密码</span>
          <input class="input" v-model="password" type="password" autocomplete="current-password" placeholder="••••••••" />
        </label>
        <button class="btn btn-primary btn-block" type="submit" :disabled="busy">{{ busy ? '登录中…' : '登录' }}</button>
      </form>
      <div class="login-hint">Sandbox 环境可用 admin / reviewer / support / operator，密码为 sandbox-&lt;账号&gt;。会话 30 分钟过期。</div>
    </div>
    <toast-host />
  </div>

  <div v-else class="app">
    <aside class="sidebar">
      <div class="dots" aria-hidden="true"><i></i><i></i><i></i></div>
      <button class="game-switch" @click="menuOpen = !menuOpen" :aria-expanded="String(menuOpen)">
        <span class="game-badge">{{ (gameName || '?').slice(0,1) }}</span>
        <span class="game-switch-text">
          <span class="game-switch-name">{{ gameName || '选择游戏' }}</span>
          <span class="game-switch-id">{{ game || 'no game' }}</span>
        </span>
        <icon name="chevron" :size="14" />
      </button>
      <div v-if="menuOpen" class="card" style="position:absolute;z-index:20;margin-top:56px;width:208px;padding:4px">
        <button v-for="g in games" :key="g.game_id" class="nav-item" style="width:100%" @click="game = g.game_id; menuOpen = false">
          <span class="game-badge" style="width:20px;height:20px;font-size:12px">{{ (g.name || g.game_id).slice(0,1) }}</span>
          {{ g.name || g.game_id }}
        </button>
      </div>
      <nav class="nav" aria-label="主导航">
        <div class="nav-label">工作区</div>
        <button v-for="item in NAV" :key="item.id" class="nav-item" :aria-current="view === item.id ? 'page' : null" @click="go(item.id)">
          <icon :name="item.icon" :size="15" />{{ item.label }}
        </button>
      </nav>
      <div class="sidebar-foot">
        <div class="identity">
          <span class="avatar">{{ (username || role || 'G').slice(0,1) }}</span>
          <span class="identity-text">
            <span class="identity-name">{{ username || 'GM' }}</span>
            <span class="identity-role">{{ role }}</span>
          </span>
        </div>
        <div class="sidebar-links">
          <button class="btn btn-quiet btn-sm" @click="cycleTheme" :title="'主题：' + themeMode"><icon :name="themeIcon" :size="14" /><span>{{ themeMode === 'auto' ? '跟随系统' : themeMode === 'dark' ? '深色' : '浅色' }}</span></button>
          <button class="btn btn-quiet btn-sm" @click="logout" title="退出登录"><icon name="logout" :size="14" /><span>退出</span></button>
        </div>
      </div>
    </aside>

    <main class="main">
      <div class="wrap">
        <header class="topbar">
          <div>
            <div class="eyebrow">{{ desktop ? 'DESKTOP' : 'WEB' }} · {{ game || 'sandbox' }}</div>
            <h1 class="page-title">{{ current.title }}</h1>
            <p class="page-sub">{{ current.sub }}</p>
          </div>
          <div class="topbar-actions">
            <button class="btn btn-ghost" @click="paletteOpen = true" title="命令面板 ⌘K"><icon name="search" :size="14" />搜索<kbd class="kbd">⌘K</kbd></button>
            <button class="btn btn-ghost" @click="refresh" title="刷新"><icon name="refresh" :size="14" /></button>
          </div>
        </header>

        <overview-view v-if="view === 'overview'" :game="game" :role="role" :tick="tick" @navigate="go" />
        <players-view v-else-if="view === 'players'" :game="game" :role="role" :tick="tick" :initial-query="playerQuery" />
        <operations-view v-else-if="view === 'operations'" :game="game" :role="role" :tick="tick" />
        <audit-view v-else-if="view === 'audit'" :game="game" :role="role" :tick="tick" />
        <system-view v-else :game="game" :role="role" :tick="tick" />
      </div>
    </main>

    <command-palette :open="paletteOpen" :games="games" :game="game" @close="paletteOpen = false" @navigate="go" @player="openPlayer" />
    <toast-host />
  </div>`,
});

// Shared design primitives are global so templates in any view can use them
// without re-importing; view-local registrations stay for readability.
app.component("icon", Icon);
app.component("toast-host", ToastHost);
app.component("drawer", Drawer);
app.component("segmented", Segmented);
app.component("empty", Empty);
app.component("skeleton-rows", SkeletonRows);
app.component("sparkline", Sparkline);
app.component("trend-chart", TrendChart);
app.component("overview-view", OverviewView);
app.component("players-view", PlayersView);
app.component("operations-view", OperationsView);
app.component("audit-view", AuditView);
app.component("system-view", SystemView);
app.mount("#app");
