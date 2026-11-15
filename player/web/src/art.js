export const GameArt = {
  props: ['game', 'large'],
  computed: {
    variant() {
      if (this.game?.game_id === 'card-game') return 'art-cards';
      if (this.game?.game_id === 'sky-island') return 'art-island';
      return 'art-emblem';
    },
    accent() {
      if (this.game?.accent) return this.game.accent;
      if (this.game?.game_id === 'card-game') return 'violet';
      if (this.game?.game_id === 'sky-island') return 'cyan';
      return 'sky';
    },
    caption() {
      if (this.game?.game_id === 'card-game') return 'A LITTLE STRATEGY. A BIG ADVENTURE.';
      if (this.game?.game_id === 'sky-island') return 'FIND YOUR OWN LITTLE WORLD.';
      return (this.game?.category || 'MINIVERSE WORLD').toUpperCase();
    },
  },
  template: `<div class="game-art" :class="[variant, {large}]" :data-accent="accent" aria-hidden="true">
    <div class="art-orbit orbit-one"></div><div class="art-orbit orbit-two"></div>
    <template v-if="variant === 'art-cards'"><div class="draw-card c-one">✦</div><div class="draw-card c-two">✧</div><div class="draw-card c-three">✦</div><span class="art-spark s-one">✧</span><span class="art-spark s-two">+</span></template>
    <template v-else-if="variant === 'art-island'"><div class="art-sun"></div><div class="cloud cloud-one"></div><div class="cloud cloud-two"></div><div class="island-small"></div><div class="island-main"><div class="island-tree"></div><div class="island-house"><i></i></div></div><span class="art-spark s-one">✧</span><span class="art-spark s-two">✦</span></template>
    <template v-else><div class="emblem-glow"></div><div class="emblem-badge"><span>{{ game?.icon || '✦' }}</span></div><span class="art-spark s-one">✧</span><span class="art-spark s-two">✦</span></template>
    <span class="art-caption">{{ caption }}</span>
  </div>`,
};
export const Companion = { props: ['small'], template: `<div class="companion-art" :class="{small}" aria-hidden="true"><div class="buddy-halo"></div><div class="buddy-ear left"></div><div class="buddy-ear right"></div><div class="buddy-body"><div class="buddy-face"><i></i><i></i><b></b></div><span>✦</span></div><div class="buddy-shadow"></div></div>` };
