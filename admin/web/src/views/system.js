import { defineComponent, ref, computed, watch, onBeforeUnmount } from "vue";
import { request, probeHealth } from "../api.js";
import { errorText, actionLabel, relative, stamp, healthBadges, count } from "../format.js";
import { Icon } from "../components/icons.js";
import { notify, Empty, ErrorBox, SkeletonRows } from "../components/ui.js";
import { isDesktop, apiBase, setApiBase } from "../desktop.js";

export const SystemView = defineComponent({
  name: "SystemView",
  components: { Icon, Empty, ErrorBox, SkeletonRows },
  props: { game: String, gameName: String, role: String, tick: Number },
  emits: ["diagnose"],
  setup(props, { emit }) {
    const health = ref(null);
    const healthLoading = ref(false);
    const telemetry = ref(null);
    const telemetryError = ref("");
    const opsEvents = ref([]);
    const opsError = ref("");
    const opsFilter = ref("");
    const failures = ref([]);
    const failuresError = ref("");
    const loading = ref(false);
    const desktop = isDesktop();
    const base = ref(apiBase());
    const saved = ref(false);
    let generation = 0;
    let activeAbort = null;

    async function loadHealth(signal) {
      const abortSignal = signal && typeof signal.aborted === "boolean" ? signal : undefined;
      healthLoading.value = true;
      try {
        health.value = await probeHealth({ signal: abortSignal });
      } catch (e) {
        if (e && e.name === "AbortError") return;
      } finally {
        healthLoading.value = false;
      }
    }

    async function loadData() {
      const targetGame = props.game;
      const run = ++generation;
      if (activeAbort) activeAbort.abort();
      if (!targetGame) {
        failures.value = [];
        opsEvents.value = [];
        telemetry.value = null;
        failuresError.value = "";
        opsError.value = "";
        telemetryError.value = "";
        loading.value = false;
        return;
      }
      activeAbort = new AbortController();
      loading.value = true;
      failuresError.value = "";
      opsError.value = "";
      telemetryError.value = "";
      try {
        const [fRes, tRes, oRes] = await Promise.allSettled([
          request(`/gm/games/${encodeURIComponent(targetGame)}/failures`, { signal: activeAbort.signal }),
          request("/gm/system/telemetry", { signal: activeAbort.signal }),
          request(`/gm/games/${encodeURIComponent(targetGame)}/ops-events?limit=80`, { signal: activeAbort.signal }),
        ]);
        if (run !== generation || targetGame !== props.game) return;
        if (fRes.status === "fulfilled") {
          failures.value = Array.isArray(fRes.value) ? fRes.value : [];
        } else if (fRes.reason?.name !== "AbortError") {
          failuresError.value = errorText(fRes.reason?.code);
          failures.value = [];
        }
        if (tRes.status === "fulfilled") {
          telemetry.value = tRes.value;
        } else if (tRes.reason?.name !== "AbortError") {
          telemetryError.value = errorText(tRes.reason?.code);
        }
        if (oRes.status === "fulfilled") {
          opsEvents.value = Array.isArray(oRes.value) ? oRes.value : [];
        } else if (oRes.reason?.name !== "AbortError") {
          opsError.value = errorText(oRes.reason?.code);
          opsEvents.value = [];
        }
      } finally {
        if (run === generation) loading.value = false;
      }
    }

    async function load() {
      await Promise.all([loadHealth(), loadData()]);
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

    function diagnoseEvent(row) {
      emit("diagnose", {
        player_id: row.player_id || "",
        request_id: row.request_id || "",
        question: `排查异常事件：${row.method || ""} ${row.path || ""} 返回 ${row.status_code || ""}（${row.error_code || row.category || "unknown"}）`,
      });
    }

    const filteredOpsEvents = computed(() => {
      const q = opsFilter.value.trim().toLowerCase();
      if (!q) return opsEvents.value;
      return opsEvents.value.filter((ev) =>
        [ev.request_id, ev.player_id, ev.error_code, ev.category, ev.path].join(" ").toLowerCase().includes(q),
      );
    });

    const snap = computed(() => (telemetry.value && telemetry.value.telemetry ? telemetry.value.telemetry : null));

    watch(
      () => props.game,
      (next, prev) => {
        if (prev !== undefined && next !== prev) {
          failures.value = [];
          opsEvents.value = [];
          failuresError.value = "";
          opsError.value = "";
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
      health, healthLoading, telemetry, telemetryError, snap, opsEvents, filteredOpsEvents, opsError, opsFilter,
      failures, failuresError, loading, desktop, base, saved,
      apiBadge, readyBadge, load, loadHealth, loadData, saveBase, diagnoseEvent, actionLabel, relative, stamp, count,
    };
  },
  template: `
  <div class="stack">
    <div class="grid-2">
      <article class="card">
        <div class="card-head">
          <div class="card-title">服务健康</div>
          <button type="button" class="link-btn" :disabled="healthLoading" @click="loadHealth()">{{ healthLoading ? '探测中…' : '重新探测' }}</button>
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
            <div v-if="telemetry" class="list-row">
              <span class="pill" :data-tone="telemetry.rulecore_reachable ? 'ok' : 'warn'">{{ telemetry.rulecore_reachable ? '可达' : '不可达' }}</span>
              <div class="list-main">
                <div class="list-title">C++ RuleCore gRPC</div>
                <div class="list-meta mono">{{ telemetry.rulecore_addr }} · {{ telemetry.rulecore_probe_ms }} ms</div>
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
            <div v-if="snap"><dt>迁移版本</dt><dd class="mono">v{{ snap.schema_version }}</dd></div>
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
      <div class="card-head">
        <div class="card-title">运行期稳定性治理指标</div>
        <div class="card-note">GET /gm/system/telemetry · 5 分钟滑动窗口</div>
      </div>
      <div class="card-body">
        <error-box v-if="telemetryError && !snap" :text="telemetryError" @retry="loadData" />
        <skeleton-rows v-else-if="loading && !snap" :rows="2" />
        <dl v-else-if="snap" class="kv">
          <div><dt>近 5 分钟请求 / 错误</dt><dd>{{ count(snap.window_5m_requests) }} / {{ count(snap.window_5m_errors) }} ({{ (snap.window_5m_error_rate * 100).toFixed(1) }}%)</dd></div>
          <div><dt>延迟 P50 / P95</dt><dd>{{ snap.latency_p50_ms }} ms / {{ snap.latency_p95_ms }} ms</dd></div>
          <div><dt>MySQL 连接池 (用/开/上限)</dt><dd class="mono">{{ snap.db_pool.in_use }} / {{ snap.db_pool.open }} / {{ snap.db_pool.max_open }}</dd></div>
          <div><dt>死锁重试 / 限流拦截</dt><dd>{{ count(snap.deadlock_retries) }} / {{ count(snap.rate_limited + snap.auth_rate_limited) }}</dd></div>
          <div><dt>RuleCore 调用 / 失败</dt><dd>{{ count(snap.rulecore_calls) }} / {{ count(snap.rulecore_errors) }}</dd></div>
          <div><dt>累计总请求 (4xx / 5xx)</dt><dd>{{ count(snap.total_requests) }} ({{ count(snap.status_4xx) }} / {{ count(snap.status_5xx) }})</dd></div>
        </dl>
      </div>
    </article>

    <article class="card">
      <div class="card-head">
        <div class="card-title">业务异常日志（ops_events）</div>
        <div class="toolbar">
          <div class="search">
            <icon name="search" :size="14" />
            <input class="input" v-model="opsFilter" placeholder="按 Request ID / 玩家 / 错误码过滤" />
          </div>
          <span class="card-note">{{ opsError ? '读取失败' : filteredOpsEvents.length + ' 条' }}</span>
        </div>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead><tr><th>时间</th><th>分类 / 状态</th><th>错误码</th><th>Request ID / 玩家</th><th>路径</th><th class="tight">诊断</th></tr></thead>
          <tbody>
            <tr v-for="row in filteredOpsEvents" :key="row.id || (row.request_id + '-' + row.created_at)">
              <td class="small muted">{{ stamp(row.created_at) }}</td>
              <td><span class="pill" :data-tone="row.status_code >= 500 ? 'danger' : 'warn'">{{ row.category }} · {{ row.status_code }}</span></td>
              <td class="mono small">{{ row.error_code }}</td>
              <td class="mono small">
                <div>{{ row.request_id || '—' }}</div>
                <div class="muted">{{ row.player_id || '' }}</div>
              </td>
              <td class="mono small">{{ row.method }} {{ row.path }}</td>
              <td class="tight"><button type="button" class="btn btn-soft btn-sm" @click="diagnoseEvent(row)">AI 排障</button></td>
            </tr>
          </tbody>
        </table>
      </div>
      <skeleton-rows v-if="loading && !opsEvents.length" :rows="3" style="padding:12px" />
      <error-box v-else-if="opsError" :text="opsError" @retry="loadData" />
      <empty v-else-if="!filteredOpsEvents.length" text="暂无 409/422/429/5xx 异常事件" />
    </article>

    <article class="card">
      <div class="card-head"><div class="card-title">失败操作与异常审计</div><div class="card-note">{{ failuresError ? '读取失败' : failures.length + ' 条' }}</div></div>
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
      <skeleton-rows v-if="loading && !failures.length" :rows="3" style="padding:12px" />
      <error-box v-else-if="failuresError" :text="failuresError" @retry="loadData" />
      <empty v-else-if="!failures.length" text="没有失败记录，服务运行正常" />
    </article>
  </div>`,
});
