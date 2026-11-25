import { defineComponent } from 'vue';
import { GameArt, Companion } from '../art.js';

export const DiscoverView = defineComponent({
  name: 'DiscoverView',
  components: { GameArt, Companion },
  props: {
    session: Object,
    recent: Array,
    error: String,
    loading: Boolean,
    featured: Object,
    games: Array,
    displayed: Array,
    categories: Array,
    category: String,
    search: String,
    favorites: Object,
    launchBusy: Boolean,
    nextCursor: String,
    loadingMore: Boolean,
    companionLevelLabel: Function,
    formatDate: Function,
  },
  emits: ['open-detail', 'go', 'launch', 'load-games', 'update:search', 'update:category', 'favorite'],
  template: `
    <div class="page-heading">
      <div>
        <div class="eyebrow">A LITTLE CURIOUS. ALWAYS.</div>
        <h1>今天，去哪个世界？<span class="heading-star">✦</span></h1>
        <p>发现新的乐趣，也遇见熟悉的伙伴。</p>
      </div>
      <span class="edition">探索版 <span>01</span></span>
    </div>

    <section v-if="session && recent.length" class="recent-section">
      <div class="section-heading">
        <h2>继续上次的冒险</h2>
        <button class="text-button" @click="$emit('go', 'library')">我的游戏 ↗</button>
      </div>
      <div class="recent-grid">
        <button v-for="game in recent" :key="game.game_id" class="recent-card" @click="$emit('launch', game)">
          <span class="mini-game-icon">{{ game.icon || '✦' }}</span>
          <div>
            <strong>{{ game.name }}</strong>
            <small>上次游玩 {{ formatDate(game.last_played_at) }}</small>
          </div>
          <span>▶</span>
        </button>
      </div>
    </section>

    <div v-if="error" class="state-box error" role="alert">
      <h3>暂时无法载入游戏</h3>
      <p>{{ error }}</p>
      <button class="button secondary" @click="$emit('load-games', false)">重新加载</button>
    </div>
    <div v-else-if="loading" class="skeleton hero-skeleton" aria-label="正在载入游戏"></div>
    <section v-else-if="featured" class="feature-row">
      <button class="feature-card" @click="$emit('open-detail', featured)">
        <GameArt :game="featured" large />
        <div class="feature-copy">
          <span class="pill">精选世界 <span>✧</span></span>
          <h2>{{ featured.name }}</h2>
          <p>{{ featured.summary }}</p>
          <span class="feature-cta">探索这个世界 <b>↗</b></span>
        </div>
      </button>
      <button class="companion-promo" @click="$emit('go', 'companion')">
        <div class="eyebrow">YOUR COMPANION</div>
        <h2>不同世界。<br>同一个朋友。</h2>
        <p>给每次冒险，多一点陪伴。</p>
        <Companion />
        <span class="promo-bottom">认识你的伙伴 <b>↗</b></span>
      </button>
    </section>

    <section class="discover-catalog">
      <div class="section-heading">
        <h2>发现游戏 <span class="count">{{ games.length }}</span></h2>
        <label class="search-field">
          <span>⌕</span>
          <input
            :value="search"
            type="search"
            aria-label="搜索游戏"
            placeholder="搜索下一个世界"
            @input="$emit('update:search', $event.target.value)"
          />
        </label>
      </div>
      <div class="category-row">
        <button
          v-for="item in categories"
          :key="item"
          :class="['category', { selected: category === item }]"
          @click="$emit('update:category', item)"
        >{{ item }}</button>
      </div>
      <div class="game-grid">
        <article v-for="game in displayed" :key="game.game_id" class="game-card">
          <button class="game-cover" @click="$emit('open-detail', game)" :aria-label="'查看 ' + game.name">
            <GameArt :game="game" />
            <span class="cover-badge">{{ companionLevelLabel(game.companion_level).split('（')[0] }}</span>
          </button>
          <div class="game-info">
            <div class="game-title-row">
              <button class="title-button" @click="$emit('open-detail', game)">{{ game.name }}</button>
              <button
                class="favorite-button"
                @click="$emit('favorite', game)"
                :aria-label="favorites.has(game.game_id) ? '取消收藏 ' + game.name : '收藏 ' + game.name"
              >{{ favorites.has(game.game_id) ? '♥' : '♡' }}</button>
            </div>
            <p>{{ game.summary }}</p>
            <div class="game-meta">
              <span>{{ game.category }} <i>·</i> 网页游戏</span>
              <button @click="$emit('launch', game)" :disabled="launchBusy">开始玩 ↗</button>
            </div>
          </div>
        </article>
      </div>
      <div v-if="nextCursor" class="load-more">
        <button class="button secondary" :disabled="loadingMore" @click="$emit('load-games', true)">
          {{ loadingMore ? '正在加载…' : '探索更多游戏 ↓' }}
        </button>
      </div>
      <div v-if="!loading && !error && !displayed.length" class="state-box">
        <span class="empty-symbol">⌕</span>
        <h3>还没找到这个世界</h3>
        <p>换个关键词，或试试其他分类。</p>
      </div>
    </section>
  `,
});
