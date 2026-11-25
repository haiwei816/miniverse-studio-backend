import { defineComponent } from 'vue';
import { Companion } from '../art.js';

export const CompanionView = defineComponent({
  name: 'CompanionView',
  components: { Companion },
  props: {
    session: Object,
    companion: Object,
    companionBusy: Boolean,
    companionLoaded: Boolean,
    companionError: String,
    companionMode: String,
    modeLabel: String,
    modeHint: String,
    worldContextText: String,
    chat: Array,
    chatText: String,
    chatBusy: Boolean,
    hasNewChat: Boolean,
    memories: Array,
    memoryText: String,
  },
  emits: [
    'require-login', 'load-companion', 'save-companion', 'send-chat', 'retry-chat',
    'chat-scroll', 'scroll-bottom', 'add-memory', 'delete-memory',
    'update:chatText', 'update:memoryText', 'register-chat-log',
  ],
  template: `
    <div class="page-heading">
      <div>
        <div class="eyebrow">SAME FRIEND. NEW WORLDS.</div>
        <h1>属于你的，冒险搭子。</h1>
        <p>有些默契，不必在每个世界重新开始。</p>
      </div>
    </div>

    <div v-if="!session" class="state-box">
      <Companion small />
      <h2>你的伙伴，等着认识你</h2>
      <p>登录后命名伙伴、对话，并保存你允许记住的经历。</p>
      <button class="button primary" @click="$emit('require-login')">认识我的伙伴 ↗</button>
    </div>
    <div v-else-if="companionBusy && !companionLoaded" class="skeleton hero-skeleton" aria-label="正在连线伙伴"></div>
    <div v-else-if="companionError && !companionLoaded" class="state-box error" role="alert">
      <h3>暂时无法读取伙伴资料</h3>
      <p>{{ companionError }}</p>
      <button class="button secondary" @click="$emit('load-companion')">重新加载</button>
    </div>
    <div v-else class="companion-layout">
      <section class="panel buddy-profile">
        <Companion />
        <h2>{{ companion.name || '星伴' }}</h2>
        <p>无论去哪里，都陪你出发。</p>
        <form @submit.prevent="$emit('save-companion')">
          <label>伙伴的名字<input v-model="companion.name" maxlength="32" required /></label>
          <label>
            相处风格
            <select v-model="companion.style">
              <option value="gentle">温暖友好</option>
              <option value="energetic">活力探索</option>
              <option value="concise">简洁可靠</option>
            </select>
          </label>
          <button class="button secondary wide" :disabled="companionBusy">保存设置</button>
        </form>
      </section>

      <div class="companion-content">
        <section class="panel chat-panel">
          <div class="section-heading">
            <h2>聊聊下一次冒险</h2>
            <span class="tag" :data-mode="companionMode">{{ modeLabel }}</span>
          </div>
          <p class="hint context-line">{{ worldContextText }}</p>
          <p class="hint">{{ modeHint }}</p>
          <div class="chat-wrap">
            <div
              :ref="el => $emit('register-chat-log', el)"
              class="chat-log"
              aria-live="polite"
              @scroll="$emit('chat-scroll')"
            >
              <div v-if="!chat.length" class="chat-welcome">
                <span>✦</span>
                <p>你好呀。想探索新游戏，<br>还是聊聊刚才的冒险？</p>
              </div>
              <div v-for="(message, index) in chat" :key="index" :class="['chat-message', message.role]">
                <small>{{ message.role === 'user' ? '你' : message.role === 'error' ? '暂时无法回复' : companion.name }}</small>
                <p>{{ message.text }}</p>
                <button
                  v-if="message.role === 'error' && message.retryText"
                  type="button"
                  class="text-button retry-chat-btn"
                  :disabled="chatBusy"
                  @click="$emit('retry-chat', index)"
                >↻ 重试发送</button>
              </div>
              <div v-if="chatBusy" class="chat-thinking">伙伴正在想一想…</div>
            </div>
            <button v-if="hasNewChat" type="button" class="new-chat-pill" @click="$emit('scroll-bottom', true)">↓ 有新消息</button>
          </div>
          <form class="chat-form" @submit.prevent="$emit('send-chat')">
            <input
              :value="chatText"
              maxlength="2000"
              placeholder="和伙伴说点什么…"
              aria-label="对话内容"
              :disabled="chatBusy || companionMode === 'unconfigured'"
              @input="$emit('update:chatText', $event.target.value)"
            />
            <button class="button primary compact" :disabled="chatBusy || !chatText.trim() || companionMode === 'unconfigured'">发送 ↗</button>
          </form>
        </section>

        <section class="panel">
          <div class="section-heading">
            <h2>允许记住的事</h2>
            <span class="count">{{ memories.length }}</span>
          </div>
          <p class="hint">只保存你主动写下的偏好和共同经历。你可以随时删除。</p>
          <form class="memory-form" @submit.prevent="$emit('add-memory')">
            <input
              :value="memoryText"
              maxlength="500"
              placeholder="例如：我喜欢自己探索，卡关时再提示"
              aria-label="新增记忆"
              @input="$emit('update:memoryText', $event.target.value)"
            />
            <button class="button secondary compact" :disabled="!memoryText.trim()">记住</button>
          </form>
          <div v-if="!memories.length" class="memory-empty">留一点空间，给未来的共同经历。</div>
          <div v-for="memory in memories" :key="memory.id" class="memory-row">
            <span>✧</span>
            <p>{{ memory.text }}</p>
            <button class="text-button" @click="$emit('delete-memory', memory)" aria-label="删除这条记忆">删除</button>
          </div>
        </section>
      </div>
    </div>
  `,
});
