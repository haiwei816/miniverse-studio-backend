import { defineComponent, ref, watch, computed } from "vue";
import { request } from "../api.js";
import { errorText, count, stamp, relative } from "../format.js";
import { Icon } from "../components/icons.js";
import { Drawer, notify, Empty, SkeletonRows } from "../components/ui.js";

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
  components: { Icon, Drawer, Empty, SkeletonRows },
  props: { game: String, role: String, tick: Number, initialQuery: String },
  setup(props) {
    const query = ref("");
    const rows = ref([]);
    const loading = ref(false);
    const detail = ref(null);
    const detailLoading = ref(false);
    const tab = ref("inventory");
    const drawerOpen = ref(false);

    async function load() {
      if (!props.game) return;
      loading.value = true;
      try {
        rows.value = await request(`/gm/games/${props.game}/players?q=${encodeURIComponent(query.value)}`);
      } catch (e) {
        notify(errorText(e.code), "error");
        rows.value = [];
      } finally {
        loading.value = false;
      }
    }

    async function inspect(row) {
      drawerOpen.value = true;
      detailLoading.value = true;
      detail.value = null;
      tab.value = "inventory";
      try {
        detail.value = await request(`/gm/games/${props.game}/players/${row.player_id}`);
      } catch (e) {
        notify(errorText(e.code), "error");
        drawerOpen.value = false;
      } finally {
        detailLoading.value = false;
      }
    }

    watch([() => props.game, () => props.tick], load, { immediate: true });
    watch(
      () => props.initialQuery,
      (value) => {
        if (value === undefined || value === null) return;
        query.value = value;
        load();
      },
    );

    const empty = computed(() => !loading.value && rows.value.length === 0);
    const detailTitle = computed(() => (detail.value ? detail.value.name || detail.value.player_id : "玩家详情"));

    return { query, rows, loading, detail, detailLoading, drawerOpen, tab, TABS, load, inspect, empty, detailTitle, count, stamp, relative };
  },
  template: `
  <div class="stack">
    <div class="toolbar">
      <div class="search">
        <icon name="search" :size="14" />
        <input class="input" v-model="query" placeholder="玩家 UID 或角色名，回车查询" @keyup.enter="load" />
      </div>
      <button class="btn btn-ghost" @click="load"><icon name="refresh" :size="14" />查询</button>
      <span class="small muted">最多返回 100 条 · 点击行查看资产</span>
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
      <empty v-else-if="empty" text="没有匹配的玩家，换个 UID 或角色名试试" />
    </article>

    <drawer :open="drawerOpen" :title="detailTitle" :subtitle="detail ? detail.player_id : ''" @close="drawerOpen = false">
      <skeleton-rows v-if="detailLoading" :rows="5" />
      <div v-else-if="detail">
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
