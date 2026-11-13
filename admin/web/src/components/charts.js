import { defineComponent, h, ref, onMounted, onBeforeUnmount, computed } from "vue";
import { dayLabel, count } from "../format.js";

function useWidth(initial = 720) {
  const el = ref(null);
  const width = ref(initial);
  let observer;
  onMounted(() => {
    if (!el.value) return;
    observer = new ResizeObserver((entries) => {
      for (const entry of entries) width.value = Math.max(240, entry.contentRect.width);
    });
    observer.observe(el.value);
    width.value = Math.max(240, el.value.clientWidth || initial);
  });
  onBeforeUnmount(() => observer && observer.disconnect());
  return { el, width };
}

// Catmull-Rom to cubic Bezier: the dashboard reads as a smooth trend line
// without smoothing away real zeros.
function smooth(points) {
  if (points.length === 0) return "";
  if (points.length < 3) return points.map((p, i) => (i ? "L" : "M") + p[0] + "," + p[1]).join(" ");
  let d = "M" + points[0][0] + "," + points[0][1];
  for (let i = 0; i < points.length - 1; i++) {
    const p0 = points[i - 1] || points[i];
    const p1 = points[i];
    const p2 = points[i + 1];
    const p3 = points[i + 2] || p2;
    const c1x = p1[0] + (p2[0] - p0[0]) / 6;
    const c1y = p1[1] + (p2[1] - p0[1]) / 6;
    const c2x = p2[0] - (p3[0] - p1[0]) / 6;
    const c2y = p2[1] - (p3[1] - p1[1]) / 6;
    d += " C" + c1x + "," + c1y + " " + c2x + "," + c2y + " " + p2[0] + "," + p2[1];
  }
  return d;
}

export const Sparkline = defineComponent({
  name: "Sparkline",
  props: {
    values: { type: Array, default: () => [] },
    width: { type: Number, default: 78 },
    height: { type: Number, default: 26 },
    series: { type: String, default: "a" },
  },
  setup(props) {
    return () => {
      const values = props.values.map((v) => Number(v) || 0);
      if (values.length < 2) return null;
      const max = Math.max(...values, 1);
      const step = props.width / (values.length - 1);
      const pts = values.map((v, i) => [i * step, props.height - 3 - (v / max) * (props.height - 6)]);
      const line = smooth(pts);
      const area = line + " L" + props.width + "," + props.height + " L0," + props.height + " Z";
      return h(
        "svg",
        { class: "spark", width: props.width, height: props.height, viewBox: "0 0 " + props.width + " " + props.height, "aria-hidden": "true" },
        [
          h("path", { class: "spark-area", d: area, "data-series": props.series }),
          h("path", { class: "spark-line", d: line, "data-series": props.series }),
          h("circle", { class: "spark-dot", cx: pts[pts.length - 1][0], cy: pts[pts.length - 1][1], r: 2.4, "data-series": props.series }),
        ],
      );
    };
  },
});

