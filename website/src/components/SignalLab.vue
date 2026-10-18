<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue';

const mode = ref<'connect' | 'mcu'>('connect');
const enabled = ref(true);
const phase = ref<'idle' | 'found' | 'connected'>('idle');
const sample = ref('esp32');
const analyzed = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;

const samples = {
  esp32: { log: 'ESP-ROM:esp32s3-20210327\nBuild:Mar 27 2021\nrst:0x1 (POWERON),boot:0x8 (SPI_FAST_FLASH_BOOT)', model: 'ESP32-S3', vendor: 'Espressif', core: 'Xtensa LX7', evidence: 'RX 中的 esp32s3 型号标记' },
  stm32: { log: 'MCU: STM32F103C8T6\nFirmware: 1.2.0\nSystem ready', model: 'STM32F103C8T6', vendor: 'STMicroelectronics', core: 'Arm Cortex-M3', evidence: 'RX 中的 STM32F103C8T6 型号标记' },
  unknown: { log: 'System ready\nTemperature: 24.6 C\nOK', model: '尚未识别', vendor: '未知', core: '未知', evidence: '普通业务数据不足以判断 MCU，不强行猜测' },
};
const current = computed(() => samples[sample.value as keyof typeof samples]);

function reset() {
  clearTimeout(timer);
  phase.value = 'idle';
  analyzed.value = false;
}
function plugIn() {
  reset();
  phase.value = 'found';
  if (enabled.value) timer = setTimeout(() => { phase.value = 'connected'; }, 850);
}
function switchMode(next: 'connect' | 'mcu') {
  reset();
  mode.value = next;
}
onBeforeUnmount(() => clearTimeout(timer));
</script>

<template>
  <div class="signal-lab">
    <div class="lab-topline"><span class="mono"><span class="live-dot"></span> GUART / INTERACTIVE LAB</span><span class="lab-demo">网页模拟 · 不访问硬件</span></div>
    <div class="lab-tabs" role="tablist" aria-label="特色功能演示">
      <button id="connect-tab" role="tab" :aria-selected="mode === 'connect'" aria-controls="connect-panel" :tabindex="mode === 'connect' ? 0 : -1" @click="switchMode('connect')" @keydown.right.prevent="switchMode('mcu'); ($refs.mcuTab as HTMLButtonElement).focus()" @keydown.left.prevent="switchMode('mcu'); ($refs.mcuTab as HTMLButtonElement).focus()" ref="connectTab">01 自动连接</button>
      <button id="mcu-tab" role="tab" :aria-selected="mode === 'mcu'" aria-controls="mcu-panel" :tabindex="mode === 'mcu' ? 0 : -1" @click="switchMode('mcu')" @keydown.left.prevent="switchMode('connect'); ($refs.connectTab as HTMLButtonElement).focus()" @keydown.right.prevent="switchMode('connect'); ($refs.connectTab as HTMLButtonElement).focus()" ref="mcuTab">02 MCU 推测</button>
    </div>
    <div v-if="mode === 'connect'" id="connect-panel" role="tabpanel" aria-labelledby="connect-tab" class="lab-content">
      <div class="lab-controls"><span>自动连接新出现的串口</span><button class="toggle" role="switch" :aria-checked="enabled" aria-label="自动连接新出现的串口" @click="enabled = !enabled; reset()"><span></span></button></div>
      <div class="port-row"><span class="port-symbol">⌁</span><div><strong>{{ phase === 'idle' ? '等待新设备' : 'COM7 · USB Serial' }}</strong><span class="mono">115200 baud · 8N1</span></div><span class="port-status" :class="{ connected: phase === 'connected' }">{{ phase === 'connected' ? '已连接' : phase === 'found' ? '发现新串口' : '正在监听' }}</span></div>
      <div class="lab-terminal" aria-live="polite" aria-atomic="true"><p><span>01</span> 已记录当前串口，等待设备插入<span class="cursor">_</span></p><p v-if="phase !== 'idle'"><span>02</span> 检测到新增端口 COM7</p><p v-if="phase === 'connected'" class="terminal-success"><span>03</span> 沿用当前参数，自动连接成功。</p><p v-else-if="phase === 'found' && !enabled"><span>03</span> 自动连接已关闭，请在应用中手动连接。</p><p v-else class="terminal-muted">// 不抢占已有连接，不打开现有端口</p></div>
      <div class="lab-bottom"><button class="button button-dark" @click="plugIn" :disabled="phase === 'found' && enabled">{{ phase === 'idle' ? '模拟插入设备' : phase === 'found' && enabled ? '正在连接…' : '再试一次' }} <span aria-hidden="true">↗</span></button><span>真实应用约每秒检测一次</span></div>
    </div>
    <div v-else id="mcu-panel" role="tabpanel" aria-labelledby="mcu-tab" class="lab-content">
      <label class="sample-select">模拟接收日志<select v-model="sample" @change="analyzed = false"><option value="esp32">ESP32-S3 启动日志</option><option value="stm32">STM32 型号日志</option><option value="unknown">无特征数据</option></select></label>
      <pre class="lab-terminal sample-log">{{ current.log }}</pre>
      <div class="inference-result" aria-live="polite" aria-atomic="true"><span class="chip-small" aria-hidden="true">▦</span><div><strong>{{ analyzed ? current.model : '等待分析接收数据' }}</strong><span>{{ analyzed ? `${current.vendor} / ${current.core}` : '只读取日志，不发送探测指令' }}</span></div><span class="result-badge">{{ analyzed && sample !== 'unknown' ? '型号线索' : '被动推测' }}</span></div>
      <p class="evidence">{{ analyzed ? current.evidence : '选择一段示例日志，看看 GUART 如何寻找线索。' }}</p>
      <div class="lab-bottom"><button class="button button-dark" @click="analyzed = true">{{ analyzed ? '重新分析' : '分析示例日志' }} <span aria-hidden="true">↗</span></button><span>结果是推测，不是硬件认证</span></div>
    </div>
  </div>
</template>
