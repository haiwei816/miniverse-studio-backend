import { defineComponent, ref, watch } from "vue";
import { request } from "../api.js";
import { errorText, actionLabel, relative, stamp } from "../format.js";
import { Icon } from "../components/icons.js";
import { notify, Empty, SkeletonRows } from "../components/ui.js";
import { isDesktop, apiBase, setApiBase } from "../desktop.js";

export const SystemView = defineComponent({
  name: "SystemView",
  components: { Icon, Empty, SkeletonRows },
  props: { game: String, role: String, tick: Number },
  setup(props) {
    const health = ref(null);
    const failures = ref([]);
    const loading = ref(false);
    const desktop = isDesktop();
    const base = ref(apiBase());
    const saved = ref(false);

    async function load() {
      loading.value = true;
      try {
        const [h, r] = await Promise.all([request("/health"), request("/ready")]);
        health.value = { alive: !!h, ready: !!r, checked: Math.floor(Date.now() / 1000) };
      } catch {
        health.value = { alive: false, ready: false, checked: Math.floor(Date.now() / 1000) };
      }
      try {
        failures.value = await request(`/gm/games/${props.game}/failures`);
      } catch (e) {
        if (e.code !== "gm_unauthorized") notify(errorText(e.code), "error");
        failures.value = [];
      } finally {
        loading.value = false;
      }
    }

    function saveBase() {
      setApiBase(base.value);
      saved.value = true;
      notify("接口地址已保存，刷新后生效", "success");
    }

    watch([() => props.game, () => props.tick], load, { immediate: true });

    return { health, failures, loading, desktop, base, saved, load, saveBase, actionLabel, relative, stamp };
  },
  template: `
  <div class="stack">
    <div class="grid-2">
      <article class="card">
        <div class="card-head"><div class="card-title">服务健康</div><div class="card-note">10 秒超时</div></div>
        <div class="card-body">
          <div class="list">
            <div class="list-row">
              <span class="pill" :data-tone="health && health.alive ? 'ok' : 'danger'">{{ health && health.alive ? '在线' : '离线' }}</span>
              <div class="list-main"><div class="list-title">API 进程</div><div class="list-meta">GET /health</div></div>
            </div>
            <div class="list-row">
              <span class="pill" :data-tone="health && health.ready ? 'ok' : 'warn'">{{ health && health.ready ? '就绪' : '未就绪' }}</span>
              <div class="list-main"><div class="list-title">数据库与迁移</div><div class="list-meta">GET /ready · 检查 schema_migrations</div></div>
            </div>
          </div>
          <div class="small muted" style="margin-top:10px">上次探测：{{ health ? stamp(health.checked) : '—' }}</div>
        </div>
      </article>

      <article class="card">
        <div class="card-head"><div class="card-title">接入信息</div><div class="card-note">{{ desktop ? '桌面客户端' : '浏览器' }}</div></div>
        <div class="card-body">
          <dl class="kv">
            <div><dt>当前游戏</dt><dd class="mono">{{ game }}</dd></div>
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
      <div class="card-head"><div class="card-title">失败操作与异常</div><div class="card-note">{{ failures.length }} 条</div></div>
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
      <empty v-else-if="!failures.length" text="没有失败记录，服务运行正常" />
    </article>
  </div>`,
});
