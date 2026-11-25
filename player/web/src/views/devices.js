import { defineComponent } from 'vue';
import { Companion } from '../art.js';

export const DevicesView = defineComponent({
  name: 'DevicesView',
  components: { Companion },
  props: {
    session: Object,
    devicesError: String,
    capabilities: Object,
  },
  emits: ['require-login', 'load-devices'],
  template: `
    <div class="page-heading">
      <div>
        <div class="eyebrow">BEYOND THE SCREEN</div>
        <h1>让陪伴，走出屏幕。</h1>
        <p>实体伙伴的连接接口已预留，硬件能力将后续接入。</p>
      </div>
    </div>

    <section class="hardware-hero panel">
      <div class="hardware-drawing">
        <div class="device-antenna"></div>
        <div class="device-shell">
          <Companion small />
          <div class="device-grill">········</div>
          <div class="device-key"></div>
        </div>
        <div class="device-shadow"></div>
      </div>
      <div>
        <span class="tag">硬件尚未接入</span>
        <h2>同一个伙伴。<br>多一种陪伴方式。</h2>
        <p>未来通过声音与表情，延续游戏中的共同经历。当前版本仅提供设备协议说明，不能配对或连接真实硬件。</p>
        <div class="hardware-status"><span class="status-dot reserved"></span>设备连接：未开放</div>
        <button v-if="!session" class="button secondary" @click="$emit('require-login')">登录查看接口状态</button>
        <p v-else-if="devicesError" role="alert" class="error-text">
          {{ devicesError }} <button class="text-button" @click="$emit('load-devices')">重试</button>
        </p>
        <p v-else class="hint">
          {{ capabilities ? '设备协议 v' + capabilities.protocol_version + ' · ' + (capabilities.implemented ? '服务已配置' : '接口预留') : '正在读取接口状态…' }}
        </p>
      </div>
    </section>

    <div class="hardware-grid">
      <section class="panel"><span class="feature-icon">◉</span><h3>独立身份</h3><p>设备使用独立凭证。后续支持配对、解绑与吊销。</p></section>
      <section class="panel"><span class="feature-icon">↔</span><h3>状态同步</h3><p>预留游戏上下文、表情与事件同步协议。</p></section>
      <section class="panel"><span class="feature-icon">⌁</span><h3>断线可恢复</h3><p>规划快照恢复与过期事件过滤，游戏正常继续。</p></section>
    </div>
  `,
});
