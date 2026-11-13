import { defineComponent, ref, watch } from "vue";
import { request } from "../api.js";
import { errorText, stamp, relative, requestState, count } from "../format.js";
import { Icon } from "../components/icons.js";
import { Segmented, notify, Empty, SkeletonRows } from "../components/ui.js";

const REWARD_TEMPLATE = { players: [], item: "wood", amount: 10 };
const ACTIVITY_TEMPLATE = {
  id: "daily-visit",
  version: 1,
  kind: "daily",
  event_type: "visit",
  target: 1,
  starts_at: 1,
  ends_at: 4102444800,
  client_allowed: true,
  item: "wood",
  reward_amount: 5,
};

export const OperationsView = defineComponent({
  name: "OperationsView",
  components: { Icon, Segmented, Empty, SkeletonRows },
  props: { game: String, role: String, tick: Number },
  setup(props) {
    const kind = ref("reward");
    const reason = ref("");
    const requestId = ref(crypto.randomUUID());
    const players = ref("");
    const item = ref("wood");
    const amount = ref(10);
    const config = ref(JSON.stringify(ACTIVITY_TEMPLATE, null, 2));
    const brief = ref("每日升级建筑 3 次奖励 5 木材");
    const plan = ref(null);
    const notice = ref("");
    const rows = ref([]);
    const loading = ref(false);
    const busy = ref(false);
    const expanded = ref({});

    const canSubmit = () => props.role !== "support" && props.role !== "reviewer";
    const canApprove = () => props.role === "admin" || props.role === "reviewer";

    async function load() {
      if (!props.game) return;
      loading.value = true;
      try {
        rows.value = await request(`/gm/games/${props.game}/requests`);
      } catch (e) {
        notify(errorText(e.code), "error");
      } finally {
        loading.value = false;
      }
    }

    async function generate() {
      busy.value = true;
      try {
        plan.value = await request(`/gm/games/${props.game}/planner/generate`, { method: "POST", body: { brief: brief.value } });
        config.value = JSON.stringify(plan.value.config, null, 2);
        kind.value = "activity";
        reason.value = reason.value || "审核 mock 生成活动";
        notify("已生成并校验，提交后需另一位审核人批准", "success");
      } catch (e) {
        notify(errorText(e.code), "error");
      } finally {
        busy.value = false;
      }
    }

    async function submit() {
      if (reason.value.trim().length < 3) {
        notify("请填写原因 / 工单（至少 3 个字符）", "error");
        return;
      }
      busy.value = true;
      try {
        let payload;
        if (kind.value === "reward") {
          const list = players.value.split(/[\s,]+/).filter(Boolean);
          if (!list.length) {
            notify("请填写至少一个玩家 UID", "error");
            busy.value = false;
            return;
          }
          payload = { players: list, item: item.value, amount: Number(amount.value) };
        } else {
          payload = JSON.parse(config.value);
        }
        await request(`/gm/games/${props.game}/requests`, {
          method: "POST",
          body: { id: requestId.value, kind: kind.value, reason: reason.value, payload },
        });
        notify("申请已创建，等待另一位审核人批准", "success");
        requestId.value = crypto.randomUUID();
        await load();
      } catch (e) {
        notify(e instanceof SyntaxError ? "活动 JSON 格式不正确" : errorText(e.code), "error");
      } finally {
        busy.value = false;
      }
    }

    async function act(row, action) {
      if (reason.value.trim().length < 3) {
        notify("批准或撤销前请先填写原因 / 工单", "error");
        return;
      }
      busy.value = true;
      try {
        await request(`/gm/games/${props.game}/requests/${row.id}/${action}`, { method: "POST", body: { reason: reason.value } });
        notify(action === "approve" ? "已批准并执行" : "已撤销申请", "success");
        await load();
      } catch (e) {
        notify(errorText(e.code), "error");
      } finally {
        busy.value = false;
      }
    }

    async function rollback(row) {
      try {
        kind.value = "activity";
        const old = row.payload;
        const versions = await request(`/gm/games/${props.game}/activities/${old.id}/versions`);
        const latest = Math.max(...versions.map((v) => v.version), old.version);
        config.value = JSON.stringify({ ...old, version: latest + 1 }, null, 2);
        reason.value = "回滚到历史配置：" + old.id + " v" + old.version;
        notify("已载入历史配置，提交后需要审核", "info");
      } catch (e) {
        notify(errorText(e.code), "error");
      }
    }

    async function stop(row) {
      try {
        await request(`/gm/games/${props.game}/activities/${row.payload.id}/stop`, { method: "POST", body: { reason: reason.value } });
        notify("活动已停止", "success");
        await load();
      } catch (e) {
        notify(errorText(e.code), "error");
      }
    }

    async function announce() {
      try {
        await request(`/gm/games/${props.game}/announcements/notice`, {
          method: "PUT",
          body: { body: notice.value, expires_at: Math.floor(Date.now() / 1000) + 86400, reason: reason.value },
        });
        notify("公告已发布，有效期 24 小时", "success");
      } catch (e) {
        notify(errorText(e.code), "error");
      }
    }

    function summary(row) {
      const p = row.payload || {};
      if (row.kind === "reward") return (p.players ? p.players.length : 0) + " 名玩家 · " + p.item + " × " + p.amount;
      return "活动 " + (p.id || "?") + " v" + (p.version || "?");
    }

    watch([() => props.game, () => props.tick], load, { immediate: true });

    return {
      kind, reason, requestId, players, item, amount, config, brief, plan, notice, rows, loading, busy, expanded,
      canSubmit, canApprove, load, generate, submit, act, rollback, stop, announce, summary,
      stamp, relative, requestState, count,
    };
  },
  template: `
  <div class="stack">
    <div class="banner" data-tone="warn">
      <icon name="alert" :size="14" />
      补发与活动发布需要另一位审核人批准；执行后的补发不可撤销。所有操作必须填写原因 / 工单。
    </div>

    <div class="grid-2">
      <article class="card">
        <div class="card-head"><div class="card-title">活动策划 · 确定性 Mock</div><div class="card-note">生成草稿，不直接发布</div></div>
        <div class="card-body">
          <div class="toolbar">
            <input class="input" style="flex:1" v-model="brief" placeholder="例如：每日升级建筑 3 次奖励 5 木材" />
            <button class="btn btn-ghost" :disabled="busy" @click="generate"><icon name="spark" :size="14" />生成</button>
          </div>
          <div v-if="plan" class="plan">
            <span class="pill" data-tone="info">{{ plan.provider }}</span>
            <span class="pill">尝试 {{ plan.attempts }} 次</span>
            <span class="pill" :data-tone="plan.real_ai ? 'ok' : 'warn'">{{ plan.real_ai ? '真实 AI' : 'Mock 生成' }}</span>
          </div>

          <hr class="hr" />

          <div class="field">
            <span class="field-label">原因 / 工单（所有操作必填）</span>
            <input class="input" v-model="reason" placeholder="例如 ticket-123：补偿停服" />
          </div>

          <div class="field" style="margin-top:12px">
            <span class="field-label">申请类型</span>
            <segmented v-model="kind" :options="[{value:'reward',label:'补发奖励'},{value:'activity',label:'活动配置'}]" label="申请类型" />
          </div>

          <div class="field" style="margin-top:12px">
            <span class="field-label">申请 ID（重试时保持不变）</span>
            <input class="input mono" v-model="requestId" />
          </div>

          <template v-if="kind === 'reward'">
            <div class="field" style="margin-top:12px">
              <span class="field-label">玩家 UID（逗号或换行分隔，每批最多 100 人）</span>
              <textarea class="textarea" v-model="players" rows="4" placeholder="8f3a…&#10;b21c…"></textarea>
            </div>
            <div class="grid-2" style="margin-top:12px;gap:10px">
              <label class="field"><span class="field-label">道具 ID</span><input class="input mono" v-model="item" /></label>
              <label class="field"><span class="field-label">数量</span><input class="input" type="number" min="1" max="1000000" v-model="amount" /></label>
            </div>
          </template>

          <div class="field" v-else style="margin-top:12px">
            <span class="field-label">活动 JSON</span>
            <textarea class="textarea" v-model="config" rows="12"></textarea>
          </div>

          <div class="toolbar" style="margin-top:14px">
            <button class="btn btn-primary" :disabled="!canSubmit() || busy" @click="submit">提交申请</button>
            <button class="btn btn-ghost" @click="load">刷新记录</button>
            <span v-if="!canSubmit()" class="small muted">当前角色只能审批，不能创建申请</span>
          </div>
        </div>
      </article>

      <article class="card">
        <div class="card-head"><div class="card-title">申请与执行记录</div><div class="card-note">{{ rows.length }} 条</div></div>
        <div class="table-wrap">
          <table class="table">
            <thead><tr><th>内容</th><th>状态</th><th class="tight">操作</th></tr></thead>
            <tbody>
              <tr v-for="row in rows" :key="row.id" data-clickable="true" @click="expanded[row.id] = !expanded[row.id]">
                <td>
                  <div class="list-title">{{ summary(row) }}</div>
                  <div class="list-meta">{{ row.kind === 'reward' ? '补发' : '活动' }} · {{ row.creator }} → {{ row.reviewer || '待指派' }} · {{ relative(row.created_at) }}</div>
                  <div class="list-meta">{{ row.reason }}</div>
                  <pre v-if="expanded[row.id]" class="raw">{{ JSON.stringify(row.payload, null, 2) }}</pre>
                </td>
                <td><span class="pill" :data-tone="requestState(row.state).tone">{{ requestState(row.state).label }}</span></td>
                <td class="tight">
                  <div class="actions" @click.stop>
                    <template v-if="row.state === 'pending'">
                      <button class="btn btn-soft btn-sm" :disabled="!canApprove()" @click="act(row,'approve')">批准</button>
                      <button class="btn btn-quiet btn-sm" @click="act(row,'cancel')">撤销</button>
                    </template>
                    <template v-else-if="row.kind === 'activity' && row.state === 'executed'">
                      <button class="btn btn-quiet btn-sm" @click="rollback(row)">载入</button>
                      <button class="btn btn-quiet btn-sm" @click="stop(row)">停止</button>
                    </template>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
          <skeleton-rows v-if="loading" :rows="3" style="padding:12px" />
          <empty v-else-if="!rows.length" text="还没有申请记录" />
        </div>
      </article>
    </div>

    <article class="card">
      <div class="card-head"><div class="card-title">系统公告</div><div class="card-note">有效期 24 小时</div></div>
      <div class="card-body">
        <textarea class="textarea" v-model="notice" rows="3" placeholder="公告正文，玩家登录后可见"></textarea>
        <div class="toolbar" style="margin-top:10px">
          <button class="btn btn-soft" @click="announce">发布公告</button>
          <span class="small muted">需要填写上面的原因 / 工单</span>
        </div>
      </div>
    </article>
  </div>`,
});
