import { defineComponent, ref, computed, watch, onBeforeUnmount } from "vue";
import { request } from "../api.js";
import { errorText, stamp, relative } from "../format.js";
import { Icon } from "../components/icons.js";
import { notify, Empty, ErrorBox, SkeletonRows } from "../components/ui.js";

export const OpsAgentView = defineComponent({
  name: "OpsAgentView",
  components: { Icon, Empty, ErrorBox, SkeletonRows },
  props: {
    game: String,
    gameName: String,
    role: String,
    tick: Number,
    initialTarget: Object,
  },
  emits: ["navigate"],
  setup(props, { emit }) {
    const question = ref("检查当前服务健康状态和近期异常日志，定位潜在故障根因");
    const playerId = ref("");
    const requestId = ref("");
    const activityId = ref("");
    const allowPropose = ref(false);
    const proposalItem = ref("wood");
    const proposalAmount = ref(5);

    const running = ref(false);
    const loadingRuns = ref(false);
    const runsError = ref("");
    const runs = ref([]);
    const activeRun = ref(null);
    const expandedSteps = ref({});

    // Runtime LLM API configuration (admin-editable; API key is write-only)
    const configOpen = ref(false);
    const configLoading = ref(false);
    const configSaving = ref(false);
    const configTesting = ref(false);
    const cfgProtocol = ref("openai");
    const cfgUrl = ref("");
    const cfgModel = ref("");
    const cfgApiKey = ref("");
    const cfgHasKey = ref(false);
    const cfgKeyMasked = ref("");
    const cfgConfigured = ref(false);
    const cfgCanEdit = ref(false);
    const cfgTestResult = ref(null);
    const MODELS_CACHE_KEY = "miniverse.opsAgent.models";
    let initialCachedModels = [];
    try {
      const raw = localStorage.getItem(MODELS_CACHE_KEY);
      if (raw) {
        const parsed = JSON.parse(raw);
        if (Array.isArray(parsed)) initialCachedModels = parsed.filter((x) => typeof x === "string");
      }
    } catch {}
    const cfgModels = ref(initialCachedModels);
    const cfgModelsLoading = ref(false);

    let generation = 0;
    let activeAbort = null;

    const canPropose = computed(() => props.role === "admin" || props.role === "operator");
    const isAdmin = computed(() => props.role === "admin" || cfgCanEdit.value);

    function applyConfigSnapshot(c) {
      if (!c) return;
      cfgProtocol.value = c.protocol || "openai";
      cfgUrl.value = c.url || "";
      cfgModel.value = c.model || "";
      cfgHasKey.value = Boolean(c.has_api_key);
      cfgKeyMasked.value = c.api_key_masked || "";
      cfgConfigured.value = Boolean(c.configured);
      cfgCanEdit.value = Boolean(c.can_edit);
    }

    async function loadConfig() {
      configLoading.value = true;
      try {
        const c = await request("/gm/system/ops-agent/config");
        applyConfigSnapshot(c);
      } catch (e) {
        // Ignore silent load errors if session just expired
      } finally {
        configLoading.value = false;
      }
    }

    function applyPreset(provider) {
      cfgTestResult.value = null;
      if (provider === "claude") {
        cfgProtocol.value = "anthropic";
        cfgUrl.value = "https://api.anthropic.com/v1/messages";
        cfgModel.value = "claude-sonnet-4-20250514";
      } else if (provider === "deepseek") {
        cfgProtocol.value = "openai";
        cfgUrl.value = "https://api.deepseek.com/v1/chat/completions";
        cfgModel.value = "deepseek-chat";
      } else if (provider === "openai") {
        cfgProtocol.value = "openai";
        cfgUrl.value = "https://api.openai.com/v1/chat/completions";
        cfgModel.value = "gpt-4o-mini";
      } else if (provider === "qwen") {
        cfgProtocol.value = "openai";
        cfgUrl.value = "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions";
        cfgModel.value = "qwen-plus";
      } else if (provider === "ollama") {
        cfgProtocol.value = "openai";
        cfgUrl.value = "http://host.docker.internal:11434/v1/chat/completions";
        cfgModel.value = "qwen2.5:7b";
      }
    }

    async function saveConfig() {
      if (!isAdmin.value || configSaving.value) return;
      configSaving.value = true;
      cfgTestResult.value = null;
      try {
        const updated = await request("/gm/system/ops-agent/config", {
          method: "PUT",
          body: {
            protocol: cfgProtocol.value,
            url: cfgUrl.value.trim(),
            model: cfgModel.value.trim(),
            api_key: cfgApiKey.value.trim(),
          },
        });
        applyConfigSnapshot(updated);
        cfgApiKey.value = "";
        notify(updated.configured ? "LLM 接口配置已保存并实时生效" : "已切换回沙盒规则引擎模式", "success");
      } catch (e) {
        notify(errorText(e.code), "error");
      } finally {
        configSaving.value = false;
      }
    }

    async function clearConfig() {
      if (!isAdmin.value || configSaving.value) return;
      configSaving.value = true;
      cfgTestResult.value = null;
      try {
        const updated = await request("/gm/system/ops-agent/config", {
          method: "PUT",
          body: { protocol: "openai", url: "", model: "", api_key: "", clear_api_key: true },
        });
        applyConfigSnapshot(updated);
        cfgApiKey.value = "";
        notify("已清空外部 LLM 配置，恢复沙盒规则引擎", "success");
      } catch (e) {
        notify(errorText(e.code), "error");
      } finally {
        configSaving.value = false;
      }
    }

    async function testConfig() {
      if (!isAdmin.value || configTesting.value) return;
      configTesting.value = true;
      cfgTestResult.value = null;
      try {
        const res = await request("/gm/system/ops-agent/test", {
          method: "POST",
          body: {
            protocol: cfgProtocol.value,
            url: cfgUrl.value.trim(),
            model: cfgModel.value.trim(),
            api_key: cfgApiKey.value.trim(),
          },
        });
        cfgTestResult.value = res;
        if (res.ok) {
          notify(`连通成功（${res.latency_ms} ms）`, "success");
        } else {
          notify("连通失败：" + errorText(res.error) + (res.detail ? `（${res.detail}）` : ""), "error");
        }
      } catch (e) {
        notify(errorText(e.code), "error");
      } finally {
        configTesting.value = false;
      }
    }

    async function fetchModels() {
      if (!isAdmin.value || cfgModelsLoading.value) return;
      cfgModelsLoading.value = true;
      cfgTestResult.value = null;
      try {
        const res = await request("/gm/system/ops-agent/models", {
          method: "POST",
          body: {
            protocol: cfgProtocol.value,
            url: cfgUrl.value.trim(),
            api_key: cfgApiKey.value.trim(),
            model: cfgModel.value.trim(),
            save: true,
          },
        });
        if (res.ok && Array.isArray(res.models)) {
          cfgModels.value = res.models;
          try {
            localStorage.setItem(MODELS_CACHE_KEY, JSON.stringify(res.models));
          } catch {}
          if (res.config) {
            applyConfigSnapshot(res.config);
            if (res.saved) cfgApiKey.value = "";
          }
          if (res.models.length > 0 && !cfgModel.value.trim()) {
            cfgModel.value = res.models[0];
          }
          const suffix = res.saved ? "，配置已自动保存" : "";
          notify(res.models.length > 0 ? `已获取 ${res.models.length} 个可用模型（${res.latency_ms} ms）${suffix}` : "接口返回成功，但模型列表为空", res.models.length > 0 ? "success" : "info");
        } else {
          cfgTestResult.value = { ok: false, latency_ms: res.latency_ms || 0, error: res.error, detail: res.detail || "" };
          notify("获取模型失败：" + errorText(res.error) + (res.detail ? `（${res.detail}）` : ""), "error");
        }
      } catch (e) {
        notify(errorText(e.code), "error");
      } finally {
        cfgModelsLoading.value = false;
      }
    }

    async function onPickModel(nextModel) {
      cfgModel.value = nextModel;
      if (isAdmin.value && (cfgUrl.value.trim() || cfgConfigured.value) && nextModel) {
        await saveConfig();
      }
    }

    function applyScenario(kind) {
      if (kind === "health") {
        playerId.value = "";
        requestId.value = "";
        activityId.value = "";
        allowPropose.value = false;
        question.value = "检查当前服务健康、数据库连接池、RuleCore 连通性与近期 5xx/429 异常日志";
      } else if (kind === "reward") {
        activityId.value = "";
        question.value = "排查该玩家为什么完成活动或任务后仍未收到道具奖励，定位到具体环节";
      } else if (kind === "trace") {
        question.value = "根据提供的 X-Request-ID 追踪全链路异常日志与审计记录并分析根因";
      }
    }

    async function loadRuns() {
      const targetGame = props.game;
      const run = ++generation;
      if (activeAbort) activeAbort.abort();
      if (!targetGame) {
        runs.value = [];
        runsError.value = "";
        loadingRuns.value = false;
        return;
      }
      activeAbort = new AbortController();
      loadingRuns.value = true;
      runsError.value = "";
      try {
        const list = await request(`/gm/games/${encodeURIComponent(targetGame)}/ops-agent/runs`, { signal: activeAbort.signal });
        if (run !== generation || targetGame !== props.game) return;
        runs.value = Array.isArray(list) ? list : [];
      } catch (e) {
        if (e && e.name === "AbortError") return;
        if (run !== generation || targetGame !== props.game) return;
        runsError.value = errorText(e.code);
        runs.value = [];
      } finally {
        if (run === generation) loadingRuns.value = false;
      }
    }

    async function selectRun(row) {
      const targetGame = props.game;
      if (!targetGame || !row || !row.run_id) return;
      try {
        const detail = await request(`/gm/games/${encodeURIComponent(targetGame)}/ops-agent/runs/${encodeURIComponent(row.run_id)}`);
        if (targetGame !== props.game) return;
        activeRun.value = detail;
        expandedSteps.value = {};
      } catch (e) {
        if (targetGame === props.game) notify(errorText(e.code), "error");
      }
    }

    async function diagnose() {
      const targetGame = props.game;
      if (!targetGame || running.value) return;
      if (question.value.trim().length < 2) {
        notify("请输入要排查的问题描述", "error");
        return;
      }
      running.value = true;
      try {
        const result = await request(`/gm/games/${encodeURIComponent(targetGame)}/ops-agent/diagnose`, {
          method: "POST",
          body: {
            question: question.value.trim(),
            player_id: playerId.value.trim(),
            request_id: requestId.value.trim(),
            activity_id: activityId.value.trim(),
            allow_propose: canPropose.value && allowPropose.value,
            proposal_item: proposalItem.value.trim(),
            proposal_amount: Number(proposalAmount.value) || 0,
          },
        });
        if (targetGame !== props.game) return;
        activeRun.value = result;
        expandedSteps.value = {};
        notify(result.proposal_request_id ? "排障完成，已生成待审工单 " + result.proposal_request_id : "排障诊断完成", "success");
        await loadRuns();
      } catch (e) {
        if (targetGame === props.game) notify(errorText(e.code), "error");
      } finally {
        running.value = false;
      }
    }

    watch(
      () => props.initialTarget,
      (target) => {
        if (!target) return;
        if (target.player_id !== undefined) playerId.value = target.player_id || "";
        if (target.request_id !== undefined) requestId.value = target.request_id || "";
        if (target.activity_id !== undefined) activityId.value = target.activity_id || "";
        if (target.question) question.value = target.question;
      },
      { immediate: true },
    );

    watch(
      () => props.game,
      (next, prev) => {
        if (prev !== undefined && next !== prev) {
          activeRun.value = null;
          runs.value = [];
          expandedSteps.value = {};
        }
        loadRuns();
      },
      { immediate: true },
    );
    watch(() => props.tick, () => { loadRuns(); loadConfig(); });
    loadConfig();
    onBeforeUnmount(() => activeAbort && activeAbort.abort());

    return {
      question, playerId, requestId, activityId, allowPropose, proposalItem, proposalAmount,
      running, loadingRuns, runsError, runs, activeRun, expandedSteps, canPropose,
      configOpen, configLoading, configSaving, configTesting,
      cfgProtocol, cfgUrl, cfgModel, cfgApiKey, cfgHasKey, cfgKeyMasked, cfgConfigured, isAdmin, cfgTestResult,
      cfgModels, cfgModelsLoading,
      applyScenario, applyPreset, loadConfig, saveConfig, clearConfig, testConfig, fetchModels, onPickModel,
      diagnose, loadRuns, selectRun, stamp, relative, errorText,
      goOperations: () => emit("navigate", "operations"),
    };
  },
  template: `
  <div class="stack">
    <div class="banner" data-tone="info">
      <icon name="spark" :size="14" />
      <span>AI Ops Agent 通过标准 <b>MCP Tools</b> 关联玩家数据、<code>ops_events</code> 日志与运行时指标。AI 无权直接发奖或批准变更，受控提案仅进入 <code>pending</code> 审批队列。</span>
    </div>

    <article class="card">
      <div class="card-head" style="cursor:pointer" @click="configOpen = !configOpen">
        <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
          <div class="card-title">LLM 大模型 API 配置</div>
          <span class="pill" :data-tone="cfgConfigured ? 'ok' : 'warn'">
            {{ cfgConfigured ? ('已接入外部模型：' + cfgModel + ' (' + (cfgProtocol === 'anthropic' ? 'Claude' : 'OpenAI') + ')') : '未配置外部 LLM（当前使用沙盒规则引擎）' }}
          </span>
          <span v-if="cfgHasKey" class="small muted mono">Key: {{ cfgKeyMasked }}</span>
        </div>
        <button type="button" class="btn btn-ghost btn-sm" @click.stop="configOpen = !configOpen">
          {{ configOpen ? '收起配置' : (isAdmin ? '配置 / 切换模型接口' : '查看配置') }}
        </button>
      </div>
      <div v-if="configOpen" class="card-body">
        <div v-if="isAdmin" class="toolbar" style="margin-bottom:10px;flex-wrap:wrap">
          <span class="small muted">快捷填充：</span>
          <button type="button" class="btn btn-ghost btn-sm" @click="applyPreset('claude')">Claude (Anthropic)</button>
          <button type="button" class="btn btn-ghost btn-sm" @click="applyPreset('deepseek')">DeepSeek</button>
          <button type="button" class="btn btn-ghost btn-sm" @click="applyPreset('openai')">OpenAI</button>
          <button type="button" class="btn btn-ghost btn-sm" @click="applyPreset('qwen')">通义千问 (DashScope)</button>
          <button type="button" class="btn btn-ghost btn-sm" @click="applyPreset('ollama')">本地 Ollama</button>
        </div>
        <div class="grid-2" style="gap:10px">
          <label class="field">
            <span class="field-label">兼容协议</span>
            <select class="select" v-model="cfgProtocol" :disabled="!isAdmin">
              <option value="openai">OpenAI 兼容（/v1/chat/completions）</option>
              <option value="anthropic">Anthropic Claude（/v1/messages）</option>
            </select>
          </label>
          <label class="field">
            <span class="field-label">模型名称（Model）</span>
            <div style="display:flex;gap:8px;align-items:center">
              <input class="input mono" style="flex:1" v-model="cfgModel" :disabled="!isAdmin" :placeholder="cfgProtocol === 'anthropic' ? 'claude-sonnet-4-20250514' : 'deepseek-chat / gpt-4o-mini'" />
              <button v-if="isAdmin" type="button" class="btn btn-ghost btn-sm" style="white-space:nowrap" :disabled="cfgModelsLoading || (!cfgUrl.trim() && !cfgConfigured)" @click="fetchModels">
                {{ cfgModelsLoading ? '获取中…' : '获取模型' }}
              </button>
            </div>
            <select v-if="cfgModels.length" class="select mono" style="margin-top:6px" :value="cfgModels.includes(cfgModel) ? cfgModel : ''" @change="onPickModel($event.target.value)" :disabled="!isAdmin">
              <option value="" disabled>从已获取的 {{ cfgModels.length }} 个模型中选择…</option>
              <option v-for="m in cfgModels" :key="m" :value="m">{{ m }}</option>
            </select>
          </label>
          <label class="field" style="grid-column:1 / -1">
            <span class="field-label">API 接口地址（支持直接填 Base URL 或完整 {{ cfgProtocol === 'anthropic' ? '/v1/messages' : '/v1/chat/completions' }}）</span>
            <input class="input mono" v-model="cfgUrl" :disabled="!isAdmin" :placeholder="cfgProtocol === 'anthropic' ? 'https://api.anthropic.com/v1/messages' : 'https://api.deepseek.com/v1/chat/completions'" />
          </label>
          <label class="field" style="grid-column:1 / -1">
            <span class="field-label">API Key（{{ cfgProtocol === 'anthropic' ? 'x-api-key' : 'Bearer Token' }}，持久化保存在服务端，绝不回显明文）</span>
            <input class="input mono" type="password" autocomplete="off" v-model="cfgApiKey" :disabled="!isAdmin" :placeholder="cfgHasKey ? ('已配置 ' + cfgKeyMasked + '，留空保存则保留原 Key') : (cfgProtocol === 'anthropic' ? 'sk-ant-...' : 'sk-...（本地服务可留空）')" />
          </label>
        </div>
        <div v-if="cfgTestResult" class="banner" :data-tone="cfgTestResult.ok ? 'ok' : 'error'" style="margin-top:10px">
          <span v-if="cfgTestResult.ok">接口连通成功（耗时 {{ cfgTestResult.latency_ms }} ms），模型回复：<code>{{ cfgTestResult.reply_preview || 'OK' }}</code></span>
          <span v-else>请求失败（耗时 {{ cfgTestResult.latency_ms }} ms）：{{ errorText(cfgTestResult.error) }}<template v-if="cfgTestResult.detail"> — <code class="mono">{{ cfgTestResult.detail }}</code></template></span>
        </div>
        <div class="toolbar" style="margin-top:12px;flex-wrap:wrap">
          <template v-if="isAdmin">
            <button type="button" class="btn btn-primary btn-sm" :disabled="configSaving" @click="saveConfig">
              {{ configSaving ? '正在保存…' : '保存并实时生效' }}
            </button>
            <button type="button" class="btn btn-ghost btn-sm" :disabled="configTesting || (!cfgUrl.trim() && !cfgConfigured)" @click="testConfig">
              {{ configTesting ? '正在探测…' : '测试连通性' }}
            </button>
            <button v-if="cfgConfigured || cfgHasKey" type="button" class="btn btn-ghost btn-sm" :disabled="configSaving" @click="clearConfig">
              清空并切回沙盒引擎
            </button>
          </template>
          <span v-else class="small muted">当前角色（{{ role }}）仅可查看状态；请使用 admin 账号修改外部 LLM 接口配置。</span>
        </div>
        <p class="small muted" style="margin-top:8px">
          说明：配置会持久保存到 MySQL 数据库，重启容器与桌面端均会保留；点击「获取模型」或在下拉框切换模型时会自动保存。若连接宿主机本地服务（如本地代理 / Ollama），在沙盒模式下可直接填 <code>http://127.0.0.1:端口/v1</code> 或 <code>http://host.docker.internal:端口/v1</code>（需确保本地服务未拒绝 Docker 宿主机访问）。
        </p>
      </div>
    </article>

    <div class="grid-2">
      <article class="card">
        <div class="card-head">
          <div class="card-title">发起智能排障</div>
          <div class="card-note">{{ gameName || game }}</div>
        </div>
        <div class="card-body">
          <div class="toolbar" style="margin-bottom:10px;flex-wrap:wrap">
            <button type="button" class="btn btn-ghost btn-sm" @click="applyScenario('health')">服务健康排查</button>
            <button type="button" class="btn btn-ghost btn-sm" @click="applyScenario('reward')">玩家奖励未到账</button>
            <button type="button" class="btn btn-ghost btn-sm" @click="applyScenario('trace')">Request ID 追踪</button>
          </div>

          <label class="field">
            <span class="field-label">故障现象 / 排查问题</span>
            <textarea class="textarea" v-model="question" rows="3" placeholder="例如：玩家完成活动后没收到木材奖励，或 RuleCore 返回 503"></textarea>
          </label>

          <div class="grid-2" style="margin-top:10px;gap:10px">
            <label class="field">
              <span class="field-label">玩家 UID（可选）</span>
              <input class="input mono" v-model="playerId" placeholder="player_id" />
            </label>
            <label class="field">
              <span class="field-label">X-Request-ID（可选）</span>
              <input class="input mono" v-model="requestId" placeholder="24 位十六进制 ID" />
            </label>
          </div>

          <div style="margin-top:12px;padding:10px;border:1px solid var(--line);border-radius:var(--r-md);background:var(--surface-2)">
            <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
              <input type="checkbox" v-model="allowPropose" :disabled="!canPropose" />
              <span class="small"><b>允许 AI 生成受控待审提案</b>（仅创建 <code>pending</code> 工单，需另一位审核人批准）</span>
            </label>
            <div v-if="!canPropose" class="small muted" style="margin-top:4px">当前角色（{{ role }}）仅可运行只读诊断工具。</div>
            <div v-if="canPropose && allowPropose" class="grid-2" style="margin-top:10px;gap:10px">
              <label class="field"><span class="field-label">补偿道具 ID（可选）</span><input class="input mono" v-model="proposalItem" placeholder="wood" /></label>
              <label class="field"><span class="field-label">补偿数量</span><input class="input" type="number" min="1" max="1000000" v-model="proposalAmount" /></label>
              <label class="field" style="grid-column:1 / -1"><span class="field-label">或待停用异常活动 ID（可选）</span><input class="input mono" v-model="activityId" placeholder="填 activity_id 时优先生成停用活动提案" /></label>
            </div>
          </div>

          <div class="toolbar" style="margin-top:14px">
            <button type="button" class="btn btn-primary" :disabled="running" @click="diagnose">
              <icon name="spark" :size="14" />{{ running ? '正在通过 MCP 排障…' : '开始智能诊断' }}
            </button>
            <button type="button" class="btn btn-ghost" :disabled="loadingRuns" @click="loadRuns">刷新历史</button>
          </div>
        </div>
      </article>

      <article class="card">
        <div class="card-head">
          <div class="card-title">诊断结果与 MCP 证据链</div>
          <div v-if="activeRun" class="toolbar">
            <span class="pill" :data-tone="activeRun.real_ai ? 'ok' : 'info'">{{ activeRun.real_ai ? '真实 AI Agent' : '确定性 Playbook · 真实 MCP' }}</span>
            <span class="pill pill-plain">{{ activeRun.duration_ms }} ms</span>
          </div>
        </div>
        <div class="card-body">
          <empty v-if="!activeRun" text="提交一次排障请求，或点击下方历史记录查看完整的 MCP 工具调用轨迹" />
          <div v-else class="stack">
            <div v-if="activeRun.proposal_request_id" class="banner" data-tone="warn">
              <icon name="alert" :size="14" />
              <span>已生成待审受控工单 <b class="mono">{{ activeRun.proposal_request_id }}</b>（<code>pending</code>）</span>
              <button type="button" class="link-btn" style="margin-left:auto" @click="goOperations">前往运营台审批 →</button>
            </div>

            <dl class="kv">
              <div><dt>运行模式 / 模型</dt><dd class="mono">{{ activeRun.mode }} · {{ activeRun.model }}</dd></div>
              <div><dt>状态 / 工具步数</dt><dd>{{ activeRun.status }} · {{ activeRun.steps_count }} 步</dd></div>
            </dl>

            <div>
              <div class="field-label">根因判断</div>
              <p class="small" style="margin-top:4px">{{ activeRun.root_cause }}</p>
            </div>
            <div>
              <div class="field-label">证据摘要</div>
              <p class="small muted" style="margin-top:4px">{{ activeRun.summary }}</p>
            </div>
            <div>
              <div class="field-label">处置建议</div>
              <p class="small" style="margin-top:4px">{{ activeRun.recommendation }}</p>
            </div>

            <hr class="hr" />
            <div class="field-label">MCP 工具调用轨迹（{{ (activeRun.steps || []).length }} 步）</div>
            <div class="timeline">
              <div v-for="step in (activeRun.steps || [])" :key="step.step_index" class="timeline-row" style="align-items:flex-start">
                <span class="timeline-dot" :data-tone="step.is_error ? 'danger' : 'ok'" style="margin-top:6px"></span>
                <div style="flex:1;min-width:0">
                  <div class="spread">
                    <span class="small"><b>#{{ step.step_index }} <span class="mono">{{ step.tool_name }}</span></b> <span class="muted">· {{ step.duration_ms }} ms</span></span>
                    <button type="button" class="link-btn" @click="expandedSteps[step.step_index] = !expandedSteps[step.step_index]">
                      {{ expandedSteps[step.step_index] ? '收起' : '查看入参/返回' }}
                    </button>
                  </div>
                  <div v-if="expandedSteps[step.step_index]" style="margin-top:6px">
                    <div class="small muted">Arguments:</div>
                    <pre class="raw">{{ step.arguments_json }}</pre>
                    <div class="small muted" style="margin-top:6px">Result:</div>
                    <pre class="raw">{{ step.result_summary }}</pre>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </article>
    </div>

    <article class="card">
      <div class="card-head">
        <div class="card-title">历史排障记录</div>
        <div class="card-note">{{ runsError ? '读取失败' : runs.length + ' 条' }}</div>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead><tr><th>时间</th><th>操作人</th><th>模式</th><th>问题</th><th>步数</th><th>生成工单</th></tr></thead>
          <tbody>
            <tr v-for="row in runs" :key="row.run_id" data-clickable="true" @click="selectRun(row)">
              <td class="small muted">{{ stamp(row.created_at) }}</td>
              <td class="mono small">{{ row.admin_id }}</td>
              <td><span class="pill" :data-tone="row.real_ai ? 'ok' : 'info'">{{ row.mode }}</span></td>
              <td class="small">{{ row.question }}</td>
              <td class="num">{{ row.steps_count }}</td>
              <td class="mono small">{{ row.proposal_request_id || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <skeleton-rows v-if="loadingRuns" :rows="3" style="padding:12px" />
      <error-box v-else-if="runsError" :text="runsError" @retry="loadRuns" />
      <empty v-else-if="!runs.length" text="还没有智能排障记录" />
    </article>
  </div>`,
});
