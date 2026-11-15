import { defineComponent, h } from "vue";

// Lucide-style 24px stroke paths, drawn at 16px. Keeping them inline avoids a
// runtime icon font and keeps the console readable at small sizes.
const paths = {
  overview: ["M4 19V5", "M4 19h16", "M7 15l3.5-4 3 2.5L20 7"],
  players: ["M16 20v-1.5a3.5 3.5 0 0 0-3.5-3.5h-5A3.5 3.5 0 0 0 4 18.5V20", "M10 11a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7", "M20 20v-1.5a3.5 3.5 0 0 0-2.6-3.4", "M15.5 4.2a3.5 3.5 0 0 1 0 6.6"],
  operations: ["M4 7h10", "M4 12h7", "M4 17h13", "M17 5l3 3-3 3", "M20 15l-3 3 3 3"],
  audit: ["M6 3h9l4 4v14H6z", "M14 3v5h5", "M9 13h6", "M9 17h4"],
  system: ["M3 12h4l2.5 6 4-14L16 12h5"],
  search: ["M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14", "M20 20l-4-4"],
  refresh: ["M20 11a8 8 0 1 0-2.3 6.3", "M20 5v6h-6"],
  sun: ["M12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10", "M12 2v2", "M12 20v2", "M2 12h2", "M20 12h2", "M5 5l1.5 1.5", "M17.5 17.5L19 19", "M19 5l-1.5 1.5", "M6.5 17.5L5 19"],
  moon: ["M20 14.5A8.5 8.5 0 1 1 9.5 4a7 7 0 0 0 10.5 10.5"],
  logout: ["M14 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2v-2", "M18 12H9", "M15 9l3 3-3 3"],
  chevron: ["M8 10l4 4 4-4"],
  close: ["M6 6l12 12", "M18 6L6 18"],
  clock: ["M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18", "M12 7v5l3 2"],
  alert: ["M12 3l9 16H3z", "M12 9v5", "M12 17.2v.1"],
  check: ["M4 12.5l5 5L20 6.5"],
  arrow: ["M7 17L17 7", "M9 7h8v8"],
  spark: ["M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"],
  mail: ["M3 6h18v12H3z", "M3 7l9 6 9-6"],
  box: ["M3 8l9-5 9 5v8l-9 5-9-5z", "M3 8l9 5 9-5", "M12 13v8"],
  save: ["M5 4h11l3 3v13H5z", "M8 4v6h7V4", "M8 20v-6h8v6"],
  plus: ["M12 5v14", "M5 12h14"],
  play: ["M7 4l12 8-12 8z"],
  trash: ["M4 7h16", "M9 7V5h6v2", "M6 7l1 13h10l1-13", "M10 11v6", "M14 11v6"],
  menu: ["M4 7h16", "M4 12h16", "M4 17h16"],
};

export const Icon = defineComponent({
  name: "Icon",
  props: {
    name: { type: String, required: true },
    size: { type: [Number, String], default: 16 },
    stroke: { type: [Number, String], default: 1.6 },
  },
  setup(props) {
    return () =>
      h(
        "svg",
        {
          class: "icon",
          width: props.size,
          height: props.size,
          viewBox: "0 0 24 24",
          fill: "none",
          stroke: "currentColor",
          "stroke-width": props.stroke,
          "stroke-linecap": "round",
          "stroke-linejoin": "round",
          "aria-hidden": "true",
          focusable: "false",
        },
        (paths[props.name] || paths.spark).map((d) => h("path", { d })),
      );
  },
});
