import { defineComponent, ref, computed, watch, onBeforeUnmount } from "vue";
import { request, probeHealth } from "../api.js";
import { errorText, actionLabel, relative, stamp, healthBadges } from "../format.js";
import { Icon } from "../components/icons.js";
import { notify, Empty, ErrorBox, SkeletonRows } from "../components/ui.js";
import { isDesktop, apiBase, setApiBase } from "../desktop.js";

export const SystemView = defineComponent({
  name: "SystemView",
  components: { Icon, Empty, ErrorBox, SkeletonRows },
  props: { game: String, gameName: String, role: String, tick: Number },
  setup(props) {
    const health = ref(null);
    const healthLoading = ref(false);
    const failures = ref([]);
    const failuresError = ref("");
    const loading = ref(false);
    const desktop = isDesktop();
    const base = ref(apiBase());
    const saved = ref(false);
    let generation = 0;
    let activeAbort = null;

    async function loadHealth() {
      healthLoading.value = true;
      try {
        health.value = await probeHealth();
      } finally {
        healthLoading.value = false;
      }
    }

    async function loadFailures() {
      const targetGame = props.game;
      const run = ++generation;
      if (activeAbort) activeAbort.abort();
      if (!targetGame) {
        failures.value = [];
        failuresError.value = "";
        loading.value = false;
        return;
      }
      activeAbort = new AbortController();
      loading.value = true;
      failuresError.value = "";
      try {
        const result = await request(`/gm/games/${encodeURIComponent(targetGame)}/failures`, { signal: activeAbort.signal });
        if (run !== generation || targetGame !== props.game) return;
        failures.value = Array.isArray(result) ? result : [];
      } catch (e) {
        if (e && e.name === "AbortError") return;
        if (run !== generation || targetGame !== props.game) return;
        failuresError.value = errorText(e.code);
        failures.value = [];
        if (e.code !== "gm_unauthorized") notify(failuresError.value, "error");
      } finally {
        if (run === generation) loading.value = false;
      }
    }

    async function load() {
      await Promise.all([loadHealth(), loadFailures()]);
    }

    function saveBase() {
      try {
        base.value = setApiBase(base.value);
        saved.value = true;
        notify("接口地址已保存，刷新后生效", "success");
      } catch (e) {
        saved.value = false;
        notify(errorText((e && e.code) || "invalid_api_base"), "error");
      }
    }

    watch(
      () => props.game,
      (next, prev) => {
        if (prev !== undefined && next !== prev) {
          failures.value = [];
          failuresError.value = "";
        }
        load();
      },
      { immediate: true },
    );
    watch(() => props.tick, load);
    onBeforeUnmount(() => activeAbort && activeAbort.abort());

    const badges = computed(() => healthBadges(health.value, healthLoading.value));
    const apiBadge = computed(() => badges.value.api);
    const readyBadge = computed(() => badges.value.ready);

    return {
      health, healthLoading, failures, failuresError, loading, desktop, base, saved,
      apiBadge, readyBadge, load, loadHealth, loadFailures, saveBase, actionLabel, relative, stamp,
    };
  },
  template: `
  <div class="stack">
    <div class="grid-2">
      <article class="card">
        <div class="card-head">
          <div class="card-title">服务健康</div>
          <button type="button" class="link-btn" :disabled="healthLoading" @click="loadHealth">{{ healthLoading ? '探测中…' : '重新探测' }}</button>
        </div>
        <div class="card-body">
          <div class="list">
            <div class="list-row">
              <span class="pill" :data-tone="apiBadge.tone">{{ apiBadge.label }}</span>
              <div class="list-main">
                <div class="list-title">API 进程</div>
                <div class="list-meta">{{ health && health.apiDetail ? health.apiDetail : 'GET /health' }}</div>
              </div>
            </div>
            <div class="list-row">
              <span class="pill" :data-tone="readyBadge.tone">{{ readyBadge.label }}</span>
              <div class="list-main">
                <div class="list-title">数据库与迁移</div>
                <div class="list-meta">{{ health && health.readyDetail ? health.readyDetail : 'GET /ready · 检查 schema_migrations' }}</div>
              </div>
            </div>
          </div>
          <div class="small muted" style="margin-top:10px">上次探测：{{ health ? stamp(health.checked) : '—' }}</div>
        </div>
      </article>

      <article class="card">
        <div class="card-head"><div class="card-title">接入信息</div><div class="card-note">{{ desktop ? '桌面客户端' : '浏览器' }}</div></div>
        <div class="card-body">
          <dl class="kv">
            <div><dt>当前游戏</dt><dd class="mono">{{ gameName ? gameName + ' (' + game + ')' : game }}</dd></div>
            <div><dt>角色</dt><dd>{{ role }}</dd></div>
          </dl>
          <template v-if="desktop">
            <hr class="hr" />
            <label class="field">
              <span class="field-label">API 地址</span>
              <input class="input mono" v-model="base" placeholder="http://127.0.0.1:18080" />
            </label>
            <div class="toolbar" style="margin-top:8px">
              <button class="btn btn-ghost btn-sm" @click="saveBase">保存</button>
              <span class="small muted">修改后需要重新登录</span>
            </div>
          </template>
        </div>
      </article>
    </div>

    <article class="card">
      <div class="card-head"><div class="card-title">失败操作与异常</div><div class="card-note">{{ failuresError ? '读取失败' : failures.length + ' 条' }}</div></div>
      <div class="table-wrap">
        <table class="table">
          <thead><tr><th>时间</th><th>类型</th><th>错误码</th><th>操作人</th></tr></thead>
          <tbody>
            <tr v-for="row in failures" :key="row.id">
              <td class="small muted">{{ stamp(row.created_at) }}</td>
              <td><span class="pill" data-tone="danger">{{ actionLabel(row.action) }}</span></td>
              <td class="mono small">{{ row.reason }}</td>
              <td class="mono small">{{ row.admin || '系统' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <skeleton-rows v-if="loading" :rows="3" style="padding:12px" />
      <error-box v-else-if="failuresError" :text="failuresError" @retry="loadFailures" />
      <empty v-else-if="!failures.length" text="没有失败记录，服务运行正常" />
    </article>
  </div>`,
});
