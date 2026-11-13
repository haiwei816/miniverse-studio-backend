import { defineComponent, ref, computed, watch } from "vue";
import { request } from "../api.js";
import { errorText, actionLabel, isFailure, relative, stamp, count } from "../format.js";
import { Icon } from "../components/icons.js";
import { Segmented, notify, Empty, SkeletonRows } from "../components/ui.js";

export const AuditView = defineComponent({
  name: "AuditView",
  components: { Icon, Segmented, Empty, SkeletonRows },
  props: { game: String, role: String, tick: Number },
  setup(props) {
    const mode = ref("all");
    const rows = ref([]);
    const loading = ref(false);
    const query = ref("");
    const expanded = ref({});

    async function load() {
      if (!props.game) return;
      loading.value = true;
      try {
        rows.value = await request(mode.value === "failures" ? `/gm/games/${props.game}/failures` : `/gm/games/${props.game}/audit`);
      } catch (e) {
        notify(errorText(e.code), "error");
        rows.value = [];
      } finally {
        loading.value = false;
      }
    }

    watch([() => props.game, () => props.tick, mode], load, { immediate: true });

    const filtered = computed(() => {
      const q = query.value.trim().toLowerCase();
      if (!q) return rows.value;
      return rows.value.filter((r) =>
        [r.action, r.admin, r.reason, JSON.stringify(r.details || {})].join(" ").toLowerCase().includes(q),
      );
    });

    return { mode, rows, loading, query, expanded, filtered, load, actionLabel, isFailure, relative, stamp, count };
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
      <span class="small muted">最近 200 条</span>
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
      <empty v-else-if="!filtered.length" :text="mode === 'failures' ? '没有失败或异常记录' : '还没有审计记录'" />
    </article>
  </div>`,
});