export const TrendChart = defineComponent({
  name: "TrendChart",
  props: {
    series: { type: Array, required: true },
    height: { type: Number, default: 216 },
  },
  setup(props) {
    const { el, width } = useWidth();
    const hover = ref(-1);

    const geom = computed(() => {
      const pad = { l: 34, r: 14, t: 14, b: 24 };
      const n = props.series[0] ? props.series[0].points.length : 0;
      const innerW = Math.max(80, width.value - pad.l - pad.r);
      const innerH = Math.max(60, props.height - pad.t - pad.b);
      const peak = Math.max(1, ...props.series.flatMap((s) => s.points.map((p) => Number(p.count) || 0)));
      const step = peak <= 4 ? 1 : Math.ceil(peak / 4);
      const top = step * 4;
      const x = (i) => pad.l + (n <= 1 ? innerW / 2 : (i * innerW) / (n - 1));
      const y = (v) => pad.t + innerH - (Number(v) || 0) / top * innerH;
      const normalized = props.series.map((s) => ({ ...s, path: smooth(s.points.map((p, i) => [x(i), y(p.count)])) }));
      return { pad, n, innerW, innerH, step, top, x, y, normalized };
    });

    const onMove = (event) => {
      const g = geom.value;
      const rect = event.currentTarget.getBoundingClientRect();
      const px = event.clientX - rect.left;
      const ratio = (px - g.pad.l) / (g.innerW || 1);
      hover.value = Math.min(g.n - 1, Math.max(0, Math.round(ratio * (g.n - 1))));
    };

    return () => {
      const g = geom.value;
      if (!g.n) return h("div", { class: "empty" }, "暂无趋势数据");
      const dates = props.series[0].points.map((p) => p.date);
      const ticks = [0, 1, 2, 3, 4].map((i) => i * g.step);
      const labelEvery = g.innerW / g.n < 44 ? 2 : 1;
      const active = hover.value;
      return h("div", { class: "chart", ref: el, onMousemove: onMove, onMouseleave: () => (hover.value = -1) }, [
        h("svg", { viewBox: "0 0 " + width.value + " " + props.height, width: width.value, height: props.height, role: "img", "aria-label": "近七日趋势" }, [
          h("defs", null, props.series.map((s) =>
            h("linearGradient", { id: "grad-" + s.key, x1: "0", y1: "0", x2: "0", y2: "1" }, [
              h("stop", { offset: "0%", class: "stop-top", "data-series": s.key }),
              h("stop", { offset: "100%", class: "stop-bottom", "data-series": s.key }),
            ]),
          )),
          ...ticks.map((t) =>
            h("g", { key: "t" + t }, [
              h("line", { class: "chart-grid", x1: g.pad.l, x2: width.value - g.pad.r, y1: g.y(t), y2: g.y(t) }),
              h("text", { class: "chart-axis", x: g.pad.l - 8, y: g.y(t) + 3.5, "text-anchor": "end" }, String(t)),
            ]),
          ),
          ...dates.map((d, i) =>
            i % labelEvery === 0
              ? h("text", { key: "x" + i, class: "chart-axis", x: g.x(i), y: props.height - 6, "text-anchor": "middle" }, dayLabel(d))
              : null,
          ),
          ...g.normalized.map((s) => [
            h("path", {
              key: "area" + s.key,
              class: "chart-area",
              "data-series": s.key,
              d: s.path + " L" + g.x(g.n - 1) + "," + (props.height - g.pad.b) + " L" + g.x(0) + "," + (props.height - g.pad.b) + " Z",
              fill: "url(#grad-" + s.key + ")",
            }),
            h("path", { key: "line" + s.key, class: "chart-line", "data-series": s.key, d: s.path }),
          ]),
          active >= 0
            ? h("line", { class: "chart-cross", x1: g.x(active), x2: g.x(active), y1: g.pad.t, y2: props.height - g.pad.b })
            : null,
          active >= 0
            ? g.normalized.map((s) =>
                h("circle", {
                  key: "dot" + s.key,
                  class: "chart-dot",
                  "data-series": s.key,
                  cx: g.x(active),
                  cy: g.y(s.points[active].count),
                  r: 3.6,
                }),
              )
            : null,
        ]),
        active >= 0
          ? h("div", {
              class: "chart-tip",
              style: { left: g.x(active) + "px", top: g.y(Math.max(...g.normalized.map((s) => s.points[active].count))) + "px" },
            }, [
              h("div", { class: "muted small" }, props.series[0].points[active].date),
              ...props.series.map((s) =>
                h("div", { key: s.key, style: { display: "flex", gap: "8px", justifyContent: "space-between" } }, [
                  h("span", { class: "muted" }, s.name),
                  h("b", null, count(s.points[active].count)),
                ]),
              ),
            ])
          : null,
      ]);
    };
  },
});
