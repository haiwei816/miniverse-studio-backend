import { defineComponent, ref, watch, computed, onBeforeUnmount } from "vue";
import { request } from "../api.js";
import { errorText, count, stamp, relative } from "../format.js";
import { Icon } from "../components/icons.js";
import { Drawer, notify, Empty, ErrorBox, SkeletonRows } from "../components/ui.js";

const TABS = [
  { id: "inventory", label: "背包" },
  { id: "ledger", label: "流水" },
  { id: "mail", label: "邮件" },
  { id: "saves", label: "存档" },
  { id: "grants", label: "补发" },
  { id: "raw", label: "原始" },
];

export const PlayersView = defineComponent({
  name: "PlayersView",
  components: { Icon, Drawer, Empty, ErrorBox, SkeletonRows },
  props: { game: String, gameName: String, role: String, tick: Number, initialQuery: String, initialQuerySeq: Number },
  emits: ["award"],
  setup(props, { emit }) {
    const query = ref(props.initialQuery || "");
    const rows = ref([]);
    const loading = ref(false);
    const error = ref("");
    const detail = ref(null);
    const detailLoading = ref(false);
    const detailError = ref("");
    const detailRow = ref(null);
    const tab = ref("inventory");
    const drawerOpen = ref(false);
    const banReason = ref("");
    const banHours = ref("");
    const banBusy = ref(false);
    let listGen = 0;
    let detailGen = 0;
    let listAbort = null;
    let detailAbort = null;

    function abortAll() {
      if (listAbort) {
        listAbort.abort();
        listAbort = null;
      }
      if (detailAbort) {
        detailAbort.abort();
        detailAbort = null;
      }
    }

    async function load() {
      const targetGame = props.game;
      const run = ++listGen;
      if (listAbort) listAbort.abort();
      if (!targetGame) {
        rows.value = [];
        error.value = "";
        loading.value = false;
        return;
      }
      listAbort = new AbortController();
      loading.value = true;
      error.value = "";
      try {
        const result = await request(`/gm/games/${encodeURIComponent(targetGame)}/players?q=${encodeURIComponent(query.value.trim())}`, {
          signal: listAbort.signal,
        });
        if (run !== listGen || targetGame !== props.game) return;
        rows.value = Array.isArray(result) ? result : [];
      } catch (e) {
        if (e && e.name === "AbortError") return;
        if (run !== listGen || targetGame !== props.game) return;
        error.value = errorText(e.code);
        rows.value = [];
        notify(error.value, "error");
      } finally {
        if (run === listGen) loading.value = false;
      }
    }

    async function inspect(row) {
      const targetGame = props.game;
      if (!targetGame || !row) return;
      const run = ++detailGen;
      if (detailAbort) detailAbort.abort();
      detailAbort = new AbortController();
      detailRow.value = row;
      drawerOpen.value = true;
      detailLoading.value = true;
      detailError.value = "";
      detail.value = null;
      tab.value = "inventory";
      try {
        const result = await request(`/gm/games/${encodeURIComponent(targetGame)}/players/${encodeURIComponent(row.player_id)}`, {
          signal: detailAbort.signal,
        });
        if (run !== detailGen || targetGame !== props.game) return;
        detail.value = result;
      } catch (e) {
        if (e && e.name === "AbortError") return;
        if (run !== detailGen || targetGame !== props.game) return;
        detailError.value = errorText(e.code);
        notify(detailError.value, "error");
      } finally {
        if (run === detailGen) detailLoading.value = false;
      }
    }

    function resetContext() {
      detailGen++;
      if (detailAbort) {
        detailAbort.abort();
        detailAbort = null;
      }
      drawerOpen.value = false;
      detail.value = null;
      detailRow.value = null;
      detailError.value = "";
      detailLoading.value = false;
      rows.value = [];
      error.value = "";
    }

    watch(
      () => props.game,
      (next, prev) => {
        if (prev !== undefined && next !== prev) resetContext();
        load();
      },
      { immediate: true },
    );

    watch(
      () => props.tick,
      () => load(),
    );

    watch(
      () => [props.initialQuery, props.initialQuerySeq],
      ([value]) => {
        if (value === undefined || value === null) return;
        query.value = value;
        resetContext();
        load();
      },
    );

    onBeforeUnmount(abortAll);

    function awardCurrent() {
      const uid = (detail.value && detail.value.player_id) || (detailRow.value && detailRow.value.player_id);
      if (!uid) return;
      drawerOpen.value = false;
      emit("award", uid);
    }

    const empty = computed(() => !loading.value && !error.value && rows.value.length === 0);
    const detailTitle = computed(() => (detail.value ? detail.value.name || detail.value.player_id : detailRow.value ? detailRow.value.name || detailRow.value.player_id : "玩家详情"));
    const canAward = computed(() => props.role !== "support" && props.role !== "reviewer");
    const canBan = computed(() => props.role === "admin" || props.role === "reviewer");

    async function toggleBan(banned) {
      const targetGame = props.game;
      const uid = detail.value && detail.value.player_id;
      if (!targetGame || !uid || banBusy.value || !canBan.value) return;
      if (banReason.value.trim().length < 3) {
        notify("请填写封禁/解封原因（至少 3 个字符）", "error");
        return;
      }
      banBusy.value = true;
      try {
        const body = { reason: banReason.value.trim() };
        if (!banned) {
          const hrs = Number(banHours.value);
          if (hrs > 0) body.expires_at = Math.floor(Date.now() / 1000) + Math.round(hrs * 3600);
        }
        await request(`/gm/games/${encodeURIComponent(targetGame)}/players/${encodeURIComponent(uid)}/${banned ? "unban" : "ban"}`, {
          method: "POST",
          body,
        });
        if (targetGame !== props.game) return;
        banReason.value = "";
        banHours.value = "";
        notify(banned ? "已解除玩家封禁" : "已封禁玩家并踢出在线会话", "success");
        await inspect(detailRow.value || { player_id: uid, name: detail.value.name });
      } catch (e) {
        if (targetGame === props.game) notify(errorText(e.code), "error");
      } finally {
        banBusy.value = false;
      }
    }

    return {
      query, rows, loading, error, detail, detailLoading, detailError, detailRow, drawerOpen, tab, TABS,
      banReason, banHours, banBusy,
      load, inspect, awardCurrent, toggleBan, empty, detailTitle, canAward, canBan, count, stamp, relative,
    };
  },
  template: `
  <div class="stack">
    <div class="toolbar">
      <div class="search">
        <icon name="search" :size="14" />
        <input class="input" v-model="query" placeholder="玩家 UID 或角色名，回车查询" @keyup.enter="load" />
      </div>
      <button class="btn btn-ghost" @click="load"><icon name="refresh" :size="14" />查询</button>
      <span class="small muted">{{ gameName || game }} · 最多返回 100 条 · 点击行查看资产</span>
    </div>

    <article class="card">
      <div class="table-wrap">
        <table class="table">
          <thead><tr><th>玩家 UID</th><th>角色名</th><th class="tight"></th></tr></thead>
          <tbody>
            <tr v-for="row in rows" :key="row.player_id" data-clickable="true" @click="inspect(row)">
              <td class="mono">{{ row.player_id }}</td>
              <td>{{ row.name || '未命名' }}</td>
              <td class="tight muted"><icon name="arrow" :size="13" /></td>
            </tr>
          </tbody>
        </table>
      </div>
      <skeleton-rows v-if="loading" :rows="4" style="padding:12px" />
      <error-box v-else-if="error" :text="error" @retry="load" />
      <empty v-else-if="empty" text="没有匹配的玩家，换个 UID 或角色名试试" />
    </article>

    <drawer :open="drawerOpen" :title="detailTitle" :subtitle="detail ? (gameName || game) + ' · ' + detail.player_id : (detailRow ? detailRow.player_id : '')" @close="drawerOpen = false">
      <skeleton-rows v-if="detailLoading" :rows="5" />
      <error-box v-else-if="detailError" :text="detailError" @retry="detailRow && inspect(detailRow)" />
      <div v-else-if="detail">
        <div class="toolbar" style="margin-bottom:12px;justify-content:space-between">
          <div style="display:flex;gap:8px;align-items:center">
            <span class="pill" data-tone="info">{{ gameName || game }}</span>
            <span class="pill" :data-tone="detail.ban ? 'danger' : 'ok'">{{ detail.ban ? '已封禁' : '状态正常' }}</span>
          </div>
          <button v-if="canAward" type="button" class="btn btn-soft btn-sm" @click="awardCurrent">
            <icon name="operations" :size="13" />为此玩家补发
          </button>
        </div>

        <div v-if="detail.ban" class="banner" data-tone="warn" style="margin-bottom:12px">
          <icon name="alert" :size="14" />
          <span>封禁原因：{{ detail.ban.reason }} · 操作人 {{ detail.ban.banned_by }} · {{ detail.ban.expires_at ? '到期 ' + stamp(detail.ban.expires_at) : '永久封禁' }}</span>
        </div>

        <div v-if="canBan" class="toolbar" style="margin-bottom:12px;gap:8px;flex-wrap:wrap">
          <input class="input" style="flex:1;min-width:160px" v-model="banReason" placeholder="封禁 / 解封原因（至少 3 个字符）" />
          <input v-if="!detail.ban" class="input" style="width:110px" type="number" min="1" v-model="banHours" placeholder="时长(小时)" />
          <button type="button" :class="detail.ban ? 'btn btn-soft btn-sm' : 'btn btn-danger btn-sm'" :disabled="banBusy" @click="toggleBan(!!detail.ban)">
            {{ banBusy ? '处理中…' : detail.ban ? '解除封禁' : '封禁并踢下线' }}
          </button>
        </div>

        <div class="tabs" role="tablist">
          <button v-for="t in TABS" :key="t.id" role="tab" :aria-selected="String(tab === t.id)" @click="tab = t.id">{{ t.label }}</button>
        </div>

        <div v-if="tab === 'inventory'" style="margin-top:14px">
          <table class="table" v-if="detail.inventory && detail.inventory.length">
            <thead><tr><th>道具</th><th class="num">数量</th></tr></thead>
            <tbody><tr v-for="item in detail.inventory" :key="item.item"><td class="mono">{{ item.item }}</td><td class="num">{{ count(item.amount) }}</td></tr></tbody>
          </table>
          <empty v-else text="该玩家还没有任何库存" />
        </div>

        <div v-if="tab === 'ledger'" style="margin-top:14px">
          <table class="table" v-if="detail.inventory_ledger && detail.inventory_ledger.length">
            <thead><tr><th>时间</th><th>道具</th><th class="num">变动</th><th class="num">余额</th><th>原因</th></tr></thead>
            <tbody>
              <tr v-for="row in detail.inventory_ledger" :key="row.id">
                <td class="small muted">{{ stamp(row.created_at) }}</td>
                <td class="mono">{{ row.item }}</td>
                <td class="num" :style="{ color: row.delta >= 0 ? 'var(--ok)' : 'var(--danger)' }">{{ row.delta > 0 ? '+' : '' }}{{ row.delta }}</td>
                <td class="num">{{ row.balance }}</td>
                <td class="small">{{ row.reason }}</td>
              </tr>
            </tbody>
          </table>
          <empty v-else text="没有库存流水" />
        </div>

        <div v-if="tab === 'mail'" style="margin-top:14px">
          <table class="table" v-if="detail.mail && detail.mail.length">
            <thead><tr><th>标题</th><th>状态</th><th>到期</th></tr></thead>
            <tbody>
              <tr v-for="m in detail.mail" :key="m.id">
                <td>{{ m.title }}</td>
                <td>
                  <span class="pill" :data-tone="m.deleted ? 'danger' : m.claimed ? 'ok' : 'warn'">{{ m.deleted ? '已删除' : m.claimed ? '已领取' : '未领取' }}</span>
                  <span class="pill" style="margin-left:4px">{{ m.is_read ? '已读' : '未读' }}</span>
                </td>
                <td class="small muted">{{ stamp(m.expires_at) }}</td>
              </tr>
            </tbody>
          </table>
          <empty v-else text="没有邮件" />
        </div>

        <div v-if="tab === 'saves'" style="margin-top:14px">
          <table class="table" v-if="detail.saves && detail.saves.length">
            <thead><tr><th>槽位</th><th class="num">版本</th><th class="num">协议</th><th>状态</th></tr></thead>
            <tbody>
              <tr v-for="s in detail.saves" :key="s.slot">
                <td class="mono">{{ s.slot }}</td>
                <td class="num">{{ s.revision }}</td>
                <td class="num">{{ s.schema_version }}</td>
                <td><span class="pill" :data-tone="s.deleted ? 'danger' : 'ok'">{{ s.deleted ? '已删除' : '正常' }}</span></td>
              </tr>
            </tbody>
          </table>
          <empty v-else text="没有存档槽" />
        </div>

        <div v-if="tab === 'grants'" style="margin-top:14px">
          <table class="table" v-if="detail.grants && detail.grants.length">
            <thead><tr><th>来源</th><th>道具</th><th class="num">数量</th></tr></thead>
            <tbody><tr v-for="g in detail.grants" :key="g.id"><td class="mono small">{{ g.business_key }}</td><td class="mono">{{ g.item }}</td><td class="num">{{ g.amount }}</td></tr></tbody>
          </table>
          <empty v-else text="没有补发记录" />
        </div>

        <pre v-if="tab === 'raw'" class="raw">{{ JSON.stringify(detail, null, 2) }}</pre>
      </div>
    </drawer>
  </div>`,
});
