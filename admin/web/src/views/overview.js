import { defineComponent, ref, computed, onMounted, watch } from "vue";
import { request } from "../api.js";
import { errorText, count, relative, actionLabel, isFailure, requestState, ago } from "../format.js";
import { Sparkline, TrendChart } from "../components/charts.js";
import { Icon } from "../components/icons.js";
import { notify, SkeletonRows, Empty } from "../components/ui.js";

export const OverviewView = defineComponent({
  name: "OverviewView",
  props: { game: String, role: String, tick: Number },
  emits: ["navigate"],
  setup(props, { emit }) {
    const data = ref(null);
    const audits = ref([]);
    const requests = ref([]);
    const health = ref(null);
    const loading = ref(true);
    const failure = ref("");

    const totals = computed(() => (data.value ? data.value.totals : {}));
    const series = computed(() => (data.value ? data.value.series : { audits: [], grants: [] }));
    const pending = computed(() => requests.value.filter((r) => r.state === "pending"));
    const auditSeries = computed(() => (series.value.audits || []).map((p) => p.count));
    const grantSeries = computed(() => (series.value.grants || []).map((p) => p.count));

    const chartSeries = computed(() => [
      { key: "a", name: "管理操作", points: series.value.audits || [] },
      { key: "b", name: "发奖记录", points: series.value.grants || [] },
    ]);

    async function load() {
      loading.value = true;
      failure.value = "";
      try {
        const [overview, auditRows, requestRows] = await Promise.all([
          request(`/gm/games/${props.game}/overview`),
          request(`/gm/games/${props.game}/audit`),
          request(`/gm/games/${props.game}/requests`),
        ]);
        data.value = overview;
        audits.value = (auditRows || []).slice(0, 7);
        requests.value = requestRows || [];
      } catch (e) {
        failure.value = errorText(e.code);
        if (e.code !== "gm_unauthorized") notify(errorText(e.code), "error");
      } finally {
        loading.value = false;
      }
      try {
        const [h, r] = await Promise.all([request("/health"), request("/ready")]);
        health.value = { alive: !!h, ready: !!r, checked: Date.now() };
      } catch {
        health.value = { alive: false, ready: false, checked: Date.now() };
      }
    }

    onMounted(load);
    watch([() => props.game, () => props.tick], load);

    const gameLabel = computed(() => (data.value && data.value.game ? data.value.game.name : props.game));

    return {
      data, audits, requests, pending, health, loading, failure,
      totals, series, auditSeries, grantSeries, chartSeries, gameLabel,
      count, relative, actionLabel, isFailure, requestState, ago,
      go: (view) => emit("navigate", view),
    };
  },
  template: `
  <div class="stack">
    <section class="grid-stats" aria-label="关键指标">
      <article class="card stat">
        <div class="stat-label"><icon name="players" :size="13"/>玩家</div>
        <div class="stat-value">{{ count(totals.players) }}</div>
        <div class="stat-foot">当前在线 {{ count(totals.online) }} · 存档 {{ count(totals.saves) }}</div>
      </article>
      <article class="card stat" :data-tone="totals.requests_pending ? 'accent' : ''">
        <div class="stat-label"><icon name="operations" :size="13"/>待审申请</div>
        <div class="stat-value">{{ count(totals.requests_pending) }}</div>
        <div class="stat-foot">{{ totals.requests_pending ? '需要另一名审核人批准' : '审批队列已清空' }}</div>
      </article>
      <article class="card stat">
        <div class="stat-label"><icon name="audit" :size="13"/>24 小时审计</div>
        <div class="stat-value">{{ count(totals.audits_24h) }}</div>
        <div class="stat-foot">失败 {{ count(totals.failures_24h) }} 次</div>
        <sparkline class="stat-spark" :values="auditSeries" />
      </article>
      <article class="card stat">
        <div class="stat-label"><icon name="mail" :size="13"/>待领邮件</div>
        <div class="stat-value">{{ count(totals.mail_pending) }}</div>
        <div class="stat-foot">24 小时发奖 {{ count(totals.grants_24h) }} 次</div>
        <sparkline class="stat-spark" :values="grantSeries" series="b" />
      </article>
    </section>

    <div v-if="failure" class="banner" data-tone="warn"><icon name="alert" :size="14"/>{{ failure }}<button class="link-btn" style="margin-left:auto" @click="load">重试</button></div>

    <section class="grid-main">
      <div class="stack">
        <article class="card">
          <div class="chart-nav">
            <div>
              <div class="card-title">近七日运行趋势</div>
              <div class="card-note">{{ gameLabel }} · 按 UTC 日历分桶</div>
            </div>
            <div class="legend">
              <span><i style="background:var(--accent)"></i>管理操作</span>
              <span><i style="background:var(--info)"></i>发奖记录</span>
            </div>
          </div>
          <div class="card-body">
            <skeleton-rows v-if="loading && !data" :rows="3" />
            <trend-chart v-else :series="chartSeries" />
          </div>
        </article>

        <article class="card">
          <div class="card-head"><div class="card-title">资产与活动</div><div class="card-note">服务端权威计数</div></div>
          <div class="card-body">
            <dl class="kv">
              <div><dt>活跃活动</dt><dd>{{ count(totals.activities_active) }}</dd></div>
              <div><dt>有余额的道具</dt><dd>{{ count(totals.items) }}</dd></div>
              <div><dt>存档槽</dt><dd>{{ count(totals.saves) }}</dd></div>
              <div><dt>待领取邮件</dt><dd>{{ count(totals.mail_pending) }}</dd></div>
            </dl>
            <hr class="hr" />
            <div class="spread">
              <span class="small muted">库存、邮件与存档的一致性由服务端事务保证，这里只做展示。</span>
              <button class="link-btn" @click="go('players')">查看玩家 →</button>
            </div>
          </div>
        </article>
      </div>

      <div class="stack">
        <article class="card">
          <div class="card-head">
            <div class="card-title">待审申请</div>
            <button class="link-btn" @click="go('operations')">运营台 →</button>
          </div>
          <div class="card-body" style="padding-top:8px">
            <skeleton-rows v-if="loading" :rows="3" />
            <empty v-else-if="!pending.length" text="没有待审批的申请" />
            <div v-else class="list">
              <div class="list-row" v-for="row in pending.slice(0,4)" :key="row.id">
                <span class="pill" data-tone="warn">待审核</span>
                <div class="list-main">
                  <div class="list-title">{{ row.kind === 'reward' ? '补发奖励' : '活动配置' }}</div>
                  <div class="list-meta">{{ row.reason || '未填写原因' }} · {{ relative(row.created_at) }}</div>
                </div>
              </div>
            </div>
          </div>
        </article>

        <article class="card">
          <div class="card-head"><div class="card-title">最近操作</div><div class="card-note">审计流</div></div>
          <div class="card-body">
            <skeleton-rows v-if="loading" :rows="4" />
            <empty v-else-if="!audits.length" text="还没有管理操作" />
            <div v-else class="timeline">
              <div class="timeline-row" v-for="row in audits" :key="row.id">
                <span class="timeline-dot" :data-tone="isFailure(row.action) ? 'danger' : 'ok'"></span>
                <div>
                  <div class="small"><b>{{ actionLabel(row.action) }}</b> <span class="muted">· {{ row.admin || '系统' }}</span></div>
                  <div class="list-meta">{{ row.reason }} · {{ relative(row.created_at) }}</div>
                </div>
              </div>
            </div>
          </div>
        </article>

        <article class="card">
          <div class="card-head"><div class="card-title">服务状态</div><div class="card-note">实时探测</div></div>
          <div class="card-body">
            <div class="list">
              <div class="list-row">
                <span class="pill" :data-tone="health && health.alive ? 'ok' : 'danger'">{{ health && health.alive ? '在线' : '离线' }}</span>
                <div class="list-main"><div class="list-title">API 进程</div><div class="list-meta">GET /health</div></div>
              </div>
              <div class="list-row">
                <span class="pill" :data-tone="health && health.ready ? 'ok' : 'warn'">{{ health && health.ready ? '就绪' : '未就绪' }}</span>
                <div class="list-main"><div class="list-title">数据库与迁移</div><div class="list-meta">GET /ready · {{ data ? '迁移版本 v' + data.game.config_version : '—' }}</div></div>
              </div>
            </div>
          </div>
        </article>
      </div>
    </section>
  </div>`,
});
