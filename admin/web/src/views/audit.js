import { defineComponent, ref, computed, watch, onBeforeUnmount } from "vue";
import { request } from "../api.js";
import { errorText, actionLabel, isFailure, relative, stamp, count } from "../format.js";
import { Icon } from "../components/icons.js";
import { Segmented, notify, Empty, ErrorBox, SkeletonRows } from "../components/ui.js";

export const AuditView = defineComponent({
  name: "AuditView",
  components: { Icon, Segmented, Empty, ErrorBox, SkeletonRows },
  props: { game: String, gameName: String, role: String, tick: Number },
  setup(props) {
    const mode = ref("all");
    const rows = ref([]);
    const loading = ref(false);
    const error = ref("");
    const query = ref("");
    const expanded = ref({});
    let generation = 0;
    let activeAbort = null;

    async function load() {
      const targetGame = props.game;
      const run = ++generation;
      if (activeAbort) activeAbort.abort();
      if (!targetGame) {
        rows.value = [];
        error.value = "";
        loading.value = false;
        return;
      }
      activeAbort = new AbortController();
      loading.value = true;
      error.value = "";
      try {
        const path = mode.value === "failures" ? `/gm/games/${encodeURIComponent(targetGame)}/failures` : `/gm/games/${encodeURIComponent(targetGame)}/audit`;
        const result = await request(path, { signal: activeAbort.signal });
        if (run !== generation || targetGame !== props.game) return;
        rows.value = Array.isArray(result) ? result : [];
      } catch (e) {
        if (e && e.name === "AbortError") return;
        if (run !== generation || targetGame !== props.game) return;
        error.value = errorText(e.code);
        rows.value = [];
        notify(error.value, "error");
      } finally {
        if (run === generation) loading.value = false;
      }
    }

    watch(
      () => props.game,
      (next, prev) => {
        if (prev !== undefined && next !== prev) {
          rows.value = [];
          expanded.value = {};
          error.value = "";
        }
        load();
      },
      { immediate: true },
    );
    watch([() => props.tick, mode], load);
    onBeforeUnmount(() => activeAbort && activeAbort.abort());

    const filtered = computed(() => {
      const q = query.value.trim().toLowerCase();
      if (!q) return rows.value;
      return rows.value.filter((r) =>
        [r.action, r.admin, r.reason, JSON.stringify(r.details || {})].join(" ").toLowerCase().includes(q),
      );
    });

    return { mode, rows, loading, error, query, expanded, filtered, load, actionLabel, isFailure, relative, stamp, count };
  },
  template: `
  <div class="stack">
    <div class="toolbar">
      <segmented v-model="mode" :options="[{value:'all',label:'全部审计'},{value:'failures',label:'失败与异常'}]" label="审计筛选" />
      <div class="search">
        <icon name="search" :size="14" />
        <input class="input" v-model="query" placeholder="按操作、操作人、原因过滤" />
      </div>
      <button class="btn btn-ghost" @click="load"><icon name="refresh" :size="14" />刷新</button>
      <span class="small muted">{{ gameName || game }} · 最近 200 条</span>
    </div>

    <article class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th class="tight"></th><th>时间</th><th>操作</th><th>操作人</th><th>原因 / 工单</th></tr>
          </thead>
          <tbody>
            <template v-for="row in filtered" :key="row.id">
              <tr data-clickable="true" @click="expanded[row.id] = !expanded[row.id]">
                <td class="tight muted"><icon name="chevron" :size="13" /></td>
                <td class="small muted">{{ stamp(row.created_at) }}</td>
                <td>
                  <span class="pill" :data-tone="isFailure(row.action) ? 'danger' : 'info'">{{ actionLabel(row.action) }}</span>
                  <span class="small muted" style="margin-left:6px">{{ relative(row.created_at) }}</span>
                </td>
                <td class="mono small">{{ row.admin || '系统' }}</td>
                <td class="small">{{ row.reason }}</td>
              </tr>
              <tr v-if="expanded[row.id]">
                <td></td>
                <td colspan="4"><pre class="raw">{{ JSON.stringify(row.details, null, 2) }}</pre></td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
      <skeleton-rows v-if="loading" :rows="5" style="padding:12px" />
      <error-box v-else-if="error" :text="error" @retry="load" />
      <empty v-else-if="!filtered.length" :text="mode === 'failures' ? '没有失败或异常记录' : '还没有审计记录'" />
    </article>
  </div>`,
});
