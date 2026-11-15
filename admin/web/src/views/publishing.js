import { defineComponent, ref, watch, computed, onBeforeUnmount } from "vue";
import { request } from "../api.js";
import { Icon } from "../components/icons.js";
import { ModalDialog, ErrorBox, SkeletonRows, notify } from "../components/ui.js";
import { errorText } from "../format.js";

const STASH_PREFIX = "miniverse.gm.publishing.draft:";

function readStash(game) {
  if (!game) return null;
  try {
    const raw = sessionStorage.getItem(STASH_PREFIX + game);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

function writeStash(game, data) {
  if (!game) return;
  try {
    sessionStorage.setItem(STASH_PREFIX + game, JSON.stringify(data));
  } catch {
    /* ignore storage quota errors */
  }
}

function clearStash(game) {
  if (!game) return;
  try {
    sessionStorage.removeItem(STASH_PREFIX + game);
  } catch {
    /* ignore */
  }
}

export const PublishingView = defineComponent({
  name: "PublishingView",
  components: { Icon, ModalDialog, ErrorBox, SkeletonRows },
  props: { game: String, gameName: String, role: String, username: String, tick: Number },
  emits: ["dirty-change"],
  setup(props, { emit }) {
    const loading = ref(false);
    const loadError = ref("");
    const busy = ref(false);
    const state = ref(null);
    const draft = ref({});
    const savedDraft = ref("");
    const restoredStash = ref(false);
    const serverChangedWhileDirty = ref(false);
    const reloadConfirmOpen = ref(false);
    const write = computed(() => ["admin", "operator"].includes(props.role));
    const approve = computed(() => ["admin", "reviewer"].includes(props.role));
    const dirty = computed(() => !!state.value && JSON.stringify(draft.value) !== savedDraft.value);
    const selfAuthoredSubmitted = computed(
      () => !!(state.value && state.value.submitted && state.value.draft_author && props.username && state.value.draft_author === props.username),
    );
    let generation = 0;
    let activeAbort = null;
    let loadedGame = "";

    const blank = () => ({
      name: "",
      summary: "",
      description: "",
      category: "休闲",
      icon: "✦",
      accent: "sky",
      version: "1.0.0",
      entry_url: "",
      companion_level: "context",
    });

    async function load({ force = false, ignoreStash = false } = {}) {
      const targetGame = props.game;
      const run = ++generation;
      if (activeAbort) activeAbort.abort();
      if (!targetGame) {
        state.value = null;
        loadError.value = "";
        loading.value = false;
        return;
      }
      const sameGameDirty = !force && loadedGame === targetGame && dirty.value;
      activeAbort = new AbortController();
      if (!sameGameDirty) loading.value = true;
      loadError.value = "";
      try {
        const value = await request(`/gm/games/${encodeURIComponent(targetGame)}/listing`, { signal: activeAbort.signal });
        if (run !== generation || targetGame !== props.game) return;
        const serverBase = { ...blank(), ...(value.draft || value.published || {}) };
        const serverBaseJson = JSON.stringify(serverBase);

        if (sameGameDirty) {
          if (state.value && state.value.draft_hash !== value.draft_hash) {
            serverChangedWhileDirty.value = true;
          }
          state.value = value;
          savedDraft.value = serverBaseJson;
          return;
        }

        loadedGame = targetGame;
        state.value = value;
        savedDraft.value = serverBaseJson;
        serverChangedWhileDirty.value = false;

        if (!ignoreStash) {
          const stashed = readStash(targetGame);
          if (stashed && stashed.draft && JSON.stringify(stashed.draft) !== serverBaseJson) {
            draft.value = { ...blank(), ...stashed.draft };
            restoredStash.value = true;
            return;
          }
        }
        clearStash(targetGame);
        draft.value = serverBase;
        restoredStash.value = false;
      } catch (e) {
        if (e && e.name === "AbortError") return;
        if (run !== generation || targetGame !== props.game) return;
        if (!sameGameDirty) {
          state.value = null;
          loadError.value = errorText(e.code);
        }
        notify(errorText(e.code), "error");
      } finally {
        if (run === generation) loading.value = false;
      }
    }

    function onReloadClick() {
      if (dirty.value) {
        reloadConfirmOpen.value = true;
        return;
      }
      load({ force: true, ignoreStash: true });
    }

    function confirmDiscardAndReload() {
      reloadConfirmOpen.value = false;
      clearStash(props.game);
      restoredStash.value = false;
      serverChangedWhileDirty.value = false;
      load({ force: true, ignoreStash: true });
    }

    async function act(action) {
      if (busy.value || !props.game) return;
      if ((action === "submit" || action === "publish") && dirty.value) {
        notify("请先保存草稿，再提交或审核发布。", "error");
        return;
      }
      if (action === "publish" && selfAuthoredSubmitted.value) {
        notify("需要由另一位有审核权限的成员审核并发布。", "error");
        return;
      }
      busy.value = true;
      const targetGame = props.game;
      const draftHash = state.value?.draft_hash;
      try {
        const path = `/gm/games/${encodeURIComponent(targetGame)}/listing`;
        if (action === "save") {
          await request(path, { method: "PUT", body: draft.value });
          clearStash(targetGame);
          restoredStash.value = false;
          serverChangedWhileDirty.value = false;
        } else {
          await request(`${path}/${action}`, { method: "POST", body: action === "unpublish" ? {} : { draft_hash: draftHash } });
        }
        notify(
          { save: "草稿已保存", submit: "已提交审核", publish: "版本已发布到玩家平台", unpublish: "已从玩家平台下架" }[action],
          "success",
        );
        if (targetGame === props.game) await load({ force: true, ignoreStash: true });
      } catch (e) {
        notify(e.code === "listing_draft_changed" ? "草稿已被修改，请重新加载并查看最新内容后再操作。" : errorText(e.code), "error");
      } finally {
        busy.value = false;
      }
    }

    watch(
      () => JSON.stringify(draft.value),
      () => {
        if (!state.value || !props.game) return;
        if (dirty.value) {
          writeStash(props.game, { draft: draft.value, baseJson: savedDraft.value, updatedAt: Date.now() });
        } else {
          clearStash(props.game);
          restoredStash.value = false;
        }
      },
    );

    watch(
      dirty,
      (isDirty) => emit("dirty-change", isDirty),
      { immediate: true },
    );

    function onBeforeUnload(event) {
      if (!dirty.value) return;
      event.preventDefault();
      event.returnValue = "";
    }
    window.addEventListener("beforeunload", onBeforeUnload);

    watch(
      () => props.game,
      (next, prev) => {
        if (prev !== undefined && next !== prev) {
          loadedGame = "";
          state.value = null;
          loadError.value = "";
          restoredStash.value = false;
          serverChangedWhileDirty.value = false;
          reloadConfirmOpen.value = false;
        }
        load();
      },
      { immediate: true },
    );
    watch(() => props.tick, () => load());

    onBeforeUnmount(() => {
      window.removeEventListener("beforeunload", onBeforeUnload);
      if (activeAbort) activeAbort.abort();
      emit("dirty-change", false);
    });

    return {
      state, draft, loading, loadError, busy, write, approve, dirty, restoredStash,
      serverChangedWhileDirty, reloadConfirmOpen, selfAuthoredSubmitted,
      load, onReloadClick, confirmDiscardAndReload, act,
    };
  },
  template: `
  <div class="stack">
    <article class="card">
      <div class="card-head">
        <div class="card-title">玩家平台 · 游戏发行（{{ gameName || game }}）</div>
        <div class="toolbar">
          <span class="pill" :data-tone="dirty ? 'warn' : state && state.submitted ? 'info' : ''">{{ dirty ? '有未保存更改' : state && state.submitted ? '待审核' : '编辑草稿' }}</span>
          <button type="button" class="btn btn-ghost btn-sm" @click="onReloadClick" :disabled="busy || loading">重新加载</button>
        </div>
      </div>
      <div class="card-body">
        <p class="muted">配置玩家可见的资料与游戏入口。草稿保存后需提交，由另一位有审核权限的成员发布。未保存的编辑按游戏本地暂存。</p>
        <p v-if="state && state.published" class="small" style="margin-top:6px">当前发布：{{ state.published.name }} · {{ state.published.version }}</p>
        <p v-else class="small muted" style="margin-top:6px">当前没有已发布版本。</p>

        <div v-if="restoredStash" class="banner" data-tone="warn" style="margin-top:12px">
          <icon name="alert" :size="14" />
          <span>已恢复「{{ gameName || game }}」本地暂存的未保存草稿。</span>
          <button type="button" class="link-btn" style="margin-left:auto" @click="confirmDiscardAndReload">放弃暂存并恢复</button>
        </div>

        <div v-if="serverChangedWhileDirty" class="banner" data-tone="warn" style="margin-top:8px">
          <icon name="alert" :size="14" />
          <span>服务端草稿已更新，当前保留您的本地编辑。可点右上角“重新加载”对比或恢复。</span>
        </div>

        <skeleton-rows v-if="loading" :rows="5" style="margin-top:12px" />
        <error-box v-else-if="loadError" :text="loadError" @retry="load({ force: true })" />
        <form v-else-if="state" @submit.prevent="act('save')" class="stack" style="margin-top:12px">
          <div class="grid-2">
            <label class="field"><span class="field-label">游戏名称</span><input class="input" v-model="draft.name" maxlength="64" required :disabled="!write || busy" /></label>
            <label class="field"><span class="field-label">分类</span><input class="input" v-model="draft.category" maxlength="32" required :disabled="!write || busy" /></label>
          </div>
          <label class="field"><span class="field-label">一句话介绍</span><input class="input" v-model="draft.summary" maxlength="256" :disabled="!write || busy" /></label>
          <label class="field"><span class="field-label">详细介绍</span><textarea class="input" v-model="draft.description" maxlength="8000" rows="5" :disabled="!write || busy"></textarea></label>
          <div class="grid-2">
            <label class="field"><span class="field-label">版本</span><input class="input" v-model="draft.version" maxlength="32" required :disabled="!write || busy" /></label>
            <label class="field"><span class="field-label">网页游戏入口</span><input class="input mono" v-model="draft.entry_url" placeholder="/games/your-game/" maxlength="256" required :disabled="!write || busy" /></label>
          </div>
          <div class="grid-2">
            <label class="field"><span class="field-label">封面符号</span><input class="input" v-model="draft.icon" maxlength="16" :disabled="!write || busy" /></label>
            <label class="field"><span class="field-label">封面主题</span><select class="select" v-model="draft.accent" :disabled="!write || busy"><option value="sky">天空</option><option value="cyan">青蓝</option><option value="violet">星紫</option><option value="mint">薄荷</option><option value="green">森林</option><option value="amber">暖阳</option><option value="rose">玫瑰</option><option value="blue">深蓝</option></select></label>
          </div>
          <label class="field"><span class="field-label">伙伴支持</span><select class="select" v-model="draft.companion_level" :disabled="!write || busy"><option value="basic">资料问答</option><option value="aware">当前场景感知</option><option value="context">游戏状态感知</option><option value="interactive">游戏互动</option></select></label>
          <p class="small muted">入口仅支持本站 /games/ 下的网页游戏路径。当前版本提供网页发行，原生包体安装不属于此入口。</p>
          <div class="toolbar" style="flex-wrap:wrap">
            <button v-if="write" class="btn btn-primary" type="submit" :disabled="busy">保存草稿</button>
            <button v-if="write" class="btn btn-ghost" type="button" @click="act('submit')" :disabled="busy || dirty || !state.draft || state.submitted">提交审核</button>
            <button v-if="approve" class="btn btn-primary" type="button" @click="act('publish')" :disabled="busy || dirty || !state.submitted || selfAuthoredSubmitted">审核并发布</button>
            <button v-if="approve" class="btn btn-ghost" type="button" @click="act('unpublish')" :disabled="busy || !state.published">下架</button>
          </div>
          <p v-if="!write" class="small muted">当前角色（{{ role }}）仅支持只读查看或审核，不能修改草稿。</p>
          <p v-if="dirty" class="small" style="color:var(--warn)">有未保存的修改（已按游戏本地暂存），请先保存草稿再提交或发布。</p>
          <p v-if="selfAuthoredSubmitted" class="small" style="color:var(--warn)">当前草稿由您（{{ state.draft_author }}）保存，需由另一位有审核权限的成员审核并发布。</p>
          <p v-else-if="approve && !state.submitted && !dirty" class="small muted">需要作者先保存草稿并提交审核，才可在此审核发布。</p>
          <p v-if="state.draft_author" class="small muted">草稿作者：{{ state.draft_author }}。草稿作者与发布者必须分离；修改草稿会重新进入待提交状态。</p>
        </form>
      </div>
    </article>

    <modal-dialog
      :open="reloadConfirmOpen"
      title="确认放弃未保存的修改？"
      :subtitle="gameName || game"
      @close="reloadConfirmOpen = false"
    >
      <div class="stack">
        <p class="small">重新加载将放弃「{{ gameName || game }}」当前所有未保存的本地编辑与暂存，恢复为服务端最新草稿或已发布内容。</p>
        <div class="toolbar" style="justify-content:flex-end">
          <button type="button" class="btn btn-ghost" @click="reloadConfirmOpen = false">保留编辑</button>
          <button type="button" class="btn btn-danger" @click="confirmDiscardAndReload">放弃修改并重新加载</button>
        </div>
      </div>
    </modal-dialog>
  </div>`,
});
