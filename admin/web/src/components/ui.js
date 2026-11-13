import { defineComponent, h, onMounted, onBeforeUnmount, reactive, ref, watch } from "vue";
import { Icon } from "./icons.js";

const tips = reactive([]);
let nextId = 1;

export function notify(text, tone = "info") {
  const id = nextId++;
  tips.push({ id, text, tone });
  setTimeout(() => {
    const at = tips.findIndex((t) => t.id === id);
    if (at >= 0) tips.splice(at, 1);
  }, tone === "error" ? 6000 : 3600);
}

export const ToastHost = defineComponent({
  name: "ToastHost",
  setup() {
    const icon = (tone) => (tone === "error" ? "alert" : tone === "success" ? "check" : "spark");
    return () =>
      h(
        "div",
        { class: "toasts", role: "status", "aria-live": "polite" },
        tips.map((t) =>
          h("div", { class: "toast", key: t.id, "data-tone": t.tone }, [
            h(Icon, { name: icon(t.tone), size: 15 }),
            h("span", null, t.text),
          ]),
        ),
      );
  },
});

// Right-hand inspector, macOS style: scrim, esc to close, focus moved on open.
export const Drawer = defineComponent({
  name: "Drawer",
  props: { open: { type: Boolean, default: false }, title: String, subtitle: String },
  emits: ["close"],
  setup(props, { emit, slots }) {
    const onKey = (e) => {
      if (e.key === "Escape") emit("close");
    };
    watch(
      () => props.open,
      (open) => {
        if (open) document.addEventListener("keydown", onKey);
        else document.removeEventListener("keydown", onKey);
        document.documentElement.style.overflow = open ? "hidden" : "";
      },
    );
    onBeforeUnmount(() => {
      document.removeEventListener("keydown", onKey);
      document.documentElement.style.overflow = "";
    });
    return () => {
      if (!props.open) return null;
      return h("div", null, [
        h("div", { class: "scrim", onClick: () => emit("close"), "aria-hidden": "true" }),
        h("aside", { class: "drawer", role: "dialog", "aria-modal": "true", "aria-label": props.title || "详情" }, [
          h("header", { class: "drawer-head" }, [
            h("div", null, [
              h("div", { class: "drawer-title" }, props.title || ""),
              props.subtitle ? h("div", { class: "mono muted", style: { marginTop: "3px" } }, props.subtitle) : null,
            ]),
            h(
              "button",
              { class: "btn btn-quiet btn-sm", onClick: () => emit("close"), "aria-label": "关闭" },
              [h(Icon, { name: "close", size: 15 })],
            ),
          ]),
          h("div", { class: "drawer-body" }, slots.default ? slots.default() : null),
        ]),
      ]);
    };
  },
});

export const Segmented = defineComponent({
  name: "Segmented",
  props: { modelValue: String, options: { type: Array, required: true }, label: String },
  emits: ["update:modelValue"],
  setup(props, { emit }) {
    return () =>
      h(
        "div",
        { class: "segmented", role: "group", "aria-label": props.label || "切换" },
        props.options.map((o) =>
          h(
            "button",
            {
              key: o.value,
              type: "button",
              "aria-pressed": String(props.modelValue === o.value),
              onClick: () => emit("update:modelValue", o.value),
            },
            o.label,
          ),
        ),
      );
  },
});

export const Empty = defineComponent({
  name: "Empty",
  props: { text: { type: String, default: "暂无数据" } },
  setup(props) {
    return () => h("div", { class: "empty" }, [h(Icon, { name: "spark", size: 18 }), h("span", null, props.text)]);
  },
});

// Only used to keep the first paint calm while a view loads.
export const SkeletonRows = defineComponent({
  name: "SkeletonRows",
  props: { rows: { type: Number, default: 3 } },
  setup(props) {
    return () =>
      h(
        "div",
        { class: "stack", "aria-hidden": "true" },
        Array.from({ length: props.rows }, (_, i) =>
          h("div", { class: "skeleton", key: i, style: { height: "34px", opacity: 1 - i * 0.18 } }),
        ),
      );
  },
});

export const MeasureMixin = { onMounted, onBeforeUnmount, ref };
