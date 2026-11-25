import "./styles/fonts.css";
import "./styles/theme.css";
import { createApp, ref, computed, onMounted, onBeforeUnmount, watch } from "vue";
import { request, setToken, setSessionActive, onSessionLost } from "./api.js";
import { notify, ToastHost, Drawer, ModalDialog, Segmented, Empty, ErrorBox, SkeletonRows } from "./components/ui.js";
import { Icon } from "./components/icons.js";
import { Sparkline, TrendChart } from "./components/charts.js";
import { errorText } from "./format.js";
import { OverviewView } from "./views/overview.js";
import { PlayersView } from "./views/players.js";
import { OperationsView } from "./views/operations.js";
import { AuditView } from "./views/audit.js";
import { OpsAgentView } from "./views/ops_agent.js";
import { SystemView } from "./views/system.js";
import { PublishingView } from "./views/publishing.js";
import { CommandPalette } from "./views/palette.js";
import { isDesktop, onTrayAction, notifyNative } from "./desktop.js";

const NAV = [
  { id: "publishing", label: "发行", icon: "operations", title: "游戏发行", sub: "编辑资料、提交审核，将游戏发布到玩家平台" },
  { id: "overview", label: "概览", icon: "overview", title: "概览", sub: "玩家、审批与运行状态的一屏摘要" },
  { id: "players", label: "玩家", icon: "players", title: "玩家与资产", sub: "搜索玩家，查看存档、库存、邮件与流水" },
  { id: "operations", label: "运营", icon: "operations", title: "运营与审批", sub: "补发、活动配置与公告，双人审批后执行" },
  { id: "audit", label: "审计", icon: "audit", title: "操作审计", sub: "所有管理写操作与异常记录" },
  { id: "ops-agent", label: "智能排障", icon: "spark", title: "智能排障与 MCP 运维", sub: "通过 MCP 关联玩家数据、业务日志与服务状态，输出受控提案" },
  { id: "system", label: "系统", icon: "system", title: "系统状态", sub: "服务健康、稳定性指标、异常日志与接入信息" },
];

const THEME_KEY = "miniverse.theme";

