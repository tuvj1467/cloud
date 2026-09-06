// P8 Task 7：分享弹层组件 ShareDialog —— 表单（有效期/次数上限/权限）→ 创建 → 成功视图（提取码 + 链接 + 复制）
// 依赖 Task 3 契约：api.js（createShare）、store.js（props.file 为 FileMeta）、common.js（toast）
//
// 【后端契约】POST /file/share/create {fileId, permission('read'|'download'), expireSeconds?, maxCount?}
//   → {shareCode, shareUrl}；expireSeconds/maxCount 为 optional：不传 = 永久 / 不限。
// 链接指向前端路由 #/share/:code（SharePage 消费），不用后端 shareUrl（其指向站点根路径）。
const { ref, computed } = Vue;
import { createShare } from '../api.js';
import { toast } from '../common.js';

// 无构建链：直接注入全局 <style>，类名加 p8- 前缀防冲突
if (!document.getElementById('p8-share-dialog-style')) {
  const s = document.createElement('style');
  s.id = 'p8-share-dialog-style';
  s.textContent = `
    .p8-sh-select { width:100%; padding:12px; border:1px solid #ddd; border-radius:8px; font-size:16px;
      background:#fff; outline:none; transition:border-color .3s; }
    .p8-sh-select:focus { border-color:#667eea; }
    .p8-sh-code-label { text-align:center; color:#888; font-size:13px; margin-bottom:6px; }
    .p8-sh-code { text-align:center; font-size:36px; font-weight:700; letter-spacing:8px; color:#667eea;
      margin-bottom:20px; user-select:all; }
    .p8-sh-row { display:flex; gap:10px; }
    .p8-sh-row .btn { flex:1; }
  `;
  document.head.appendChild(s);
}

const EXPIRE_OPTIONS = [
  { value: '1', label: '1 天', seconds: 86400 },
  { value: '7', label: '7 天', seconds: 604800 },
  { value: '30', label: '30 天', seconds: 2592000 },
  { value: '0', label: '永久有效', seconds: 0 }, // 0 = 不传 expireSeconds（后端默认永久）
];

export const ShareDialog = {
  name: 'ShareDialog',
  props: {
    file: { type: Object, required: true }, // FileMeta（camelCase；fileId 兼容 file_id 兜底）
  },
  emits: ['close', 'created'],
  setup(props, { emit }) {
    const expireDays = ref('1');    // 默认 1 天
    const maxCount = ref('');       // 空串 = 不限
    const permission = ref('download');
    const busy = ref(false);
    const result = ref(null);       // {shareCode, shareUrl} 成功后切换视图

    const link = computed(() =>
      result.value ? `${location.origin}/#/share/${result.value.shareCode}` : '');
    const expireLabel = computed(() =>
      (EXPIRE_OPTIONS.find((o) => o.value === expireDays.value) || {}).label || '-');

    async function doCreate() {
      if (busy.value) return;
      const mc = String(maxCount.value).trim();
      if (mc && (!/^\d+$/.test(mc) || Number(mc) < 1)) return toast('次数上限必须为正整数', 'error');
      busy.value = true;
      try {
        const body = { fileId: props.file.fileId || props.file.file_id, permission: permission.value };
        const opt = EXPIRE_OPTIONS.find((o) => o.value === expireDays.value);
        if (opt && opt.seconds > 0) body.expireSeconds = opt.seconds;
        if (mc) body.maxCount = Number(mc);
        const data = await createShare(body);
        result.value = data;
        toast('分享创建成功', 'success');
        emit('created');
      } catch (e) {
        toast(e.message, 'error');
      } finally {
        busy.value = false;
      }
    }

    // 复制：navigator.clipboard 优先（需安全上下文），失败降级 execCommand
    async function copyText(text) {
      let ok = false;
      try {
        if (navigator.clipboard && window.isSecureContext) {
          await navigator.clipboard.writeText(text);
          ok = true;
        }
      } catch { ok = false; }
      if (!ok) {
        try {
          const ta = document.createElement('textarea');
          ta.value = text;
          ta.style.position = 'fixed';
          ta.style.opacity = '0';
          document.body.appendChild(ta);
          ta.select();
          ok = document.execCommand('copy');
          document.body.removeChild(ta);
        } catch { ok = false; }
      }
      toast(ok ? '已复制到剪贴板' : '复制失败，请手动复制', ok ? 'success' : 'error');
    }

    function close() { emit('close'); }

    return { expireDays, maxCount, permission, busy, result, link, expireLabel, EXPIRE_OPTIONS, doCreate, copyText, close };
  },
  template: `
    <div class="modal" @click.self="close">
      <div class="modal-content">
        <!-- 表单视图 -->
        <template v-if="!result">
          <h3>分享文件</h3>
          <div class="form-group">
            <label>文件</label>
            <input type="text" :value="file.name" disabled>
          </div>
          <div class="form-group">
            <label>有效期</label>
            <select v-model="expireDays" class="p8-sh-select">
              <option v-for="o in EXPIRE_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </div>
          <div class="form-group">
            <label>访问次数上限（留空 = 不限）</label>
            <input v-model="maxCount" type="number" min="1" step="1" placeholder="不限">
          </div>
          <div class="form-group">
            <label>权限</label>
            <select v-model="permission" class="p8-sh-select">
              <option value="download">允许下载（download）</option>
              <option value="read">仅查看（read）</option>
            </select>
          </div>
          <div class="modal-actions">
            <button class="btn btn-secondary" @click="close">取消</button>
            <button class="btn btn-primary" style="width:auto" :disabled="busy" @click="doCreate">{{ busy ? '创建中…' : '创建分享' }}</button>
          </div>
        </template>

        <!-- 成功视图：提取码大字 + 链接 + 复制 -->
        <template v-else>
          <h3>分享创建成功</h3>
          <div class="p8-sh-code-label">提取码（有效期：{{ expireLabel }}）</div>
          <div class="p8-sh-code">{{ result.shareCode }}</div>
          <div class="form-group">
            <label>分享链接</label>
            <input type="text" :value="link" readonly @focus="$event.target.select()">
          </div>
          <div class="p8-sh-row" style="margin-top:20px">
            <button class="btn btn-secondary" @click="copyText(result.shareCode)">复制提取码</button>
            <button class="btn btn-primary" style="width:auto" @click="copyText(link)">复制链接</button>
          </div>
          <div class="modal-actions">
            <button class="btn btn-primary" @click="close">完成</button>
          </div>
        </template>
      </div>
    </div>
  `,
};
