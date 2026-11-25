import { defineComponent } from 'vue';
import { GameArt } from '../art.js';

export const LibraryView = defineComponent({
  name: 'LibraryView',
  components: { GameArt },
  props: {
    session: Object,
    libraryLoading: Boolean,
    libraryGames: Array,
    libraryError: String,
    launchBusy: Boolean,
    formatDate: Function,
  },
  emits: ['require-login', 'load-library', 'go', 'open-detail', 'favorite', 'launch'],
  template: `
    <div class="page-heading">
      <div>
        <div class="eyebrow">YOUR COLLECTION</div>
        <h1>随时，回到喜欢的世界。</h1>
        <p>收藏与最近游玩记录，会跟随你的账号。</p>
      </div>
    </div>

    <div v-if="!session" class="state-box">
      <span class="empty-symbol">▦</span>
      <h2>你的游戏，等你回来</h2>
      <p>登录后收藏游戏，继续上次的冒险。</p>
      <button class="button primary" @click="$emit('require-login')">登录 / 注册</button>
    </div>
    <div v-else-if="libraryLoading && !libraryGames.length" class="skeleton hero-skeleton" aria-label="正在载入游戏库"></div>
    <div v-else-if="libraryError" class="state-box error" role="alert">
      <h3>暂时无法载入游戏库</h3>
      <p>{{ libraryError }}</p>
      <button class="button secondary" @click="$emit('load-library')">重新加载</button>
    </div>
    <div v-else-if="!libraryGames.length" class="state-box">
      <span class="empty-symbol">✧</span>
      <h2>给游戏库一点新发现</h2>
      <p>开始一款游戏，或把喜欢的游戏加入收藏。</p>
      <button class="button primary" @click="$emit('go', 'discover')">去发现游戏 ↗</button>
    </div>
    <div v-else class="game-grid">
      <article v-for="game in libraryGames" :key="game.game_id" class="game-card">
        <button class="game-cover" @click="$emit('open-detail', game)" :aria-label="'查看 ' + game.name">
          <GameArt :game="game" />
        </button>
        <div class="game-info">
          <div class="game-title-row">
            <button class="title-button" @click="$emit('open-detail', game)">{{ game.name }}</button>
            <button
              class="favorite-button"
              @click="$emit('favorite', game)"
              :aria-label="game.favorite ? '取消收藏 ' + game.name : '收藏 ' + game.name"
            >{{ game.favorite ? '♥' : '♡' }}</button>
          </div>
          <p>{{ game.last_played_at ? '上次游玩 ' + formatDate(game.last_played_at) : '已加入收藏，等待第一次出发' }}</p>
          <button class="button secondary wide" @click="$emit('launch', game)" :disabled="launchBusy">继续冒险 ↗</button>
        </div>
      </article>
    </div>
  `,
});
