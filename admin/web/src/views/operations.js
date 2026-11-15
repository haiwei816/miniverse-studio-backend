import { defineComponent, ref, computed, watch, onBeforeUnmount } from "vue";
import { request } from "../api.js";
import { errorText, stamp, relative, requestState, count } from "../format.js";
import { Icon } from "../components/icons.js";
import { Segmented, ModalDialog, notify, Empty, ErrorBox, SkeletonRows } from "../components/ui.js";

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
  components: { Icon, Segmented, ModalDialog, Empty, ErrorBox, SkeletonRows },
  props: {
    game: String,
    gameName: String,
    role: String,
    username: String,
    tick: Number,
    awardTarget: Object,
  },
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
    const loadError = ref("");
    const busy = ref(false);
    const actingId = ref("");
    const expanded = ref({});
    const confirmAction = ref(null);
    let generation = 0;
    let activeAbort = null;

    const canSubmit = computed(() => props.role !== "support" && props.role !== "reviewer");
    const canApprove = computed(() => props.role === "admin" || props.role === "reviewer");

    async function load() {
      const targetGame = props.game;
      const run = ++generation;
      if (activeAbort) activeAbort.abort();
      if (!targetGame) {
        rows.value = [];
        loadError.value = "";
        loading.value = false;
        return;
      }
      activeAbort = new AbortController();
      loading.value = true;
      loadError.value = "";
      try {
        const result = await request(`/gm/games/${encodeURIComponent(targetGame)}/requests`, { signal: activeAbort.signal });
        if (run !== generation || targetGame !== props.game) return;
        rows.value = Array.isArray(result) ? result : [];
      } catch (e) {
        if (e && e.name === "AbortError") return;
        if (run !== generation || targetGame !== props.game) return;
        loadError.value = errorText(e.code);
        rows.value = [];
        notify(loadError.value, "error");
      } finally {
        if (run === generation) loading.value = false;
      }
    }

    async function generate() {
      const targetGame = props.game;
      if (!targetGame || busy.value) return;
      busy.value = true;
      try {
        const result = await request(`/gm/games/${encodeURIComponent(targetGame)}/planner/generate`, {
          method: "POST",
          body: { brief: brief.value },
        });
        if (targetGame !== props.game) return;
        plan.value = result;
        config.value = JSON.stringify(result.config, null, 2);
        kind.value = "activity";
        reason.value = reason.value || "审核 mock 生成活动";
        notify("已生成并校验，提交后需另一位审核人批准", "success");
      } catch (e) {
        if (targetGame === props.game) notify(errorText(e.code), "error");
      } finally {
        busy.value = false;
      }
    }

    async function submit() {
      const targetGame = props.game;
      if (!targetGame || busy.value) return;
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
          payload = { players: list, item: item.value.trim(), amount: Number(amount.value) };
        } else {
          payload = JSON.parse(config.value);
        }
        await request(`/gm/games/${encodeURIComponent(targetGame)}/requests`, {
          method: "POST",
          body: { id: requestId.value, kind: kind.value, reason: reason.value, payload },
        });
        if (targetGame !== props.game) return;
        notify("申请已创建，等待另一位审核人批准", "success");
        requestId.value = crypto.randomUUID();
        await load();
      } catch (e) {
        if (targetGame === props.game) {
          notify(e instanceof SyntaxError ? "活动 JSON 格式不正确" : errorText(e.code), "error");
        }
      } finally {
        busy.value = false;
      }
    }

    function openConfirm(row, action) {
      if (loading.value || actingId.value || !props.game) return;
      confirmAction.value = {
        row,
        action,
        game: props.game,
        gameName: props.gameName || props.game,
        reason: "",
      };
    }

    const confirmSelfAuthor = computed(() => {
      const c = confirmAction.value;
      return !!(c && c.action === "approve" && props.username && c.row.creator === props.username);
    });

    const confirmDetails = computed(() => {
      const c = confirmAction.value;
      if (!c) return null;
      const p = c.row.payload || {};
      if (c.row.kind === "reward") {
        const list = Array.isArray(p.players) ? p.players : [];
        const perPlayer = Number(p.amount) || 0;
        return {
          kind: "reward",
          players: list,
          playerCount: list.length,
          item: p.item || "—",
          perPlayer,
          total: list.length * perPlayer,
        };
      }
      return {
        kind: "activity",
        id: p.id || "—",
        version: p.version || "—",
        activityKind: p.kind || "—",
        eventType: p.event_type || "—",
        target: p.target ?? "—",
        item: p.item || "—",
        rewardAmount: p.reward_amount ?? "—",
        clientAllowed: !!p.client_allowed,
      };
    });

    async function executeConfirm() {
      const c = confirmAction.value;
      if (!c) return;
      if (c.game !== props.game) {
        confirmAction.value = null;
        notify("游戏上下文已切换，操作已取消", "error");
        return;
      }
      if (c.action === "approve" && (!canApprove.value || confirmSelfAuthor.value)) return;
      if (c.reason.trim().length < 3) {
        notify("批准或撤销前请填写独立的审批原因（至少 3 个字符）", "error");
        return;
      }
      actingId.value = c.row.id;
      try {
        await request(`/gm/games/${encodeURIComponent(c.game)}/requests/${encodeURIComponent(c.row.id)}/${c.action}`, {
          method: "POST",
          body: { reason: c.reason.trim() },
        });
        if (c.game !== props.game) return;
        notify(c.action === "approve" ? "已批准并执行" : "已撤销申请", "success");
        confirmAction.value = null;
        await load();
      } catch (e) {
        if (c.game === props.game) notify(errorText(e.code), "error");
      } finally {
        actingId.value = "";
      }
    }

    async function rollback(row) {
      const targetGame = props.game;
      if (!targetGame || loading.value || actingId.value) return;
      actingId.value = row.id;
      try {
        kind.value = "activity";
        const old = row.payload;
        const versions = await request(`/gm/games/${encodeURIComponent(targetGame)}/activities/${encodeURIComponent(old.id)}/versions`);
        if (targetGame !== props.game) return;
        const latest = Math.max(...versions.map((v) => v.version), old.version);
        config.value = JSON.stringify({ ...old, version: latest + 1 }, null, 2);
        reason.value = "回滚到历史配置：" + old.id + " v" + old.version;
        notify("已载入历史配置，提交后需要审核", "info");
      } catch (e) {
        if (targetGame === props.game) notify(errorText(e.code), "error");
      } finally {
        actingId.value = "";
      }
    }

    async function stop(row) {
      const targetGame = props.game;
      if (!targetGame || loading.value || actingId.value) return;
      if (reason.value.trim().length < 3) {
        notify("停止活动前请先填写原因 / 工单（至少 3 个字符）", "error");
        return;
      }
      actingId.value = row.id;
      try {
        await request(`/gm/games/${encodeURIComponent(targetGame)}/activities/${encodeURIComponent(row.payload.id)}/stop`, {
          method: "POST",
          body: { reason: reason.value },
        });
        if (targetGame !== props.game) return;
        notify("活动已停止", "success");
        await load();
      } catch (e) {
        if (targetGame === props.game) notify(errorText(e.code), "error");
      } finally {
        actingId.value = "";
      }
    }

    async function announce() {
      const targetGame = props.game;
      if (!targetGame || busy.value) return;
      if (reason.value.trim().length < 3) {
        notify("发布公告前请先填写原因 / 工单（至少 3 个字符）", "error");
        return;
      }
      busy.value = true;
      try {
        await request(`/gm/games/${encodeURIComponent(targetGame)}/announcements/notice`, {
          method: "PUT",
          body: { body: notice.value, expires_at: Math.floor(Date.now() / 1000) + 86400, reason: reason.value },
        });
        if (targetGame !== props.game) return;
        notify("公告已发布，有效期 24 小时", "success");
      } catch (e) {
        if (targetGame === props.game) notify(errorText(e.code), "error");
      } finally {
        busy.value = false;
      }
    }

    function summary(row) {
      const p = row.payload || {};
      if (row.kind === "reward") {
        const list = Array.isArray(p.players) ? p.players : [];
        const total = list.length * (Number(p.amount) || 0);
        return `${list.length} 名玩家 · ${p.item} × ${count(p.amount)}（共 ${count(total)}）`;
      }
      return "活动 " + (p.id || "?") + " v" + (p.version || "?");
    }

    watch(
      () => props.awardTarget,
      (target) => {
        if (target && target.uid) {
          kind.value = "reward";
          players.value = target.uid;
          notify("已带入玩家 UID：" + target.uid, "info");
        }
      },
      { immediate: true },
    );

    watch(
      () => props.game,
      (next, prev) => {
        if (prev !== undefined && next !== prev) {
          rows.value = [];
          expanded.value = {};
          loadError.value = "";
          plan.value = null;
          confirmAction.value = null;
          actingId.value = "";
        }
        load();
      },
      { immediate: true },
    );
    watch(() => props.tick, load);
    onBeforeUnmount(() => activeAbort && activeAbort.abort());

    return {
      kind, reason, requestId, players, item, amount, config, brief, plan, notice,
      rows, loading, loadError, busy, actingId, expanded, confirmAction, confirmSelfAuthor, confirmDetails,
      canSubmit, canApprove, load, generate, submit, openConfirm, executeConfirm, rollback, stop, announce, summary,
      stamp, relative, requestState, count,
    };
  },
  template: `
  <div class="stack">
    <div class="banner" data-tone="warn">
      <icon name="alert" :size="14" />
      <span>当前游戏：<b>{{ gameName || game }}</b>（<span class="mono">{{ game }}</span>）。补发与活动发布需要另一位审核人批准；执行后的补发不可撤销。</span>
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
            <span class="field-label">申请原因 / 工单（创建申请与公告必填）</span>
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
            <button class="btn btn-primary" :disabled="!canSubmit || busy" @click="submit">提交申请</button>
            <button class="btn btn-ghost" :disabled="loading" @click="load">刷新记录</button>
            <span v-if="!canSubmit" class="small muted">当前角色（{{ role }}）只能审批，不能创建申请</span>
          </div>
        </div>
      </article>

      <article class="card">
        <div class="card-head"><div class="card-title">申请与执行记录</div><div class="card-note">{{ loadError ? '读取失败' : rows.length + ' 条' }}</div></div>
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
                      <button class="btn btn-soft btn-sm" :disabled="!canApprove || loading || !!actingId" @click="openConfirm(row,'approve')">{{ actingId === row.id ? '处理中…' : '核对批准' }}</button>
                      <button class="btn btn-quiet btn-sm" :disabled="loading || !!actingId" @click="openConfirm(row,'cancel')">撤销</button>
                    </template>
                    <template v-else-if="row.kind === 'activity' && row.state === 'executed'">
                      <button class="btn btn-quiet btn-sm" :disabled="loading || !!actingId" @click="rollback(row)">载入</button>
                      <button class="btn btn-quiet btn-sm" :disabled="loading || !!actingId" @click="stop(row)">停止</button>
                    </template>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
          <skeleton-rows v-if="loading" :rows="3" style="padding:12px" />
          <error-box v-else-if="loadError" :text="loadError" @retry="load" />
          <empty v-else-if="!rows.length" text="还没有申请记录" />
        </div>
      </article>
    </div>

    <article class="card">
      <div class="card-head"><div class="card-title">系统公告</div><div class="card-note">有效期 24 小时</div></div>
      <div class="card-body">
        <textarea class="textarea" v-model="notice" rows="3" placeholder="公告正文，玩家登录后可见"></textarea>
        <div class="toolbar" style="margin-top:10px">
          <button class="btn btn-soft" :disabled="busy" @click="announce">发布公告</button>
          <span class="small muted">需要填写上面的申请原因 / 工单</span>
        </div>
      </div>
    </article>

    <modal-dialog
      :open="!!confirmAction"
      :title="confirmAction && confirmAction.action === 'approve' ? '核对并批准执行' : '确认撤销申请'"
      :subtitle="confirmAction ? confirmAction.gameName + ' (' + confirmAction.game + ')' : ''"
      @close="!actingId && (confirmAction = null)"
    >
      <div v-if="confirmAction && confirmDetails" class="stack">
        <div class="banner" :data-tone="confirmAction.action === 'approve' ? 'warn' : ''">
          <icon name="alert" :size="14" />
          <span>{{ confirmAction.action === 'approve' ? '批准后将立即执行，不可撤销。请逐项核对以下内容。' : '撤销后该申请将终止，不会执行。' }}</span>
        </div>

        <dl class="kv">
          <div><dt>目标游戏</dt><dd><b>{{ confirmAction.gameName }}</b> <span class="mono muted">({{ confirmAction.game }})</span></dd></div>
          <div><dt>申请编号</dt><dd class="mono">{{ confirmAction.row.id }}</dd></div>
          <div><dt>申请人</dt><dd>{{ confirmAction.row.creator || '—' }}</dd></div>
          <div><dt>申请原因</dt><dd>{{ confirmAction.row.reason || '—' }}</dd></div>
        </dl>

        <template v-if="confirmDetails.kind === 'reward'">
          <hr class="hr" />
          <dl class="kv">
            <div><dt>发放道具</dt><dd class="mono"><b>{{ confirmDetails.item }}</b></dd></div>
            <div><dt>玩家人数</dt><dd><b>{{ confirmDetails.playerCount }} 人</b></dd></div>
            <div><dt>每人数额</dt><dd><b>{{ count(confirmDetails.perPlayer) }}</b></dd></div>
            <div><dt>发放总量</dt><dd><b style="color:var(--accent)">{{ count(confirmDetails.total) }}</b></dd></div>
          </dl>
          <div class="field">
            <span class="field-label">玩家名单（{{ confirmDetails.playerCount }} 人）</span>
            <div class="uid-list">
              <span v-for="uid in confirmDetails.players" :key="uid" class="pill pill-plain mono">{{ uid }}</span>
              <span v-if="!confirmDetails.players.length" class="small muted">无玩家</span>
            </div>
          </div>
        </template>

        <template v-else>
          <hr class="hr" />
          <dl class="kv">
            <div><dt>活动 ID</dt><dd class="mono"><b>{{ confirmDetails.id }}</b></dd></div>
            <div><dt>版本</dt><dd>v{{ confirmDetails.version }}</dd></div>
            <div><dt>类型 / 事件</dt><dd>{{ confirmDetails.activityKind }} · {{ confirmDetails.eventType }} (目标 {{ confirmDetails.target }})</dd></div>
            <div><dt>活动奖励</dt><dd class="mono">{{ confirmDetails.item }} × {{ confirmDetails.rewardAmount }}</dd></div>
            <div><dt>领奖方式</dt><dd>{{ confirmDetails.clientAllowed ? '允许客户端直领' : '仅服务端发放' }}</dd></div>
          </dl>
        </template>

        <div v-if="confirmSelfAuthor" class="banner" data-tone="warn">
          <icon name="alert" :size="14" />
          <span>此申请由当前账号（{{ confirmAction.row.creator }}）创建，需由另一位审核人批准。</span>
        </div>

        <label class="field">
          <span class="field-label">审批 / 撤销原因（独立填写，至少 3 个字符）</span>
          <input class="input" v-model="confirmAction.reason" placeholder="例如：已核对工单与名单，准予执行" />
        </label>

        <div class="toolbar" style="justify-content:flex-end;margin-top:4px">
          <button type="button" class="btn btn-ghost" :disabled="!!actingId" @click="confirmAction = null">取消</button>
          <button
            type="button"
            :class="confirmAction.action === 'approve' ? 'btn btn-primary' : 'btn btn-danger'"
            :disabled="!!actingId || (confirmAction.action === 'approve' && (!canApprove || confirmSelfAuthor))"
            @click="executeConfirm"
          >
            {{ actingId ? '执行中…' : confirmAction.action === 'approve' ? '确认批准并执行' : '确认撤销' }}
          </button>
        </div>
      </div>
    </modal-dialog>
  </div>`,
});