function resolveTheme(mode) {
  if (mode === "dark") return "dark";
  if (mode === "light") return "light";
  return matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

const app = createApp({
  components: { ToastHost, CommandPalette, ModalDialog },
  setup() {
    const token = ref("");
    const signedIn = ref(false);
    const role = ref("");
    const signedInUser = ref("");
    const username = ref("");
    const password = ref("");
    const games = ref([]);
    const game = ref("");
    const view = ref("overview");
    const tick = ref(0);
    const busy = ref(false);
    const menuOpen = ref(false);
    const mobileNavOpen = ref(false);
    const paletteOpen = ref(false);
    const playerQuery = ref("");
    const playerQuerySeq = ref(0);
    const awardTarget = ref(null);
    const opsAgentTarget = ref(null);
    const publishingDirty = ref(false);
    const pendingLeave = ref(null);
    const themeMode = ref(localStorage.getItem(THEME_KEY) || "auto");
    let awardSeq = 0;
    let diagnoseSeq = 0;

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
      signedIn.value = false;
      setToken("");
      setSessionActive(false);
      role.value = "";
      signedInUser.value = "";
      games.value = [];
      game.value = "";
      view.value = "overview";
      password.value = "";
      menuOpen.value = false;
      mobileNavOpen.value = false;
      paletteOpen.value = false;
      publishingDirty.value = false;
      pendingLeave.value = null;
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
        const session = await request("/gm/login", { method: "POST", body: { username: username.value, password: password.value }, skipUnauthorized: true });
        if (desktop) {
          setToken(session.access_token);
          token.value = session.access_token;
        } else {
          setToken("");
          token.value = "";
        }
        setSessionActive(true);
        const list = await request("/gm/games");
        const parsedGames = Array.isArray(list) ? list : [];
        games.value = parsedGames;
        if (!parsedGames.some((g) => g.game_id === game.value)) {
          game.value = parsedGames.length ? parsedGames[0].game_id : "";
        }
        signedInUser.value = session.username || username.value.trim();
        role.value = session.role;
        password.value = "";
        signedIn.value = true;
        tick.value++;
      } catch (e) {
        resetSession();
        notify(errorText(e.code), "error");
      } finally {
        busy.value = false;
      }
    }

    function guardLeave(action) {
      menuOpen.value = false;
      mobileNavOpen.value = false;
      paletteOpen.value = false;
      if (view.value === "publishing" && publishingDirty.value) {
        if (action.type === "view" && action.value === "publishing") return;
        if (action.type === "game" && action.value === game.value) return;
        pendingLeave.value = action;
        return;
      }
      applyLeaveAction(action);
    }

    function applyLeaveAction(action) {
      if (!action) return;
      if (action.type === "view") {
        view.value = action.value;
      } else if (action.type === "game") {
        game.value = action.value;
      } else if (action.type === "player") {
        playerQuery.value = action.value || "";
        playerQuerySeq.value++;
        view.value = "players";
      } else if (action.type === "award") {
        awardTarget.value = { uid: action.value || "", seq: ++awardSeq };
        view.value = "operations";
      } else if (action.type === "logout") {
        request("/gm/logout", { method: "POST", skipUnauthorized: true })
          .catch(() => {})
          .finally(() => {
            resetSession();
            notify("已退出登录", "success");
          });
      }
    }

    function confirmLeave() {
      const action = pendingLeave.value;
      pendingLeave.value = null;
      publishingDirty.value = false;
      applyLeaveAction(action);
    }

    function logout() {
      guardLeave({ type: "logout" });
    }

    function go(id) {
      guardLeave({ type: "view", value: id });
    }

    function selectGame(id) {
      guardLeave({ type: "game", value: id });
    }

    function refresh() {
      tick.value++;
    }

    function openPlayer(uid) {
      guardLeave({ type: "player", value: uid || "" });
    }

    function openAward(uid) {
      guardLeave({ type: "award", value: uid || "" });
    }

    function openDiagnose(payload) {
      opsAgentTarget.value = { ...(payload || {}), seq: ++diagnoseSeq };
      guardLeave({ type: "view", value: "ops-agent" });
    }

    function onKeydown(event) {
      const meta = event.metaKey || event.ctrlKey;
      if (meta && event.key.toLowerCase() === "k" && signedIn.value) {
        event.preventDefault();
        paletteOpen.value = true;
        return;
      }
      if (meta && event.key.toLowerCase() === "r" && signedIn.value) {
        event.preventDefault();
        refresh();
        return;
      }
      if (event.key === "Escape") {
        menuOpen.value = false;
        mobileNavOpen.value = false;
      }
    }
    document.addEventListener("keydown", onKeydown);
    onBeforeUnmount(() => document.removeEventListener("keydown", onKeydown));

    watch(game, () => {
      if (signedIn.value && game.value) tick.value++;
    });

    // Desktop shell: tray menu items arrive as events, and a slow poll turns
    // new pending work into a native notification instead of a silent badge.
    let poll = null;
    if (desktop) {
      document.documentElement.dataset.shell = "desktop";
      onTrayAction((action) => {
        if (action === "award") {
          go("operations");
          paletteOpen.value = true;
        }
        if (action === "refresh") refresh();
        if (action === "overview") go("overview");
      });
      onMounted(() => {
        let last = null;
        poll = setInterval(async () => {
          if (!signedIn.value || !game.value || document.hidden) return;
          try {
            const data = await request(`/gm/games/${encodeURIComponent(game.value)}/overview`);
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
      if (desktop) {
        document.documentElement.dataset.shell = "desktop";
        return;
      }
      try {
        const me = await request("/gm/me", { skipUnauthorized: true });
        if (me && me.username) {
          const parsedGames = Array.isArray(me.games) ? me.games : [];
          games.value = parsedGames;
          if (!parsedGames.some((g) => g.game_id === game.value)) {
            game.value = parsedGames.length ? parsedGames[0].game_id : "";
          }
          signedInUser.value = me.username;
          role.value = me.role || "";
          setSessionActive(true);
          signedIn.value = true;
          tick.value++;
        }
      } catch {
        /* no active cookie session yet */
      }
    });

    return {
      token, signedIn, role, signedInUser, username, password, busy, games, game, view, tick, menuOpen, mobileNavOpen, paletteOpen,
      playerQuery, playerQuerySeq, awardTarget, opsAgentTarget, publishingDirty, pendingLeave, themeMode, themeIcon, desktop, current, NAV, gameName,
      login, logout, go, selectGame, refresh, openPlayer, openAward, openDiagnose, confirmLeave, cycleTheme,
    };
  },
  template: `
  <div v-if="!signedIn" class="login-shell">
    <div class="login">
      <div class="dots" aria-hidden="true"><i></i><i></i><i></i></div>
      <div class="eyebrow">MINIVERSE STUDIO</div>
      <h1>开发者控制台</h1>
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
      <div class="sidebar-top-row">
        <button class="game-switch" @click="menuOpen = !menuOpen; mobileNavOpen = false" :aria-expanded="String(menuOpen)">
          <span class="game-badge">{{ (gameName || '?').slice(0,1) }}</span>
          <span class="game-switch-text">
            <span class="game-switch-name">{{ gameName || '选择游戏' }}</span>
            <span class="game-switch-id">{{ game || 'no game' }}</span>
          </span>
          <icon name="chevron" :size="14" />
        </button>
        <button
          type="button"
          class="btn btn-ghost mobile-menu-btn"
          :aria-expanded="String(mobileNavOpen)"
          aria-controls="mobile-nav-panel"
          @click="mobileNavOpen = !mobileNavOpen; menuOpen = false"
        >
          <icon name="menu" :size="15" />
          <span>{{ current.label }}</span>
          <icon name="chevron" :size="13" />
        </button>
      </div>

      <div v-if="menuOpen" class="card game-menu-pop">
        <button v-for="g in games" :key="g.game_id" class="nav-item" style="width:100%" :aria-current="game === g.game_id ? 'true' : null" @click="selectGame(g.game_id)">
          <span class="game-badge" style="width:20px;height:20px;font-size:12px">{{ (g.name || g.game_id).slice(0,1) }}</span>
          <span>{{ g.name || g.game_id }}</span>
        </button>
        <div v-if="!games.length" class="small muted" style="padding:8px">没有可用游戏</div>
      </div>

      <div id="mobile-nav-panel" :class="['sidebar-drawer', { open: mobileNavOpen }]">
        <nav class="nav" aria-label="主导航">
          <div class="nav-label">工作区</div>
          <button v-for="item in NAV" :key="item.id" class="nav-item" :aria-current="view === item.id ? 'page' : null" @click="go(item.id)">
            <icon :name="item.icon" :size="15" />{{ item.label }}
          </button>
        </nav>
        <div class="sidebar-foot">
          <div class="identity">
            <span class="avatar">{{ (signedInUser || role || 'G').slice(0,1) }}</span>
            <span class="identity-text">
              <span class="identity-name">{{ signedInUser || 'GM' }}</span>
              <span class="identity-role">{{ role }}</span>
            </span>
          </div>
          <div class="sidebar-links">
            <button class="btn btn-quiet btn-sm" @click="cycleTheme" :title="'主题：' + themeMode"><icon :name="themeIcon" :size="14" /><span>{{ themeMode === 'auto' ? '跟随系统' : themeMode === 'dark' ? '深色' : '浅色' }}</span></button>
            <button class="btn btn-quiet btn-sm" @click="logout" title="退出登录"><icon name="logout" :size="14" /><span>退出</span></button>
          </div>
        </div>
      </div>
    </aside>

    <main class="main">
      <div class="wrap">
        <header class="topbar">
          <div>
            <div class="eyebrow">{{ desktop ? 'DESKTOP' : 'WEB' }} · {{ gameName || game || '未分配游戏' }}</div>
            <h1 class="page-title">{{ current.title }}</h1>
            <p class="page-sub">{{ current.sub }}</p>
          </div>
          <div class="topbar-actions">
            <button class="btn btn-ghost" @click="paletteOpen = true" title="命令面板 ⌘K"><icon name="search" :size="14" />搜索<kbd class="kbd">⌘K</kbd></button>
            <button class="btn btn-ghost" @click="refresh" title="刷新" aria-label="刷新"><icon name="refresh" :size="14" /></button>
          </div>
        </header>

        <article v-if="!game" class="card">
          <div class="card-body">
            <empty text="当前账号还没有可管理的游戏权限" />
          </div>
        </article>
        <publishing-view v-else-if="view === 'publishing'" :game="game" :game-name="gameName" :role="role" :username="signedInUser" :tick="tick" @dirty-change="publishingDirty = $event" />
        <overview-view v-else-if="view === 'overview'" :game="game" :role="role" :tick="tick" @navigate="go" />
        <players-view v-else-if="view === 'players'" :game="game" :game-name="gameName" :role="role" :tick="tick" :initial-query="playerQuery" :initial-query-seq="playerQuerySeq" @award="openAward" />
        <operations-view v-else-if="view === 'operations'" :game="game" :game-name="gameName" :role="role" :username="signedInUser" :tick="tick" :award-target="awardTarget" />
        <audit-view v-else-if="view === 'audit'" :game="game" :game-name="gameName" :role="role" :tick="tick" />
        <ops-agent-view v-else-if="view === 'ops-agent'" :game="game" :game-name="gameName" :role="role" :tick="tick" :initial-target="opsAgentTarget" @navigate="go" />
        <system-view v-else :game="game" :game-name="gameName" :role="role" :tick="tick" @diagnose="openDiagnose" />
      </div>
    </main>

    <command-palette :open="paletteOpen" :games="games" :game="game" @close="paletteOpen = false" @navigate="go" @player="openPlayer" @award="openAward" />
    <modal-dialog
      :open="!!pendingLeave"
      title="发行资料尚未保存"
      :subtitle="gameName || game"
      @close="pendingLeave = null"
    >
      <div class="stack">
        <p class="small">当前游戏「{{ gameName || game }}」的发行草稿有未保存的更改，系统已按游戏为您本地暂存，返回该游戏发行页时可继续编辑。</p>
        <div class="toolbar" style="justify-content:flex-end">
          <button type="button" class="btn btn-ghost" @click="pendingLeave = null">留在本页保存</button>
          <button type="button" class="btn btn-primary" @click="confirmLeave">暂存并离开</button>
        </div>
      </div>
    </modal-dialog>
    <toast-host />
  </div>`,
});

// Shared design primitives are global so templates in any view can use them
// without re-importing; view-local registrations stay for readability.
app.component("icon", Icon);
app.component("toast-host", ToastHost);
app.component("drawer", Drawer);
app.component("modal-dialog", ModalDialog);
app.component("segmented", Segmented);
app.component("empty", Empty);
app.component("error-box", ErrorBox);
app.component("skeleton-rows", SkeletonRows);
app.component("sparkline", Sparkline);
app.component("trend-chart", TrendChart);
app.component("overview-view", OverviewView);
app.component("players-view", PlayersView);
app.component("operations-view", OperationsView);
app.component("audit-view", AuditView);
app.component("ops-agent-view", OpsAgentView);
app.component("system-view", SystemView);
app.component("publishing-view", PublishingView);
app.mount("#app");
