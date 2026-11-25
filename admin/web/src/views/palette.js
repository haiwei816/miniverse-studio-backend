import { defineComponent, ref, computed, watch, nextTick, onBeforeUnmount } from "vue";
import { Icon } from "../components/icons.js";
import { trapFocus } from "../components/ui.js";

const PAGES = [
  { id: "publishing", label: "游戏发行", icon: "operations" },
  { id: "overview", label: "概览", icon: "overview" },
  { id: "players", label: "玩家与资产", icon: "players" },
  { id: "operations", label: "运营与审批", icon: "operations" },
  { id: "audit", label: "操作审计", icon: "audit" },
  { id: "ops-agent", label: "智能排障", icon: "spark" },
  { id: "system", label: "系统状态", icon: "system" },
];

export const CommandPalette = defineComponent({
  name: "CommandPalette",
  components: { Icon },
  props: { open: Boolean, game: String },
  emits: ["close", "navigate", "player", "award"],
  setup(props, { emit }) {
    const text = ref("");
    const cursor = ref(0);
    const input = ref(null);
    const dialog = ref(null);
    let restoreFocus = null;

    const items = computed(() => {
      const q = text.value.trim();
      const rows = PAGES.filter((p) => !q || p.label.includes(q) || p.id.includes(q.toLowerCase())).map((p) => ({
        key: "page:" + p.id,
        label: p.label,
        hint: "跳转",
        icon: p.icon,
        run: () => emit("navigate", p.id),
      }));
      if (q) {
        rows.unshift({
          key: "player:" + q,
          label: "查看玩家 " + q,
          hint: "玩家",
          icon: "search",
          run: () => emit("player", q),
        });
        rows.push({
          key: "award:" + q,
          label: "为 " + q + " 补发奖励",
          hint: "运营",
          icon: "operations",
          run: () => emit("award", q),
        });
      }
      return rows;
    });

    watch(
      () => props.open,
      async (open) => {
        if (open) {
          restoreFocus = document.activeElement;
          text.value = "";
          cursor.value = 0;
          await nextTick();
          if (input.value) input.value.focus();
        } else if (restoreFocus && typeof restoreFocus.focus === "function") {
          restoreFocus.focus();
          restoreFocus = null;
        }
      },
    );

    onBeforeUnmount(() => {
      restoreFocus = null;
    });

    function onKey(event) {
      if (event.key === "Escape") return emit("close");
      if (event.key === "Tab") {
        trapFocus(dialog.value, event);
        return;
      }
      if (event.key === "ArrowDown") {
        event.preventDefault();
        cursor.value = (cursor.value + 1) % Math.max(1, items.value.length);
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        cursor.value = (cursor.value - 1 + items.value.length) % Math.max(1, items.value.length);
      }
      if (event.key === "Enter") {
        const item = items.value[cursor.value];
        if (item) {
          item.run();
          emit("close");
        }
      }
    }

    return { text, cursor, items, input, dialog, onKey };
  },
  template: `
  <div v-if="open" class="scrim" @click.self="$emit('close')">
    <div ref="dialog" class="palette" role="dialog" aria-modal="true" aria-label="命令面板" @keydown="onKey">
      <div class="palette-input">
        <icon name="search" :size="15" />
        <input ref="input" v-model="text" placeholder="搜索页面、玩家 UID 或操作…" @input="cursor = 0" />
        <kbd class="kbd">Esc</kbd>
      </div>
      <div class="palette-list" role="listbox">
        <button v-for="(item, i) in items" :key="item.key" class="palette-item" :aria-selected="String(i === cursor)"
                role="option" @mouseenter="cursor = i" @click="item.run(); $emit('close')">
          <icon :name="item.icon" :size="15" />
          <span class="palette-label">{{ item.label }}</span>
          <span class="pill pill-plain">{{ item.hint }}</span>
        </button>
        <div v-if="!items.length" class="empty">没有匹配的命令</div>
      </div>
    </div>
  </div>`,
});
